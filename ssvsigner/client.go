package ssvsigner

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/carlmjohnson/requests"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/ssvsigner/web3signer"
)

const DefaultRequestTimeout = 10 * time.Second

type Client struct {
	logger     *zap.Logger
	baseURL    string
	httpClient *http.Client
}

// ClientOption is used to handle client options.
type ClientOption func(*Client)

// WithLogger sets a custom logger for the client.
func WithLogger(logger *zap.Logger) ClientOption {
	return func(c *Client) {
		c.logger = logger
	}
}

// WithRequestTimeout sets a custom timeout for HTTP requests.
func WithRequestTimeout(timeout time.Duration) ClientOption {
	return func(client *Client) {
		client.httpClient.Timeout = timeout
	}
}

// WithTLSConfig configures TLS for the client using a pre-configured TLS Config object.
//
// Parameters:
//   - tlsConfig: a pre-configured tls.Config object
//
// Returns a ClientOption that configures the client with the provided TLS config.
func WithTLSConfig(tlsConfig *tls.Config) ClientOption {
	return func(client *Client) {
		client.applyTLSConfig(tlsConfig)
	}
}

func NewClient(baseURL string, opts ...ClientOption) *Client {
	baseURL = strings.TrimRight(baseURL, "/")

	c := &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Transport: http.DefaultTransport,
			Timeout:   DefaultRequestTimeout,
		},
		logger: zap.NewNop(),
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

func (c *Client) ListValidators(ctx context.Context) (listResp []phase0.BLSPubKey, err error) {
	start := time.Now()
	defer func() {
		duration := time.Since(start)
		recordClientRequest(ctx, opListValidators, err, duration)
		c.logger.Debug("requested to list keys in remote signer", zap.Duration("duration", duration), zap.Error(err))
	}()
	var errBody string
	err = requests.
		URL(c.baseURL).
		Client(c.httpClient).
		Path(PathValidators).
		ToJSON(&listResp).
		AddValidator(errBodyValidator(&errBody)).
		Fetch(ctx)
	if err != nil {
		return nil, requestFailedErr(err, errBody)
	}

	return listResp, nil
}

func (c *Client) AddValidators(ctx context.Context, shares ...ShareKeys) (statuses []web3signer.Status, err error) {
	start := time.Now()
	defer func() {
		duration := time.Since(start)
		recordClientRequest(ctx, opAddValidator, err, duration)
		c.logger.Debug("requested to add keys to remote signer", zap.Int("count", len(shares)), zap.Duration("duration", duration), zap.Error(err))
	}()

	if len(shares) > addShareLimit {
		return nil, fmt.Errorf("too many shares, max allowed per request %d", addShareLimit)
	}

	encodedShares := make([]ShareKeys, 0, len(shares))
	for _, share := range shares {
		encodedShares = append(encodedShares, ShareKeys{
			EncryptedPrivKey: share.EncryptedPrivKey,
			PubKey:           share.PubKey,
		})
	}

	req := AddValidatorRequest{
		ShareKeys: encodedShares,
	}

	var resp web3signer.ImportKeystoreResponse
	var errBody string
	err = requests.
		URL(c.baseURL).
		Client(c.httpClient).
		Path(PathValidators).
		BodyJSON(req).
		Post().
		ToJSON(&resp).
		AddValidator(errBodyValidator(&errBody)).
		Fetch(ctx)

	if requests.HasStatusErr(err, http.StatusUnprocessableEntity) {
		// Wrap via requestFailedErr so the ResponseError chain survives (errors.Is /
		// requests.HasStatusErr keep working) and an empty-body 422 yields the status text
		// instead of an empty message.
		return nil, ShareDecryptionError(requestFailedErr(err, errBody))
	}

	if err != nil {
		return nil, requestFailedErr(err, errBody)
	}

	if len(resp.Data) != len(shares) {
		return nil, fmt.Errorf("unexpected statuses length, got %d, expected %d", len(resp.Data), len(shares))
	}

	statuses = make([]web3signer.Status, 0, len(resp.Data))
	for _, data := range resp.Data {
		statuses = append(statuses, data.Status)
	}

	return statuses, nil
}

func (c *Client) RemoveValidators(ctx context.Context, pubKeys ...phase0.BLSPubKey) (statuses []web3signer.Status, err error) {
	start := time.Now()
	defer func() {
		duration := time.Since(start)
		recordClientRequest(ctx, opRemoveValidator, err, duration)
		c.logger.Debug("requested to remove keys from remote signer", zap.Int("count", len(pubKeys)), zap.Duration("duration", duration), zap.Error(err))
	}()
	req := web3signer.DeleteKeystoreRequest{
		Pubkeys: pubKeys,
	}

	var resp web3signer.DeleteKeystoreResponse
	var errBody string
	err = requests.
		URL(c.baseURL).
		Client(c.httpClient).
		Path(PathValidators).
		BodyJSON(req).
		Delete().
		ToJSON(&resp).
		AddValidator(errBodyValidator(&errBody)).
		Fetch(ctx)
	if err != nil {
		return nil, requestFailedErr(err, errBody)
	}

	if len(resp.Data) != len(pubKeys) {
		return nil, fmt.Errorf("unexpected statuses length, got %d, expected %d", len(resp.Data), len(pubKeys))
	}

	statuses = make([]web3signer.Status, 0, len(resp.Data))
	for _, data := range resp.Data {
		statuses = append(statuses, data.Status)
	}

	return statuses, nil
}

func (c *Client) Sign(ctx context.Context, sharePubKey phase0.BLSPubKey, payload web3signer.SignRequest) (signature phase0.BLSSignature, err error) {
	var resp web3signer.SignResponse
	start := time.Now()
	defer func() {
		duration := time.Since(start)
		recordClientRequest(ctx, opSignValidator, err, duration)
		c.logger.Debug("requested to sign with share key", zap.Stringer("share_pubkey", sharePubKey), zap.Duration("duration", duration), zap.Error(err))
	}()
	var errBody string
	err = requests.
		URL(c.baseURL).
		Client(c.httpClient).
		Path(PathValidatorsSign + sharePubKey.String()).
		BodyJSON(payload).
		Post().
		ToJSON(&resp).
		AddValidator(errBodyValidator(&errBody)).
		Fetch(ctx)
	if err != nil {
		return phase0.BLSSignature{}, requestFailedErr(err, errBody)
	}

	return resp.Signature, nil
}

func (c *Client) OperatorIdentity(ctx context.Context) (pubKeyBase64 string, err error) {
	var resp string
	start := time.Now()
	defer func() {
		duration := time.Since(start)
		recordClientRequest(ctx, opOperatorIdentity, err, duration)
		c.logger.Debug("requested operator identity", zap.Duration("duration", duration), zap.Error(err))
	}()
	var errBody string
	err = requests.
		URL(c.baseURL).
		Client(c.httpClient).
		Path(PathOperatorIdentity).
		ToString(&resp).
		AddValidator(errBodyValidator(&errBody)).
		Fetch(ctx)
	if err != nil {
		return "", requestFailedErr(err, errBody)
	}

	return resp, nil
}

func (c *Client) OperatorSign(ctx context.Context, payload []byte) (signature []byte, err error) {
	var respBuf bytes.Buffer
	start := time.Now()
	defer func() {
		duration := time.Since(start)
		recordClientRequest(ctx, opOperatorSign, err, duration)
		c.logger.Debug("requested to sign with operator key", zap.Duration("duration", duration), zap.Error(err))
	}()
	var errBody string
	err = requests.
		URL(c.baseURL).
		Client(c.httpClient).
		Path(PathOperatorSign).
		BodyBytes(payload).
		Post().
		ToBytesBuffer(&respBuf).
		AddValidator(errBodyValidator(&errBody)).
		Fetch(ctx)
	if err != nil {
		return nil, requestFailedErr(err, errBody)
	}

	return respBuf.Bytes(), nil
}

func (c *Client) OperatorEncrypt(ctx context.Context, payload []byte) (encrypted []byte, err error) {
	var respBuf bytes.Buffer
	start := time.Now()
	defer func() {
		duration := time.Since(start)
		recordClientRequest(ctx, opOperatorEncrypt, err, duration)
		c.logger.Debug("requested operator encrypt", zap.Duration("duration", duration), zap.Error(err))
	}()
	var errBody string
	err = requests.
		URL(c.baseURL).
		Client(c.httpClient).
		Path(PathOperatorEncrypt).
		BodyBytes(payload).
		Post().
		ToBytesBuffer(&respBuf).
		AddValidator(errBodyValidator(&errBody)).
		Fetch(ctx)
	if err != nil {
		if requests.HasStatusErr(err, http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusNotImplemented) {
			return nil, fmt.Errorf("%w: %w", ErrOperatorDataProtectionUnsupported, err)
		}
		return nil, requestFailedErr(err, errBody)
	}

	return respBuf.Bytes(), nil
}

func (c *Client) OperatorDecrypt(ctx context.Context, payload []byte) (decrypted []byte, err error) {
	var respBuf bytes.Buffer
	start := time.Now()
	defer func() {
		duration := time.Since(start)
		recordClientRequest(ctx, opOperatorDecrypt, err, duration)
		c.logger.Debug("requested operator decrypt", zap.Duration("duration", duration), zap.Error(err))
	}()
	var errBody string
	err = requests.
		URL(c.baseURL).
		Client(c.httpClient).
		Path(PathOperatorDecrypt).
		BodyBytes(payload).
		Post().
		ToBytesBuffer(&respBuf).
		AddValidator(errBodyValidator(&errBody)).
		Fetch(ctx)
	if err != nil {
		if requests.HasStatusErr(err, http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusNotImplemented) {
			return nil, fmt.Errorf("%w: %w", ErrOperatorDataProtectionUnsupported, err)
		}
		return nil, requestFailedErr(err, errBody)
	}

	return respBuf.Bytes(), nil
}

// MissingKeys returns a list of public keys that are present in localKeys but not in the remote signer.
// It logs debug information about key counts to help diagnose performance issues with large key sets.
func (c *Client) MissingKeys(ctx context.Context, localKeys []phase0.BLSPubKey) ([]phase0.BLSPubKey, error) {
	remoteKeys, err := c.ListValidators(ctx)
	if err != nil {
		return nil, fmt.Errorf("get remote keys: %w", err)
	}

	remoteKeysSet := make(map[phase0.BLSPubKey]struct{}, len(remoteKeys))
	for _, remoteKey := range remoteKeys {
		remoteKeysSet[remoteKey] = struct{}{}
	}

	var missingKeys []phase0.BLSPubKey
	for _, key := range localKeys {
		if _, ok := remoteKeysSet[key]; !ok {
			missingKeys = append(missingKeys, key)
		}
	}

	c.logger.Debug("missing keys check completed",
		zap.Int("remote_count", len(remoteKeys)),
		zap.Int("local_count", len(localKeys)),
		zap.Int("missing_count", len(missingKeys)),
	)

	return missingKeys, nil
}

// maxErrBodyLen caps the error-body text attached to returned errors, so a misbehaving
// server can't bloat error messages and logs.
const maxErrBodyLen = 1024

// errBodyValidator runs requests.DefaultValidator and, on a rejected status, captures a
// bounded copy of the error body into *dst for requestFailedErr to attach, reading one byte
// past the cap so truncation stays detectable.
//
// It returns the validation error directly rather than wrapping it with
// requests.ValidatorHandler, which labels a successful body capture "handled recovery from
// invalid response". Capturing the body is the normal path here, so that phrase would be
// prefixed onto every error (and a capture-read failure would render as a multi-line join);
// returning the error directly avoids both and still satisfies requests.HasStatusErr.
func errBodyValidator(dst *string) requests.ResponseHandler {
	return func(resp *http.Response) error {
		err := requests.DefaultValidator(resp)
		if err == nil {
			return nil
		}
		if b, readErr := io.ReadAll(io.LimitReader(resp.Body, maxErrBodyLen+1)); readErr == nil {
			*dst = string(b)
		}
		return err
	}
}

// requestFailedErr wraps a failed request error, attaching the reason reported by the
// server (if any) so callers see it instead of just a status code.
func requestFailedErr(err error, errBody string) error {
	// Record whether the body was truncated from its raw length, before TrimSpace can
	// shrink a whitespace tail back under the cap and hide that content was dropped.
	truncated := len(errBody) > maxErrBodyLen
	errBody = strings.TrimSpace(errBody)
	if errBody == "" {
		return fmt.Errorf("request failed: %w", err)
	}

	// The server reports failures as {"message": ...} (web3signer.ErrorMessage), so a
	// non-empty message is the reason. A body that parses as JSON without one is the
	// zero-value success response an older server writes on error — null, {},
	// {"signature":"0x0…0"} (a zero SignResponse) — with nothing useful, so drop it and let
	// err's status speak. Keying on the message avoids enumerating those zero values, which
	// drift as the response structs change.
	var em web3signer.ErrorMessage
	if json.Unmarshal([]byte(errBody), &em) == nil {
		if msg := strings.TrimSpace(em.Message); msg != "" {
			return fmt.Errorf("request failed: %w: %s", err, msg)
		}
		return fmt.Errorf("request failed: %w", err)
	}

	// Not JSON we recognize: a plain-text error from an intermediary in front of the
	// signer, or a body truncated mid-JSON. Surface it raw, bounded.
	if len(errBody) > maxErrBodyLen {
		errBody = errBody[:maxErrBodyLen]
	}
	if truncated {
		errBody += "...(truncated)"
	}
	return fmt.Errorf("request failed: %w: %s", err, errBody)
}

// applyTLSConfig applies the given TLS configuration to the HTTP client.
// This method ensures that the HTTP client's transport is properly configured for TLS communication.
func (c *Client) applyTLSConfig(tlsConfig *tls.Config) {
	var transport *http.Transport
	if t, ok := c.httpClient.Transport.(*http.Transport); ok {
		transport = t.Clone()
	} else {
		transport = http.DefaultTransport.(*http.Transport).Clone()
	}

	transport.TLSClientConfig = tlsConfig
	c.httpClient.Transport = transport
}
