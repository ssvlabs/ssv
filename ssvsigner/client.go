package ssvsigner

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
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
	var errStr string
	err = requests.
		URL(c.baseURL).
		Client(c.httpClient).
		Path(PathValidators).
		ToJSON(&listResp).
		AddValidator(requests.ValidatorHandler(requests.DefaultValidator, toLimitedString(&errStr))).
		Fetch(ctx)
	if err != nil {
		return nil, requestFailedErr(err, errStr)
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
	var errStr string
	err = requests.
		URL(c.baseURL).
		Client(c.httpClient).
		Path(PathValidators).
		BodyJSON(req).
		Post().
		ToJSON(&resp).
		AddValidator(requests.ValidatorHandler(requests.DefaultValidator, toLimitedString(&errStr))).
		Fetch(ctx)

	if requests.HasStatusErr(err, http.StatusUnprocessableEntity) {
		return nil, ShareDecryptionError(errors.New(errStr))
	}

	if err != nil {
		return nil, requestFailedErr(err, errStr)
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
	var errStr string
	err = requests.
		URL(c.baseURL).
		Client(c.httpClient).
		Path(PathValidators).
		BodyJSON(req).
		Delete().
		ToJSON(&resp).
		AddValidator(requests.ValidatorHandler(requests.DefaultValidator, toLimitedString(&errStr))).
		Fetch(ctx)
	if err != nil {
		return nil, requestFailedErr(err, errStr)
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
	var errStr string
	err = requests.
		URL(c.baseURL).
		Client(c.httpClient).
		Path(PathValidatorsSign + sharePubKey.String()).
		BodyJSON(payload).
		Post().
		ToJSON(&resp).
		AddValidator(requests.ValidatorHandler(requests.DefaultValidator, toLimitedString(&errStr))).
		Fetch(ctx)
	if err != nil {
		return phase0.BLSSignature{}, requestFailedErr(err, errStr)
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
	var errStr string
	err = requests.
		URL(c.baseURL).
		Client(c.httpClient).
		Path(PathOperatorIdentity).
		ToString(&resp).
		AddValidator(requests.ValidatorHandler(requests.DefaultValidator, toLimitedString(&errStr))).
		Fetch(ctx)
	if err != nil {
		return "", requestFailedErr(err, errStr)
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
	var errStr string
	err = requests.
		URL(c.baseURL).
		Client(c.httpClient).
		Path(PathOperatorSign).
		BodyBytes(payload).
		Post().
		ToBytesBuffer(&respBuf).
		AddValidator(requests.ValidatorHandler(requests.DefaultValidator, toLimitedString(&errStr))).
		Fetch(ctx)
	if err != nil {
		return nil, requestFailedErr(err, errStr)
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
	var errStr string
	err = requests.
		URL(c.baseURL).
		Client(c.httpClient).
		Path(PathOperatorEncrypt).
		BodyBytes(payload).
		Post().
		ToBytesBuffer(&respBuf).
		AddValidator(requests.ValidatorHandler(requests.DefaultValidator, toLimitedString(&errStr))).
		Fetch(ctx)
	if err != nil {
		if requests.HasStatusErr(err, http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusNotImplemented) {
			return nil, fmt.Errorf("%w: %w", ErrOperatorDataProtectionUnsupported, err)
		}
		return nil, requestFailedErr(err, errStr)
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
	var errStr string
	err = requests.
		URL(c.baseURL).
		Client(c.httpClient).
		Path(PathOperatorDecrypt).
		BodyBytes(payload).
		Post().
		ToBytesBuffer(&respBuf).
		AddValidator(requests.ValidatorHandler(requests.DefaultValidator, toLimitedString(&errStr))).
		Fetch(ctx)
	if err != nil {
		if requests.HasStatusErr(err, http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusNotImplemented) {
			return nil, fmt.Errorf("%w: %w", ErrOperatorDataProtectionUnsupported, err)
		}
		return nil, requestFailedErr(err, errStr)
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

// maxErrBodyLen caps the error response body attached to returned errors, so a
// misbehaving server can't blow up error messages and log lines.
const maxErrBodyLen = 1024

// toLimitedString is like requests.ToString, but reads at most maxErrBodyLen+1
// bytes, so a misbehaving server can't force unbounded reads while the error
// response body is captured. requestFailedErr's truncation then trims anything
// past the cap.
func toLimitedString(dst *string) requests.ResponseHandler {
	return func(resp *http.Response) error {
		b, err := io.ReadAll(io.LimitReader(resp.Body, maxErrBodyLen+1))
		if err != nil {
			return err
		}
		*dst = string(b)
		return nil
	}
}

// requestFailedErr wraps a failed request error, attaching the error response body
// (if any) so callers can see the reason reported by the server instead of just a
// status code.
func requestFailedErr(err error, errBody string) error {
	errBody = strings.TrimSpace(errBody)
	// Older server versions write the zero-value response on errors, which carries
	// no information ("null" / "{}"), so don't attach it.
	if errBody == "" || errBody == "null" || errBody == "{}" {
		return fmt.Errorf("request failed: %w", err)
	}
	if len(errBody) > maxErrBodyLen {
		errBody = errBody[:maxErrBodyLen] + "...(truncated)"
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
