package goclient

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"

	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
)

// Gloas produce/publish endpoints. Produce is v4 with include_payload=true (SIP #94 §6): a self-build
// response is BlockContents — the block plus the envelope, blobs, and KZG proofs the operator reveals in
// §6 — so the reveal never depends on which beacon node built the block; any other response is the bare
// bid-carrying BeaconBlock, told apart by the Eth-Execution-Payload-Included header. Produce POSTs a
// BuilderConfig body (beacon-APIs#630) — the direct-builder overlay when configured, else a neutral
// local-build config — and falls back per beacon node to the legacy GET for nodes that predate the POST
// (beacon-APIs#580, GET-only). Publish is the standard v2 blocks endpoint (version-tagged via
// Eth-Consensus-Version).
const (
	gloasProduceBlockPath          = "/eth/v4/validator/blocks/%d?randao_reveal=%s&graffiti=%s&include_payload=true" // slot, randao 0x-hex, graffiti 0x-hex
	gloasPublishBlockPath          = "/eth/v2/beacon/blocks"
	executionPayloadIncludedHeader = "Eth-Execution-Payload-Included"
)

// GetGloasBeaconBlock produces a Gloas (ePBS) block from the first beacon node that succeeds (see
// requestGloasBeaconBlock). It is hand-rolled because go-eth2-client's ePBS proposal call is the pre-#630
// GET, with no typed equivalent for the POST body, the BlockContents response, or the response headers.
func (gc *GoClient) GetGloasBeaconBlock(ctx context.Context, slot phase0.Slot, graffiti, randao []byte, builderConfig *gloas.ProduceBuilderConfig) (*gloas.ProducedBlock, error) {
	return firstClientResultRecorded(ctx, gc, "GetGloasBeaconBlock", func(ctx context.Context, addr string, record requestRecorder) (*gloas.ProducedBlock, error) {
		return requestGloasBeaconBlock(ctx, addr, slot, graffiti, randao, builderConfig, record)
	})
}

// SubmitGloasBeaconBlock publishes a signed Gloas (ePBS) block as SSZ to all configured beacon nodes
// concurrently, succeeding if at least one accepts it. Re-publishing a signed block to multiple BNs is
// safe — they dedupe by block root. A non-empty builderURL is echoed as the Eth-Builder-Url header so the
// beacon node forwards the block to the winning builder (beacon-APIs#630); forwarding is idempotent, so
// echoing it to every node is safe.
func (gc *GoClient) SubmitGloasBeaconBlock(ctx context.Context, block *gloas.SignedBeaconBlock, builderURL string) error {
	body, err := block.MarshalSSZ()
	if err != nil {
		return fmt.Errorf("marshal signed gloas block: %w", err)
	}

	var extraHeaders map[string]string
	if builderURL != "" {
		extraHeaders = map[string]string{"Eth-Builder-Url": builderURL}
	}

	ctx, cancel := context.WithTimeout(ctx, gc.commonTimeout)
	defer cancel()

	return gc.multiClientSubmit(ctx, "SubmitGloasBeaconBlock", func(ctx context.Context, client Client) error {
		return submitGloasBeaconBlock(ctx, gc.clientAddresses[client], body, extraHeaders)
	})
}

// requestGloasBeaconBlock produces one Gloas block from a single beacon node. It POSTs builderConfig as the
// beacon-APIs#630 body (a neutral local-build config when nil), and retries as the legacy GET only on a
// 404/405, from a node that predates the POST. Each request is recorded through record.
func requestGloasBeaconBlock(ctx context.Context, addr string, slot phase0.Slot, graffiti, randao []byte, builderConfig *gloas.ProduceBuilderConfig, record requestRecorder) (*gloas.ProducedBlock, error) {
	if builderConfig == nil {
		builderConfig = gloas.NeutralProduceBuilderConfig()
	}
	// Graffiti must be a full 32-byte value in the query — lighthouse rejects a short one with 400
	// "Invalid query string" (mirror the mature GetBeaconBlock path which pads to [32]byte).
	g := [32]byte{}
	copy(g[:], graffiti)
	url := addr + fmt.Sprintf(gloasProduceBlockPath, slot, "0x"+hex.EncodeToString(randao), "0x"+hex.EncodeToString(g[:]))

	start := time.Now()
	res, err := requestGloasBeaconBlockPOST(ctx, url, builderConfig)
	record(http.MethodPost, time.Since(start), err)
	if err == nil {
		return res, nil
	}
	if !isMethodOrPathMissing(err) {
		return nil, err
	}
	// The legacy GET honors only builder_boost_factor (min_bid and the per-builder inputs are POST-only),
	// with the same semantics: bids weighed against the local payload at 100.
	url += fmt.Sprintf("&builder_boost_factor=%d", builderConfig.BuilderBoostFactor)

	start = time.Now()
	res, err = requestGloasBeaconBlockGET(ctx, url)
	record(http.MethodGet, time.Since(start), err)
	return res, err
}

// requestGloasBeaconBlockPOST sends the builder config as the produceBlockV4 JSON body and decodes the SSZ
// response.
func requestGloasBeaconBlockPOST(ctx context.Context, url string, builderConfig *gloas.ProduceBuilderConfig) (*gloas.ProducedBlock, error) {
	jsonBody, err := json.Marshal(builderConfig)
	if err != nil {
		return nil, fmt.Errorf("marshal builder config: %w", err)
	}
	respBody, header, err := gloasHTTPDo(ctx, http.MethodPost, url, jsonBody, "application/octet-stream", "application/json", nil)
	if err != nil {
		return nil, err
	}
	return decodeGloasProduceResponse(respBody, header)
}

// requestGloasBeaconBlockGET sends the legacy produceBlockV4 GET and decodes the SSZ response.
func requestGloasBeaconBlockGET(ctx context.Context, url string) (*gloas.ProducedBlock, error) {
	respBody, header, err := gloasHTTPDo(ctx, http.MethodGet, url, nil, "application/octet-stream", "", nil)
	if err != nil {
		return nil, err
	}
	return decodeGloasProduceResponse(respBody, header)
}

// decodeGloasProduceResponse decodes a produceBlockV4 SSZ response: BlockContents when the
// Eth-Execution-Payload-Included header says the beacon node self-built and included the payload, else a
// bare BeaconBlock. The winning builder's Eth-Builder-Url is read from the response header either way.
func decodeGloasProduceResponse(respBody []byte, header http.Header) (*gloas.ProducedBlock, error) {
	if err := checkGloasConsensusVersion(header); err != nil {
		return nil, err
	}
	produced := &gloas.ProducedBlock{BuilderURL: header.Get("Eth-Builder-Url")}
	included := header.Get(executionPayloadIncludedHeader)
	if !strings.EqualFold(included, "true") {
		block, err := decodeGloasBlock(respBody)
		if err != nil {
			return nil, fmt.Errorf("produce response with %s=%q: %w", executionPayloadIncludedHeader, included, err)
		}
		produced.Block = block
		return produced, nil
	}

	contents := &gloas.BlockContents{}
	if err := contents.UnmarshalSSZ(respBody); err != nil {
		return nil, fmt.Errorf("produce response with %s=%q: decode gloas block contents: %w", executionPayloadIncludedHeader, included, err)
	}
	if contents.Block == nil || contents.ExecutionPayloadEnvelope == nil {
		return nil, errors.New("gloas block contents without a block or envelope")
	}
	produced.Block = contents.Block
	produced.Envelope = &gloas.ProducedEnvelope{
		Envelope:  contents.ExecutionPayloadEnvelope,
		KZGProofs: contents.KZGProofs,
		Blobs:     contents.Blobs,
	}
	return produced, nil
}

// checkGloasConsensusVersion guards against a beacon node returning a wrong-fork block: it fails when the
// produce response's Eth-Consensus-Version is present but not "gloas". An absent header is tolerated (not
// every node sets it on the response), with the SSZ decode as the backstop.
func checkGloasConsensusVersion(header http.Header) error {
	if v := header.Get(consensusVersionHeader); v != "" && !strings.EqualFold(v, consensusVersionGloas) {
		return fmt.Errorf("produce response Eth-Consensus-Version %q, want %q", v, consensusVersionGloas)
	}
	return nil
}

// decodeGloasBlock unmarshals an SSZ produce response into a Gloas block.
func decodeGloasBlock(ssz []byte) (*gloas.BeaconBlock, error) {
	block := &gloas.BeaconBlock{}
	if err := block.UnmarshalSSZ(ssz); err != nil {
		return nil, fmt.Errorf("decode gloas beacon block: %w", err)
	}
	return block, nil
}

// submitGloasBeaconBlock POSTs an SSZ-marshaled signed Gloas block to the publish endpoint, echoing any
// Eth-Builder-Url in extraHeaders. An already-known answer counts as success (see gloasPublishSSZ): every
// operator submits the decided block for liveness redundancy, so a non-leader's submit legitimately races
// the canonical one.
func submitGloasBeaconBlock(ctx context.Context, addr string, blockSSZ []byte, extraHeaders map[string]string) error {
	return gloasPublishSSZ(ctx, addr+gloasPublishBlockPath, blockSSZ, extraHeaders)
}

// isMethodOrPathMissing reports whether err is a 404 or 405: the beacon node lacks the endpoint or method,
// so produce falls back from the POST to the legacy GET.
func isMethodOrPathMissing(err error) bool {
	status := responseStatusCode(err)
	return status == http.StatusNotFound || status == http.StatusMethodNotAllowed
}
