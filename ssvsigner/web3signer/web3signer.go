package web3signer

import (
	"context"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/carlmjohnson/requests"
	"go.uber.org/zap"
)

type Web3Signer struct {
	logger     *zap.Logger
	baseURL    string
	httpClient *http.Client
}

func New(logger *zap.Logger, baseURL string) *Web3Signer {
	baseURL = strings.TrimRight(baseURL, "/")

	return &Web3Signer{
		logger:  logger,
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// ListKeys lists keys in Web3Signer using https://consensys.github.io/web3signer/web3signer-eth2.html#tag/Public-Key/operation/ETH2_LIST
func (c *Web3Signer) ListKeys(ctx context.Context) ([]phase0.BLSPubKey, error) {
	logger := c.logger.With(zap.String("request", "ListKeys"))
	logger.Info("listing keys")

	var resp []phase0.BLSPubKey
	err := requests.
		URL(c.baseURL).
		Client(c.httpClient).
		Path("/api/v1/eth2/publicKeys").
		ToJSON(&resp).
		Fetch(ctx)
	if err != nil {
		return nil, fmt.Errorf("web3signer: %w", err)
	}

	logger.Info("listed keys", zap.Int("count", len(resp)))

	return resp, nil
}

// ImportKeystore adds a key to Web3Signer using https://consensys.github.io/web3signer/web3signer-eth2.html#tag/Keymanager/operation/KEYMANAGER_IMPORT
func (c *Web3Signer) ImportKeystore(ctx context.Context, keystoreList []Keystore, keystorePasswordList []string) ([]Status, error) {
	logger := c.logger.With(
		zap.String("request", "ImportKeystore"),
		zap.Int("count", len(keystoreList)),
	)
	logger.Info("importing keystores")

	payload := ImportKeystoreRequest{
		Keystores:          keystoreList,
		Passwords:          keystorePasswordList,
		SlashingProtection: "", // TODO
	}

	var resp ImportKeystoreResponse
	err := requests.
		URL(c.baseURL).
		Client(c.httpClient).
		Path("/eth/v1/keystores").
		BodyJSON(payload).
		Post().
		ToJSON(&resp).
		Fetch(ctx)
	if err != nil {
		return nil, fmt.Errorf("web3signer: %w", err)
	}

	logger.Info("imported keystores")

	var statuses []Status
	for _, data := range resp.Data {
		statuses = append(statuses, data.Status)
	}

	return statuses, nil
}

// DeleteKeystore removes a key from Web3Signer using https://consensys.github.io/web3signer/web3signer-eth2.html#operation/KEYMANAGER_DELETE
func (c *Web3Signer) DeleteKeystore(ctx context.Context, sharePubKeyList []phase0.BLSPubKey) ([]Status, error) {
	logger := c.logger.With(
		zap.String("request", "DeleteKeystore"),
		zap.Int("count", len(sharePubKeyList)),
	)
	logger.Info("deleting keystores")

	payload := DeleteKeystoreRequest{
		Pubkeys: sharePubKeyList,
	}

	var resp DeleteKeystoreResponse
	err := requests.
		URL(c.baseURL).
		Client(c.httpClient).
		Path("/eth/v1/keystores").
		BodyJSON(payload).
		Delete().
		ToJSON(&resp).
		Fetch(ctx)
	if err != nil {
		return nil, fmt.Errorf("web3signer: %w", err)
	}

	logger.Info("deleted keystores")

	var statuses []Status
	for _, data := range resp.Data {
		statuses = append(statuses, data.Status)
	}

	return statuses, nil
}

// Sign signs using https://consensys.github.io/web3signer/web3signer-eth2.html#tag/Signing/operation/ETH2_SIGN
func (c *Web3Signer) Sign(ctx context.Context, sharePubKey phase0.BLSPubKey, payload SignRequest) (phase0.BLSSignature, error) {
	logger := c.logger.With(
		zap.String("request", "Sign"),
		zap.String("share_pubkey", sharePubKey.String()),
		zap.String("type", string(payload.Type)),
	)
	logger.Info("signing")

	var resp string
	err := requests.
		URL(c.baseURL).
		Client(c.httpClient).
		Pathf("/api/v1/eth2/sign/%s", sharePubKey.String()).
		BodyJSON(payload).
		Post().
		ToString(&resp).
		Fetch(ctx)
	if err != nil {
		return phase0.BLSSignature{}, fmt.Errorf("web3signer: %w", err)
	}

	sigBytes, err := hex.DecodeString(strings.TrimPrefix(resp, "0x"))
	if err != nil {
		logger.Error("failed to decode signature",
			zap.String("signature", resp),
			zap.Error(err),
		)
		return phase0.BLSSignature{}, fmt.Errorf("decode signature: %w", err)
	}

	if len(sigBytes) != len(phase0.BLSSignature{}) {
		logger.Error("unexpected signature length",
			zap.Int("length", len(sigBytes)),
			zap.Int("expected", len(phase0.BLSSignature{})),
		)
		return phase0.BLSSignature{}, fmt.Errorf("unexpected signature length %d, expected %d", len(sigBytes), len(phase0.BLSSignature{}))
	}

	return phase0.BLSSignature(sigBytes), nil
}
