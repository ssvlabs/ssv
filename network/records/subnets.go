package records

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"math/bits"
	"strings"

	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/pkg/errors"

	"github.com/ssvlabs/ssv/network/commons"
)

const (
	SubnetsCount = int(commons.SubnetsCount)
	byteCnt      = SubnetsCount / 8
)

var (
	// ZeroSubnets is the representation of no subnets
	ZeroSubnets = Subnets{v: [byteCnt]byte(bytes.Repeat([]byte{0x00}, byteCnt))}
	// AllSubnets is the representation of all subnets
	AllSubnets = Subnets{v: [byteCnt]byte(bytes.Repeat([]byte{0xFF}, byteCnt))}
)

// Subnets holds all the subscribed subnets of a specific node.
// The array index represents a subnet number,
// the value holds either 0 or 1 representing if the node is subscribed to the subnet number.
type Subnets struct {
	v [byteCnt]byte // wrapped by a struct to forbid indexing outsize of this package
}

// SubnetsFromString parses a given subnet string
func SubnetsFromString(subnetsStr string) (Subnets, error) {
	subnetsStr = strings.TrimPrefix(subnetsStr, "0x")
	data, err := hex.DecodeString(subnetsStr)
	if err != nil {
		return Subnets{}, err
	}

	if len(data) != byteCnt {
		return Subnets{}, fmt.Errorf("invalid subnets length %d, expected %d", len(data), byteCnt)
	}

	var subnets Subnets
	copy(subnets.v[:], data)
	return subnets, nil
}

// IsSet checks if the i-th subnet is set.
func (s *Subnets) IsSet(i int) bool {
	if i < 0 || i >= SubnetsCount {
		return false
	}
	byteIndex := i / 8
	bitIndex := uint(i % 8) // #nosec G115 -- subnets has a constant max len of 128
	return (s.v[byteIndex] & (1 << bitIndex)) != 0
}

// Set marks the i-th subnet as set.
func (s *Subnets) Set(i int) {
	if i < 0 || i >= SubnetsCount {
		return
	}
	byteIndex := i / 8
	bitIndex := uint(i % 8) // #nosec G115 -- subnets has a constant max len of 128
	s.v[byteIndex] |= (1 << bitIndex)
}

// Clear marks the i-th subnet as not set.
func (s *Subnets) Clear(i int) {
	if i < 0 || i >= SubnetsCount {
		return
	}
	byteIndex := i / 8
	bitIndex := uint(i % 8) // #nosec G115 -- subnets has a constant max len of 128
	s.v[byteIndex] &^= (1 << bitIndex)
}

func (s *Subnets) String() string {
	return hex.EncodeToString(s.v[:])
}

func (s *Subnets) SubnetList() []int {
	indices := make([]int, 0)
	for byteIdx, b := range s.v {
		if byteIdx >= SubnetsCount {
			break
		}
		for bitIdx := 0; bitIdx < 8; bitIdx++ {
			bit := byte(1 << uint(bitIdx)) // #nosec G115 -- subnets has a constant max len of 128
			if b&bit == bit {
				subnet := byteIdx*8 + bitIdx
				indices = append(indices, subnet)
			}
		}
	}

	return indices
}

func (s *Subnets) ActiveCount() int {
	active := 0
	for _, b := range s.v {
		active += bits.OnesCount8(b)
	}
	return active
}

// ToMap returns a map with all subnets and their values
func (s *Subnets) ToMap() map[int]bool {
	m := make(map[int]bool)
	for i := 0; i < SubnetsCount; i++ {
		m[i] = s.IsSet(i)
	}
	return m
}

// SharedSubnets returns the shared subnets
func (s *Subnets) SharedSubnets(other Subnets) []int {
	return s.SharedSubnetsN(other, 0)
}

func (s *Subnets) SharedSubnetsN(other Subnets, n int) []int {
	var shared []int
	if n <= 0 {
		n = SubnetsCount
	}
	for i := 0; i < SubnetsCount; i++ {
		if s.IsSet(i) && other.IsSet(i) {
			shared = append(shared, i)
			if len(shared) == n {
				break
			}
		}
	}
	return shared
}

func (s *Subnets) DiffSubnets(other Subnets) (added Subnets, removed Subnets) {
	for i := 0; i < byteCnt; i++ {
		// Bits to add: set in 'other' but not in 's'
		added.v[i] = other.v[i] &^ s.v[i]
		// Bits to remove: set in 's' but not in 'other'
		removed.v[i] = s.v[i] &^ other.v[i]
	}
	return added, removed
}

// UpdateSubnets updates subnets entry according to the given changes.
// count is the amount of subnets, in case that the entry doesn't exist as we want to initialize it
func UpdateSubnets(node *enode.LocalNode, added []uint64, removed []uint64) (Subnets, bool, error) {
	subnets, err := GetSubnetsEntry(node.Node().Record())
	if err != nil && !errors.Is(err, ErrEntryNotFound) {
		return Subnets{}, false, fmt.Errorf("could not read subnets entry: %w", err)
	}

	orig := subnets
	for _, i := range added {
		subnets.Set(int(i)) // #nosec G115 -- subnets has a constant max len of 128
	}
	for _, i := range removed {
		subnets.Clear(int(i)) // #nosec G115 -- subnets has a constant max len of 128
	}
	if orig == subnets {
		return Subnets{}, false, nil
	}
	if err := SetSubnetsEntry(node, subnets); err != nil {
		return Subnets{}, false, fmt.Errorf("could not update subnets entry: %w", err)
	}
	return subnets, true, nil
}
