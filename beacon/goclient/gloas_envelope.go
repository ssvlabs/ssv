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
// beacon block root as a path segment; publish posts the full signed envelope (see SubmitExecutionPayloadEnvelope),
// with the required blobDataIncludedHeader selecting the body form (beacon-APIs#624): "false" is the full
// SignedExecutionPayloadEnvelope (stateful flow), "true" the blobs-carrying Contents.
const (
	gloasProduceEnvelopePath = "/eth/v1/validator/execution_payload_envelopes/%d/%s" // slot, beacon_block_root 0x-hex
	gloasPublishEnvelopePath = "/eth/v1/beacon/execution_payload_envelopes"
	blobDataIncludedHeader   = "Eth-Blob-Data-Included"
)

// GetExecutionPayloadEnvelope fetches the §6 execution-payload envelope (the payload the proposer
// committed to for the slot) as SSZ — go-eth2-client has no Gloas types.
func (gc *GoClient) GetExecutionPayloadEnvelope(ctx context.Context, slot phase0.Slot, beaconBlockRoot phase0.Root) (*gloas.ExecutionPayloadEnvelope, error) {
	return firstClientResult(ctx, gc, "GetExecutionPayloadEnvelope", http.MethodGet, func(ctx context.Context, addr string) (*gloas.ExecutionPayloadEnvelope, error) {
		return requestExecutionPayloadEnvelope(ctx, addr, slot, beaconBlockRoot)
	})
}

// SubmitExecutionPayloadEnvelope publishes the full signed §6 envelope as SSZ to all configured beacon
// nodes concurrently, succeeding if at least one accepts it. Re-publishing to multiple BNs is safe — they
// dedupe by block root.
//
// The body is the full SignedExecutionPayloadEnvelope, whose hash-tree root equals the blinded root the §6
// QBFT signed, so the reconstructed signature stays valid. beacon-APIs#580 also defined a blinded body, but
// beacon-APIs#624 removed it; the required Eth-Blob-Data-Included header now selects the full envelope
// (false, the stateful flow — the beacon node attaches the blobs it cached at production) over the deferred,
// stateless SignedExecutionPayloadEnvelopeContents (true — envelope + blobs + KZG, not yet wired). Lodestar
// v1.43.0, the first CL to implement the endpoint, takes the full envelope.
func (gc *GoClient) SubmitExecutionPayloadEnvelope(ctx context.Context, signed *gloas.SignedExecutionPayloadEnvelope) error {
	body, err := signed.MarshalSSZ()
	if err != nil {
		return fmt.Errorf("marshal signed execution payload envelope: %w", err)
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

// submitExecutionPayloadEnvelope POSTs the SSZ full signed envelope, tagged Eth-Blob-Data-Included: false
// per beacon-APIs#624 (see SubmitExecutionPayloadEnvelope). An already-known response is treated as success:
// on the self-build path every operator publishes the identical envelope, so the non-winning ones race the
// canonical one and get EXECUTION_PAYLOAD_ENVELOPE_ERROR_ALREADY_KNOWN — the §6 analog of the §4 block
// submit (see submitGloasBeaconBlock).
func submitExecutionPayloadEnvelope(ctx context.Context, addr string, envelopeSSZ []byte) error {
	headers := map[string]string{blobDataIncludedHeader: "false"}
	_, err := gloasOctetStreamHTTP(ctx, http.MethodPost, addr+gloasPublishEnvelopePath, envelopeSSZ, headers)
	if isAlreadyKnown(err) {
		return nil
	}
	return err
}
