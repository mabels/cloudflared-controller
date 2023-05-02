package leader

import (
	"context"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"

	"github.com/mabels/cloudflared-controller/controller"
)

func getNewLock(cfc *controller.CFController, lockname, namespace string) *resourcelock.LeaseLock {
	return &resourcelock.LeaseLock{
		LeaseMeta: metav1.ObjectMeta{
			Name:      lockname,
			Namespace: namespace,
		},
		Client: cfc.Rest.K8s.CoordinationV1(),
		LockConfig: resourcelock.ResourceLockConfig{
			Identity: cfc.Cfg.Identity,
		},
	}
}

func runLeaderElection(cfc *controller.CFController, lock *resourcelock.LeaseLock, ctx context.Context) {
	leaderelection.RunOrDie(ctx, leaderelection.LeaderElectionConfig{
		Lock:            lock,
		ReleaseOnCancel: true,
		LeaseDuration:   15 * time.Second,
		RenewDeadline:   10 * time.Second,
		RetryPeriod:     2 * time.Second,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(c context.Context) {
				cfc.Log.Info().Msg("became leader, starting work.")
				for {
					time.Sleep(5 * time.Second)
				}
			},
			OnStoppedLeading: func() {
				cfc.Log.Info().Msg("no longer the leader, staying inactive.")
			},
			OnNewLeader: func(current_id string) {
				if current_id == cfc.Cfg.Identity {
					cfc.Log.Info().Msg("still the leader!")
					return
				}
				cfc.Log.Info().Msgf("new leader is %s", current_id)
			},
		},
	})
}

func LeaderSelection(cfc *controller.CFController, lockName, lockNamespace string) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	lock := getNewLock(cfc, lockName, lockNamespace)
	runLeaderElection(cfc, lock, ctx)
}
