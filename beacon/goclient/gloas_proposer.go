package goclient

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/attestantio/go-eth2-client/spec/phase0"

	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
)

// Gloas produce/publish endpoints (beacon-APIs#580, unmerged). produceBlockV4 reuses the v3 produce
// path with a Gloas SSZ response; publish is the standard v2 blocks endpoint. The exact path/headers
// may still shift upstream — these are best-effort and must be verified against a real Gloas devnet BN.
const (
	gloasProduceBlockPath = "/eth/v3/validator/blocks/%d?randao_reveal=%s&graffiti=%s" // slot, randao 0x-hex, graffiti 0x-hex
	gloasPublishBlockPath = "/eth/v2/beacon/blocks"
)

// GetGloasBeaconBlock produces a Gloas (ePBS) block via the produce endpoint as SSZ — go-eth2-client
// has no Gloas types. Only the bare block is handled; a payload-included BlockContents response
// (blobs/KZG) is deferred.
func (gc *GoClient) GetGloasBeaconBlock(ctx context.Context, slot phase0.Slot, graffiti, randao []byte) (*gloas.BeaconBlock, error) {
	return firstClientResult(ctx, gc, "GetGloasBeaconBlock", http.MethodGet, func(ctx context.Context, addr string) (*gloas.BeaconBlock, error) {
		return requestGloasBeaconBlock(ctx, addr, slot, graffiti, randao)
	})
}

// SubmitGloasBeaconBlock publishes a signed Gloas (ePBS) block as SSZ.
func (gc *GoClient) SubmitGloasBeaconBlock(ctx context.Context, block *gloas.SignedBeaconBlock) error {
	body, err := block.MarshalSSZ()
	if err != nil {
		return fmt.Errorf("marshal signed gloas block: %w", err)
	}
	_, err = firstClientResult(ctx, gc, "SubmitGloasBeaconBlock", http.MethodPost, func(ctx context.Context, addr string) (struct{}, error) {
		return struct{}{}, submitGloasBeaconBlock(ctx, addr, body)
	})
	return err
}

// requestGloasBeaconBlock GETs the produce endpoint and decodes the SSZ response into a Gloas block.
func requestGloasBeaconBlock(ctx context.Context, addr string, slot phase0.Slot, graffiti, randao []byte) (*gloas.BeaconBlock, error) {
	url := addr + fmt.Sprintf(gloasProduceBlockPath, slot, "0x"+hex.EncodeToString(randao), "0x"+hex.EncodeToString(graffiti))
	body, err := gloasOctetStreamHTTP(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	block := &gloas.BeaconBlock{}
	if err := block.UnmarshalSSZ(body); err != nil {
		return nil, fmt.Errorf("decode gloas beacon block: %w", err)
	}
	return block, nil
}

// submitGloasBeaconBlock POSTs an SSZ-marshaled signed Gloas block to the publish endpoint.
func submitGloasBeaconBlock(ctx context.Context, addr string, blockSSZ []byte) error {
	_, err := gloasOctetStreamHTTP(ctx, http.MethodPost, addr+gloasPublishBlockPath, blockSSZ)
	return err
}

// gloasOctetStreamHTTP issues an octet-stream (SSZ) request to a Gloas produce/publish endpoint and returns
// the response body on a 2xx. A nil body GETs; a non-nil body POSTs SSZ tagged with the Gloas
// consensus version.
func gloasOctetStreamHTTP(ctx context.Context, method, url string, body []byte) ([]byte, error) {
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
		return nil, fmt.Errorf("%s %s: status %d: %s", method, url, resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	return respBody, nil
}
