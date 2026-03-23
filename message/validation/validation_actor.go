package validation

import (
	"errors"
	"sync"

	spectypes "github.com/ssvlabs/ssv-spec/types"
)

var errValidationActorClosed = errors.New("validation actor closed")

type validationRequest struct {
	committeeInfo CommitteeInfo
	precheck      func(*ValidatorState) error
	verify        func() error
	commit        func(*ValidatorState) error
	result        chan error

	respondOnce sync.Once
}

func (r *validationRequest) respond(err error) {
	r.respondOnce.Do(func() {
		r.result <- err
	})
}

type validationVerified struct {
	request *validationRequest
	err     error
}

type validationActor struct {
	inbox       chan any
	stopCh      chan struct{}
	stopOnce    sync.Once
	lifecycleMu sync.Mutex
	stopped     bool
	active      int
	drained     *sync.Cond
}

func newValidationActor() *validationActor {
	actor := &validationActor{
		inbox:  make(chan any, 64),
		stopCh: make(chan struct{}),
	}
	actor.drained = sync.NewCond(&actor.lifecycleMu)
	return actor
}

func (a *validationActor) stop() {
	a.stopOnce.Do(func() {
		a.lifecycleMu.Lock()
		a.stopped = true
		for a.active > 0 {
			a.drained.Wait()
		}
		close(a.stopCh)
		a.lifecycleMu.Unlock()
	})
}

func (a *validationActor) submit(msg any) bool {
	a.lifecycleMu.Lock()
	if a.stopped {
		a.lifecycleMu.Unlock()
		return false
	}
	a.active++
	a.lifecycleMu.Unlock()

	a.inbox <- msg

	a.lifecycleMu.Lock()
	a.active--
	if a.active == 0 {
		a.drained.Broadcast()
	}
	a.lifecycleMu.Unlock()

	return true
}

func (a *validationActor) run(mv *messageValidator, key spectypes.MessageID) {
	for {
		select {
		case raw := <-a.inbox:
			switch msg := raw.(type) {
			case *validationRequest:
				state := mv.validatorState(key, msg.committeeInfo)
				if err := msg.precheck(state); err != nil {
					msg.respond(err)
					continue
				}

				go a.verifyAndResubmit(msg)

			case *validationVerified:
				if msg.err != nil {
					msg.request.respond(msg.err)
					continue
				}

				state := mv.validatorState(key, msg.request.committeeInfo)
				if err := msg.request.precheck(state); err != nil {
					msg.request.respond(err)
					continue
				}

				msg.request.respond(msg.request.commit(state))
			}

		case <-a.stopCh:
			a.drainPending()
			return
		}
	}
}

func (a *validationActor) verifyAndResubmit(req *validationRequest) {
	err := req.verify()
	// Callers block on req.result, so a stopped actor must still produce a terminal response.
	if !a.submit(&validationVerified{request: req, err: err}) {
		req.respond(errValidationActorClosed)
	}
}

func (a *validationActor) drainPending() {
	for {
		select {
		case raw := <-a.inbox:
			switch msg := raw.(type) {
			case *validationRequest:
				msg.respond(errValidationActorClosed)
			case *validationVerified:
				msg.request.respond(errValidationActorClosed)
			}
		default:
			return
		}
	}
}
