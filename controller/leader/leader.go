package leader

import (
	"github.com/mabels/cloudflared-controller/controller/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
)

func LeaderSelection(cfc types.CFController, lcb leaderelection.LeaderCallbacks) {
	cfc.Log().Info().Str("namespace", cfc.Cfg().Leader.Namespace).Str("name", cfc.Cfg().Leader.Name).Msg("Start Leader Election")
	lock := resourcelock.LeaseLock{
		LeaseMeta: metav1.ObjectMeta{
			Name:      cfc.Cfg().Leader.Name,
			Namespace: cfc.Cfg().Leader.Namespace,
		},
		Client: cfc.Rest().K8s().CoordinationV1(),
		LockConfig: resourcelock.ResourceLockConfig{
			Identity: cfc.Cfg().Identity,
		},
	}
	leaderelection.RunOrDie(cfc.Context(), leaderelection.LeaderElectionConfig{
		Lock:            &lock,
		ReleaseOnCancel: true,
		LeaseDuration:   cfc.Cfg().Leader.LeaseDuration,
		RenewDeadline:   cfc.Cfg().Leader.RenewDeadline,
		RetryPeriod:     cfc.Cfg().Leader.RetryPeriod,
		Callbacks:       lcb,
	})
}
