package goclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// consensusVersionHeader is the beacon-APIs consensus-version header; consensusVersionGloas is its
// value on Gloas requests.
const (
	consensusVersionHeader = "Eth-Consensus-Version"
	consensusVersionGloas  = "gloas"
)

// gloasHTTPClient issues the hand-rolled Gloas requests; per-call deadlines come from the request context.
// Basic-auth in the (unmasked) beacon address is applied by net/http; custom TLS/client-cert is not — but
// the main eth2clienthttp path doesn't configure it either (system-CA https + basic-auth only), so no
// regression. Interim surface, retired once these requests move onto the fork's typed calls.
var gloasHTTPClient = &http.Client{}

// httpStatusError is a non-2xx response to a hand-rolled Gloas request. It keeps the status code
// so callers can tell a missing route (404 — a BN build without the endpoint) from a transient
// failure. The "METHOD URL: status N: body" message format is pinned by tests.
type httpStatusError struct {
	method string
	url    string
	status int
	body   string
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("%s %s: status %d: %s", e.method, e.url, e.status, e.body)
}

// httpDo issues a hand-rolled Gloas HTTP request and returns the response body, headers, and status
// code on a 2xx, or a *httpStatusError otherwise. accept sets the Accept header; a non-nil body is
// sent with contentType; extraHeaders are applied last. The status lets a 2xx caller tell a 200 from
// a 204. Shared core of the JSON (jsonDo) and SSZ (gloasHTTPDo) helpers.
func httpDo(ctx context.Context, httpClient *http.Client, method, url string, body []byte, accept, contentType string, extraHeaders map[string]string) ([]byte, http.Header, int, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Accept", accept)
	if body != nil && contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("%s %s: %w", method, url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, resp.StatusCode, fmt.Errorf("read response body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, nil, resp.StatusCode, &httpStatusError{method: method, url: url, status: resp.StatusCode, body: strings.TrimSpace(string(respBody))}
	}
	return respBody, resp.Header, resp.StatusCode, nil
}

// jsonDo issues a JSON request and, on a 2xx response, decodes the body into out (out may be nil to
// ignore the body). A nil body sends no request payload; extraHeaders are applied last. Non-2xx
// responses surface as *httpStatusError.
func jsonDo(ctx context.Context, httpClient *http.Client, method, url string, body []byte, extraHeaders map[string]string, out any) error {
	respBody, _, _, err := httpDo(ctx, httpClient, method, url, body, "application/json", "application/json", extraHeaders)
	if err != nil {
		return err
	}
	if out != nil {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

// gloasHTTPDo issues a request to a Gloas endpoint and returns the response body and headers on a 2xx (see
// httpDo). accept sets the Accept header; a non-nil body is sent with the given contentType; extraHeaders are
// applied last, except Eth-Consensus-Version, which is always the Gloas version on requests with a body.
func gloasHTTPDo(ctx context.Context, method, url string, body []byte, accept, contentType string, extraHeaders map[string]string) ([]byte, http.Header, error) {
	if body != nil {
		merged := make(map[string]string, len(extraHeaders)+1)
		for k, v := range extraHeaders {
			merged[k] = v
		}
		merged[consensusVersionHeader] = consensusVersionGloas
		extraHeaders = merged
	}
	respBody, header, _, err := httpDo(ctx, gloasHTTPClient, method, url, body, accept, contentType, extraHeaders)
	return respBody, header, err
}

// gloasPublishSSZ POSTs an SSZ body to a Gloas publish endpoint, returning nil on a 2xx; extraHeaders are
// applied as in gloasHTTPDo. It accepts JSON: a publish route answers a 2xx with no content and errors as
// JSON, so a beacon node that enforces Accept (Prysm) refuses an SSZ-only one with 406.
func gloasPublishSSZ(ctx context.Context, url string, body []byte, extraHeaders map[string]string) error {
	_, _, err := gloasHTTPDo(ctx, http.MethodPost, url, body, "application/json", "application/octet-stream", extraHeaders)
	return err
}
