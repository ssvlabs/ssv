package web3signer

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"strings"
	"time"

	v1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/carlmjohnson/requests"
)

const DefaultRequestTimeout = 10 * time.Second

const (
	pathPublicKeys = "/api/v1/eth2/publicKeys"
	pathKeystores  = "/eth/v1/keystores"
	pathSign       = "/api/v1/eth2/sign/"
)

type Web3Signer struct {
	baseURL    string
	httpClient *http.Client
}

// New creates a new Web3Signer client with the given base URL and optional configuration.
func New(baseURL string, opts ...Option) *Web3Signer {
	baseURL = strings.TrimRight(baseURL, "/")

	w3s := &Web3Signer{
		baseURL: baseURL,
		httpClient: &http.Client{
			Transport: http.DefaultTransport,
			Timeout:   DefaultRequestTimeout,
		},
	}

	for _, opt := range opts {
		opt(w3s)
	}

	return w3s
}

type ListKeysResponse []phase0.BLSPubKey

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

type ImportKeystoreRequest struct {
	Keystores          []string `json:"keystores"`
	Passwords          []string `json:"passwords"`
	SlashingProtection string   `json:"slashing_protection,omitempty"`
}

type ImportKeystoreResponse struct {
	Data    []KeyManagerResponseData `json:"data,omitempty"`
	Message string                   `json:"message,omitempty"`
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

type DeleteKeystoreRequest struct {
	Pubkeys []phase0.BLSPubKey `json:"pubkeys"`
}

type DeleteKeystoreResponse struct {
	Data               []KeyManagerResponseData `json:"data,omitempty"`
	SlashingProtection string                   `json:"slashing_protection,omitempty"`
	Message            string                   `json:"message,omitempty"`
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

type SignRequest struct {
	ForkInfo                    ForkInfo                          `json:"fork_info"`
	SigningRoot                 phase0.Root                       `json:"signing_root,omitempty"`
	Type                        SignedObjectType                  `json:"type"`
	Attestation                 *phase0.AttestationData           `json:"attestation,omitempty"`
	BeaconBlock                 *BeaconBlockData                  `json:"beacon_block,omitempty"`
	VoluntaryExit               *phase0.VoluntaryExit             `json:"voluntary_exit,omitempty"`
	AggregateAndProof           *AggregateAndProof                `json:"aggregate_and_proof,omitempty"`
	AggregationSlot             *AggregationSlot                  `json:"aggregation_slot,omitempty"`
	RandaoReveal                *RandaoReveal                     `json:"randao_reveal,omitempty"`
	SyncCommitteeMessage        *SyncCommitteeMessage             `json:"sync_committee_message,omitempty"`
	SyncAggregatorSelectionData *SyncCommitteeAggregatorSelection `json:"sync_aggregator_selection_data,omitempty"`
	ContributionAndProof        *altair.ContributionAndProof      `json:"contribution_and_proof,omitempty"`
	ValidatorRegistration       *v1.ValidatorRegistration         `json:"validator_registration,omitempty"`
}

type SignResponse struct {
	Signature phase0.BLSSignature `json:"signature"`
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

// applyTLSConfig clones the existing transport and applies the TLS configuration to the HTTP client.
func (w3s *Web3Signer) applyTLSConfig(tlsConfig *tls.Config) {
	var transport *http.Transport
	if t, ok := w3s.httpClient.Transport.(*http.Transport); ok {
		transport = t.Clone()
	} else {
		transport = http.DefaultTransport.(*http.Transport).Clone()
	}

	transport.TLSClientConfig = tlsConfig
	w3s.httpClient.Transport = transport
}
