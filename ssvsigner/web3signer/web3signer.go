package web3signer

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/carlmjohnson/requests"

	ssvsignertls "github.com/ssvlabs/ssv/ssvsigner/tls"
)

type Web3Signer struct {
	baseURL    string
	httpClient *http.Client
}

// Option defines a function that configures a Web3Signer client.
type Option func(*Web3Signer) error

// WithTLS is for setting the TLS.
func WithTLS(certPath, keyPath, caPath string) Option {
	return func(client *Web3Signer) error {
		return client.configureTLS(certPath, keyPath, caPath)
	}
}

// New creates a new Web3Signer client with the given base URL and optional configuration.
func New(baseURL string, opts ...Option) (*Web3Signer, error) {
	baseURL = strings.TrimRight(baseURL, "/")

	client := &Web3Signer{
		baseURL:    baseURL,
		httpClient: &http.Client{Transport: http.DefaultTransport, Timeout: 30 * time.Second},
	}

	for _, opt := range opts {
		if err := opt(client); err != nil {
			return nil, err
		}
	}

	return client, nil
}

// ListKeys lists keys in Web3Signer using https://consensys.github.io/web3signer/web3signer-eth2.html#tag/Public-Key/operation/ETH2_LIST
func (c *Web3Signer) ListKeys(ctx context.Context) (ListKeysResponse, error) {
	var resp ListKeysResponse
	err := requests.
		URL(c.baseURL).
		Client(c.httpClient).
		Path(pathPublicKeys).
		ToJSON(&resp).
		Fetch(ctx)
	return resp, c.handleWeb3SignerErr(err)
}

// ImportKeystore adds a key to Web3Signer using https://consensys.github.io/web3signer/web3signer-eth2.html#tag/Keymanager/operation/KEYMANAGER_IMPORT
func (c *Web3Signer) ImportKeystore(ctx context.Context, req ImportKeystoreRequest) (ImportKeystoreResponse, error) {
	var resp ImportKeystoreResponse
	err := requests.
		URL(c.baseURL).
		Client(c.httpClient).
		Path(pathKeystores).
		BodyJSON(req).
		Post().
		ToJSON(&resp).
		Fetch(ctx)
	return resp, c.handleWeb3SignerErr(err)
}

// DeleteKeystore removes a key from Web3Signer using https://consensys.github.io/web3signer/web3signer-eth2.html#operation/KEYMANAGER_DELETE
func (c *Web3Signer) DeleteKeystore(ctx context.Context, req DeleteKeystoreRequest) (DeleteKeystoreResponse, error) {
	var resp DeleteKeystoreResponse
	err := requests.
		URL(c.baseURL).
		Client(c.httpClient).
		Path(pathKeystores).
		BodyJSON(req).
		Delete().
		ToJSON(&resp).
		Fetch(ctx)
	return resp, c.handleWeb3SignerErr(err)
}

// Sign signs using https://consensys.github.io/web3signer/web3signer-eth2.html#tag/Signing/operation/ETH2_SIGN
func (c *Web3Signer) Sign(ctx context.Context, sharePubKey phase0.BLSPubKey, req SignRequest) (SignResponse, error) {
	var resp SignResponse
	err := requests.
		URL(c.baseURL).
		Client(c.httpClient).
		Path(pathSign + sharePubKey.String()).
		BodyJSON(req).
		Post().
		Accept("application/json").
		ToJSON(&resp).
		Fetch(ctx)
	return resp, c.handleWeb3SignerErr(err)
}

func (c *Web3Signer) handleWeb3SignerErr(err error) error {
	if err == nil {
		return nil
	}

	if se := new(requests.ResponseError); errors.As(err, &se) {
		return HTTPResponseError{Err: err, Status: se.StatusCode}
	}

	return HTTPResponseError{Err: err, Status: http.StatusInternalServerError}
}

// configureTLS configures TLS for the Web3Signer by setting up the HTTP client with certificates and CA paths.
func (c *Web3Signer) configureTLS(certPath, keyPath, caPath string) error {
	tc, err := ssvsignertls.LoadTLSConfig(certPath, keyPath, caPath, false)
	if err != nil {
		return fmt.Errorf("web3signer TLS: %w", err)
	}

	c.httpClient.Transport = &http.Transport{TLSClientConfig: tc}

	return nil
}
