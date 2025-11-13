package commons

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"math/big"
	"math/bits"
	"strings"
	"sync"

	spectypes "github.com/ssvlabs/ssv-spec/types"
)

const (
	// SubnetsCount returns the subnet count for this fork. It must be power of 2.
	SubnetsCount = 128

	byteCount = SubnetsCount / 8

	UnknownSubnetId = math.MaxUint64

	// UnknownSubnet is used when a validator public key is invalid
	UnknownSubnet = "unknown"

	topicPrefix = "ssv.v2"
)

// BigIntSubnetsCount is the big.Int representation of SubnetsCount
var bigIntSubnetsCount = new(big.Int).SetUint64(SubnetsCount)

// SubnetTopicID returns the topic to use for the given subnet
func SubnetTopicID(subnet uint64) string {
	if subnet == UnknownSubnetId {
		return UnknownSubnet
	}
	return fmt.Sprintf("%d", subnet)
}

// CommitteeTopicIDAlan returns topics for given committee ID.
// There's no similar post-fork function because post-fork requires operator IDs for calculation.
func CommitteeTopicIDAlan(cid spectypes.CommitteeID) []string {
	return []string{fmt.Sprintf("%d", CommitteeSubnetAlan(cid))}
}

// GetTopicFullName returns the topic full name, including prefix
func GetTopicFullName(baseName string) string {
	return fmt.Sprintf("%s.%s", topicPrefix, baseName)
}

// GetTopicBaseName return the base topic name of the topic, w/o ssv prefix
func GetTopicBaseName(topicName string) string {
	return strings.TrimPrefix(topicName, topicPrefix+".")
}

var bigIntPool = sync.Pool{
	New: func() any { return new(big.Int) },
}

// CommitteeSubnet returns the subnet for the given committee calculated as (lowestHash % bigIntSubnetsCount).
// It requires committee to be valid and have length >=1.
// It uses sync.Pool to reduce heap allocations.
func CommitteeSubnet(committee []spectypes.OperatorID) uint64 {
	var operatorBytes [8]byte
	var hash [32]byte

	lowest := bigIntPool.Get().(*big.Int)
	hashNum := bigIntPool.Get().(*big.Int)
	result := bigIntPool.Get().(*big.Int)

	defer bigIntPool.Put(lowest)
	defer bigIntPool.Put(hashNum)
	defer bigIntPool.Put(result)

	binary.LittleEndian.PutUint64(operatorBytes[:], committee[0])
	hash = sha256.Sum256(operatorBytes[:])
	lowest.SetBytes(hash[:])

	for i := 1; i < len(committee); i++ {
		binary.LittleEndian.PutUint64(operatorBytes[:], committee[i])
		hash = sha256.Sum256(operatorBytes[:])
		hashNum.SetBytes(hash[:])

		if hashNum.Cmp(lowest) < 0 {
			lowest.Set(hashNum)
		}
	}

	result.Mod(lowest, bigIntSubnetsCount)
	subnet := result.Uint64()

	return subnet
}

// CommitteeSubnetAlan returns the subnet for the given committee for Alan fork
func CommitteeSubnetAlan(cid spectypes.CommitteeID) uint64 {
	bi := bigIntPool.Get().(*big.Int)
	defer bigIntPool.Put(bi)

	bi.SetBytes(cid[:])
	bi.Mod(bi, bigIntSubnetsCount)
	return bi.Uint64()
}

// Topics returns the available topics for this fork.
func Topics() []string {
	topics := make([]string, SubnetsCount)
	for i := uint64(0); i < SubnetsCount; i++ {
		topics[i] = GetTopicFullName(SubnetTopicID(i))
	}
	return topics
}

var (
	// ZeroSubnets is the representation of no subnets
	ZeroSubnets = Subnets{v: [byteCount]byte(bytes.Repeat([]byte{0x00}, byteCount))}
	// AllSubnets is the representation of all subnets
	AllSubnets = Subnets{v: [byteCount]byte(bytes.Repeat([]byte{0xFF}, byteCount))}
)

// Subnets holds all the subscribed subnets of a specific node.
// The array index represents a subnet number,
// the value holds either 0 or 1 representing if the node is subscribed to the subnet number.
type Subnets struct {
	v [byteCount]byte // wrapped by a struct to forbid indexing outsize of this package
}

// SubnetsFromString parses a given subnet string
func SubnetsFromString(subnetsStr string) (Subnets, error) {
	if len(subnetsStr) == 0 {
		return ZeroSubnets, nil
	}

	subnetsStr = strings.TrimPrefix(subnetsStr, "0x")
	data, err := hex.DecodeString(subnetsStr)
	if err != nil {
		return Subnets{}, err
	}

	if len(data) != byteCount {
		return Subnets{}, fmt.Errorf("invalid subnets length %d, expected %d", len(data), byteCount)
	}

	var subnets Subnets
	copy(subnets.v[:], data)
	return subnets, nil
}

// IsSet checks if the i-th subnet is set.
func (s *Subnets) IsSet(i uint64) bool {
	if i >= SubnetsCount {
		return false
	}
	byteIndex := i / 8
	bitIndex := i % 8
	return (s.v[byteIndex] & (1 << bitIndex)) != 0
}

// Set marks the i-th subnet as set.
func (s *Subnets) Set(i uint64) {
	if i >= SubnetsCount {
		return
	}
	byteIndex := i / 8
	bitIndex := i % 8
	s.v[byteIndex] |= 1 << bitIndex
}

// Clear marks the i-th subnet as not set.
func (s *Subnets) Clear(i uint64) {
	if i >= SubnetsCount {
		return
	}
	byteIndex := i / 8
	bitIndex := i % 8
	s.v[byteIndex] &^= 1 << bitIndex
}

func (s *Subnets) String() string {
	return hex.EncodeToString(s.v[:])
}

func (s *Subnets) SubnetList() []uint64 {
	indices := make([]uint64, 0)
	for byteIdx, b := range s.v {
		if byteIdx >= SubnetsCount {
			break
		}
		for bitIdx := uint64(0); bitIdx < 8; bitIdx++ {
			bit := byte(1 << uint(bitIdx)) // #nosec G115 -- subnets has a constant max len of 128
			if b&bit == bit {
				subnet := uint64(byteIdx)*8 + bitIdx
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

func (s *Subnets) HasActive() bool {
	return s.ActiveCount() > 0
}

// ToMap returns a map with all subnets and their values
func (s *Subnets) ToMap() map[uint64]bool {
	m := make(map[uint64]bool)
	for i := uint64(0); i < SubnetsCount; i++ {
		m[i] = s.IsSet(i)
	}
	return m
}

// SharedSubnets returns the shared subnets
func (s *Subnets) SharedSubnets(other Subnets) []uint64 {
	return s.SharedSubnetsN(other, 0)
}

func (s *Subnets) SharedSubnetsN(other Subnets, n int) []uint64 {
	var shared []uint64
	if n <= 0 {
		n = SubnetsCount
	}
	for i := uint64(0); i < SubnetsCount; i++ {
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
	for i := 0; i < byteCount; i++ {
		// Bits to add: set in 'other' but not in 's'
		added.v[i] = other.v[i] &^ s.v[i]
		// Bits to remove: set in 's' but not in 'other'
		removed.v[i] = s.v[i] &^ other.v[i]
	}
	return added, removed
}
