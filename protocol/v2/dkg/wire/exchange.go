package wire

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/drand/kyber/share/dkg"
)

// KyberSuite is the kyber-side suite used to deserialize Point / Scalar
// fields inside the bundle bodies. It's the same `dkg.Suite` interface
// kyber's DKG protocol consumes; we re-export the alias here so callers
// don't need to import `share/dkg` just to get the type for Unwrap.
type KyberSuite = dkg.Suite

// Exchange announces an operator's kyber-side long-term pubkey for the
// upcoming DKG ceremony. Each operator generates a fresh kyber scalar at
// ceremony start, broadcasts the corresponding G1 point in an Exchange
// message, and waits until it has an Exchange from every cluster member
// before constructing kyber's DKG `Config.NewNodes`.
//
// This pre-phase exists because kyber's DKG protocol takes the long-term
// pubkeys in `Config.NewNodes` as a precondition — we have no shared key
// material at ceremony start, so the cluster bootstraps via Exchange.
//
// The PubKey field is the marshaled bytes of the operator's kyber G1 point
// (the public key corresponding to the fresh scalar that will serve as
// `Config.Longterm` and as the deal-decryption key).
type Exchange struct {
	ClusterID  [32]byte
	Generation uint64
	OperatorID uint64
	PubKey     []byte
}

// exchangeJSON is the on-wire JSON shape. Byte slices that aren't natural
// JSON types are hex-encoded.
type exchangeJSON struct {
	ClusterID  string `json:"cluster_id"`
	Generation uint64 `json:"generation"`
	OperatorID uint64 `json:"operator_id"`
	PubKey     string `json:"pub_key"`
}

// EncodeExchange serializes an Exchange to JSON bytes.
func EncodeExchange(e *Exchange) ([]byte, error) {
	if e == nil {
		return nil, errors.New("wire: nil exchange")
	}
	if len(e.PubKey) == 0 {
		return nil, errors.New("wire: exchange has empty pub_key")
	}
	out := exchangeJSON{
		ClusterID:  hex.EncodeToString(e.ClusterID[:]),
		Generation: e.Generation,
		OperatorID: e.OperatorID,
		PubKey:     hex.EncodeToString(e.PubKey),
	}
	return json.Marshal(out)
}

// DecodeExchange parses Exchange JSON bytes back into an *Exchange.
func DecodeExchange(data []byte) (*Exchange, error) {
	var in exchangeJSON
	if err := json.Unmarshal(data, &in); err != nil {
		return nil, fmt.Errorf("wire: unmarshal exchange json: %w", err)
	}
	cidBytes, err := hex.DecodeString(in.ClusterID)
	if err != nil {
		return nil, fmt.Errorf("wire: decode cluster_id hex: %w", err)
	}
	if len(cidBytes) != 32 {
		return nil, fmt.Errorf("wire: cluster_id must be 32 bytes, got %d", len(cidBytes))
	}
	pkBytes, err := hex.DecodeString(in.PubKey)
	if err != nil {
		return nil, fmt.Errorf("wire: decode pub_key hex: %w", err)
	}
	if len(pkBytes) == 0 {
		return nil, errors.New("wire: exchange has empty pub_key")
	}
	out := &Exchange{
		Generation: in.Generation,
		OperatorID: in.OperatorID,
		PubKey:     pkBytes,
	}
	copy(out.ClusterID[:], cidBytes)
	return out, nil
}
