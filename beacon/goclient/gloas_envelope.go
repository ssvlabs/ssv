package goclient

import (
	"context"
	"encoding/hex"
	"fmt"
	"net/http"

	"github.com/attestantio/go-eth2-client/spec/phase0"

	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
)

// Gloas §6 envelope produce/publish endpoints (beacon-APIs#580, merged 2026-06-29). Produce takes the
// beacon block root as a path segment; publish posts the blinded body (see SubmitExecutionPayloadEnvelope).
const (
	gloasProduceEnvelopePath = "/eth/v1/validator/execution_payload_envelopes/%d/%s" // slot, beacon_block_root 0x-hex
	gloasPublishEnvelopePath = "/eth/v1/beacon/execution_payload_envelopes"
)

// GetExecutionPayloadEnvelope fetches the §6 execution-payload envelope (the payload the proposer
// committed to for the slot) as SSZ — go-eth2-client has no Gloas types.
func (gc *GoClient) GetExecutionPayloadEnvelope(ctx context.Context, slot phase0.Slot, beaconBlockRoot phase0.Root) (*gloas.ExecutionPayloadEnvelope, error) {
	return firstClientResult(ctx, gc, "GetExecutionPayloadEnvelope", http.MethodGet, func(ctx context.Context, addr string) (*gloas.ExecutionPayloadEnvelope, error) {
		return requestExecutionPayloadEnvelope(ctx, addr, slot, beaconBlockRoot)
	})
}

// SubmitExecutionPayloadEnvelope publishes the signed §6 envelope as its blinded SSZ form to all
// configured beacon nodes concurrently, succeeding if at least one accepts it. The producing BN
// reconstructs the full payload from its cache. Re-publishing to multiple BNs is safe — they dedupe by
// block root.
func (gc *GoClient) SubmitExecutionPayloadEnvelope(ctx context.Context, signed *gloas.SignedExecutionPayloadEnvelope) error {
	blinded, err := signed.Blinded()
	if err != nil {
		return fmt.Errorf("blind execution payload envelope: %w", err)
	}
	body, err := blinded.MarshalSSZ()
	if err != nil {
		return fmt.Errorf("marshal signed blinded execution payload envelope: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, gc.commonTimeout)
	defer cancel()

	return gc.multiClientSubmit(ctx, "SubmitExecutionPayloadEnvelope", func(ctx context.Context, client Client) error {
		return submitExecutionPayloadEnvelope(ctx, gc.clientAddresses[client], body)
	})
}

// requestExecutionPayloadEnvelope GETs the produce endpoint and decodes the SSZ response into an envelope.
func requestExecutionPayloadEnvelope(ctx context.Context, addr string, slot phase0.Slot, beaconBlockRoot phase0.Root) (*gloas.ExecutionPayloadEnvelope, error) {
	url := addr + fmt.Sprintf(gloasProduceEnvelopePath, slot, "0x"+hex.EncodeToString(beaconBlockRoot[:]))
	body, err := gloasOctetStreamHTTP(ctx, http.MethodGet, url, nil, nil)
	if err != nil {
		return nil, err
	}
	envelope := &gloas.ExecutionPayloadEnvelope{}
	if err := envelope.UnmarshalSSZ(body); err != nil {
		return nil, fmt.Errorf("decode execution payload envelope: %w", err)
	}
	return envelope, nil
}

// submitExecutionPayloadEnvelope POSTs the SSZ-marshaled signed blinded envelope to the publish endpoint,
// tagged Eth-Execution-Payload-Blinded.
func submitExecutionPayloadEnvelope(ctx context.Context, addr string, blindedEnvelopeSSZ []byte) error {
	headers := map[string]string{"Eth-Execution-Payload-Blinded": "true"}
	_, err := gloasOctetStreamHTTP(ctx, http.MethodPost, addr+gloasPublishEnvelopePath, blindedEnvelopeSSZ, headers)
	return err
}
