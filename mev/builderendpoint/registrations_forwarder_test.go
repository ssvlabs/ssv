package builderendpoint_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	builderclient "github.com/attestantio/go-builder-client"
	builderapi "github.com/attestantio/go-builder-client/api"
	apiv1 "github.com/attestantio/go-builder-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"

	"github.com/ssvlabs/ssv/mev/builderendpoint"
)

type fakeSubmitter struct {
	addr  string
	calls *int32
	err   error
}

func (s fakeSubmitter) Name() string              { return "fake" }
func (s fakeSubmitter) Address() string           { return s.addr }
func (s fakeSubmitter) Pubkey() *phase0.BLSPubKey { return nil }

func (s fakeSubmitter) SubmitValidatorRegistrations(context.Context, *builderapi.SubmitValidatorRegistrationsOpts) error {
	atomic.AddInt32(s.calls, 1)
	return s.err
}

type fakeFactory struct {
	submitters map[string]builderclient.ValidatorRegistrationsSubmitter
}

func (f fakeFactory) FetchSubmitter(address string) (builderclient.ValidatorRegistrationsSubmitter, error) {
	s, ok := f.submitters[address]
	if !ok {
		return nil, errors.New("missing")
	}
	return s, nil
}

func TestRegistrationsForwarderForwardsToAllRelays(t *testing.T) {
	t.Parallel()

	var calls int32
	f := fakeFactory{
		submitters: map[string]builderclient.ValidatorRegistrationsSubmitter{
			"a": fakeSubmitter{addr: "a", calls: &calls},
			"b": fakeSubmitter{addr: "b", calls: &calls},
		},
	}

	forwarder := &builderendpoint.RegistrationsForwarder{
		Factory: f,
		Relays:  []string{"a", "b"},
	}

	errs, err := forwarder.ForwardValidatorRegistrations(context.Background(), []*apiv1.SignedValidatorRegistration{})
	if err != nil {
		t.Fatalf("forward: %v", err)
	}
	if len(errs) != 0 {
		t.Fatalf("expected no errors, got %v", errs)
	}
	if atomic.LoadInt32(&calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", atomic.LoadInt32(&calls))
	}
}
