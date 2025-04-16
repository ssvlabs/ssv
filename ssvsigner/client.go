package ssvsigner

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
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

// ClientTLSConfigOptions contains options for configuring TLS.
type ClientTLSConfigOptions struct {
	// CACert is the certificate authority certificate.
	CACert []byte
	// ClientCert is the client certificate.
	ClientCert []byte
	// ClientKey is the client private key.
	ClientKey []byte
	// MinVersion is the minimum TLS version.
	MinVersion uint16
	// InsecureSkipVerify skips certificate verification (not recommended for production).
	InsecureSkipVerify bool
}

type Client struct {
	logger     *zap.Logger
	baseURL    string
	httpClient *http.Client

	clientCert []byte
	clientKey  []byte
	caCert     []byte
}

// ClientOption defines a function that configures a Client.
type ClientOption func(*Client)

// WithLogger sets a custom logger for the client.
func WithLogger(logger *zap.Logger) ClientOption {
	return func(client *Client) {
		client.logger = logger
	}
}

// WithClientCert sets the bytes of the client TLS certificate.
func WithClientCert(cert []byte) ClientOption {
	return func(client *Client) {
		client.clientCert = cert
	}
}

// WithClientKey sets the bytes of the client TLS key.
func WithClientKey(key []byte) ClientOption {
	return func(client *Client) {
		client.clientKey = key
	}
}

// WithCACert sets the bytes of the certificate authority TLS certificate.
func WithCACert(cert []byte) ClientOption {
	return func(client *Client) {
		client.caCert = cert
	}
}

// WithClientInsecureSkipVerify configures the client to skip TLS certificate verification (not recommended for production)
func WithClientInsecureSkipVerify() ClientOption {
	return func(client *Client) {
		client.httpClient.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		}
	}
}

func NewClient(baseURL string, opts ...ClientOption) (*Client, error) {
	baseURL = strings.TrimRight(baseURL, "/")

	c := &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger: zap.NewNop(),
	}

	for _, opt := range opts {
		opt(c)
	}

	// set up client connection with TLS if certificates are provided
	if len(c.clientCert) > 0 || len(c.clientKey) > 0 || len(c.caCert) > 0 {
		tlsConfig, err := createClientTLSConfig(ClientTLSConfigOptions{
			ClientCert: c.clientCert,
			ClientKey:  c.clientKey,
			CACert:     c.caCert,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create TLS config: %w", err)
		}

		c.httpClient.Transport = &http.Transport{
			TLSClientConfig: tlsConfig,
		}
	}

	return c, nil
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

// createClientTLSConfig creates a TLS configuration for clients.
func createClientTLSConfig(opts ClientTLSConfigOptions) (*tls.Config, error) {
	tlsConfig := &tls.Config{
		MinVersion:         tls.VersionTLS13,
		InsecureSkipVerify: opts.InsecureSkipVerify,
	}

	if opts.MinVersion != 0 {
		tlsConfig.MinVersion = opts.MinVersion
	}

	if len(opts.ClientCert) > 0 && len(opts.ClientKey) > 0 {
		cert, err := tls.X509KeyPair(opts.ClientCert, opts.ClientKey)
		if err != nil {
			return nil, fmt.Errorf("failed to load client certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	if len(opts.CACert) > 0 {
		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(opts.CACert) {
			return nil, fmt.Errorf("failed to append CA certificate to pool")
		}
		tlsConfig.RootCAs = caCertPool
	}

	return tlsConfig, nil
}
