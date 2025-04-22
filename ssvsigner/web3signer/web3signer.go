package web3signer

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/carlmjohnson/requests"
)

const DefaultRequestTimeout = 10 * time.Second

type Web3Signer struct {
	baseURL    string
	httpClient *http.Client
}

func New(baseURL string, opts ...Option) *Web3Signer {
	baseURL = strings.TrimRight(baseURL, "/")

	w3s := &Web3Signer{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: DefaultRequestTimeout,
		},
	}

	for _, opt := range opts {
		opt(w3s)
	}

	return w3s
}

type Option func(*Web3Signer)

func WithRequestTimeout(timeout time.Duration) Option {
	return func(w3s *Web3Signer) {
		w3s.httpClient.Timeout = timeout
	}
}

// ListKeys lists keys in Web3Signer using https://consensys.github.io/web3signer/web3signer-eth2.html#tag/Public-Key/operation/ETH2_LIST
func (w3s *Web3Signer) ListKeys(ctx context.Context) (ListKeysResponse, error) {
	var resp ListKeysResponse
	err := requests.
		URL(w3s.baseURL).
		Client(w3s.httpClient).
		Path(pathPublicKeys).
		ToJSON(&resp).
		Fetch(ctx)
	return resp, w3s.handleWeb3SignerErr(err)
}

// ImportKeystore adds a key to Web3Signer using https://consensys.github.io/web3signer/web3signer-eth2.html#tag/Keymanager/operation/KEYMANAGER_IMPORT
func (w3s *Web3Signer) ImportKeystore(ctx context.Context, req ImportKeystoreRequest) (ImportKeystoreResponse, error) {
	var resp ImportKeystoreResponse
	err := requests.
		URL(w3s.baseURL).
		Client(w3s.httpClient).
		Path(pathKeystores).
		BodyJSON(req).
		Post().
		ToJSON(&resp).
		Fetch(ctx)
	return resp, w3s.handleWeb3SignerErr(err)
}

// DeleteKeystore removes a key from Web3Signer using https://consensys.github.io/web3signer/web3signer-eth2.html#operation/KEYMANAGER_DELETE
func (w3s *Web3Signer) DeleteKeystore(ctx context.Context, req DeleteKeystoreRequest) (DeleteKeystoreResponse, error) {
	var resp DeleteKeystoreResponse
	err := requests.
		URL(w3s.baseURL).
		Client(w3s.httpClient).
		Path(pathKeystores).
		BodyJSON(req).
		Delete().
		ToJSON(&resp).
		Fetch(ctx)
	return resp, w3s.handleWeb3SignerErr(err)
}

// Sign signs using https://consensys.github.io/web3signer/web3signer-eth2.html#tag/Signing/operation/ETH2_SIGN
func (w3s *Web3Signer) Sign(ctx context.Context, sharePubKey phase0.BLSPubKey, req SignRequest) (SignResponse, error) {
	var resp SignResponse
	err := requests.
		URL(w3s.baseURL).
		Client(w3s.httpClient).
		Path(pathSign + sharePubKey.String()).
		BodyJSON(req).
		Post().
		Accept("application/json").
		ToJSON(&resp).
		Fetch(ctx)
	return resp, w3s.handleWeb3SignerErr(err)
}

func (w3s *Web3Signer) handleWeb3SignerErr(err error) error {
	if err == nil {
		return nil
	}

	if se := new(requests.ResponseError); errors.As(err, &se) {
		return HTTPResponseError{Err: err, Status: se.StatusCode}
	}

	return HTTPResponseError{Err: err, Status: http.StatusInternalServerError}
}
