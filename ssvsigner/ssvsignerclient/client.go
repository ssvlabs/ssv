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

	"github.com/ssvlabs/ssv/ssvsigner/remotesigner/server"
	"github.com/ssvlabs/ssv/ssvsigner/remotesigner/web3signer"
)

type Status = server.Status

const (
	StatusImported   Status = "imported"
	StatusDuplicated Status = "duplicate"
	StatusDeleted    Status = "deleted"
)

type ShareDecryptionError error

type SSVSignerClient struct {
	baseURL    string
	httpClient *http.Client
}

func New(baseURL string) *SSVSignerClient {
	baseURL = strings.TrimRight(baseURL, "/")

	return &SSVSignerClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *SSVSignerClient) AddValidators(encryptedPrivKeys ...[]byte) ([]Status, [][]byte, error) {
	privKeyStrs := make([]string, 0, len(encryptedPrivKeys))
	for _, privKey := range encryptedPrivKeys {
		privKeyStrs = append(privKeyStrs, hex.EncodeToString(privKey))
	}

	req := server.AddValidatorRequest{
		EncryptedSharePrivateKeys: privKeyStrs,
	}

	reqBytes, err := json.Marshal(req)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/v1/validators/add", c.baseURL)
	httpResp, err := c.httpClient.Post(url, "application/json", bytes.NewReader(reqBytes))
	if err != nil {
		return nil, nil, fmt.Errorf("request failed: %w", err)
	}
	defer httpResp.Body.Close()

	respBytes, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("read response body: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		if httpResp.StatusCode == http.StatusUnauthorized {
			return nil, nil, ShareDecryptionError(errors.New(string(respBytes)))
		}
		return nil, nil, fmt.Errorf("unexpected status code %d: %s", httpResp.StatusCode, string(respBytes))
	}

	var resp server.AddValidatorResponse
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return nil, nil, fmt.Errorf("unmarshal response body: %w", err)
	}

	if len(resp.Statuses) != len(encryptedPrivKeys) {
		return nil, nil, fmt.Errorf("unexpected statuses length, got %d, expected %d", len(resp.Statuses), len(encryptedPrivKeys))
	}

	if len(resp.PublicKeys) != len(encryptedPrivKeys) {
		return nil, nil, fmt.Errorf("unexpected public keys length, got %d, expected %d", len(resp.PublicKeys), len(encryptedPrivKeys))
	}

	publicKeys := make([][]byte, 0, len(resp.PublicKeys))
	for _, pkStr := range resp.PublicKeys {
		pk, err := hex.DecodeString(strings.TrimPrefix(pkStr, "0x"))
		if err != nil {
			return nil, nil, fmt.Errorf("decode public key: %w", err)
		}
		publicKeys = append(publicKeys, pk)
	}

	return resp.Statuses, publicKeys, nil
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
	defer httpResp.Body.Close()

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
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(respBytes))
	}

	signature, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	return signature, nil
}

func (c *SSVSignerClient) GetOperatorIdentity() (string, error) {
	url := fmt.Sprintf("%s/v1/operator/identity", c.baseURL)

	resp, err := c.httpClient.Get(url)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBytes, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(respBytes))
	}

	publicKey, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	return string(publicKey), nil
}

func (c *SSVSignerClient) OperatorSign(payload []byte) ([]byte, error) {
	url := fmt.Sprintf("%s/v1/operator/sign", c.baseURL)

	resp, err := c.httpClient.Post(url, "application/json", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(respBytes))
	}

	signature, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	return signature, nil
}
