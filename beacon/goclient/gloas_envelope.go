package goclient

import (
	"context"
	"fmt"
	"net/http"

	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
)

// Gloas §6 envelope publish endpoint (beacon-APIs#580, merged 2026-06-29). The body form is selected by the
// required Eth-Blob-Data-Included header (beacon-APIs#624): "true" is SignedExecutionPayloadEnvelopeContents,
// the signed envelope with its blobs and KZG proofs, which any beacon node can broadcast; "false" is the
// bare signed envelope, accepted only by the node that built the block and cached them. The node always
// sends the former (SIP #94 §6).
const (
	gloasPublishEnvelopePath = "/eth/v1/beacon/execution_payload_envelopes"
	blobDataIncludedHeader   = "Eth-Blob-Data-Included"
)

// SubmitExecutionPayloadEnvelope publishes the §6 reveal as SSZ to all configured beacon nodes concurrently,
// succeeding if at least one accepts it. Carrying the blob data lets every node broadcast it, so the reveal
// has the same all-node redundancy as the §4 block publish. Re-publishing is safe — nodes dedupe by block
// root. Hand-rolled: it predates the go-eth2-client fork's typed envelope calls.
func (gc *GoClient) SubmitExecutionPayloadEnvelope(ctx context.Context, contents *gloas.SignedExecutionPayloadEnvelopeContents) error {
	body, err := contents.MarshalSSZ()
	if err != nil {
		return fmt.Errorf("marshal signed execution payload envelope contents: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, gc.commonTimeout)
	defer cancel()

	return gc.multiClientSubmit(ctx, "SubmitExecutionPayloadEnvelope", func(ctx context.Context, client Client) error {
		return submitExecutionPayloadEnvelope(ctx, gc.clientAddresses[client], body)
	})
}

// submitExecutionPayloadEnvelope POSTs the SSZ contents tagged Eth-Blob-Data-Included: true. An
// already-known response is treated as success: the builder operator publishes to each of its beacon nodes,
// and operators sharing a beacon node both publish the identical reveal, so a repeat gets
// EXECUTION_PAYLOAD_ENVELOPE_ERROR_ALREADY_KNOWN — the §6 analog of the §4 block submit (see
// submitGloasBeaconBlock).
func submitExecutionPayloadEnvelope(ctx context.Context, addr string, contentsSSZ []byte) error {
	headers := map[string]string{blobDataIncludedHeader: "true"}
	_, err := gloasOctetStreamHTTP(ctx, http.MethodPost, addr+gloasPublishEnvelopePath, contentsSSZ, headers)
	if isAlreadyKnown(err) {
		return nil
	}
	return err
}
