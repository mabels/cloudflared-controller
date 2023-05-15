package watcher

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/mabels/cloudflared-controller/controller/types"
)

// cfc.Rest.K8s.NetworkingV1().Ingresses(namespace)i

type watchState string

const (
	watchStateStopped  watchState = "stopped"
	watchStateStarted  watchState = "started"
	watchStateStopping watchState = "stopping"
)

type Watcher[R any, RO runtime.Object, I types.K8SItem[RO], C types.K8SClient[RO, I]] struct {
	types.WatcherConfig[R, RO, I, C]

	watchState   watchState
	restartCount int
	restartFunc  func()

	wif       watch.Interface
	stateSync sync.Mutex
	state     map[string]RO

	bindingsSync sync.Mutex
	bindings     map[string]types.WatchFunc[RO]

	watcher sync.WaitGroup
}

func NewWatcher[R any, RO runtime.Object, I types.K8SItem[RO], C types.K8SClient[RO, I]](in types.WatcherConfig[R, RO, I, C]) types.Watcher[RO] {
	my := Watcher[R, RO, I, C]{
		watchState: watchStateStopped,
		state:      make(map[string]RO),
		bindings:   make(map[string]types.WatchFunc[RO]),
	}
	my.WatcherConfig = in
	if my.Context == nil {
		my.Context = context.Background()
	}
	if my.Log == nil {
		log := zerolog.New(os.Stderr).With().Logger()
		my.Log = &log
	}
	return &my
}

func (w *Watcher[R, RO, C, L]) GetContext() context.Context {
	return w.Context
}

// returns a function to unregister the event
func (w *Watcher[R, RO, C, L]) RegisterEvent(fn types.WatchFunc[RO]) func() {
	w.bindingsSync.Lock()
	id := uuid.New().String()
	w.bindings[id] = fn
	w.bindingsSync.Unlock()

	state := w.GetState()
	for _, st := range state {
		fn(state, watch.Event{
			Type:   watch.Added,
			Object: st,
		})
	}

	return func() {
		w.bindingsSync.Lock()
		defer w.bindingsSync.Unlock()
		delete(w.bindings, id)
	}
}

func (w *Watcher[R, RO, C, L]) fetchFullState() error {
	list, err := w.K8sClient.List(w.Context, w.ListOptions)
	if err != nil {
		log.Error().Err(err).Msg("Failed to list")
		return err
	}
	w.stateSync.Lock()
	defer w.stateSync.Unlock()

	for _, item := range list.GetItems() {
		r := item.GetItem()
		w.state[string(item.GetUID())] = r
	}
	return nil
}

func (w *Watcher[R, RO, C, L]) GetState() []RO {
	out := make([]RO, 0, len(w.state))
	w.stateSync.Lock()
	defer w.stateSync.Unlock()
	for _, item := range w.state {
		out = append(out, item)
	}
	return out
}

func (w *Watcher[R, RO, C, L]) getResultChan() (<-chan watch.Event, error) {
	var err error
	w.wif, err = w.K8sClient.Watch(w.Context, w.ListOptions)
	if err != nil {
		w.Log.Error().Err(err).Msg("Error watching")
		return nil, err
	}
	return w.wif.ResultChan(), nil
}

func (w *Watcher[R, RO, C, L]) Start() error {
	if w.watchState != watchStateStopped {
		err := fmt.Errorf("Already started")
		w.Log.Err(err).Msg(err.Error())
		return err
	}
	// setup watch to queue the events during startup
	wifChan, err := w.getResultChan()
	if err != nil {
		return err
	}
	// read initial state
	err = w.fetchFullState()
	if err != nil {
		return err
	}
	w.watcher.Add(1)
	// async watch loop
	w.watchState = watchStateStarted
	go func() {
		w.Log.Info().Msg("Start watching")
		for {
			ev, more := <-wifChan // <-w.wif.ResultChan()
			if !more {
				if w.watchState == watchStateStarted {
					for !more {
						w.Log.Info().Msgf("Restarting watch:%d", w.restartCount)
						wifChan, err = w.getResultChan()
						w.restartCount++
						if w.restartFunc != nil {
							w.restartFunc()
						}
						if err != nil {
							w.Log.Error().Err(err).Msgf("Error restarting watch:%d", w.restartCount)
							time.Sleep(5 * time.Second)
							continue
						}
						more = true
					}
					continue
				} else {
					w.Log.Info().Msgf("Break event")
					break
				}
			}
			obj, found := ev.Object.(metav1.Object)
			if !found {
				status, found := ev.Object.(*metav1.Status)
				if !found {
					w.Log.Warn().Msgf("Unknown object type: %T", ev.Object)
					continue
				}
				w.Log.Warn().Any("status", status).Msgf("Watch closed")
				continue
			}
			ostr := string(obj.GetUID())
			switch ev.Type {
			case watch.Added:
				w.stateSync.Lock()
				w.state[ostr] = ev.Object.(RO)
				w.stateSync.Unlock()

			case watch.Modified:
				w.stateSync.Lock()
				w.state[ostr] = ev.Object.(RO)
				w.stateSync.Unlock()

			case watch.Deleted:
				w.stateSync.Lock()
				delete(w.state, ostr)
				w.stateSync.Unlock()
			default:
				w.Log.Warn().Msgf("Unknown event type: %s", ev.Type)
			}
			state := w.GetState()
			var bindings []types.WatchFunc[RO]
			w.bindingsSync.Lock()
			bindings = make([]types.WatchFunc[RO], 0, len(w.bindings))
			for _, fn := range w.bindings {
				bindings = append(bindings, fn)
			}
			w.bindingsSync.Unlock()
			for _, fn := range bindings {
				fn(state, ev)
			}
		}
		w.Log.Info().Msg("Stop watching")
		w.watcher.Done()
	}()
	return nil
}

func (w *Watcher[R, RO, C, L]) Stop() {
	if w.watchState == watchStateStarted {
		w.watchState = watchStateStopping
		w.wif.Stop()
		w.Log.Debug().Msg("Waiting for watcher to stop")
		w.watcher.Wait()
		w.watchState = watchStateStopped
		w.wif = nil
		w.restartCount = 0
		w.state = make(map[string]RO)
		w.bindings = make(map[string]types.WatchFunc[RO])
	} else {
		w.Log.Warn().Msgf("Not started:%s", w.watchState)
	}
}
