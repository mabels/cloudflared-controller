package ingress

import (
	"fmt"

	"github.com/mabels/cloudflared-controller/controller"
	"github.com/mabels/cloudflared-controller/controller/config"
	"github.com/mabels/cloudflared-controller/controller/tunnel"
	"github.com/rs/zerolog/log"
	netv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
)

func directIngress(_cfc *controller.CFController, ev watch.Event, ingress *netv1.Ingress) {
	// cfc := _cfc.WithComponent("directIngress", func(cfc *controller.CFController) {
	// 	log := cfc.Log.With().Str("ingress", ingress.Name).Logger()
	// 	cfc.Log = &log
	// })

}

func stackedIngress(_cfc *controller.CFController, ev watch.Event, ingress *netv1.Ingress) {
	cfc := _cfc.WithComponent("stackedIngress", func(cfc *controller.CFController) {
		log := cfc.Log.With().Str("ingress", ingress.Name).Logger()
		cfc.Log = &log
	})

	tp := tunnel.UpsertTunnelParams{
		Namespace: ingress.Namespace,
	}
	annotations := ingress.GetAnnotations()
	var ok bool
	tp.ExternalName, ok = annotations[config.AnnotationCloudflareTunnelExternalName]
	if !ok {
		log.Debug().Msgf("does not have %s annotation", config.AnnotationCloudflareTunnelExternalName)
		return

	}
	// tp.DefaultSecretName = false
	// tp.SecretName, ok = annotations[config.AnnotationCloudflareTunnelKeySecret]
	// if !ok {
	// 	tp.DefaultSecretName = true
	// 	tp.SecretName = fmt.Sprintf("cf-tunnel-key.%s",
	// 		reSanitze.ReplaceAllString(strings.ToLower(*tunnel.GetTunnelNameFromIngress(ingress)), "-"))
	// }
	nid, ok := annotations[config.AnnotationCloudflareTunnelName]
	if ok {
		my := nid
		tp.Name = &my
	}
	// nid, ok = annotations[config.AnnotationCloudflareTunnelId]
	// if ok {
	// 	id, err := uuid.Parse(nid)
	// 	if err != nil {
	// 		log.Error().Str("tunnelId", nid).Err(err).Msg("Failed to parse tunnel id")
	// 	} else {
	// 		tp.TunnelID = &id
	// 	}
	// }
	tp.Labels = ingress.GetLabels()
	ts, err := tunnel.UpsertTunnel(cfc, tp)
	if err != nil {
		cfc.Log.Error().Err(err).Msg("Failed to upsert tunnel")
		return
	}
	cfc.Log.Info().Str("tunnel", ts.TunnelID.String()).Str("externalName", tp.ExternalName).Msg("Upserted tunnel")

	cfcis := []config.CFConfigIngress{}
	for _, rule := range ingress.Spec.Rules {
		if rule.HTTP == nil {
			cfc.Log.Warn().Str("name", *tp.Name).Str("host", rule.Host).Msg("Skipping non-http ingress rule")
			continue
		}
		schema := "http"
		port := ""
		for _, tls := range ingress.Spec.TLS {
			for _, thost := range tls.Hosts {
				if thost == rule.Host {
					schema = "https"
					break
				}
			}
		}
		_port, ok := annotations[config.AnnotationCloudflareTunnelPort]
		if ok {
			port = ":" + _port
		}
		for _, path := range rule.HTTP.Paths {
			cci := config.CFConfigIngress{
				Hostname: tp.ExternalName,
				Path:     path.Path,
				Service:  fmt.Sprintf("%s://%s%s", schema, rule.Host, port),
				OriginRequest: &config.CFConfigOriginRequest{
					HttpHostHeader: rule.Host,
				},
			}
			cfcis = append(cfcis, cci)
		}
	}

	tunnel.WriteCloudflaredConfig(cfc, tp, ts, ingress.ObjectMeta.GetUID(), cfcis)
}

func WatchIngress(_cfc *controller.CFController, namespace string) (watch.Interface, error) {
	cfc := _cfc.WithComponent("watchIngress", func(cfc *controller.CFController) {
		my := cfc.Log.With().Str("ns", namespace).Logger()
		cfc.Log = &my
	})
	wif, err := cfc.Rest.K8s.NetworkingV1().Ingresses(namespace).Watch(cfc.Context, metav1.ListOptions{})
	if err != nil {
		cfc.Log.Error().Err(err).Msg("Error watching ingress")
	}
	go func() {
		cfc.Log.Info().Msg("Start watching ingress")
		for {
			ev, more := <-wif.ResultChan()
			if !more {
				break
			}
			ingress, ok := ev.Object.(*netv1.Ingress)
			if !ok {
				cfc.Log.Error().Msg("Failed to cast to Ingress")
				continue
			}
			if ingress.Namespace != namespace {
				cfc.Log.Error().Msg("Ingress not in watched namespace")
				continue
			}
			annotations := ingress.GetAnnotations()
			_, foundCTN := annotations[config.AnnotationCloudflareTunnelName]
			// _, foundCID := annotations[config.AnnotationCloudflareTunnelId]
			if !foundCTN {
				cfc.Log.Debug().Str("name", ingress.Name).Msgf("skipping not cloudflared annotated(%s)",
					config.AnnotationCloudflareTunnelName)
				continue
			}
			if ingress.Spec.IngressClassName != nil && *ingress.Spec.IngressClassName == "cloudfared" {
				directIngress(cfc, ev, ingress)
			} else {
				stackedIngress(cfc, ev, ingress)
			}

		}
	}()
	return wif, nil
}
