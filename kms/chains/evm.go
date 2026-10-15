package chains

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strconv"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

// EVM serves every EVM chain (CAIP-2 namespace eip155; the reference is the chain ID). Transactions
// are EIP-1559 and signed with the key's secp256k1 ECDSA share, so one key has the same address on
// every EVM chain.
type EVM struct{}

func init() { Register(EVM{}) }

func (EVM) Namespace() string { return "eip155" }
func (EVM) Scheme() string    { return "ecdsa-secp256k1-cggmp21" }

func (EVM) ValidReference(ref string) bool {
	id, err := strconv.ParseUint(ref, 10, 64)
	return err == nil && id > 0 && strconv.FormatUint(id, 10) == ref
}

// EVMTx is the "tx" of an eip155 request: an EIP-1559 transaction. Amounts are decimal wei strings.
type EVMTx struct {
	ChainID              uint64          `json:"chain_id"`
	Nonce                uint64          `json:"nonce"`
	To                   *common.Address `json:"to"`
	Value                string          `json:"value"`
	Data                 hexutil.Bytes   `json:"data"`
	Gas                  uint64          `json:"gas"`
	MaxFeePerGas         string          `json:"max_fee_per_gas"`
	MaxPriorityFeePerGas string          `json:"max_priority_fee_per_gas"`
}

func (EVM) Parse(ref string, payload []byte) (Tx, error) {
	var t EVMTx
	if err := json.Unmarshal(payload, &t); err != nil {
		return nil, fmt.Errorf("invalid EVM transaction: %w", err)
	}
	chainID, _ := strconv.ParseUint(ref, 10, 64)
	if t.ChainID != chainID {
		return nil, fmt.Errorf("transaction chain_id %d does not match chain eip155:%s", t.ChainID, ref)
	}
	if t.To == nil {
		return nil, errors.New("contract creation is not supported (to is required)")
	}
	value, ok1 := new(big.Int).SetString(orZero(t.Value), 10)
	maxFee, ok2 := new(big.Int).SetString(orZero(t.MaxFeePerGas), 10)
	tip, ok3 := new(big.Int).SetString(orZero(t.MaxPriorityFeePerGas), 10)
	if !ok1 || !ok2 || !ok3 || value.Sign() < 0 || maxFee.Sign() < 0 || tip.Sign() < 0 || value.BitLen() > 256 {
		return nil, errors.New("invalid transaction amounts")
	}
	if t.Gas == 0 {
		return nil, errors.New("gas must be set")
	}
	tx := ethtypes.NewTx(&ethtypes.DynamicFeeTx{
		ChainID:   new(big.Int).SetUint64(t.ChainID),
		Nonce:     t.Nonce,
		GasTipCap: tip,
		GasFeeCap: maxFee,
		Gas:       t.Gas,
		To:        t.To,
		Value:     value,
		Data:      t.Data,
	})
	return &evmTx{tx: tx, signer: ethtypes.LatestSignerForChainID(tx.ChainId())}, nil
}

type evmTx struct {
	tx     *ethtypes.Transaction
	signer ethtypes.Signer
}

func (t *evmTx) Digest() []byte { return t.signer.Hash(t.tx).Bytes() }

func (t *evmTx) Policy() PolicyFields {
	return PolicyFields{Target: t.tx.To().Bytes(), Value: new(big.Int).Set(t.tx.Value()), Data: t.tx.Data()}
}

func (t *evmTx) Sequence() (uint64, bool) { return t.tx.Nonce(), true }

func (t *evmTx) Assemble(sig []byte) ([]byte, string, error) {
	signed, err := t.tx.WithSignature(t.signer, sig)
	if err != nil {
		return nil, "", err
	}
	raw, err := signed.MarshalBinary()
	if err != nil {
		return nil, "", err
	}
	return raw, signed.Hash().Hex(), nil
}

func (t *evmTx) Summary() string {
	return fmt.Sprintf("to %s value %s nonce %d data %d bytes", t.tx.To().Hex(), t.tx.Value(), t.tx.Nonce(), len(t.tx.Data()))
}

// orZero treats an empty amount as zero.
func orZero(s string) string {
	if s == "" {
		return "0"
	}
	return s
}
