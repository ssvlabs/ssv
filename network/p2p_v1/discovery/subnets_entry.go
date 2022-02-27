package discovery

import (
	"crypto/md5"
	"encoding/binary"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/enr"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/go-bitfield"
)

const (
	// SubnetsCount is the count of subnets in the network
	SubnetsCount = 128
	// ENRKeySubnets is the entry key for saving subnets
	ENRKeySubnets = "subnets"
)

func nsToSubnet(ns string) uint64 {
	h := md5.Sum([]byte(ns))
	val := binary.BigEndian.Uint64(h[:])
	return val % uint64(SubnetsCount)
}

// isSubnet checks if the given string is a subnet string
func isSubnet(ns string) bool {
	// TODO: check if ns is a subnet
	return true
}

func setSubnetsEntry(node *enode.LocalNode, subnets []bool) error {
	bl := bitfield.NewBitlist(uint64(SubnetsCount))
	for i, state := range subnets {
		bl.SetBitAt(uint64(i), state)
	}
	node.Set(enr.WithEntry(ENRKeySubnets, bl))
	return nil
}

func getSubnetsEntry(node *enode.Node) ([]bool, error) {
	var subnets []bool
	bl := bitfield.NewBitlist(uint64(SubnetsCount))
	err := node.Record().Load(enr.WithEntry(ENRKeySubnets, &bl))
	if err != nil {
		return subnets, err
	}
	l := len(bl)
	if l == 0 {
		return nil, errors.New("subnets entry not found")
	}
	if l > byteCount(SubnetsCount)+1 || l < byteCount(SubnetsCount)-1 {
		return subnets, errors.Errorf("invalid bitvector provided, it has a size of %d", l)
	}
	for i := 0; i < SubnetsCount; i++ {
		subnets = append(subnets, bl.BitAt(uint64(i)))
	}
	return subnets, nil
}

// Determines the number of bytes that are used
// to represent the provided number of bits.
func byteCount(bitCount int) int {
	numOfBytes := bitCount / 8
	if bitCount%8 != 0 {
		numOfBytes++
	}
	return numOfBytes
}
