package ssvsigner

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/carlmjohnson/requests"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/ssvsigner/web3signer"
)

type Client struct {
	logger     *zap.Logger
	baseURL    string
	httpClient *http.Client
}

func NewClient(baseURL string, opts ...ClientOption) *Client {
	baseURL = strings.TrimRight(baseURL, "/")

	c := &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

type ClientOption func(*Client)

func WithLogger(logger *zap.Logger) ClientOption {
	return func(client *Client) {
		client.logger = logger
	}
}

func (c *Client) ListValidators(ctx context.Context) ([]string, error) {
	var resp ListValidatorsResponse
	err := requests.
		URL(c.baseURL).
		Client(c.httpClient).
		Path("/v1/validators").
		ToJSON(&resp).
		Fetch(ctx)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	return resp, nil
}

func (c *Client) AddValidators(ctx context.Context, shares ...ClientShareKeys) ([]web3signer.Status, error) {
	encodedShares := make([]ServerShareKeys, 0, len(shares))
	for _, share := range shares {
		encodedShares = append(encodedShares, ServerShareKeys{
			EncryptedPrivKey: hex.EncodeToString(share.EncryptedPrivKey),
			PublicKey:        hex.EncodeToString(share.PublicKey),
		})
	}

	req := AddValidatorRequest{
		ShareKeys: encodedShares,
	}

	var resp AddValidatorResponse
	var errStr string
	err := requests.
		URL(c.baseURL).
		Client(c.httpClient).
		Path("/v1/validators").
		BodyJSON(req).
		Post().
		ToJSON(&resp).
		AddValidator(requests.ValidatorHandler(requests.DefaultValidator, requests.ToString(&errStr))).
		Fetch(ctx)

	if requests.HasStatusErr(err, http.StatusUnprocessableEntity) {
		return nil, ShareDecryptionError(errors.New(errStr))
	}

	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if len(resp.Statuses) != len(shares) {
		return nil, fmt.Errorf("unexpected statuses length, got %d, expected %d", len(resp.Statuses), len(shares))
	}

	return resp.Statuses, nil
}

func (c *Client) RemoveValidators(ctx context.Context, sharePubKeys ...[]byte) ([]web3signer.Status, error) {
	pubKeyStrs := make([]string, 0, len(sharePubKeys))
	for _, pubKey := range sharePubKeys {
		pubKeyStrs = append(pubKeyStrs, hex.EncodeToString(pubKey))
	}

	req := RemoveValidatorRequest{
		PublicKeys: pubKeyStrs,
	}

	var resp RemoveValidatorResponse
	err := requests.
		URL(c.baseURL).
		Client(c.httpClient).
		Path("/v1/validators").
		BodyJSON(req).
		Delete().
		ToJSON(&resp).
		Fetch(ctx)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if len(resp.Statuses) != len(sharePubKeys) {
		return nil, fmt.Errorf("unexpected statuses length, got %d, expected %d", len(resp.Statuses), len(sharePubKeys))
	}

	return resp.Statuses, nil
}

func (c *Client) Sign(ctx context.Context, sharePubKey []byte, payload web3signer.SignRequest) ([]byte, error) {
	var resp bytes.Buffer
	err := requests.
		URL(c.baseURL).
		Client(c.httpClient).
		Pathf("/v1/validators/sign/%s", hex.EncodeToString(sharePubKey)).
		BodyJSON(payload).
		Post().
		ToBytesBuffer(&resp).
		Fetch(ctx)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	return resp.Bytes(), nil
}

func (c *Client) OperatorIdentity(ctx context.Context) (string, error) {
	var resp string
	err := requests.
		URL(c.baseURL).
		Client(c.httpClient).
		Path("/v1/operator/identity").
		ToString(&resp).
		Fetch(ctx)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}

	return resp, nil
}

func (c *Client) OperatorSign(ctx context.Context, payload []byte) ([]byte, error) {
	var resp bytes.Buffer
	err := requests.
		URL(c.baseURL).
		Client(c.httpClient).
		Path("/v1/operator/sign").
		BodyBytes(payload).
		Post().
		ToBytesBuffer(&resp).
		Fetch(ctx)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	return resp.Bytes(), nil
}

func (c *Client) MissingKeys(ctx context.Context, localKeys []string) ([]string, error) {
	remoteKeys, err := c.ListValidators(ctx)
	if err != nil {
		return nil, fmt.Errorf("get remote keys: %w", err)
	}

	remoteKeysSet := make(map[string]struct{}, len(remoteKeys))
	for _, remoteKey := range remoteKeys {
		remoteKeysSet[remoteKey] = struct{}{}
	}

	var missing []string
	for _, key := range localKeys {
		if _, ok := remoteKeysSet[key]; !ok {
			missing = append(missing, key)
		}
	}

	return missing, nil
}
