package web3signer

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"
)

type Web3Signer struct {
	logger     *zap.Logger
	baseURL    string
	httpClient *http.Client
}

func New(logger *zap.Logger, baseURL string) (*Web3Signer, error) {
	baseURL = strings.TrimRight(baseURL, "/")

	return &Web3Signer{
		logger:  logger,
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}, nil
}

// ImportKeystore adds a key to Web3Signer using https://consensys.github.io/web3signer/web3signer-eth2.html#tag/Keymanager/operation/KEYMANAGER_IMPORT
func (c *Web3Signer) ImportKeystore(keystoreList, keystorePasswordList []string) ([]Status, error) {
	logger := c.logger.With(zap.String("request", "ImportKeystore"), zap.Int("count", len(keystoreList)))
	logger.Info("importing keystores")

	payload := ImportKeystoreRequest{
		Keystores:          keystoreList,
		Passwords:          keystorePasswordList,
		SlashingProtection: "", // TODO
	}

	body, err := json.Marshal(payload)
	if err != nil {
		logger.Error("failed to marshal payload", zap.Error(err))
		return nil, fmt.Errorf("marshal payload: %w", err)
	}

	url := fmt.Sprintf("%s/eth/v1/keystores", c.baseURL)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		logger.Error("failed to create http request", zap.Error(err))
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	httpResp, err := c.httpClient.Do(req)
	if err != nil {
		logger.Error("failed to send http request", zap.Error(err))
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer httpResp.Body.Close()

	respBytes, err := io.ReadAll(httpResp.Body)
	if err != nil {
		logger.Error("failed to read http response body", zap.Error(err))
		return nil, fmt.Errorf("read response body: %w", err)
	}
	var resp ImportKeystoreResponse
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		logger.Error("failed to unmarshal http response body", zap.String("body", string(respBytes)), zap.Error(err))
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		logger.Error("failed to import keystore",
			zap.Int("status_code", httpResp.StatusCode),
			zap.String("message", resp.Message))
		return nil, fmt.Errorf("unexpected status %d: %v", httpResp.StatusCode, resp.Message)
	}

	logger.Info("imported keystores", zap.Any("response", string(respBytes)))

	var statuses []Status
	for _, data := range resp.Data {
		statuses = append(statuses, data.Status)
	}

	return statuses, nil
}

// DeleteKeystore removes a key from Web3Signer using https://consensys.github.io/web3signer/web3signer-eth2.html#operation/KEYMANAGER_DELETE
func (c *Web3Signer) DeleteKeystore(sharePubKeyList []string) ([]Status, error) {
	logger := c.logger.With(zap.String("request", "DeleteKeystore"), zap.Int("count", len(sharePubKeyList)))
	logger.Info("deleting keystores")

	payload := DeleteKeystoreRequest{
		Pubkeys: sharePubKeyList,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		logger.Error("failed to marshal payload", zap.Error(err))
		return nil, fmt.Errorf("marshal payload: %w", err)
	}

	url := fmt.Sprintf("%s/eth/v1/keystores", c.baseURL)
	req, err := http.NewRequest(http.MethodDelete, url, bytes.NewReader(body))
	if err != nil {
		logger.Error("failed to create http request", zap.Error(err))
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpResp, err := c.httpClient.Do(req)
	if err != nil {
		logger.Error("failed to send http request", zap.Error(err))
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer httpResp.Body.Close()

	respBytes, err := io.ReadAll(httpResp.Body)
	if err != nil {
		logger.Error("failed to read http response body", zap.Error(err))
		return nil, fmt.Errorf("read response body: %w", err)
	}
	var resp DeleteKeystoreResponse
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		logger.Error("failed to unmarshal http response body", zap.String("body", string(respBytes)), zap.Error(err))
	}

	if httpResp.StatusCode != http.StatusOK {
		logger.Error("failed to delete keystore",
			zap.Int("status_code", httpResp.StatusCode),
			zap.String("message", resp.Message))
		return nil, fmt.Errorf("unexpected status %d: %v", httpResp.StatusCode, resp.Message)
	}

	logger.Info("deleted keystores", zap.Any("response", string(respBytes)))

	var statuses []Status
	for _, data := range resp.Data {
		statuses = append(statuses, data.Status)
	}

	return statuses, nil
}

// Sign signs using https://consensys.github.io/web3signer/web3signer-eth2.html#tag/Signing/operation/ETH2_SIGN
func (c *Web3Signer) Sign(sharePubKey []byte, payload SignRequest) ([]byte, error) {
	sharePubKeyHex := "0x" + hex.EncodeToString(sharePubKey)
	logger := c.logger.With(
		zap.String("request", "Sign"),
		zap.String("share_pubkey", sharePubKeyHex),
		zap.String("type", string(payload.Type)),
	)
	logger.Info("signing keystore")

	body, err := json.Marshal(payload)
	if err != nil {
		logger.Error("failed to marshal payload", zap.Error(err))
		return nil, fmt.Errorf("marshal payload: %w", err)
	}

	url := fmt.Sprintf("%s/api/v1/eth2/sign/%s", c.baseURL, sharePubKeyHex)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		logger.Error("failed to create http request", zap.Error(err))
		return nil, fmt.Errorf("create sign request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		logger.Error("failed to send http request", zap.Error(err))
		return nil, fmt.Errorf("sign request failed: %w", err)
	}
	defer resp.Body.Close()

	respData, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Error("failed to read http response body", zap.Error(err))
		return nil, fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		logger.Error("sign request failed",
			zap.Int("status_code", resp.StatusCode),
			zap.Any("response", string(respData)),
			zap.Any("request", string(body)),
			zap.Any("url", url))
		return nil, fmt.Errorf("unexpected status code %d", resp.StatusCode)
	}

	sigBytes, err := hex.DecodeString(strings.TrimPrefix(string(respData), "0x"))
	if err != nil {
		logger.Error("failed to decode signature", zap.String("signature", string(respData)), zap.Error(err))
		return nil, fmt.Errorf("decode signature: %w", err)
	}

	return sigBytes, nil
}
