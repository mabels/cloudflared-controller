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

	"github.com/google/uuid"
	"github.com/mabels/cloudflared-controller/controller/config"
	"github.com/mabels/cloudflared-controller/controller/k8s_data"
	"github.com/mabels/cloudflared-controller/controller/types"
	"github.com/rs/zerolog"
	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/watch"
	"toolman.org/encoding/base56"
)

type runningInstance struct {
	id                 string // uuid.UUID
	tunnel             *Tunnel
	currentDir         string
	configfname        string
	cmd                *exec.Cmd
	log                *zerolog.Logger
	currentConfigMap   *corev1.ConfigMap
	unregisterShutdown func()
}

func (ri *runningInstance) buildCredentialsFile(cfc types.CFController, cm *corev1.ConfigMap) (credfname string, err error) {
	tunnelIdStr, found := cm.ObjectMeta.GetAnnotations()[config.AnnotationCloudflareTunnelId()]
	if !found {
		return credfname, fmt.Errorf("missing label %s", config.AnnotationCloudflareTunnelId())
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
		return nil, fmt.Errorf("missing label %s", config.AnnotationCloudflareTunnelId())
	}
	cfis := []types.CFConfigIngress{}
	for _, rules := range cm.Data {
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

func (ri *runningInstance) Stop(cfc types.CFController) {
	if ri.unregisterShutdown != nil {
		ri.unregisterShutdown()
		ri.unregisterShutdown = nil
	}
	if ri.cmd != nil {
		err := ri.cmd.Process.Kill()
		if err != nil {
			ri.log.Error().Err(err).Msg("error killing process")
		}
		if !cfc.Cfg().Debug {
			err := os.RemoveAll(ri.currentDir)
			if err != nil {
				ri.log.Error().Err(err).Str("dir", ri.currentDir).Msg("removing runtime dir")
			}
		}
		ri.cmd = nil
	}
}

func (ri *runningInstance) Start(cfc types.CFController) error {
	log := cfc.Log().With().Str("component", "cloudflared").Str("id", ri.id).Logger()
	cfdFname, err := exec.LookPath(cfc.Cfg().CloudFlaredFname)
	if err != nil {
		return err
	}
	// cloudflared tunnel --config ./config.yml  run
	cmds := []string{cfdFname, "tunnel", "--no-autoupdate", "--config", ri.configfname, "run"}
	ri.cmd = exec.Command(cfdFname, cmds[1:]...)

	log = log.With().Strs("cmds", cmds).Logger()
	stdErr, err := ri.cmd.StderrPipe()
	if err != nil {
		log.Error().Err(err).Msg("error getting stderr pipe")
		return err
	}
	stdOut, err := ri.cmd.StdoutPipe()
	if err != nil {
		log.Error().Err(err).Msg("error getting stdout pipe")
		return err
	}
	err = ri.cmd.Start()
	if err != nil {
		log.Error().Err(err).Msg("error starting command")
		return err
	}
	log = log.With().Int("pid", ri.cmd.Process.Pid).Logger()
	ri.log = &log
	action := func(pi io.ReadCloser) {
		log.Debug().Msg("started reading")
		fileScanner := bufio.NewScanner(pi)
		fileScanner.Split(bufio.ScanLines)
		for fileScanner.Scan() {
			log.Debug().Msg(fileScanner.Text())
		}
	}
	go action(stdErr)
	go action(stdOut)

	log.Info().Msg("started")
	return err
}

type Tunnel struct {
	processing   sync.Mutex
	tunnelRunner *TunnelRunner
	ri           *runningInstance
}

func idFromConfigMap(cm *corev1.ConfigMap) string {
	keys := make([]string, 0, len(cm.Data))
	for k, _ := range cm.Data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	data := make([]string, 0, 2*len(keys))
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

func (t *Tunnel) newRunningInstance(cfc types.CFController, cm *corev1.ConfigMap) (*runningInstance, error) {
	id := idFromConfigMap(cm)
	log := cfc.Log().With().Str("id", id).Str("component", "cloudflared").Logger()
	ri := &runningInstance{
		tunnel:           t,
		id:               id,
		currentConfigMap: cm.DeepCopy(),
		currentDir:       path.Join(cfc.Cfg().RunningInstanceDir, id),
		log:              &log,
	}
	ri.unregisterShutdown = cfc.RegisterShutdown(func() {
		ri.Stop(cfc)
	})
	stat, err := os.Stat(ri.currentDir)
	if err == nil && stat.IsDir() {
		log.Info().Msg("already running")
		return nil, fmt.Errorf("already running")
	}
	err = os.MkdirAll(ri.currentDir, 0700)
	if err != nil {
		log.Error().Err(err).Msg("error creating runtime dir")
		ri.Stop(cfc)
		return nil, err
	}
	credfname, err := ri.buildCredentialsFile(cfc, cm)
	if err != nil {
		log.Error().Err(err).Msg("error building credentials file")
		ri.Stop(cfc)
		return nil, err
	}
	cfgYaml, err := ri.buildConfig(credfname, cm)
	if err != nil {
		log.Error().Err(err).Msg("error building config file")
		ri.Stop(cfc)
		return nil, err
	}
	for _, rule := range cfgYaml.Ingress {
		if rule.Hostname == "" {
			continue
		}
		uid, err := uuid.Parse(cfgYaml.Tunnel)
		if err != nil {
			log.Error().Err(err).Str("tunnelId", cfgYaml.Tunnel).Msg("error parsing tunnel id")
			continue
		}
		registerCFDnsEndpoint(cfc, uid, rule.Hostname)
	}
	err = ri.Start(cfc)
	if err != nil {
		log.Error().Err(err).Msg("error starting cloudflared")
		ri.Stop(cfc)
		return nil, err
	}
	// var waitFn func()
	waitFn := func() {
		err := ri.cmd.Wait()
		if err != nil && strings.Contains(err.Error(), "signal: killed") {
			ri.Stop(cfc)
			ri.cmd = nil // just in case
			// time.Sleep(cfc.Cfg().RestartDelay)
			// err = ri.Start(cfc)
			// if err != nil {
			// 	log.Error().Err(err).Msg("error starting cloudflared")
			// 	ri.Stop(cfc)
			// 	return
			// }
			// waitFn()
			return
		}
		if err != nil {
			ri.log.Error().Err(err).Msg("error waiting for command")
		}
		ri.Stop(cfc)
		ri.cmd = nil // just in case
	}
	go waitFn()
	return ri, nil
}

func (t *Tunnel) Start(cfc types.CFController, cm *corev1.ConfigMap) {
	t.processing.Lock()
	defer t.processing.Unlock()
	if t.ri != nil && reflect.DeepEqual(t.ri.currentConfigMap.Data, cm.Data) {
		t.ri.log.Info().Msg("already running no change")
		return
	}

	instanceToStop := t.ri
	newri, err := t.newRunningInstance(cfc, cm)
	if err != nil {
		cfc.Log().Error().Err(err).Msg("error starting cloudflared")
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

func (tr *TunnelRunner) getTunnel(name string) *Tunnel {
	tr.block.Lock()
	defer tr.block.Unlock()
	tunnel, found := tr.tunnels[name]
	if !found {
		tunnel = &Tunnel{
			tunnelRunner: tr,
		}
		tr.tunnels[name] = tunnel
	}
	return tunnel
}

func (tr *TunnelRunner) Start(cfc types.CFController, cm *corev1.ConfigMap) {
	tr.getTunnel(cm.Name).Start(cfc, cm)
}

func (tr *TunnelRunner) Stop(cfc types.CFController, cm *corev1.ConfigMap) {
	tr.getTunnel(cm.Name).Stop(cfc)
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
		state, found := cm.Annotations[config.AnnotationCloudflareTunnelState()]
		if !found {
			cfc.Log().Error().Msg("error getting state")
			return
		}
		switch state {
		case "ready":
		case "preparing":
			cfc.Log().Debug().Msg("ignoring preparing state")
			return
		default:
			cfc.Log().Error().Str("state", state).Msg("unknown state")
			return
		}
		switch ev.Type {
		case watch.Added:
			tr.Start(cfc, cm)
		case watch.Modified:
			tr.Start(cfc, cm)
		case watch.Deleted:
			tr.Stop(cfc, cm)
		default:
			cfc.Log().Error().Str("event", string(ev.Type)).Msg("unknown event type")
		}
	}
}
