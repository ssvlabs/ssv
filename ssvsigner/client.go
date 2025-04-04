package ssvsigner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/carlmjohnson/requests"
	"github.com/ssvlabs/ssv/logging/fields"
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

func (c *Client) ListValidators(ctx context.Context) ([]phase0.BLSPubKey, error) {
	var resp web3signer.ListKeysResponse
	start := time.Now() // TODO: add metrics, consider removing logs with request duration
	err := requests.
		URL(c.baseURL).
		Client(c.httpClient).
		Path(pathValidators).
		ToJSON(&resp).
		Fetch(ctx)
	c.logger.Debug("requested to list keys in remote signer", fields.Duration(start))

	return resp, err
}

func (c *Client) AddValidators(ctx context.Context, shares ...ShareKeys) error {

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
	start := time.Now()
	err := requests.
		URL(c.baseURL).
		Client(c.httpClient).
		Path(pathValidators).
		BodyJSON(req).
		Post().
		ToJSON(&resp).
		AddValidator(requests.ValidatorHandler(requests.DefaultValidator, requests.ToString(&errStr))).
		Fetch(ctx)
	c.logger.Debug("requested to add keys to remote signer", fields.Count(len(shares)), fields.Duration(start))

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

func (c *Client) RemoveValidators(ctx context.Context, pubKeys ...phase0.BLSPubKey) error {
	req := web3signer.DeleteKeystoreRequest{
		Pubkeys: pubKeys,
	}

	var resp web3signer.DeleteKeystoreResponse
	start := time.Now()
	err := requests.
		URL(c.baseURL).
		Client(c.httpClient).
		Path(pathValidators).
		BodyJSON(req).
		Delete().
		ToJSON(&resp).
		Fetch(ctx)
	c.logger.Debug("requested to remove keys from remote signer", fields.Count(len(pubKeys)), fields.Duration(start))

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

func (c *Client) Sign(ctx context.Context, sharePubKey phase0.BLSPubKey, payload web3signer.SignRequest) (phase0.BLSSignature, error) {
	var resp web3signer.SignResponse
	start := time.Now()
	err := requests.
		URL(c.baseURL).
		Client(c.httpClient).
		Path(pathValidatorsSign + sharePubKey.String()).
		BodyJSON(payload).
		Post().
		ToJSON(&resp).
		Fetch(ctx)
	c.logger.Debug("requested to sign with share key", fields.PubKey(sharePubKey[:]), fields.Duration(start))
	if err != nil {
		return phase0.BLSSignature{}, fmt.Errorf("request failed: %w", err)
	}

	return resp.Signature, nil
}

func (c *Client) OperatorIdentity(ctx context.Context) (string, error) {
	var resp string
	start := time.Now()
	err := requests.
		URL(c.baseURL).
		Client(c.httpClient).
		Path(pathOperatorIdentity).
		ToString(&resp).
		Fetch(ctx)
	c.logger.Debug("requested operator identity", fields.Duration(start))

	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}

	return resp, nil
}

func (c *Client) OperatorSign(ctx context.Context, payload []byte) ([]byte, error) {
	var resp bytes.Buffer
	start := time.Now()
	err := requests.
		URL(c.baseURL).
		Client(c.httpClient).
		Path(pathOperatorSign).
		BodyBytes(payload).
		Post().
		ToBytesBuffer(&resp).
		Fetch(ctx)
	c.logger.Debug("requested to sign with operator key", fields.Duration(start))

	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	return resp.Bytes(), nil
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
