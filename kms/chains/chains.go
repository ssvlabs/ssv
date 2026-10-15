// Package chains holds the dKMS chain adapters: how a signing request's transaction is parsed for a
// given chain, what digest the key signs, what the policy sees, and how the signed transaction is
// assembled.
//
// Chains are named by CAIP-2 identifiers (https://chainagnostic.org/CAIPs/caip-2):
// "<namespace>:<reference>", e.g. "eip155:1" (Ethereum), "eip155:8453" (Base). A Family serves one
// namespace; EVM chains (eip155) are the first. Non-EVM families (solana, bip122, cosmos…) plug in
// by implementing Family and registering it; the consensus, presigning and policy layers only see
// the Tx interface.
package chains

import (
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"sort"
	"strings"

	"github.com/ethereum/go-ethereum/crypto"
)

// Tx is a parsed transaction for one chain.
type Tx interface {
	// Digest is the 32-byte hash the key signs.
	Digest() []byte
	// Policy is what the on-chain policy evaluates.
	Policy() PolicyFields
	// Sequence is the transaction's replay-protection sequence (an account nonce), if the chain has
	// one: two different transactions with the same sequence are never both signed.
	Sequence() (uint64, bool)
	// Assemble attaches the combined signature (65 bytes: R || S || V, V in {0,1}) and returns the
	// broadcastable transaction and its hash as the chain displays it.
	Assemble(sig []byte) (raw []byte, hash string, err error)
	// Summary is a one-line description for logs.
	Summary() string
}

// PolicyFields is a transaction as the policy contract sees it, for any chain.
type PolicyFields struct {
	// Target is the recipient (or called program/contract) in the chain's own address bytes.
	Target []byte
	// Value is the native amount moved, in the chain's base unit (wei, lamports, satoshi…).
	Value *big.Int
	// Data is the call payload (EVM calldata; empty for a plain transfer).
	Data []byte
}

// Family serves the chains of one CAIP-2 namespace.
type Family interface {
	Namespace() string
	// Scheme is the threshold signature scheme the family's keys use (an ssv-dkg kms SchemeID).
	Scheme() string
	// ValidReference reports whether a chain reference is well formed for this namespace.
	ValidReference(reference string) bool
	// Parse parses a transaction payload (the request's "tx") for chain `reference`.
	Parse(reference string, payload []byte) (Tx, error)
}

var families = map[string]Family{}

// Register adds a chain family.
func Register(f Family) { families[f.Namespace()] = f }

// Namespaces lists the registered namespaces.
func Namespaces() []string {
	out := make([]string, 0, len(families))
	for ns := range families {
		out = append(out, ns)
	}
	sort.Strings(out)
	return out
}

var caip2 = regexp.MustCompile(`^([-a-z0-9]{3,8}):([-_a-zA-Z0-9]{1,32})$`)

// ID is a parsed CAIP-2 chain identifier.
type ID struct {
	Namespace string
	Reference string
}

func (id ID) String() string { return id.Namespace + ":" + id.Reference }

// Hash is the chain's identifier in the policy contract: keccak256 of the CAIP-2 string.
func (id ID) Hash() [32]byte { return keccak([]byte(id.String())) }

// ParseID parses and validates a CAIP-2 chain identifier against the registered families.
func ParseID(s string) (ID, Family, error) {
	m := caip2.FindStringSubmatch(s)
	if m == nil {
		return ID{}, nil, fmt.Errorf("chain %q is not a CAIP-2 identifier (e.g. eip155:1)", s)
	}
	f, ok := families[m[1]]
	if !ok {
		return ID{}, nil, fmt.Errorf("unsupported chain family %q (supported: %s)", m[1], strings.Join(Namespaces(), ", "))
	}
	if !f.ValidReference(m[2]) {
		return ID{}, nil, fmt.Errorf("invalid %s chain reference %q", m[1], m[2])
	}
	return ID{Namespace: m[1], Reference: m[2]}, f, nil
}

// Parse parses a request's transaction for its chain.
func Parse(chain string, payload []byte) (ID, Tx, error) {
	id, f, err := ParseID(chain)
	if err != nil {
		return ID{}, nil, err
	}
	if len(payload) == 0 {
		return ID{}, nil, errors.New("missing transaction")
	}
	tx, err := f.Parse(id.Reference, payload)
	if err != nil {
		return ID{}, nil, err
	}
	if len(tx.Digest()) != 32 {
		return ID{}, nil, errors.New("transaction digest must be 32 bytes")
	}
	return id, tx, nil
}

func keccak(b ...[]byte) [32]byte { return crypto.Keccak256Hash(b...) }
