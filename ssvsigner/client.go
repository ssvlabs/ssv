package ssvsigner

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/carlmjohnson/requests"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging/fields"

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
	err = requests.
		URL(c.baseURL).
		Client(c.httpClient).
		Path(pathValidators).
		ToJSON(&listResp).
		Fetch(ctx)

	return listResp, err
}

func (c *Client) AddValidators(ctx context.Context, shares ...ShareKeys) (err error) {
	start := time.Now()
	defer func() {
		duration := time.Since(start)
		recordClientRequest(ctx, opAddValidator, err, duration)
		c.logger.Debug("requested to add keys to remote signer", fields.Count(len(shares)), zap.Duration("duration", duration), zap.Error(err))
	}()

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
		Path(pathValidators).
		BodyJSON(req).
		Post().
		ToJSON(&resp).
		AddValidator(requests.ValidatorHandler(requests.DefaultValidator, requests.ToString(&errStr))).
		Fetch(ctx)

	if requests.HasStatusErr(err, http.StatusUnprocessableEntity) {
		return ShareDecryptionError(errors.New(errStr))
	}

	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}

	if len(resp.Data) != len(shares) {
		return fmt.Errorf("unexpected statuses length, got %d, expected %d", len(resp.Data), len(shares))
	}

	for _, data := range resp.Data {
		if data.Status != web3signer.StatusImported {
			return fmt.Errorf("unexpected status %s", data.Status)
		}
	}

	return nil
}

func (c *Client) RemoveValidators(ctx context.Context, pubKeys ...phase0.BLSPubKey) (err error) {
	start := time.Now()
	defer func() {
		duration := time.Since(start)
		recordClientRequest(ctx, opRemoveValidator, err, duration)
		c.logger.Debug("requested to remove keys from remote signer", fields.Count(len(pubKeys)), zap.Duration("duration", duration), zap.Error(err))
	}()
	req := web3signer.DeleteKeystoreRequest{
		Pubkeys: pubKeys,
	}

	var resp web3signer.DeleteKeystoreResponse
	err = requests.
		URL(c.baseURL).
		Client(c.httpClient).
		Path(pathValidators).
		BodyJSON(req).
		Delete().
		ToJSON(&resp).
		Fetch(ctx)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}

	if len(resp.Data) != len(pubKeys) {
		return fmt.Errorf("unexpected statuses length, got %d, expected %d", len(resp.Data), len(pubKeys))
	}

	for _, data := range resp.Data {
		if data.Status != web3signer.StatusDeleted {
			return fmt.Errorf("received status %s", data.Status)
		}
	}

	return nil
}

func (c *Client) Sign(ctx context.Context, sharePubKey phase0.BLSPubKey, payload web3signer.SignRequest) (signature phase0.BLSSignature, err error) {
	var resp web3signer.SignResponse
	start := time.Now()
	defer func() {
		duration := time.Since(start)
		recordClientRequest(ctx, opSignValidator, err, duration)
		c.logger.Debug("requested to sign with share key", fields.PubKey(sharePubKey[:]), zap.Duration("duration", duration), zap.Error(err))
	}()
	err = requests.
		URL(c.baseURL).
		Client(c.httpClient).
		Path(pathValidatorsSign + sharePubKey.String()).
		BodyJSON(payload).
		Post().
		ToJSON(&resp).
		Fetch(ctx)
	if err != nil {
		return phase0.BLSSignature{}, fmt.Errorf("request failed: %w", err)
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
	err = requests.
		URL(c.baseURL).
		Client(c.httpClient).
		Path(pathOperatorIdentity).
		ToString(&resp).
		Fetch(ctx)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}

	return resp, nil
}

func (c *Client) OperatorSign(ctx context.Context, payload []byte) (signature []byte, err error) {
	var respBuf bytes.Buffer
	start := time.Now()
	defer func() {
		duration := time.Since(start)
		recordClientRequest(ctx, opSignOperator, err, duration)
		c.logger.Debug("requested to sign with operator key", zap.Duration("duration", duration), zap.Error(err))
	}()
	err = requests.
		URL(c.baseURL).
		Client(c.httpClient).
		Path(pathOperatorSign).
		BodyBytes(payload).
		Post().
		ToBytesBuffer(&respBuf).
		Fetch(ctx)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	return respBuf.Bytes(), nil
}

func (c *Client) MissingKeys(ctx context.Context, localKeys []phase0.BLSPubKey) ([]phase0.BLSPubKey, error) {
	remoteKeys, err := c.ListValidators(ctx)
	if err != nil {
		return nil, fmt.Errorf("get remote keys: %w", err)
	}

	remoteKeysSet := make(map[phase0.BLSPubKey]struct{}, len(remoteKeys))
	for _, remoteKey := range remoteKeys {
		remoteKeysSet[remoteKey] = struct{}{}
	}

	var missing []phase0.BLSPubKey
	for _, key := range localKeys {
		if _, ok := remoteKeysSet[key]; !ok {
			missing = append(missing, key)
		}
	}

	return missing, nil
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
