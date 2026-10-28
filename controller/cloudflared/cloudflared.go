package cloudflared

import (
	"bufio"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/mabels/cloudflared-controller/controller/config"
	"github.com/mabels/cloudflared-controller/controller/k8s_data"
	"github.com/mabels/cloudflared-controller/controller/types"
	"github.com/mabels/cloudflared-controller/utils"
	"github.com/rs/zerolog"
	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/watch"
	"toolman.org/encoding/base56"
)

type runningInstance struct {
	id                 string
	currentDir         string
	configfname        string
	log                *zerolog.Logger
	currentConfigMap   *corev1.ConfigMap
	unregisterShutdown func()

	lock    sync.Mutex
	cmd     *exec.Cmd
	stopped bool
	done    chan struct{}
}

func (ri *runningInstance) buildCredentialsFile(cfc types.CFController, cm *corev1.ConfigMap) (credfname string, err error) {
	tunnelIdStr, found := cm.ObjectMeta.GetAnnotations()[config.AnnotationCloudflareTunnelId()]
	if !found {
		return credfname, fmt.Errorf("missing annotation %s", config.AnnotationCloudflareTunnelId())
	}
	tunnelId, err := uuid.Parse(tunnelIdStr)
	if err != nil {
		return credfname, fmt.Errorf("invalid uuid %s:%v", tunnelIdStr, err)
	}

	var ns, name string
	secretName, found := cm.ObjectMeta.GetAnnotations()[config.AnnotationCloudflareTunnelK8sSecret()]
	if !found {
		tunnelName, found := cm.ObjectMeta.GetAnnotations()[config.AnnotationCloudflareTunnelName()]
		if !found {
			return credfname, fmt.Errorf("missing annotation %s", config.AnnotationCloudflareTunnelName())
		}
		utp := types.CFTunnelParameter{
			Namespace: cfc.Cfg().CloudFlare.TunnelConfigMapNamespace,
			Name:      tunnelName,
		}
		ns = utp.K8SSecretName().Namespace
		name = utp.K8SSecretName().Name
	} else {
		ret := types.FromFQDN(secretName, cfc.Cfg().CloudFlare.TunnelConfigMapNamespace)
		ns = ret.Namespace
		name = ret.Name
	}
	cts, err := k8s_data.FetchSecret(cfc, ns, name, tunnelIdStr)
	if err != nil {
		return credfname, err
	}
	fname := fmt.Sprintf("%s.json", tunnelId.String())
	bytesCts, err := json.Marshal(cts)
	if err != nil {
		return credfname, err
	}
	credfname = path.Join(ri.currentDir, fname)
	return credfname, os.WriteFile(credfname, bytesCts, 0600)
}

func (ri *runningInstance) buildConfig(credfname string, cm *corev1.ConfigMap) (*types.CFConfigYaml, error) {
	tunnelId, found := cm.ObjectMeta.GetAnnotations()[config.AnnotationCloudflareTunnelId()]
	if !found {
		return nil, fmt.Errorf("missing annotation %s", config.AnnotationCloudflareTunnelId())
	}
	// stable order, the map iteration order is random
	keys := make([]string, 0, len(cm.Data))
	for k := range cm.Data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	cfis := []types.CFConfigIngress{}
	for _, k := range keys {
		rules := cm.Data[k]
		rule := []types.CFConfigIngress{}
		err := yaml.Unmarshal([]byte(rules), &rule)
		if err != nil {
			ri.log.Error().Err(err).Str("rules", rules).Msg("error unmarshalling rules")
			continue
		}
		cfis = append(cfis, rule...)
	}
	cfis = append(cfis, types.CFConfigIngress{Service: "http_status:404"})
	igss := types.CFConfigYaml{
		Tunnel:          tunnelId,
		CredentialsFile: credfname,
		Ingress:         cfis,
	}
	yConfigYamlByte, err := yaml.Marshal(igss)
	if err != nil {
		return nil, err
	}
	ri.configfname = path.Join(ri.currentDir, "config.yaml")
	return &igss, os.WriteFile(ri.configfname, yConfigYamlByte, 0600)
}

func (ri *runningInstance) isStopped() bool {
	ri.lock.Lock()
	defer ri.lock.Unlock()
	return ri.stopped
}

func (ri *runningInstance) Stop(cfc types.CFController) {
	ri.lock.Lock()
	if ri.stopped {
		ri.lock.Unlock()
		return
	}
	ri.stopped = true
	close(ri.done)
	cmd := ri.cmd
	unregister := ri.unregisterShutdown
	ri.unregisterShutdown = nil
	ri.lock.Unlock()

	if unregister != nil {
		unregister()
	}
	if cmd != nil && cmd.Process != nil {
		err := cmd.Process.Kill()
		if err != nil {
			ri.log.Debug().Err(err).Msg("error killing process")
		}
	}
	if !cfc.Cfg().Debug {
		err := os.RemoveAll(ri.currentDir)
		if err != nil {
			ri.log.Error().Err(err).Str("dir", ri.currentDir).Msg("removing runtime dir")
		}
	}
	ri.log.Info().Msg("stopped")
}

func (ri *runningInstance) start(cfc types.CFController) (*exec.Cmd, error) {
	cfdFname, err := exec.LookPath(cfc.Cfg().CloudFlaredFname)
	if err != nil {
		return nil, err
	}
	// cloudflared tunnel --config ./config.yml  run
	cmds := []string{cfdFname, "tunnel", "--no-autoupdate", "--config", ri.configfname, "run"}
	cmd := exec.Command(cfdFname, cmds[1:]...)

	ri.log.Info().Strs("cmds", cmds).Msg("starting cloudflared")
	stdErr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	stdOut, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	ri.lock.Lock()
	defer ri.lock.Unlock()
	if ri.stopped {
		return nil, fmt.Errorf("instance stopped")
	}
	err = cmd.Start()
	if err != nil {
		return nil, err
	}
	ri.cmd = cmd
	log := ri.log.With().Int("pid", cmd.Process.Pid).Logger()
	action := func(pi io.ReadCloser) {
		fileScanner := bufio.NewScanner(pi)
		fileScanner.Split(bufio.ScanLines)
		for fileScanner.Scan() {
			utils.TransfromSimpleZeroLogLine(fileScanner.Text(), &log)
		}
	}
	go action(stdErr)
	go action(stdOut)
	log.Info().Msg("started")
	return cmd, nil
}

// supervise restarts cloudflared after RestartDelay until the instance is stopped
func (ri *runningInstance) supervise(cfc types.CFController, cmd *exec.Cmd) {
	for {
		err := cmd.Wait()
		if ri.isStopped() {
			return
		}
		ri.log.Error().Err(err).Dur("restartDelay", cfc.Cfg().RestartDelay).Msg("cloudflared exited, restarting")
		for {
			select {
			case <-ri.done:
				return
			case <-cfc.Context().Done():
				return
			case <-time.After(cfc.Cfg().RestartDelay):
			}
			cmd, err = ri.start(cfc)
			if err == nil {
				break
			}
			if ri.isStopped() {
				return
			}
			ri.log.Error().Err(err).Msg("error restarting cloudflared")
		}
	}
}

type Tunnel struct {
	processing sync.Mutex
	ri         *runningInstance
}

func idFromConfigMap(cm *corev1.ConfigMap) string {
	keys := make([]string, 0, len(cm.Data))
	for k := range cm.Data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	data := make([]string, 0, 2*len(keys)+1)
	data = append(data, cm.GetAnnotations()[config.AnnotationCloudflareTunnelId()])
	for _, k := range keys {
		data = append(data, k, cm.Data[k])
	}
	hash := sha256.Sum256([]byte(strings.Join(data, ",")))
	parts := make([]string, 0, len(hash)/(64/8))
	for i := 0; i < len(hash); i += 64 / 8 {
		id64 := uint64(hash[i])<<56 | uint64(hash[i+1])<<48 | uint64(hash[i+2])<<40 | uint64(hash[i+3])<<32 |
			uint64(hash[i+4])<<24 | uint64(hash[i+5])<<16 | uint64(hash[i+6])<<8 | uint64(hash[i+7])
		parts = append(parts, base56.Encode(uint64(id64)))
	}
	return strings.Join(parts, "-")
}

func newRunningInstance(cfc types.CFController, cm *corev1.ConfigMap) (*runningInstance, error) {
	id := idFromConfigMap(cm)
	log := cfc.Log().With().Str("id", id).Str("component", "cloudflared").Logger()
	ri := &runningInstance{
		id:               id,
		currentConfigMap: cm.DeepCopy(),
		currentDir:       path.Join(cfc.Cfg().RunningInstanceDir, id),
		log:              &log,
		done:             make(chan struct{}),
	}
	ri.unregisterShutdown = cfc.RegisterShutdown(func() {
		ri.Stop(cfc)
	})
	// leftovers of an instance with the same config
	err := os.RemoveAll(ri.currentDir)
	if err == nil {
		err = os.MkdirAll(ri.currentDir, 0700)
	}
	if err != nil {
		log.Error().Err(err).Msg("error creating runtime dir")
		ri.Stop(cfc)
		return nil, err
	}
	credfname, err := ri.buildCredentialsFile(cfc, cm)
	if err != nil {
		log.Warn().Err(err).Msg("tunnel not prepared yet")
		ri.Stop(cfc)
		return nil, err
	}
	_, err = ri.buildConfig(credfname, cm)
	if err != nil {
		log.Error().Err(err).Msg("error building config file")
		ri.Stop(cfc)
		return nil, err
	}
	cmd, err := ri.start(cfc)
	if err != nil {
		log.Error().Err(err).Msg("error starting cloudflared")
		ri.Stop(cfc)
		return nil, err
	}
	go ri.supervise(cfc, cmd)
	return ri, nil
}

func sameConfig(a, b *corev1.ConfigMap) bool {
	tid := config.AnnotationCloudflareTunnelId()
	return reflect.DeepEqual(a.Data, b.Data) && a.GetAnnotations()[tid] == b.GetAnnotations()[tid]
}

func (t *Tunnel) Start(cfc types.CFController, cm *corev1.ConfigMap) {
	t.processing.Lock()
	defer t.processing.Unlock()
	if t.ri != nil && !t.ri.isStopped() && sameConfig(t.ri.currentConfigMap, cm) {
		t.ri.log.Debug().Msg("already running no change")
		return
	}

	// start the new instance before stopping the old one to avoid a gap
	instanceToStop := t.ri
	newri, err := newRunningInstance(cfc, cm)
	if err != nil {
		return
	}
	t.ri = newri
	if instanceToStop != nil {
		instanceToStop.Stop(cfc)
	}
}

func (t *Tunnel) Stop(cfc types.CFController) {
	t.processing.Lock()
	defer t.processing.Unlock()
	if t.ri != nil {
		t.ri.Stop(cfc)
		t.ri = nil
	}
}

type TunnelRunner struct {
	block   sync.Mutex
	tunnels map[string]*Tunnel
}

func NewTunnelRunner() *TunnelRunner {
	return &TunnelRunner{
		tunnels: make(map[string]*Tunnel),
	}
}

func (tr *TunnelRunner) getTunnel(cm *corev1.ConfigMap) *Tunnel {
	tr.block.Lock()
	defer tr.block.Unlock()
	key := cm.Namespace + "/" + cm.Name
	tunnel, found := tr.tunnels[key]
	if !found {
		tunnel = &Tunnel{}
		tr.tunnels[key] = tunnel
	}
	return tunnel
}

func (tr *TunnelRunner) Start(cfc types.CFController, cm *corev1.ConfigMap) {
	tr.getTunnel(cm).Start(cfc, cm)
}

func (tr *TunnelRunner) Stop(cfc types.CFController, cm *corev1.ConfigMap) {
	tr.getTunnel(cm).Stop(cfc)
}

func ConfigMapHandlerStartCloudflared(_cfc types.CFController) func(cms []*corev1.ConfigMap, ev watch.Event) {
	cfc := _cfc.WithComponent("cloudflared")
	tr := NewTunnelRunner()
	return func(cms []*corev1.ConfigMap, ev watch.Event) {
		cm, found := ev.Object.(*corev1.ConfigMap)
		if !found {
			cfc.Log().Error().Msg("error casting object")
			return
		}
		switch ev.Type {
		case watch.Added, watch.Modified:
			tr.Start(cfc, cm)
		case watch.Deleted:
			tr.Stop(cfc, cm)
		default:
			cfc.Log().Error().Str("event", string(ev.Type)).Msg("unknown event type")
		}
	}
}
