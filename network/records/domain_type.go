package records

import (
	"io"

	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/networkconfig"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/enr"
	"github.com/ethereum/go-ethereum/rlp"
)

// DomainTypeEntry holds the domain type of the node
type DomainTypeEntry spectypes.DomainType

// ENRKey implements enr.Entry, returns the entry key
func (dt DomainTypeEntry) ENRKey() string { return "domaintype" }

// EncodeRLP implements rlp.Encoder, encodes domain type as bytes
func (dt DomainTypeEntry) EncodeRLP(w io.Writer) error {
	return rlp.Encode(w, dt[:])
}

// DecodeRLP implements rlp.Decoder, decodes domain type from bytes
func (dt *DomainTypeEntry) DecodeRLP(s *rlp.Stream) error {
	var buf []byte
	if err := s.Decode(&buf); err != nil {
		return err
	}
	*dt = DomainTypeEntry(buf)
	return nil
}

// SetDomainTypeEntry adds domain type entry to the node
func SetDomainTypeEntry(node *enode.LocalNode, domainType spectypes.DomainType) error {
	node.Set(DomainTypeEntry(domainType))
	return nil
}

// GetDomainTypeEntry extracts the value of domain type entry
func GetDomainTypeEntry(record *enr.Record) (spectypes.DomainType, error) {
	dt := new(DomainTypeEntry)
	if err := record.Load(dt); err != nil {
		if enr.IsNotFound(err) {
			// If not found, assuming jato-v2 for backwards-compatibility.
			return networkconfig.JatoV2.Domain, nil
		}
		return spectypes.DomainType{}, err
	}
	return spectypes.DomainType(*dt), nil
}
