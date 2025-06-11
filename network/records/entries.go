package records

import (
	"io"

	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/enr"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/go-bitfield"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/network/commons"
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
func SetSubnetsEntry(node *enode.LocalNode, subnets commons.Subnets) error {
	subnetsVec := bitfield.NewBitvector128()

	for i := uint64(0); i < commons.SubnetsCount; i++ {
		subnetsVec.SetBitAt(i, subnets.IsSet(i))
	}
	node.Set(enr.WithEntry("subnets", &subnetsVec))
	return nil
}

// GetSubnetsEntry extracts the value of subnets entry from some record
func GetSubnetsEntry(record *enr.Record) (commons.Subnets, error) {
	subnetsVec := bitfield.NewBitvector128()
	if err := record.Load(enr.WithEntry("subnets", &subnetsVec)); err != nil {
		if enr.IsNotFound(err) {
			return commons.Subnets{}, ErrEntryNotFound
		}
		return commons.Subnets{}, err
	}
	res := commons.Subnets{}
	for i := uint64(0); i < commons.SubnetsCount; i++ {
		if subnetsVec.BitAt(i) {
			res.Set(i)
		} else {
			res.Clear(i)
		}
	}
	return res, nil
}
