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
}

func newValidationActor() *validationActor {
	return &validationActor{
		inbox:  make(chan any, 64),
		stopCh: make(chan struct{}),
	}
}

func (a *validationActor) stop() {
	a.stopOnce.Do(func() {
		a.lifecycleMu.Lock()
		defer a.lifecycleMu.Unlock()

		a.stopped = true
		close(a.stopCh)
	})
}

func (a *validationActor) submit(msg any) bool {
	a.lifecycleMu.Lock()
	defer a.lifecycleMu.Unlock()

	if a.stopped {
		return false
	}

	a.inbox <- msg
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
