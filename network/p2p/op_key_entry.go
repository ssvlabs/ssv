package p2p

import (
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/enr"
	"github.com/ethereum/go-ethereum/rlp"
	"io"
)

// addOperatorPubKeyHashEntry adds operator-public-key-hash entry ('pkh') to the node
func addOperatorPubKeyHashEntry(node *enode.LocalNode, pkHash string) (*enode.LocalNode, error) {
	node.Set(PublicKeyHash(pkHash))
	return node, nil
}

// extractOperatorPubKeyHashEntry extracts the value of operator-public-key-hash entry ('pkh')
func extractOperatorPubKeyHashEntry(record *enr.Record) (*PublicKeyHash, error) {
	pkh := new(PublicKeyHash)
	if err := record.Load(pkh); err != nil {
		if enr.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return pkh, nil
}

// PublicKeyHash is the "pkh" key, which holds a public key hash
type PublicKeyHash string

func (pkh PublicKeyHash) ENRKey() string { return "pkh" }

// EncodeRLP implements rlp.Encoder.
func (pkh PublicKeyHash) EncodeRLP(w io.Writer) error {
	return rlp.Encode(w, []byte(pkh))
}

// DecodeRLP implements rlp.Decoder.
func (pkh *PublicKeyHash) DecodeRLP(s *rlp.Stream) error {
	buf, err := s.Bytes()
	if err != nil {
		return err
	}
	*pkh = PublicKeyHash(buf)
	return nil
}

