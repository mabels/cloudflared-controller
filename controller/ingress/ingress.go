package ingress

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/mabels/cloudflared-controller/controller"
	"github.com/mabels/cloudflared-controller/controller/config"
	"github.com/mabels/cloudflared-controller/controller/tunnel"
	netv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
)

func WatchIngress(_cfc *controller.CFController, namespace string) (watch.Interface, error) {
	cfc := _cfc.WithComponent("watchIngress", func(cfc *controller.CFController) {
		my := cfc.Log.With().Str("namespace", namespace).Logger()
		cfc.Log = &my
	})
	wif, err := cfc.Rest.K8s.NetworkingV1().Ingresses(namespace).Watch(context.Background(), metav1.ListOptions{})
	if err != nil {
		cfc.Log.Error().Str("ns", namespace).Err(err).Msg("Error watching ingress")
	}
	cfc.Log.Info().Str("ns", namespace).Msg("Start watching ingress")
	go func() {
		for {
			ev := <-wif.ResultChan()
			ingress, ok := ev.Object.(*netv1.Ingress)
			if !ok {
				cfc.Log.Error().Msg("Failed to cast to Ingress")
				continue
			}
			if ingress.Namespace != namespace {
				cfc.Log.Error().Msg("Ingress not in watched namespace")
				continue
			}
			tp := tunnel.UpsertTunnelParams{}
			// log.Debug().Str("name", ingress.Name).Msg("found ingress")
			annotations := ingress.GetAnnotations()
			tp.ExternalName, ok = annotations[config.AnnotationCloudflareTunnelExternalName]
			if !ok {
				// log.Debug().Msgf("does not have %s annotation", CloudflareTunnelExternalName)
				continue
			}
			// tp.accountID, ok = annotations[CloudflareTunnelAccountId]
			// if !ok {
			// 	tp.accountID = cfg.CloudFlare.AccountId
			// }
			// tp.zoneID, ok = annotations[CloudflareTunnelZoneId]
			// if !ok {
			// 	tp.zoneID = cfg.CloudFlare.ZoneId
			// }
			// if tp.accountID == "" || tp.zoneID == "" {
			// 	err := fmt.Errorf("accountID and zoneID must be set")
			// 	log.Error().Err(err).Msg("missing accountID or zoneID")
			// 	continue
			// }
			// tp.apiToken = cfg.CloudFlare.ApiToken
			// if tp.apiToken == "" {
			// 	log.Error().Msg("missing CLOUDFLARE_API_TOKEN")
			// 	continue
			// }

			tp.DefaultSecretName = false
			tp.SecretName, ok = ingress.Annotations[config.AnnotationCloudflareTunnelKeySecret]
			if !ok {
				tp.DefaultSecretName = true
				reSanitze := regexp.MustCompile(`[^a-zA-Z0-9]+`)
				tp.SecretName = fmt.Sprintf("cf-tunnel-key.%s",
					reSanitze.ReplaceAllString(strings.ToLower(*tunnel.GetTunnelNameFromIngress(ingress)), "-"))
			}
			tp.Namespace = ingress.Namespace
			// tp.rs.Identifier = accountId
			tp.Name = tunnel.GetTunnelNameFromIngress(ingress)
			tp.Ingress = ingress
			ts, err := tunnel.UpsertTunnel(cfc, tp)
			if err != nil {
				cfc.Log.Error().Err(err).Msg("Failed to upsert tunnel")
				continue
			}
			cfc.Log.Info().Str("tunnel", ts.TunnelID.String()).Str("externalName", tp.ExternalName).Msg("Upserted tunnel")

			tunnel.WriteCloudflaredConfig(cfc, tp, ts)
		}
	}()
	return wif, nil
}
