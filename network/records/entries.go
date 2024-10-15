package records

import (
	"io"

	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/enr"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/go-bitfield"

	spectypes "github.com/ssvlabs/ssv-spec/types"
)

type ENRKey string

const (
	KeyDomainType     = "domaintype"
	KeyNextDomainType = "next_domaintype"
)

var ErrEntryNotFound = errors.New("not found")

// DomainTypeEntry holds the domain type of the node
type DomainTypeEntry struct {
	Key        ENRKey
	DomainType spectypes.DomainType
}

// ENRKey implements enr.Entry, returns the entry key
func (dt DomainTypeEntry) ENRKey() string { return string(dt.Key) }

// EncodeRLP implements rlp.Encoder, encodes domain type as bytes
func (dt DomainTypeEntry) EncodeRLP(w io.Writer) error {
	return rlp.Encode(w, dt.DomainType[:])
}

// DecodeRLP implements rlp.Decoder, decodes domain type from bytes
func (dt *DomainTypeEntry) DecodeRLP(s *rlp.Stream) error {
	var buf []byte
	if err := s.Decode(&buf); err != nil {
		return err
	}
	dt.DomainType = spectypes.DomainType(buf)
	return nil
}

// SetDomainTypeEntry adds domain type entry to the node
func SetDomainTypeEntry(node *enode.LocalNode, key ENRKey, domainType spectypes.DomainType) error {
	node.Set(DomainTypeEntry{
		Key:        key,
		DomainType: domainType,
	})
	return nil
}

// GetDomainTypeEntry extracts the value of domain type entry
func GetDomainTypeEntry(record *enr.Record, key ENRKey) (spectypes.DomainType, error) {
	dt := DomainTypeEntry{
		Key: key,
	}
	if err := record.Load(&dt); err != nil {
		if enr.IsNotFound(err) {
			return spectypes.DomainType{}, ErrEntryNotFound
		}
		return spectypes.DomainType{}, err
	}
	return dt.DomainType, nil
}

// SetSubnetsEntry adds subnets entry to our enode.LocalNode
func SetSubnetsEntry(node *enode.LocalNode, subnets []byte) error {
	subnetsVec := bitfield.NewBitvector128()
	for i, subnet := range subnets { // #nosec G115 -- subnets has a constant len of 128
		subnetsVec.SetBitAt(uint64(i), subnet > 0)
	}
	node.Set(enr.WithEntry("subnets", &subnetsVec))
	return nil
}

// GetSubnetsEntry extracts the value of subnets entry from some record
func GetSubnetsEntry(record *enr.Record) ([]byte, error) {
	subnetsVec := bitfield.NewBitvector128()
	if err := record.Load(enr.WithEntry("subnets", &subnetsVec)); err != nil {
		if enr.IsNotFound(err) {
			return nil, ErrEntryNotFound
		}
		return nil, err
	}
	res := make([]byte, 0, subnetsVec.Len())
	for i := uint64(0); i < subnetsVec.Len(); i++ {
		val := byte(0)
		if subnetsVec.BitAt(i) {
			val = 1
		}
		res = append(res, val)
	}
	return res, nil
}
