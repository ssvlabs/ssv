package web3signer

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
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
	PathPublicKeys = "/api/v1/eth2/publicKeys"
	PathKeystores  = "/eth/v1/keystores"
	PathSign       = "/api/v1/eth2/sign/"
	PathUpCheck    = "/upcheck"
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
	var errResp string
	err := requests.
		URL(w3s.baseURL).
		Client(w3s.httpClient).
		Path(PathPublicKeys).
		ToJSON(&resp).
		AddValidator(errTextValidator(requests.DefaultValidator, &errResp)).
		Fetch(ctx)
	return resp, w3s.handleWeb3SignerErr(err, errResp)
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
	var errResp string
	err := requests.
		URL(w3s.baseURL).
		Client(w3s.httpClient).
		Path(PathKeystores).
		BodyJSON(req).
		Post().
		ToJSON(&resp).
		AddValidator(errTextValidator(requests.DefaultValidator, &errResp)).
		Fetch(ctx)
	return resp, w3s.handleWeb3SignerErr(err, errResp)
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
	var errResp string
	err := requests.
		URL(w3s.baseURL).
		Client(w3s.httpClient).
		Path(PathKeystores).
		BodyJSON(req).
		Delete().
		ToJSON(&resp).
		AddValidator(errTextValidator(requests.DefaultValidator, &errResp)).
		Fetch(ctx)
	return resp, w3s.handleWeb3SignerErr(err, errResp)
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
	var errResp string
	err := requests.
		URL(w3s.baseURL).
		Client(w3s.httpClient).
		Path(PathSign + sharePubKey.String()).
		BodyJSON(req).
		Post().
		Accept("application/json").
		ToJSON(&resp).
		AddValidator(errTextValidator(requests.DefaultValidator, &errResp)).
		Fetch(ctx)
	return resp, w3s.handleWeb3SignerErr(err, errResp)
}

// maxErrTextLen caps the upstream error body captured into HTTPResponseError.ErrText, so a
// misbehaving Web3Signer can't force unbounded reads and allocations on failure.
//
// It stays well below the node's cap (client.go maxErrBodyLen, 1024): ErrText reaches the node
// twice-escaped — HTTPResponseError.Error() quotes it with %q, then writeJSONErr JSON-encodes
// that into the {"message":...} body — so a value near the node's cap would overflow the node's
// read budget and arrive truncated mid-JSON, unparseable, instead of as the reason. 256 leaves
// room for that expansion plus the envelope.
const maxErrTextLen = 256

// errTextValidator runs the given validator and, on a rejected response, captures a bounded copy
// of the upstream error body into *dst for handleWeb3SignerErr to attach as ErrText. It reads one
// byte past the cap and appends a truncation marker when the body is over-long, so a body sliced
// mid-JSON is visibly cut rather than passing as complete (the node's errBodyValidator reads the
// same extra byte but marks it later, in requestFailedErr).
//
// It returns the validator's error directly rather than wrapping it with
// requests.ValidatorHandler, which labels a successful body capture "handled recovery from
// invalid response" — a phrase that would otherwise travel into HTTPResponseError.Err, the
// signer's logs, and the error body returned to the node.
//
// Taking the validator as a parameter lets UpCheck run its stricter requests.CheckStatus(200)
// through the same capture: a status check added separately would short-circuit ahead of this
// handler (requests stops at the first failing validator) and leave ErrText empty on that
// endpoint alone.
func errTextValidator(validator requests.ResponseHandler, dst *string) requests.ResponseHandler {
	return func(resp *http.Response) error {
		err := validator(resp)
		if err == nil {
			return nil
		}
		if b, readErr := io.ReadAll(io.LimitReader(resp.Body, maxErrTextLen+1)); readErr == nil {
			text := string(b)
			if len(b) > maxErrTextLen {
				text = text[:maxErrTextLen] + "...(truncated)"
			}
			*dst = text
		}
		return err
	}
}

func (w3s *Web3Signer) handleWeb3SignerErr(err error, errResp string) error {
	if err == nil {
		return nil
	}

	if re := new(requests.ResponseError); errors.As(err, &re) {
		return HTTPResponseError{Err: err, Status: re.StatusCode, ErrText: errResp}
	}

	return HTTPResponseError{Err: err, Status: http.StatusInternalServerError, ErrText: errResp}
}

// UpCheck checks if Web3Signer is up and running
func (w3s *Web3Signer) UpCheck(ctx context.Context) error {
	var errResp string
	err := requests.
		URL(w3s.baseURL).
		Client(w3s.httpClient).
		Path(PathUpCheck).
		AddValidator(errTextValidator(requests.CheckStatus(http.StatusOK), &errResp)).
		Fetch(ctx)
	return w3s.handleWeb3SignerErr(err, errResp)
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
