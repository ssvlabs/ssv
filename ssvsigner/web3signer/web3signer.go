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

type Web3Signer struct {
	baseURL    string
	httpClient *http.Client
}

func New(baseURL string) *Web3Signer {
	baseURL = strings.TrimRight(baseURL, "/")

	return &Web3Signer{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
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
