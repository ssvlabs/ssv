package goclient

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/attestantio/go-eth2-client/spec/phase0"

	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
)

// Gloas produce/publish endpoints (beacon-APIs#580, merged 2026-06-29). Produce is v4 with
// include_payload=false: a Gloas block carries only the execution-payload bid (the payload ships in the
// §6 envelope), so the response is a bare BeaconBlock — no BlockContents. Publish is the standard v2
// blocks endpoint (version-tagged via Eth-Consensus-Version).
const (
	gloasProduceBlockPath = "/eth/v4/validator/blocks/%d?randao_reveal=%s&graffiti=%s&include_payload=false" // slot, randao 0x-hex, graffiti 0x-hex
	gloasPublishBlockPath = "/eth/v2/beacon/blocks"
)

// GetGloasBeaconBlock produces a Gloas (ePBS) block via the v4 produce endpoint as SSZ — go-eth2-client
// has no Gloas types. The response is a bare BeaconBlock (see the include_payload=false path).
func (gc *GoClient) GetGloasBeaconBlock(ctx context.Context, slot phase0.Slot, graffiti, randao []byte) (*gloas.BeaconBlock, error) {
	return firstClientResult(ctx, gc, "GetGloasBeaconBlock", http.MethodGet, func(ctx context.Context, addr string) (*gloas.BeaconBlock, error) {
		return requestGloasBeaconBlock(ctx, addr, slot, graffiti, randao)
	})
}

// SubmitGloasBeaconBlock publishes a signed Gloas (ePBS) block as SSZ to all configured beacon
// nodes concurrently, succeeding if at least one accepts it. Re-publishing a signed block to
// multiple BNs is safe — they dedupe by block root.
func (gc *GoClient) SubmitGloasBeaconBlock(ctx context.Context, block *gloas.SignedBeaconBlock) error {
	body, err := block.MarshalSSZ()
	if err != nil {
		return fmt.Errorf("marshal signed gloas block: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, gc.commonTimeout)
	defer cancel()

	return gc.multiClientSubmit(ctx, "SubmitGloasBeaconBlock", func(ctx context.Context, client Client) error {
		return submitGloasBeaconBlock(ctx, gc.clientAddresses[client], body)
	})
}

// requestGloasBeaconBlock GETs the produce endpoint and decodes the SSZ response into a Gloas block.
func requestGloasBeaconBlock(ctx context.Context, addr string, slot phase0.Slot, graffiti, randao []byte) (*gloas.BeaconBlock, error) {
	// Graffiti must be a full 32-byte value in the query — lighthouse rejects a short one with
	// 400 "Invalid query string" (mirror the mature GetBeaconBlock path which pads to [32]byte).
	g := [32]byte{}
	copy(g[:], graffiti)
	url := addr + fmt.Sprintf(gloasProduceBlockPath, slot, "0x"+hex.EncodeToString(randao), "0x"+hex.EncodeToString(g[:]))
	body, err := gloasOctetStreamHTTP(ctx, http.MethodGet, url, nil, nil)
	if err != nil {
		return nil, err
	}
	block := &gloas.BeaconBlock{}
	if err := block.UnmarshalSSZ(body); err != nil {
		return nil, fmt.Errorf("decode gloas beacon block: %w", err)
	}
	return block, nil
}

// submitGloasBeaconBlock POSTs an SSZ-marshaled signed Gloas block to the publish endpoint. A response
// signaling the block is already known is treated as success: every operator submits the decided block
// for liveness redundancy, so a non-leader's submit legitimately races the canonical one, and some beacon
// nodes (e.g. Lodestar) report that duplicate as an error rather than deduping silently.
func submitGloasBeaconBlock(ctx context.Context, addr string, blockSSZ []byte) error {
	_, err := gloasOctetStreamHTTP(ctx, http.MethodPost, addr+gloasPublishBlockPath, blockSSZ, nil)
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
	var httpErr *gloasHTTPError
	if !errors.As(err, &httpErr) {
		return false
	}
	body := strings.ToLower(httpErr.body)
	return strings.Contains(body, "already known") || strings.Contains(body, "already_known")
}

// gloasOctetStreamHTTP issues an octet-stream (SSZ) request to a Gloas produce/publish endpoint and returns
// the response body on a 2xx. A nil body GETs; a non-nil body POSTs SSZ tagged with the Gloas
// consensus version. extraHeaders (e.g. Eth-Execution-Payload-Blinded for the §6 envelope) are applied last.
func gloasOctetStreamHTTP(ctx context.Context, method, url string, body []byte, extraHeaders map[string]string) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Accept", "application/octet-stream")
	if body != nil {
		req.Header.Set("Content-Type", "application/octet-stream")
		req.Header.Set("Eth-Consensus-Version", consensusVersionGloas)
	}
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	resp, err := ptcHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", method, url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &gloasHTTPError{method: method, url: url, statusCode: resp.StatusCode, body: strings.TrimSpace(string(respBody))}
	}
	return respBody, nil
}

// gloasHTTPError is the error gloasOctetStreamHTTP returns for a non-2xx response. It exposes the status
// code and body so callers can special-case specific beacon-node responses (see isAlreadyKnown).
type gloasHTTPError struct {
	method, url string
	statusCode  int
	body        string
}

func (e *gloasHTTPError) Error() string {
	return fmt.Sprintf("%s %s: status %d: %s", e.method, e.url, e.statusCode, e.body)
}
