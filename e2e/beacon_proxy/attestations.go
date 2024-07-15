package beaconproxy

import (
	"encoding/json"
	"fmt"
	"net/http"

	eth2client "github.com/attestantio/go-eth2-client"
	"github.com/attestantio/go-eth2-client/api"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

func (b *BeaconProxy) handleAttesterDuties(w http.ResponseWriter, r *http.Request) {
	logger, gateway := b.requestContext(r)

	// Parse request.
	var epoch phase0.Epoch
	if chi.URLParam(r, "epoch") != "" {
		if _, err := fmt.Sscanf(chi.URLParam(r, "epoch"), "%d", &epoch); err != nil {
			b.error(r, w, 400, fmt.Errorf("failed to parse request: %w", err))
			return
		}
	}
	indices, err := parseIndicesFromRequest(r, true)
	if err != nil {
		b.error(r, w, 400, fmt.Errorf("failed to read request: %w", err))
		return
	}

	// Obtain duties.
	dutiesResp, err := b.client.(eth2client.AttesterDutiesProvider).AttesterDuties(
		r.Context(),
		&api.AttesterDutiesOpts{
			Epoch:   epoch,
			Indices: indices,
		},
	)
	if err != nil {
		b.error(r, w, 500, fmt.Errorf("failed to obtain attester duties: %w", err))
		return
	}
	if dutiesResp == nil {
		b.error(r, w, 500, fmt.Errorf("failed to obtain attester duties, duties response is nil"))
		return
	}

	// Intercept.
	dutiesResp.Data, err = gateway.Interceptor.InterceptAttesterDuties(r.Context(), epoch, indices, dutiesResp.Data)
	if err != nil {
		b.error(r, w, 500, fmt.Errorf("failed to intercept attester duties: %w", err))
		return
	}

	// Respond.
	if err := b.respond(r, w, dutiesResp.Data); err != nil {
		b.error(r, w, 500, fmt.Errorf("failed to encode response: %w", err))
		return
	}

	logger.Info("obtained attester duties",
		zap.Uint64("epoch", uint64(epoch)),
		zap.Int("indices", len(indices)),
	)
}

func (b *BeaconProxy) handleAttestationData(w http.ResponseWriter, r *http.Request) {
	logger, gateway := b.requestContext(r)

	// Parse request.
	var (
		slot           phase0.Slot
		committeeIndex phase0.CommitteeIndex
	)
	if err := scanURL(r, "slot:%d", &slot, "committee_index:%d", &committeeIndex); err != nil {
		b.error(r, w, 400, fmt.Errorf("failed to parse request: %w", err))
		return
	}

	// Obtain attestation data.
	attestationDataResp, err := b.client.(eth2client.AttestationDataProvider).AttestationData(
		r.Context(),
		&api.AttestationDataOpts{
			Slot:           slot,
			CommitteeIndex: committeeIndex,
		},
	)
	if err != nil {
		b.error(r, w, 500, fmt.Errorf("failed to obtain attestation data: %w", err))
		return
	}
	if attestationDataResp == nil {
		b.error(r, w, 500, fmt.Errorf("failed to obtain attestation data, response is nil"))
		return
	}

	// Intercept.
	attestationDataResp.Data, err = gateway.Interceptor.InterceptAttestationData(
		r.Context(),
		slot,
		committeeIndex,
		attestationDataResp.Data,
	)

	if err != nil {
		b.error(r, w, 500, fmt.Errorf("failed to intercept attestation data: %w", err))
		return
	}

	// Respond.
	if err := b.respond(r, w, attestationDataResp.Data); err != nil {
		b.error(r, w, 500, fmt.Errorf("failed to encode response: %w", err))
		return
	}

	logger.Info("obtained attestation data",
		zap.Uint64("slot", uint64(slot)),
		zap.Uint64("committee_index", uint64(committeeIndex)),
	)
}

func (b *BeaconProxy) handleSubmitAttestations(w http.ResponseWriter, r *http.Request) {
	logger, gateway := b.requestContext(r)

	// Parse request.
	var attestations []*phase0.Attestation
	if err := json.NewDecoder(r.Body).Decode(&attestations); err != nil {
		b.error(r, w, 400, fmt.Errorf("failed to parse request: %w", err))
		return
	}

	// Intercept.
	attestations, err := gateway.Interceptor.InterceptSubmitAttestations(r.Context(), attestations)
	if err != nil {
		b.error(r, w, 500, fmt.Errorf("failed to intercept attestation: %w", err))
		return
	}

	// Submit.
	//if err := b.client.(eth2client.AttestationsSubmitter).SubmitAttestations(r.Context(), attestations); err != nil {
	//	b.error(r, w, 500, fmt.Errorf("failed to submit attestation: %w", err))
	//	return
	//}

	// Respond.
	if err := b.respond(r, w, nil); err != nil {
		b.error(r, w, 500, fmt.Errorf("failed to encode response: %w", err))
		return
	}

	logger.Info("submitted attestation")
}
