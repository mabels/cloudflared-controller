package tunnel

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/mabels/cloudflared-controller/controller"
	"github.com/mabels/cloudflared-controller/controller/config"
	"github.com/rs/zerolog"
	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
)

type runningInstance struct {
	id                 uuid.UUID
	tunnel             *Tunnel
	currentDir         string
	configfname        string
	cmd                *exec.Cmd
	log                *zerolog.Logger
	unregisterShutdown func()
}

func (ri *runningInstance) buildCredentialsFile(cfc *controller.CFController, cm *corev1.ConfigMap) (credfname string, err error) {
	tunnelId, found := cm.ObjectMeta.GetAnnotations()[config.AnnotationCloudflareTunnelId]
	if !found {
		return credfname, fmt.Errorf("missing label %s", config.AnnotationCloudflareTunnelId)
	}
	tunnelName, found := cm.ObjectMeta.GetAnnotations()[config.AnnotationCloudflareTunnelName]
	if !found {
		return credfname, fmt.Errorf("missing annotation %s", config.AnnotationCloudflareTunnelName)
	}
	utp := UpsertTunnelParams{
		Namespace: cm.Namespace,
		Name:      &tunnelName,
	}
	cts, err := MatchK8SSecret(cfc, tunnelId, utp)
	if err != nil {
		return credfname, err
	}
	fname := fmt.Sprintf("%s.json", tunnelId)
	bytesCts, err := json.Marshal(cts)
	if err != nil {
		return credfname, err
	}
	credfname = path.Join(ri.currentDir, fname)
	return credfname, os.WriteFile(credfname, bytesCts, 0600)
}

func (ri *runningInstance) buildConfig(credfname string, cm *corev1.ConfigMap) (*config.CFConfigYaml, error) {
	tunnelId, found := cm.ObjectMeta.GetAnnotations()[config.AnnotationCloudflareTunnelId]
	if !found {
		return nil, fmt.Errorf("missing label %s", config.AnnotationCloudflareTunnelId)
	}
	cfis := []config.CFConfigIngress{}
	for _, rules := range cm.Data {
		rule := []config.CFConfigIngress{}
		err := yaml.Unmarshal([]byte(rules), &rule)
		if err != nil {
			ri.log.Error().Err(err).Str("rules", rules).Msg("error unmarshalling rules")
			continue
		}
		cfis = append(cfis, rule...)
	}
	cfis = append(cfis, config.CFConfigIngress{Service: "http_status:404"})
	igss := config.CFConfigYaml{
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

func (ri *runningInstance) Stop(cfc *controller.CFController) {
	if ri.unregisterShutdown != nil {
		ri.unregisterShutdown()
		ri.unregisterShutdown = nil
	}
	if ri.cmd != nil {
		err := ri.cmd.Process.Kill()
		if err != nil {
			ri.log.Error().Err(err).Msg("error killing process")
		}
		if !cfc.Cfg.Debug {
			err := os.RemoveAll(ri.currentDir)
			if err != nil {
				ri.log.Error().Err(err).Str("dir", ri.currentDir).Msg("removing runtime dir")
			}
		}
		ri.cmd = nil
	}
}

func (ri *runningInstance) Start(cfc *controller.CFController) error {
	log := cfc.Log.With().Str("component", "cloudflared").Str("id", ri.id.String()).Logger()
	cfdFname, err := exec.LookPath(cfc.Cfg.CloudFlaredFname)
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

func (t *Tunnel) newRunningInstance(cfc *controller.CFController, cm *corev1.ConfigMap) (*runningInstance, error) {
	id := uuid.New()
	log := cfc.Log.With().Str("id", id.String()).Str("component", "cloudflared").Logger()
	ri := &runningInstance{
		tunnel:     t,
		id:         id,
		currentDir: path.Join(cfc.Cfg.RunningInstanceDir, id.String()),
		log:        &log,
	}
	ri.unregisterShutdown = cfc.RegisterShutdown(func() {
		ri.Stop(cfc)
	})
	err := os.MkdirAll(ri.currentDir, 0700)
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
		RegisterCFDnsEndpoint(cfc, uid, rule.Hostname)
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
			// time.Sleep(cfc.Cfg.RestartDelay)
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

func (t *Tunnel) Start(cfc *controller.CFController, cm *corev1.ConfigMap) {
	t.processing.Lock()
	defer t.processing.Unlock()
	instanceToStop := t.ri
	newri, err := t.newRunningInstance(cfc, cm)
	if err != nil {
		cfc.Log.Error().Err(err).Msg("error starting cloudflared")
		return
	}
	t.ri = newri
	if instanceToStop != nil {
		instanceToStop.Stop(cfc)
	}

}

func (t *Tunnel) Stop(cfc *controller.CFController) {
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

func (tr *TunnelRunner) Start(cfc *controller.CFController, cm *corev1.ConfigMap) {
	tr.getTunnel(cm.Name).Start(cfc, cm)
}

func (tr *TunnelRunner) Stop(cfc *controller.CFController, cm *corev1.ConfigMap) {
	tr.getTunnel(cm.Name).Stop(cfc)
}
