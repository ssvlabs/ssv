package ssvsigner

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

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
	url := fmt.Sprintf("%s/v1/validators", c.baseURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpResp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	defer func() {
		if err := httpResp.Body.Close(); err != nil {
			c.logger.Error("failed to close http response body", zap.Error(err))
		}
	}()

	respBytes, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code %d", httpResp.StatusCode)
	}

	var resp ListValidatorsResponse
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response body: %w", err)
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

	reqBytes, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/v1/validators", c.baseURL)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqBytes))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	defer func() {
		if err := httpResp.Body.Close(); err != nil {
			c.logger.Error("failed to close http response body", zap.Error(err))
		}
	}()

	respBytes, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		if httpResp.StatusCode == http.StatusUnprocessableEntity {
			return nil, ShareDecryptionError(errors.New(string(respBytes)))
		}
		return nil, fmt.Errorf("unexpected status code %d: %s", httpResp.StatusCode, string(respBytes))
	}

	var resp AddValidatorResponse
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response body: %w", err)
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

	reqBytes, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/v1/validators", c.baseURL)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, bytes.NewReader(reqBytes))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	defer func() {
		if err := httpResp.Body.Close(); err != nil {
			c.logger.Error("failed to close http response body", zap.Error(err))
		}
	}()

	respBytes, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code %d: %s", httpResp.StatusCode, string(respBytes))
	}

	var resp RemoveValidatorResponse
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response body: %w", err)
	}

	if len(resp.Statuses) != len(sharePubKeys) {
		return nil, fmt.Errorf("unexpected statuses length, got %d, expected %d", len(resp.Statuses), len(sharePubKeys))
	}

	return resp.Statuses, nil
}

func (c *Client) Sign(ctx context.Context, sharePubKey []byte, payload web3signer.SignRequest) ([]byte, error) {
	url := fmt.Sprintf("%s/v1/validators/sign/%s", c.baseURL, hex.EncodeToString(sharePubKey))

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	defer func() {
		if err := httpResp.Body.Close(); err != nil {
			c.logger.Error("failed to close http response body", zap.Error(err))
		}
	}()

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code %d: %s", httpResp.StatusCode, string(body))
	}

	return body, nil
}

func (c *Client) OperatorIdentity(ctx context.Context) (string, error) {
	url := fmt.Sprintf("%s/v1/operator/identity", c.baseURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	httpResp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}

	defer func() {
		if err := httpResp.Body.Close(); err != nil {
			c.logger.Error("failed to close http response body", zap.Error(err))
		}
	}()

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code %d: %s", httpResp.StatusCode, string(body))
	}

	return string(body), nil
}

func (c *Client) OperatorSign(ctx context.Context, payload []byte) ([]byte, error) {
	url := fmt.Sprintf("%s/v1/operator/sign", c.baseURL)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	defer func() {
		if err := httpResp.Body.Close(); err != nil {
			c.logger.Error("failed to close http response body", zap.Error(err))
		}
	}()

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code %d: %s", httpResp.StatusCode, string(body))
	}

	return body, nil
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
