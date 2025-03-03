package ssvsignerclient

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/ssvsigner/server"
	"github.com/ssvlabs/ssv/ssvsigner/web3signer"
)

type Status = server.Status

const (
	StatusImported   Status = "imported"
	StatusDuplicated Status = "duplicate"
	StatusDeleted    Status = "deleted"
	StatusNotActive  Status = "not_active"
	StatusNotFound   Status = "not_found"
	StatusError      Status = "error"
)

type ShareDecryptionError error

type SSVSignerClient struct {
	logger     *zap.Logger
	baseURL    string
	httpClient *http.Client
}

func New(baseURL string, opts ...Option) *SSVSignerClient {
	baseURL = strings.TrimRight(baseURL, "/")

	c := &SSVSignerClient{
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

type Option func(*SSVSignerClient)

func WithLogger(logger *zap.Logger) Option {
	return func(client *SSVSignerClient) {
		client.logger = logger
	}
}

type ShareKeys struct {
	EncryptedPrivKey []byte
	PublicKey        []byte
}

func (c *SSVSignerClient) AddValidators(shares ...ShareKeys) ([]Status, error) {
	encodedShares := make([]server.ShareKeys, 0, len(shares))
	for _, share := range shares {
		encodedShares = append(encodedShares, server.ShareKeys{
			EncryptedPrivKey: hex.EncodeToString(share.EncryptedPrivKey),
			PublicKey:        hex.EncodeToString(share.PublicKey),
		})
	}

	req := server.AddValidatorRequest(encodedShares)

	reqBytes, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/v1/validators/add", c.baseURL)
	httpResp, err := c.httpClient.Post(url, "application/json", bytes.NewReader(reqBytes))
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
		if httpResp.StatusCode == http.StatusUnauthorized {
			return nil, ShareDecryptionError(errors.New(string(respBytes)))
		}
		return nil, fmt.Errorf("unexpected status code %d: %s", httpResp.StatusCode, string(respBytes))
	}

	var resp server.AddValidatorResponse
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response body: %w", err)
	}

	if len(resp) != len(shares) {
		return nil, fmt.Errorf("unexpected statuses length, got %d, expected %d", len(resp), len(shares))
	}

	return resp, nil
}

func (c *SSVSignerClient) RemoveValidators(sharePubKeys ...[]byte) ([]Status, error) {
	pubKeyStrs := make([]string, 0, len(sharePubKeys))
	for _, pubKey := range sharePubKeys {
		pubKeyStrs = append(pubKeyStrs, hex.EncodeToString(pubKey))
	}

	req := server.RemoveValidatorRequest{
		PublicKeys: pubKeyStrs,
	}

	reqBytes, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/v1/validators/remove", c.baseURL)
	httpResp, err := c.httpClient.Post(url, "application/json", bytes.NewReader(reqBytes))
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

	var resp server.RemoveValidatorResponse
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response body: %w", err)
	}

	if len(resp.Statuses) != len(sharePubKeys) {
		return nil, fmt.Errorf("unexpected statuses length, got %d, expected %d", len(resp.Statuses), len(sharePubKeys))
	}

	return resp.Statuses, nil
}

func (c *SSVSignerClient) Sign(sharePubKey []byte, payload web3signer.SignRequest) ([]byte, error) {
	url := fmt.Sprintf("%s/v1/validators/sign/%s", c.baseURL, hex.EncodeToString(sharePubKey))

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	resp, err := c.httpClient.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			c.logger.Error("failed to close http response body", zap.Error(err))
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	return body, nil
}

func (c *SSVSignerClient) GetOperatorIdentity() (string, error) {
	url := fmt.Sprintf("%s/v1/operator/identity", c.baseURL)

	resp, err := c.httpClient.Get(url)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			c.logger.Error("failed to close http response body", zap.Error(err))
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	return string(body), nil
}

func (c *SSVSignerClient) OperatorSign(payload []byte) ([]byte, error) {
	url := fmt.Sprintf("%s/v1/operator/sign", c.baseURL)

	resp, err := c.httpClient.Post(url, "application/json", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			c.logger.Error("failed to close http response body", zap.Error(err))
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	return body, nil
}
