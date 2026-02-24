package registrations

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"

	builderclient "github.com/attestantio/go-builder-client"
	builderapi "github.com/attestantio/go-builder-client/api"
	apiv1 "github.com/attestantio/go-builder-client/api/v1"
	builderspec "github.com/attestantio/go-builder-client/spec"
	"github.com/pkg/errors"
)

type SubmitterFactory interface {
	FetchSubmitter(address string) (builderclient.ValidatorRegistrationsSubmitter, error)
}

// Forwarder forwards validator registrations to relays.
type Forwarder struct {
	Factory SubmitterFactory
	Relays  []string
}

func (f *Forwarder) ForwardValidatorRegistrations(ctx context.Context, body io.ReadCloser) ([]string, error) {
	if body == nil {
		return nil, errors.New("nil body")
	}
	defer func() { _ = body.Close() }()

	if f == nil || f.Factory == nil || len(f.Relays) == 0 {
		return nil, nil
	}

	var regs []*apiv1.SignedValidatorRegistration
	if err := json.NewDecoder(body).Decode(&regs); err != nil {
		return nil, errors.Wrap(err, "invalid JSON")
	}
	versioned := make([]*builderapi.VersionedSignedValidatorRegistration, 0, len(regs))
	for _, r := range regs {
		if r == nil {
			return nil, errors.New("nil registration")
		}
		versioned = append(versioned, &builderapi.VersionedSignedValidatorRegistration{
			Version: builderspec.BuilderVersionV1,
			V1:      r,
		})
	}

	var mu sync.Mutex
	failures := make([]string, 0)

	var wg sync.WaitGroup
	wg.Add(len(f.Relays))
	for _, relay := range f.Relays {
		relayAddr := relay
		go func() {
			defer wg.Done()

			submitter, err := f.Factory.FetchSubmitter(relayAddr)
			if err != nil {
				mu.Lock()
				failures = append(failures, fmt.Sprintf("%s: %v", relayAddr, err))
				mu.Unlock()
				return
			}
			if err := submitter.SubmitValidatorRegistrations(ctx, &builderapi.SubmitValidatorRegistrationsOpts{
				Registrations: versioned,
			}); err != nil {
				mu.Lock()
				failures = append(failures, fmt.Sprintf("%s: %v", relayAddr, err))
				mu.Unlock()
				return
			}
		}()
	}
	wg.Wait()

	return failures, nil
}
