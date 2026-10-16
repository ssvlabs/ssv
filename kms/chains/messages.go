package chains

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
)

// Off-chain families: messages a key signs rather than transactions. They are not broadcast and
// have no account sequence, so the operators need no executor for them. The request, consensus
// and policy path is the same as for transactions; PolicyFields describe the message:
//
//   - eip191 (personal_sign). Reference: the SIWE message's "Chain ID", or "0" for any other text.
//     A Sign-In with Ethereum (EIP-4361) message has Target = the domain it signs in to (as written
//     on its first line, e.g. "app.example.com" or "localhost:3000") and no Data. Any other text has
//     no Target and Data = the text, so agents can sign it only where the chain policy allows
//     contract calls (arbitrary payloads).
//   - eip712 (typed data, eth_signTypedData_v4). Reference: the domain's chainId, or "0" if it has
//     none. Target = the domain separator (32 bytes) followed by keccak256 of the primary type's
//     name (32 bytes), so a policy allowlists exact (domain, message type) pairs. No Data.
//
// Both have Value 0. The signature is returned as R || S || V with V in {27, 28}.

// Offchain is implemented by families whose payloads are messages, not transactions.
type Offchain interface {
	Offchain() bool
}

// IsOffchain reports whether a family signs messages rather than transactions.
func IsOffchain(f Family) bool {
	o, ok := f.(Offchain)
	return ok && o.Offchain()
}

// OffchainNamespaces lists the registered off-chain families.
func OffchainNamespaces() []string {
	var out []string
	for _, ns := range Namespaces() {
		if IsOffchain(families[ns]) {
			out = append(out, ns)
		}
	}
	return out
}

func init() {
	Register(EIP191{})
	Register(EIP712{})
}

// decimalRef accepts a canonical non-negative decimal (including "0").
func decimalRef(ref string) bool {
	n, err := strconv.ParseUint(ref, 10, 64)
	return err == nil && strconv.FormatUint(n, 10) == ref
}

// messageTx is a parsed message: what it signs and what the policy sees.
type messageTx struct {
	digest  []byte
	policy  PolicyFields
	summary string
}

func (m *messageTx) Digest() []byte           { return m.digest }
func (m *messageTx) Policy() PolicyFields     { return m.policy }
func (m *messageTx) Sequence() (uint64, bool) { return 0, false }
func (m *messageTx) Summary() string          { return m.summary }

// Assemble returns the signature itself (V as 27/28, as wallets return it) and the digest.
func (m *messageTx) Assemble(sig []byte) ([]byte, string, error) {
	if len(sig) != 65 {
		return nil, "", errors.New("signature must be 65 bytes")
	}
	out := append([]byte(nil), sig...)
	if out[64] < 27 {
		out[64] += 27
	}
	return out, hexutil.Encode(m.digest), nil
}

// ---------- eip191 ----------

// EIP191 signs personal messages (EIP-191 version 0x45).
type EIP191 struct{}

func (EIP191) Namespace() string              { return "eip191" }
func (EIP191) Scheme() string                 { return "ecdsa-secp256k1-cggmp21" }
func (EIP191) ValidReference(ref string) bool { return decimalRef(ref) }
func (EIP191) Offchain() bool                 { return true }

// EIP191Message is the "tx" of an eip191 request.
type EIP191Message struct {
	Message string `json:"message"`
}

var (
	siweFirstLine = regexp.MustCompile(`^(?:[a-zA-Z][a-zA-Z0-9+.-]*://)?([^\s/]+) wants you to sign in with your Ethereum account:$`)
	siweChainID   = regexp.MustCompile(`(?m)^Chain ID: ([0-9]+)$`)
)

// SIWE returns the domain and chain ID of a Sign-In with Ethereum (EIP-4361) message; ok is false
// for any other text. chainID is "" when the message has no Chain ID line.
func SIWE(message string) (domain, chainID string, ok bool) {
	first, _, _ := strings.Cut(message, "\n")
	m := siweFirstLine.FindStringSubmatch(strings.TrimRight(first, "\r"))
	if m == nil {
		return "", "", false
	}
	if c := siweChainID.FindStringSubmatch(message); c != nil {
		chainID = c[1]
	}
	return m[1], chainID, true
}

func (EIP191) Parse(ref string, payload []byte) (Tx, error) {
	var p EIP191Message
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil, fmt.Errorf("invalid eip191 message: %w", err)
	}
	if p.Message == "" {
		return nil, errors.New("empty message")
	}
	tx := &messageTx{digest: accounts.TextHash([]byte(p.Message)), policy: PolicyFields{Value: new(big.Int)}}
	if domain, chainID, ok := SIWE(p.Message); ok {
		want := chainID
		if want == "" {
			want = "0"
		}
		if ref != want {
			return nil, fmt.Errorf("sign-in message for chain %s does not match chain eip191:%s", want, ref)
		}
		tx.policy.Target = []byte(domain)
		tx.summary = fmt.Sprintf("sign in to %s", domain)
		return tx, nil
	}
	if ref != "0" {
		return nil, fmt.Errorf("a message that is not a sign-in message must use eip191:0, not eip191:%s", ref)
	}
	tx.policy.Data = []byte(p.Message)
	tx.summary = fmt.Sprintf("message of %d bytes", len(p.Message))
	return tx, nil
}

// ---------- eip712 ----------

// EIP712 signs typed data (EIP-712).
type EIP712 struct{}

func (EIP712) Namespace() string              { return "eip712" }
func (EIP712) Scheme() string                 { return "ecdsa-secp256k1-cggmp21" }
func (EIP712) ValidReference(ref string) bool { return decimalRef(ref) }
func (EIP712) Offchain() bool                 { return true }

// TypedTarget is the policy target of typed data: its domain separator || keccak256(primary type).
func TypedTarget(domainSeparator []byte, primaryType string) []byte {
	return append(append([]byte(nil), domainSeparator...), crypto.Keccak256([]byte(primaryType))...)
}

// ParseTypedData decodes eth_signTypedData_v4 JSON. Numbers keep full precision (they are passed on
// as decimal strings), and EIP712Domain is derived from the domain's fields when types omit it.
func ParseTypedData(payload []byte) (*apitypes.TypedData, error) {
	var raw struct {
		Types       map[string][]apitypes.Type `json:"types"`
		PrimaryType string                     `json:"primaryType"`
		Domain      map[string]any             `json:"domain"`
		Message     map[string]any             `json:"message"`
	}
	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.UseNumber()
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("invalid typed data: %w", err)
	}
	if raw.PrimaryType == "" || raw.Types == nil || raw.Types[raw.PrimaryType] == nil {
		return nil, errors.New("typed data: primaryType must name one of its types")
	}
	if raw.PrimaryType == "EIP712Domain" {
		return nil, errors.New("typed data: primaryType can't be EIP712Domain")
	}
	td := &apitypes.TypedData{Types: apitypes.Types{}, PrimaryType: raw.PrimaryType, Message: numbersToStrings(raw.Message).(map[string]any)}
	for k, v := range raw.Types {
		td.Types[k] = v
	}
	d := raw.Domain
	str := func(k string) (string, error) {
		v, ok := d[k]
		if !ok || v == nil {
			return "", nil
		}
		s, ok := v.(string)
		if !ok {
			return "", fmt.Errorf("typed data: domain %s must be a string", k)
		}
		return s, nil
	}
	var err error
	if td.Domain.Name, err = str("name"); err != nil {
		return nil, err
	}
	if td.Domain.Version, err = str("version"); err != nil {
		return nil, err
	}
	if td.Domain.VerifyingContract, err = str("verifyingContract"); err != nil {
		return nil, err
	}
	if td.Domain.Salt, err = str("salt"); err != nil {
		return nil, err
	}
	if v, ok := d["chainId"]; ok && v != nil {
		var s string
		switch c := v.(type) {
		case json.Number:
			s = c.String()
		case string:
			s = c
		default:
			return nil, errors.New("typed data: domain chainId must be a number")
		}
		n, ok := new(big.Int).SetString(s, 0)
		if !ok || n.Sign() < 0 || n.BitLen() > 64 {
			return nil, fmt.Errorf("typed data: invalid domain chainId %q", s)
		}
		td.Domain.ChainId = (*math.HexOrDecimal256)(n)
	}
	if _, ok := td.Types["EIP712Domain"]; !ok {
		var fields []apitypes.Type
		if td.Domain.Name != "" {
			fields = append(fields, apitypes.Type{Name: "name", Type: "string"})
		}
		if td.Domain.Version != "" {
			fields = append(fields, apitypes.Type{Name: "version", Type: "string"})
		}
		if td.Domain.ChainId != nil {
			fields = append(fields, apitypes.Type{Name: "chainId", Type: "uint256"})
		}
		if td.Domain.VerifyingContract != "" {
			fields = append(fields, apitypes.Type{Name: "verifyingContract", Type: "address"})
		}
		if td.Domain.Salt != "" {
			fields = append(fields, apitypes.Type{Name: "salt", Type: "bytes32"})
		}
		td.Types["EIP712Domain"] = fields
	}
	return td, nil
}

// numbersToStrings replaces JSON numbers with their decimal text, which the EIP-712 encoder parses
// exactly (it would round float64s above 2^53).
func numbersToStrings(v any) any {
	switch x := v.(type) {
	case json.Number:
		return x.String()
	case map[string]any:
		for k, e := range x {
			x[k] = numbersToStrings(e)
		}
		return x
	case []any:
		for i, e := range x {
			x[i] = numbersToStrings(e)
		}
		return x
	case nil:
		return map[string]any{}
	default:
		return x
	}
}

func (EIP712) Parse(ref string, payload []byte) (Tx, error) {
	td, err := ParseTypedData(payload)
	if err != nil {
		return nil, err
	}
	want := "0"
	if td.Domain.ChainId != nil {
		want = (*big.Int)(td.Domain.ChainId).String()
	}
	if ref != want {
		return nil, fmt.Errorf("typed data for chain %s does not match chain eip712:%s", want, ref)
	}
	separator, err := td.HashStruct("EIP712Domain", td.Domain.Map())
	if err != nil {
		return nil, fmt.Errorf("typed data domain: %w", err)
	}
	digest, _, err := apitypes.TypedDataAndHash(*td)
	if err != nil {
		return nil, fmt.Errorf("typed data: %w", err)
	}
	return &messageTx{
		digest:  digest,
		policy:  PolicyFields{Target: TypedTarget(separator, td.PrimaryType), Value: new(big.Int)},
		summary: fmt.Sprintf("%s for %q", td.PrimaryType, td.Domain.Name),
	}, nil
}
