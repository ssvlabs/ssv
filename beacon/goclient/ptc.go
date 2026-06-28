package goclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"

	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
)

// Gloas (ePBS) Payload Timeliness Committee endpoints. go-eth2-client has no Gloas provider
// yet, so these are issued as hand-rolled HTTP requests until it is rebased onto a Gloas-aware
// release, at which point they become typed provider calls like the rest of GoClient.
const (
	ptcDutiesPath              = "/eth/v1/validator/duties/ptc/%d"               // epoch
	payloadAttestationDataPath = "/eth/v1/validator/payload_attestation_data/%d" // slot
	payloadAttestationsPath    = "/eth/v1/beacon/pool/payload_attestations"

	// consensusVersionGloas is the Eth-Consensus-Version header value for Gloas payload attestations.
	consensusVersionGloas = "gloas"
)

// ptcHTTPClient issues the hand-rolled PTC requests; per-call deadlines come from the request
// context. Basic-auth embedded in the (unmasked) beacon address is applied by net/http; custom
// TLS/client-cert transport is not — acceptable for this interim surface, to be retired with the
// go-eth2-client rebase.
var ptcHTTPClient = &http.Client{}

// PayloadAttestationDuties returns the PTC duties for the given validators at the epoch, from
// the first beacon client that responds.
func (gc *GoClient) PayloadAttestationDuties(ctx context.Context, epoch phase0.Epoch, validatorIndices []phase0.ValidatorIndex) ([]*gloas.PTCDuty, error) {
	return firstClientResult(ctx, gc, "PayloadAttestationDuties", http.MethodPost, func(ctx context.Context, addr string) ([]*gloas.PTCDuty, error) {
		return requestPTCDuties(ctx, ptcHTTPClient, addr, epoch, validatorIndices)
	})
}

// PayloadAttestationData returns the PayloadAttestationData to attest to for the slot, from the
// first beacon client that responds.
func (gc *GoClient) PayloadAttestationData(ctx context.Context, slot phase0.Slot) (*gloas.PayloadAttestationData, error) {
	return firstClientResult(ctx, gc, "PayloadAttestationData", http.MethodGet, func(ctx context.Context, addr string) (*gloas.PayloadAttestationData, error) {
		return requestPayloadAttestationData(ctx, ptcHTTPClient, addr, slot)
	})
}

// SubmitPayloadAttestationMessages broadcasts signed PTC messages to every beacon client's pool,
// succeeding if at least one accepts them.
func (gc *GoClient) SubmitPayloadAttestationMessages(ctx context.Context, messages []*gloas.PayloadAttestationMessage) error {
	ctx, cancel := context.WithTimeout(ctx, gc.commonTimeout)
	defer cancel()

	return gc.multiClientSubmit(ctx, "SubmitPayloadAttestationMessages", func(ctx context.Context, client Client) error {
		return submitPayloadAttestationMessages(ctx, ptcHTTPClient, gc.clientAddresses[client], messages)
	})
}

// firstClientResult runs fn against each beacon client in turn, each under its own common-timeout
// budget, returning the first success; on all failures it joins the per-client errors.
func firstClientResult[T any](ctx context.Context, gc *GoClient, routeName, httpMethod string, fn func(ctx context.Context, addr string) (T, error)) (T, error) {
	var zero T
	var errs error
	for _, client := range gc.clients {
		// Per-client timeout so a hung primary doesn't starve the fallbacks.
		clientCtx, cancel := context.WithTimeout(ctx, gc.commonTimeout)
		start := time.Now()
		res, err := fn(clientCtx, gc.clientAddresses[client])
		recordRequest(clientCtx, gc.log, routeName, client, httpMethod, false, time.Since(start), err)
		cancel()
		if err != nil {
			errs = errors.Join(errs, errSingleClient(err, client.Address(), routeName))
			continue
		}
		return res, nil
	}
	return zero, errs
}

// requestPTCDuties POSTs the validator indices and returns their PTC duties for the epoch.
func requestPTCDuties(ctx context.Context, httpClient *http.Client, addr string, epoch phase0.Epoch, validatorIndices []phase0.ValidatorIndex) ([]*gloas.PTCDuty, error) {
	indices := make([]string, len(validatorIndices))
	for i, idx := range validatorIndices {
		indices[i] = strconv.FormatUint(uint64(idx), 10)
	}
	body, err := json.Marshal(indices)
	if err != nil {
		return nil, fmt.Errorf("marshal validator indices: %w", err)
	}

	var resp struct {
		Data []*gloas.PTCDuty `json:"data"`
	}
	if err := ptcDo(ctx, httpClient, http.MethodPost, addr+fmt.Sprintf(ptcDutiesPath, epoch), body, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// requestPayloadAttestationData GETs the PayloadAttestationData the PTC member must attest to for the slot.
func requestPayloadAttestationData(ctx context.Context, httpClient *http.Client, addr string, slot phase0.Slot) (*gloas.PayloadAttestationData, error) {
	var resp struct {
		Data *gloas.PayloadAttestationData `json:"data"`
	}
	if err := ptcDo(ctx, httpClient, http.MethodGet, addr+fmt.Sprintf(payloadAttestationDataPath, slot), nil, nil, &resp); err != nil {
		return nil, err
	}
	if resp.Data == nil {
		return nil, errors.New("no payload attestation data in response")
	}
	return resp.Data, nil
}

// submitPayloadAttestationMessages POSTs signed PTC messages to the beacon node's pool.
func submitPayloadAttestationMessages(ctx context.Context, httpClient *http.Client, addr string, messages []*gloas.PayloadAttestationMessage) error {
	body, err := json.Marshal(messages)
	if err != nil {
		return fmt.Errorf("marshal payload attestation messages: %w", err)
	}
	headers := map[string]string{"Eth-Consensus-Version": consensusVersionGloas}
	return ptcDo(ctx, httpClient, http.MethodPost, addr+payloadAttestationsPath, body, headers, nil)
}

// ptcDo issues a JSON request and, on a 2xx response, decodes the body into out (out may be nil
// to ignore the body). A nil body sends no request payload; extraHeaders are applied last.
func ptcDo(ctx context.Context, httpClient *http.Client, method, url string, body []byte, extraHeaders map[string]string, out any) error {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%s %s: status %d: %s", method, url, resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	if out != nil {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}
