package goclient

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/attestantio/go-eth2-client/spec/phase0"

	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
)

// Gloas produce/publish endpoints. Produce is v4 with include_payload=false: a Gloas block carries only
// the execution-payload bid (the payload ships in the §6 envelope), so the response is a bare BeaconBlock —
// no BlockContents. The direct-builder overlay (beacon-APIs#630) sends a BuilderConfig POST body; beacon
// nodes that predate it (beacon-APIs#580, GET-only) are handled by the per-node GET fallback. Publish is
// the standard v2 blocks endpoint (version-tagged via Eth-Consensus-Version).
const (
	gloasProduceBlockPath = "/eth/v4/validator/blocks/%d?randao_reveal=%s&graffiti=%s&include_payload=false" // slot, randao 0x-hex, graffiti 0x-hex
	gloasPublishBlockPath = "/eth/v2/beacon/blocks"
)

// gloasBlockResult is the produce result threaded through firstClientResult: the block plus the winning
// builder's Eth-Builder-Url (empty when self-built or won by a p2p bid).
type gloasBlockResult struct {
	block      *gloas.BeaconBlock
	builderURL string
}

// GetGloasBeaconBlock produces a Gloas (ePBS) block via the v4 produce endpoint, decoding the SSZ response
// (go-eth2-client has no Gloas types). A non-nil builderConfig is POSTed as the produceBlockV4 body
// (beacon-APIs#630, direct-builder overlay), falling back per beacon node to the GET for nodes that predate
// it; the returned string is the winning builder's Eth-Builder-Url, if any.
func (gc *GoClient) GetGloasBeaconBlock(ctx context.Context, slot phase0.Slot, graffiti, randao []byte, builderConfig *gloas.ProduceBuilderConfig) (*gloas.BeaconBlock, string, error) {
	// Telemetry labels the route by its primary method: POST when the cluster configured the overlay,
	// else GET. A per-node GET fallback (a beacon node predating beacon-APIs#630) is still counted under
	// POST — an accepted transitional inaccuracy, tracked with the POST-first flip on issue #2962.
	httpMethod := http.MethodGet
	if builderConfig != nil {
		httpMethod = http.MethodPost
	}
	res, err := firstClientResult(ctx, gc, "GetGloasBeaconBlock", httpMethod, func(ctx context.Context, addr string) (gloasBlockResult, error) {
		return requestGloasBeaconBlock(ctx, addr, slot, graffiti, randao, builderConfig)
	})
	return res.block, res.builderURL, err
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

// requestGloasBeaconBlock produces one Gloas block from a single beacon node. With a builderConfig it POSTs
// the beacon-APIs#630 body and, only on a 404/405 (the node predates the POST), retries as the GET carrying
// builder_boost_factor (the sole knob the pre-#630 GET also honors).
func requestGloasBeaconBlock(ctx context.Context, addr string, slot phase0.Slot, graffiti, randao []byte, builderConfig *gloas.ProduceBuilderConfig) (gloasBlockResult, error) {
	// Graffiti must be a full 32-byte value in the query — lighthouse rejects a short one with 400
	// "Invalid query string" (mirror the mature GetBeaconBlock path which pads to [32]byte).
	g := [32]byte{}
	copy(g[:], graffiti)
	url := addr + fmt.Sprintf(gloasProduceBlockPath, slot, "0x"+hex.EncodeToString(randao), "0x"+hex.EncodeToString(g[:]))

	if builderConfig != nil {
		res, err := requestGloasBeaconBlockPOST(ctx, url, builderConfig)
		if err == nil {
			return res, nil
		}
		if !isMethodOrPathMissing(err) {
			return gloasBlockResult{}, err
		}
		// The node predates the produceBlockV4 POST; fall back to the GET, still carrying
		// builder_boost_factor — the one knob the pre-#630 GET (beacon-APIs#580) also honors, same
		// semantics (builder bids weighed against the local payload at 100). min_bid and the per-builder
		// inputs have no GET counterpart, so they are POST-only.
		url += fmt.Sprintf("&builder_boost_factor=%d", builderConfig.BuilderBoostFactor)
	}

	respBody, header, err := gloasHTTPDo(ctx, http.MethodGet, url, nil, "", nil)
	if err != nil {
		return gloasBlockResult{}, err
	}
	if err := checkGloasConsensusVersion(header); err != nil {
		return gloasBlockResult{}, err
	}
	block, err := decodeGloasBlock(respBody)
	if err != nil {
		return gloasBlockResult{}, err
	}
	return gloasBlockResult{block: block}, nil
}

// requestGloasBeaconBlockPOST sends the builder config as the produceBlockV4 JSON body and decodes the SSZ
// block response, reading the winning builder's Eth-Builder-Url from the response header.
func requestGloasBeaconBlockPOST(ctx context.Context, url string, builderConfig *gloas.ProduceBuilderConfig) (gloasBlockResult, error) {
	jsonBody, err := json.Marshal(builderConfig)
	if err != nil {
		return gloasBlockResult{}, fmt.Errorf("marshal builder config: %w", err)
	}
	respBody, header, err := gloasHTTPDo(ctx, http.MethodPost, url, jsonBody, "application/json", nil)
	if err != nil {
		return gloasBlockResult{}, err
	}
	if err := checkGloasConsensusVersion(header); err != nil {
		return gloasBlockResult{}, err
	}
	block, err := decodeGloasBlock(respBody)
	if err != nil {
		return gloasBlockResult{}, err
	}
	return gloasBlockResult{block: block, builderURL: header.Get("Eth-Builder-Url")}, nil
}

// checkGloasConsensusVersion guards against a beacon node returning a wrong-fork block: it fails when the
// produce response's Eth-Consensus-Version is present but not "gloas". An absent header is tolerated (not
// every node sets it on the response), with the SSZ decode as the backstop.
func checkGloasConsensusVersion(header http.Header) error {
	if v := header.Get("Eth-Consensus-Version"); v != "" && !strings.EqualFold(v, consensusVersionGloas) {
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
// Eth-Builder-Url in extraHeaders. A response signaling the block is already known is treated as success:
// every operator submits the decided block for liveness redundancy, so a non-leader's submit legitimately
// races the canonical one, and some beacon nodes (e.g. Lodestar) report that duplicate as an error rather
// than deduping silently.
func submitGloasBeaconBlock(ctx context.Context, addr string, blockSSZ []byte, extraHeaders map[string]string) error {
	_, err := gloasOctetStreamHTTP(ctx, http.MethodPost, addr+gloasPublishBlockPath, blockSSZ, extraHeaders)
	if isAlreadyKnown(err) {
		return nil
	}
	return err
}

// isAlreadyKnown reports whether err is a beacon-node response signaling the submitted object is already
// known (i.e. already canonical) — for both the §4 block and the §6 envelope publish, where every operator
// redundantly submits the same object and the non-winning ones race the canonical one. Beacon-APIs has no
// standard code for this, so match on the message: Lodestar returns 500 "BLOCK_ERROR_ALREADY_KNOWN" and
// "EXECUTION_PAYLOAD_ENVELOPE_ERROR_ALREADY_KNOWN".
func isAlreadyKnown(err error) bool {
	var httpErr *httpStatusError
	if !errors.As(err, &httpErr) {
		return false
	}
	body := strings.ToLower(httpErr.body)
	return strings.Contains(body, "already known") || strings.Contains(body, "already_known")
}

// isMethodOrPathMissing reports whether err is a 404/405 — the beacon node does not implement the endpoint
// or method, the signal to fall back from the produceBlockV4 POST to the legacy GET.
func isMethodOrPathMissing(err error) bool {
	var httpErr *httpStatusError
	return errors.As(err, &httpErr) && (httpErr.status == http.StatusNotFound || httpErr.status == http.StatusMethodNotAllowed)
}

// gloasHTTPDo issues an SSZ-accepting request to a Gloas endpoint and returns the response body and headers
// on a 2xx (see httpDo). A non-nil body is sent with the given contentType; extraHeaders are applied last,
// except Eth-Consensus-Version, which is always the Gloas version on requests with a body.
func gloasHTTPDo(ctx context.Context, method, url string, body []byte, contentType string, extraHeaders map[string]string) ([]byte, http.Header, error) {
	if body != nil {
		merged := make(map[string]string, len(extraHeaders)+1)
		for k, v := range extraHeaders {
			merged[k] = v
		}
		merged["Eth-Consensus-Version"] = consensusVersionGloas
		extraHeaders = merged
	}
	return httpDo(ctx, gloasHTTPClient, method, url, body, "application/octet-stream", contentType, extraHeaders)
}

// gloasOctetStreamHTTP issues an octet-stream (SSZ) request to a Gloas produce/publish endpoint and returns
// the response body on a 2xx. A nil body GETs; a non-nil body POSTs SSZ tagged with the Gloas consensus
// version. extraHeaders (e.g. Eth-Builder-Url on publish, Eth-Execution-Payload-Blinded for the §6
// envelope) are applied last.
func gloasOctetStreamHTTP(ctx context.Context, method, url string, body []byte, extraHeaders map[string]string) ([]byte, error) {
	respBody, _, err := gloasHTTPDo(ctx, method, url, body, "application/octet-stream", extraHeaders)
	return respBody, err
}
