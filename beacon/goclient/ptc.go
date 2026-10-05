package goclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/jellydator/ttlcache/v3"

	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
)

// Gloas (ePBS) Payload Timeliness Committee endpoints. These hand-rolled HTTP requests predate the
// go-eth2-client fork's typed PTC calls (PTCDuties, PayloadAttestationData,
// SubmitPayloadAttestationMessages); moving onto those, like the rest of GoClient, is a follow-up
// (issue #3014).
const (
	ptcDutiesPath              = "/eth/v1/validator/duties/ptc/%d"                    // epoch
	payloadAttestationDataPath = "/eth/v1/validator/payload_attestation_data?slot=%d" // slot
	payloadAttestationsPath    = "/eth/v1/beacon/pool/payload_attestations"
)

// PayloadAttestationDuties returns the PTC duties for the given validators at the epoch, with the
// dependent_root they were derived from, from the first beacon client that responds.
func (gc *GoClient) PayloadAttestationDuties(ctx context.Context, epoch phase0.Epoch, validatorIndices []phase0.ValidatorIndex) (*gloas.PTCDuties, error) {
	return firstClientResult(ctx, gc, "PayloadAttestationDuties", http.MethodPost, func(ctx context.Context, addr string) (*gloas.PTCDuties, error) {
		return requestPTCDuties(ctx, gloasHTTPClient, addr, epoch, validatorIndices)
	})
}

// PayloadAttestationData returns the PayloadAttestationData to attest to for the slot, or (nil, nil) if
// the beacon node reports no block for the slot (204) — the SIP #94 §3 abstain signal. The data is
// slot-level and every PTC member of the slot asks for it at the same cutoff, so calls for a slot that are
// in flight together are joined into one request, and a fetched result is reused for the slot's later
// callers (issue #3031). The abstain signal is not cached: a block may still arrive for a later caller.
func (gc *GoClient) PayloadAttestationData(ctx context.Context, slot phase0.Slot) (*gloas.PayloadAttestationData, error) {
	data, err, _ := gc.payloadAttestationReqInflight.Do(slot, func() (*gloas.PayloadAttestationData, error) {
		if cached := gc.payloadAttestationDataCache.Get(slot); cached != nil {
			return cached.Value(), nil
		}
		// Detach from the leader caller's ctx so its cancellation doesn't fail the callers joined into this
		// request; the fetch carries its own per-client timeout.
		data, err := gc.fetchPayloadAttestationDataFunc(context.WithoutCancel(ctx), slot)
		if err != nil {
			return nil, err
		}
		if data != nil {
			gc.payloadAttestationDataCache.Set(slot, data, ttlcache.DefaultTTL)
		}
		return data, nil
	})
	return data, err
}

// fetchPayloadAttestationData fetches the slot's payload-attestation data from the first beacon client
// that responds. A 204 is an answer, not an error, so it stops the client fallback — the operator abstains
// on its own node's view rather than polling the rest for a block.
func (gc *GoClient) fetchPayloadAttestationData(ctx context.Context, slot phase0.Slot) (*gloas.PayloadAttestationData, error) {
	return firstClientResult(ctx, gc, "PayloadAttestationData", http.MethodGet, func(ctx context.Context, addr string) (*gloas.PayloadAttestationData, error) {
		return requestPayloadAttestationData(ctx, gloasHTTPClient, addr, slot)
	})
}

// SubmitPayloadAttestationMessages broadcasts signed PTC messages to every beacon client's pool,
// succeeding if at least one accepts them.
func (gc *GoClient) SubmitPayloadAttestationMessages(ctx context.Context, messages []*gloas.PayloadAttestationMessage) error {
	ctx, cancel := context.WithTimeout(ctx, gc.commonTimeout)
	defer cancel()

	return gc.multiClientSubmit(ctx, "SubmitPayloadAttestationMessages", func(ctx context.Context, client Client) error {
		return submitPayloadAttestationMessages(ctx, gloasHTTPClient, gc.clientAddresses[client], messages)
	})
}

// requestPTCDuties POSTs the validator indices and returns their PTC duties for the epoch, with the
// dependent_root the beacon node derived them from.
func requestPTCDuties(ctx context.Context, httpClient *http.Client, addr string, epoch phase0.Epoch, validatorIndices []phase0.ValidatorIndex) (*gloas.PTCDuties, error) {
	indices := make([]string, len(validatorIndices))
	for i, idx := range validatorIndices {
		indices[i] = strconv.FormatUint(uint64(idx), 10)
	}
	body, err := json.Marshal(indices)
	if err != nil {
		return nil, fmt.Errorf("marshal validator indices: %w", err)
	}

	var resp struct {
		DependentRoot phase0.Root      `json:"dependent_root"`
		Data          []*gloas.PTCDuty `json:"data"`
	}
	if err := jsonDo(ctx, httpClient, http.MethodPost, addr+fmt.Sprintf(ptcDutiesPath, epoch), body, nil, &resp); err != nil {
		return nil, err
	}
	return &gloas.PTCDuties{DependentRoot: resp.DependentRoot, Duties: resp.Data}, nil
}

// requestPayloadAttestationData GETs the PayloadAttestationData for the slot. A 204 No Content —
// the beacon-APIs "no block seen" signal — returns (nil, nil) rather than a decode error on the
// empty body.
func requestPayloadAttestationData(ctx context.Context, httpClient *http.Client, addr string, slot phase0.Slot) (*gloas.PayloadAttestationData, error) {
	respBody, _, status, err := httpDo(ctx, httpClient, http.MethodGet, addr+fmt.Sprintf(payloadAttestationDataPath, slot), nil, "application/json", "", nil)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNoContent {
		return nil, nil
	}
	var resp struct {
		Data *gloas.PayloadAttestationData `json:"data"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if resp.Data == nil {
		return nil, errors.New("no payload attestation data in response")
	}
	// SIP #94 §3 takes the slot from the duty: data for another slot would be signed under this slot's
	// domain and refused on submit, so refuse it here, before it is cached.
	if resp.Data.Slot != slot {
		return nil, fmt.Errorf("payload attestation data slot mismatch: got %d, want %d", resp.Data.Slot, slot)
	}
	return resp.Data, nil
}

// submitPayloadAttestationMessages POSTs signed PTC messages to the beacon node's pool.
func submitPayloadAttestationMessages(ctx context.Context, httpClient *http.Client, addr string, messages []*gloas.PayloadAttestationMessage) error {
	body, err := json.Marshal(messages)
	if err != nil {
		return fmt.Errorf("marshal payload attestation messages: %w", err)
	}
	headers := map[string]string{consensusVersionHeader: consensusVersionGloas}
	return jsonDo(ctx, httpClient, http.MethodPost, addr+payloadAttestationsPath, body, headers, nil)
}
