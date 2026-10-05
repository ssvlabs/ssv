package goclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// consensusVersionHeader is the beacon-APIs consensus-version header; consensusVersionGloas is its
// value on Gloas requests.
const (
	consensusVersionHeader = "Eth-Consensus-Version"
	consensusVersionGloas  = "gloas"
)

// gloasHTTPClient issues the hand-rolled Gloas requests; per-call deadlines come from the request context.
// Like the main eth2clienthttp path, it applies basic-auth from the (unmasked) beacon address and uses no
// custom TLS or client certificate. Interim: retired once these requests move onto the fork's typed calls
// (issue #3014).
var gloasHTTPClient = &http.Client{}

// requestRecorder records one HTTP request a route made to a beacon node.
type requestRecorder func(httpMethod string, took time.Duration, err error)

// firstClientResult runs fn against each beacon client in turn, each under its own common-timeout
// budget, and returns the first success. When every client fails it returns their joined errors; with no
// clients it fails outright. Each attempt is recorded as one request under httpMethod.
func firstClientResult[T any](ctx context.Context, gc *GoClient, routeName, httpMethod string, fn func(ctx context.Context, addr string) (T, error)) (T, error) {
	return firstClientResultWithRecorder(ctx, gc, routeName, func(ctx context.Context, addr string, record requestRecorder) (T, error) {
		start := time.Now()
		res, err := fn(ctx, addr)
		record(httpMethod, time.Since(start), err)
		return res, err
	})
}

// firstClientResultWithRecorder is firstClientResult for a route that can make more than one request to a
// beacon node, such as a POST with a GET fallback: fn records each request it makes through record.
func firstClientResultWithRecorder[T any](ctx context.Context, gc *GoClient, routeName string, fn func(ctx context.Context, addr string, record requestRecorder) (T, error)) (T, error) {
	var zero T
	if len(gc.clients) == 0 {
		// Without this, the loop below would return the zero result with a nil error, read as a success.
		return zero, errMultiClient(errors.New("no clients available"), routeName)
	}
	var errs error
	for _, client := range gc.clients {
		// Per-client timeout so a hung primary doesn't starve the fallbacks.
		clientCtx, cancel := context.WithTimeout(ctx, gc.commonTimeout)
		record := func(httpMethod string, took time.Duration, err error) {
			recordRequest(clientCtx, gc.log, routeName, client, httpMethod, false, took, err)
		}
		res, err := fn(clientCtx, gc.clientAddresses[client], record)
		cancel()
		if err != nil {
			errs = errors.Join(errs, errSingleClient(err, client.Address(), routeName))
			continue
		}
		return res, nil
	}
	return zero, errs
}

// httpStatusError is a non-2xx response to a hand-rolled Gloas request. It keeps the status (read through
// responseStatusCode) and the body (read by isAlreadyKnown) so callers can classify the failure.
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
// a 204. Shared core of jsonDo and gloasHTTPDo.
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

// gloasHTTPDo is httpDo on gloasHTTPClient, returning the response body and headers on a 2xx. A request
// with a body always carries the Gloas Eth-Consensus-Version, whatever extraHeaders say.
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

// gloasPublishSSZ POSTs an SSZ body to a Gloas publish endpoint, returning nil on a 2xx or when the beacon
// node already has the object (see isAlreadyKnown); extraHeaders are applied as in gloasHTTPDo. It accepts
// JSON: a publish route answers a 2xx with no content and errors as JSON, so a beacon node that enforces
// Accept (Prysm) refuses an SSZ-only one with 406.
func gloasPublishSSZ(ctx context.Context, url string, body []byte, extraHeaders map[string]string) error {
	_, _, err := gloasHTTPDo(ctx, http.MethodPost, url, body, "application/json", "application/octet-stream", extraHeaders)
	if isAlreadyKnown(err) {
		return nil
	}
	return err
}

// isAlreadyKnown reports whether err is a beacon node refusing a published object it already has. Most
// beacon nodes answer a repeat with a 2xx, and beacon-APIs has no standard code for those that don't, so
// match on the message: Lodestar's 500 "BLOCK_ERROR_ALREADY_KNOWN" (before v1.47) and
// "EXECUTION_PAYLOAD_ENVELOPE_ERROR_ALREADY_KNOWN" (before v1.46), and Lighthouse's "duplicate block" when
// --http-duplicate-block-status is not a 2xx.
func isAlreadyKnown(err error) bool {
	var httpErr *httpStatusError
	if !errors.As(err, &httpErr) {
		return false
	}
	body := strings.ToLower(httpErr.body)
	return strings.Contains(body, "already known") || strings.Contains(body, "already_known") ||
		strings.Contains(body, "duplicate block")
}
