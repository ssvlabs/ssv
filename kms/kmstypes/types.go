// Package kmstypes holds the wire types of the friends-mode distributed KMS (dKMS). It is a leaf
// package so the message queue and message validation can decode dKMS messages without importing
// the service.
package kmstypes

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

// Kind is the kind of a dKMS (non-QBFT) message.
type Kind string

const (
	// KindRequest gossips a signing request so every operator of the key queues it.
	KindRequest Kind = "request"
	// KindPresignStart asks the operators of a signer set to run a presigning session.
	KindPresignStart Kind = "presign_start"
	// KindMPC carries one presigning round message; point-to-point ones are sealed to the recipient.
	KindMPC Kind = "mpc"
	// KindShare carries a signer's signature share for a decided request.
	KindShare Kind = "share"
	// KindHeartbeat tells the committee an operator is up, so leaders pick presignatures whose
	// signers are all online.
	KindHeartbeat Kind = "heartbeat"
)

// MaxMessageSize bounds an encoded dKMS message (presigning rounds carry ZK proofs).
const MaxMessageSize = 512 * 1024

// Message is a dKMS message carried in SSVMessage.Data with MsgType SSVKMSMsgType.
type Message struct {
	Kind    Kind         `json:"kind"`
	Request *SignRequest `json:"request,omitempty"`
	// Session identifies a presigning session.
	Session hexutil.Bytes `json:"session,omitempty"`
	Signers []uint64      `json:"signers,omitempty"`
	// To is the recipient operator of a point-to-point MPC message (0: all signers).
	To      uint64        `json:"to,omitempty"`
	Payload hexutil.Bytes `json:"payload,omitempty"`
	// Height and RequestID identify the decided request a signature share is for.
	Height    uint64        `json:"height,omitempty"`
	RequestID string        `json:"request_id,omitempty"`
	Share     hexutil.Bytes `json:"share,omitempty"`
	// Time (unix ms) makes otherwise identical messages (heartbeats) distinct: gossip drops
	// messages it has already seen.
	Time int64 `json:"time,omitempty"`
}

func (m *Message) Encode() ([]byte, error) { return json.Marshal(m) }

func (m *Message) Decode(data []byte) error {
	if len(data) > MaxMessageSize {
		return errors.New("dkms message too large")
	}
	return json.Unmarshal(data, m)
}

// KeyIDFromMessageID returns the key ID carried in a dKMS message ID's duty executor ID (the first
// 32 of its 48 bytes).
func KeyIDFromMessageID(id spectypes.MessageID) [32]byte {
	var k [32]byte
	copy(k[:], id.GetDutyExecutorID())
	return k
}

// NewMessageID builds the SSV message ID for a key: domain | RoleKMS | key ID (zero padded to 48).
func NewMessageID(domain spectypes.DomainType, keyID [32]byte, role spectypes.RunnerRole) spectypes.MessageID {
	var executor spectypes.ValidatorPK
	copy(executor[:], keyID[:])
	return spectypes.NewValidatorMsgID(domain, executor, role)
}

// EVMTx is an EIP-1559 transaction to sign. Amounts are decimal wei strings.
type EVMTx struct {
	ChainID              uint64         `json:"chain_id"`
	Nonce                uint64         `json:"nonce"`
	To                   common.Address `json:"to"`
	Value                string         `json:"value"`
	Data                 hexutil.Bytes  `json:"data"`
	Gas                  uint64         `json:"gas"`
	MaxFeePerGas         string         `json:"max_fee_per_gas"`
	MaxPriorityFeePerGas string         `json:"max_priority_fee_per_gas"`
}

// Transaction builds the unsigned transaction.
func (t *EVMTx) Transaction() (*ethtypes.Transaction, error) {
	value, ok1 := new(big.Int).SetString(orZero(t.Value), 10)
	maxFee, ok2 := new(big.Int).SetString(orZero(t.MaxFeePerGas), 10)
	tip, ok3 := new(big.Int).SetString(orZero(t.MaxPriorityFeePerGas), 10)
	if !ok1 || !ok2 || !ok3 || value.Sign() < 0 || maxFee.Sign() < 0 || tip.Sign() < 0 {
		return nil, errors.New("invalid transaction amounts")
	}
	if t.Gas == 0 {
		return nil, errors.New("gas must be set")
	}
	to := t.To
	return ethtypes.NewTx(&ethtypes.DynamicFeeTx{
		ChainID:   new(big.Int).SetUint64(t.ChainID),
		Nonce:     t.Nonce,
		GasTipCap: tip,
		GasFeeCap: maxFee,
		Gas:       t.Gas,
		To:        &to,
		Value:     value,
		Data:      t.Data,
	}), nil
}

// Digest is the transaction's signing hash.
func (t *EVMTx) Digest() ([]byte, error) {
	tx, err := t.Transaction()
	if err != nil {
		return nil, err
	}
	return ethtypes.LatestSignerForChainID(tx.ChainId()).Hash(tx).Bytes(), nil
}

func orZero(s string) string {
	if s == "" {
		return "0"
	}
	return s
}

// SignRequest asks the operators of a key to sign a transaction. The requester (the key's owner or
// an agent the owner registered in the policy contract) authorizes it by signing RequestHash.
type SignRequest struct {
	KeyID     hexutil.Bytes  `json:"key_id"`
	Chain     string         `json:"chain"`
	Tx        EVMTx          `json:"tx"`
	Requester common.Address `json:"requester"`
	Signature hexutil.Bytes  `json:"signature"`
}

// ChainEVM is the only chain adapter for now.
const ChainEVM = "evm"

// RequestHash binds the requester's authorization to the key and the exact transaction digest:
// keccak256("ssv-dkms:request:v1" || key ID || tx signing hash). The requester signs it with
// EIP-191 (personal_sign over these 32 bytes).
func RequestHash(keyID []byte, digest []byte) []byte {
	return crypto.Keccak256([]byte("ssv-dkms:request:v1"), keyID, digest)
}

// ID is the request's identifier: its request hash.
func (r *SignRequest) ID() (string, error) {
	d, err := r.Tx.Digest()
	if err != nil {
		return "", err
	}
	return hexutil.Encode(RequestHash(r.KeyID, d)), nil
}

// VerifyRequester checks that Requester signed the request.
func (r *SignRequest) VerifyRequester() error {
	if r.Chain != ChainEVM {
		return fmt.Errorf("unsupported chain %q", r.Chain)
	}
	d, err := r.Tx.Digest()
	if err != nil {
		return err
	}
	if len(r.Signature) != 65 {
		return errors.New("requester signature must be 65 bytes")
	}
	sig := append([]byte(nil), r.Signature...)
	if sig[64] >= 27 {
		sig[64] -= 27
	}
	pk, err := crypto.SigToPub(accounts.TextHash(RequestHash(r.KeyID, d)), sig)
	if err != nil {
		return fmt.Errorf("requester signature: %w", err)
	}
	if crypto.PubkeyToAddress(*pk) != r.Requester {
		return errors.New("requester signature does not match requester")
	}
	return nil
}

// ConsensusValue is what the operators of a key agree on with QBFT for one height: the request and
// the presignature (and so the signer set) that will sign it.
type ConsensusValue struct {
	Request SignRequest `json:"request"`
	// BlockNumber pins the chain state the policy is evaluated against, so every operator reaches
	// the same verdict.
	BlockNumber uint64        `json:"block_number"`
	PresignID   hexutil.Bytes `json:"presign_id"`
	Signers     []uint64      `json:"signers"`
	// Commitment is the presignature's public nonce point; non-signers need it to combine shares.
	Commitment hexutil.Bytes `json:"commitment"`
}

func (v *ConsensusValue) Encode() ([]byte, error) { return json.Marshal(v) }

func (v *ConsensusValue) Decode(data []byte) error {
	if len(data) > MaxMessageSize {
		return errors.New("consensus value too large")
	}
	return json.Unmarshal(data, v)
}
