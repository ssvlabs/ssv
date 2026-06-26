package goclient

import (
	"context"
	"encoding/hex"
	"fmt"
	"net/http"

	"github.com/attestantio/go-eth2-client/spec/phase0"

	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
)

// Gloas §6 envelope produce/publish endpoints (beacon-APIs#580, unmerged). Best-effort paths, as with
// the §4 block endpoints — verify against a real Gloas devnet BN. The publish body is the bare signed
// envelope (stateful path); the blob-carrying Contents body is deferred.
const (
	gloasProduceEnvelopePath = "/eth/v1/validator/execution_payload_envelope/%d?beacon_block_root=%s" // slot, root 0x-hex
	gloasPublishEnvelopePath = "/eth/v1/beacon/execution_payload_envelope"
)

// GetExecutionPayloadEnvelope fetches the §6 execution-payload envelope (the payload the proposer
// committed to for the slot) as SSZ — go-eth2-client has no Gloas types.
func (gc *GoClient) GetExecutionPayloadEnvelope(ctx context.Context, slot phase0.Slot, beaconBlockRoot phase0.Root) (*gloas.ExecutionPayloadEnvelope, error) {
	return firstClientResult(ctx, gc, "GetExecutionPayloadEnvelope", http.MethodGet, func(ctx context.Context, addr string) (*gloas.ExecutionPayloadEnvelope, error) {
		return requestExecutionPayloadEnvelope(ctx, addr, slot, beaconBlockRoot)
	})
}

// SubmitExecutionPayloadEnvelope publishes a signed §6 envelope as SSZ.
func (gc *GoClient) SubmitExecutionPayloadEnvelope(ctx context.Context, signed *gloas.SignedExecutionPayloadEnvelope) error {
	body, err := signed.MarshalSSZ()
	if err != nil {
		return fmt.Errorf("marshal signed execution payload envelope: %w", err)
	}
	_, err = firstClientResult(ctx, gc, "SubmitExecutionPayloadEnvelope", http.MethodPost, func(ctx context.Context, addr string) (struct{}, error) {
		return struct{}{}, submitExecutionPayloadEnvelope(ctx, addr, body)
	})
	return err
}

// requestExecutionPayloadEnvelope GETs the produce endpoint and decodes the SSZ response into an envelope.
func requestExecutionPayloadEnvelope(ctx context.Context, addr string, slot phase0.Slot, beaconBlockRoot phase0.Root) (*gloas.ExecutionPayloadEnvelope, error) {
	url := addr + fmt.Sprintf(gloasProduceEnvelopePath, slot, "0x"+hex.EncodeToString(beaconBlockRoot[:]))
	body, err := gloasOctetStreamHTTP(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	envelope := &gloas.ExecutionPayloadEnvelope{}
	if err := envelope.UnmarshalSSZ(body); err != nil {
		return nil, fmt.Errorf("decode execution payload envelope: %w", err)
	}
	return envelope, nil
}

// submitExecutionPayloadEnvelope POSTs an SSZ-marshaled signed envelope to the publish endpoint.
func submitExecutionPayloadEnvelope(ctx context.Context, addr string, envelopeSSZ []byte) error {
	_, err := gloasOctetStreamHTTP(ctx, http.MethodPost, addr+gloasPublishEnvelopePath, envelopeSSZ)
	return err
}
