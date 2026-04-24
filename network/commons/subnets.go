package commons

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/big"
	"math/bits"
	"strconv"
	"strings"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/networkconfig"
)

const (
	// SubnetsCount returns the subnet count for this fork. It must be power of 2.
	SubnetsCount = 128

	byteCount = SubnetsCount / 8

	alanTopicPrefix = "ssv.v2"
	topicRoot       = "/ssv"
	booleTopicFork  = "boole"
)

type Subnet uint64

// BigIntSubnetsCount is the big.Int representation of SubnetsCount
var bigIntSubnetsCount = new(big.Int).SetUint64(SubnetsCount)

func (s Subnet) AlanTopic() string {
	return fmt.Sprintf("%s.%d", alanTopicPrefix, s)
}

func (s Subnet) BooleTopic(networkName string) string {
	return fmt.Sprintf("%s/%s/%s/%d", topicRoot, networkName, booleTopicFork, s)
}

// ParseTopicSubnet extracts the subnet number from a full topic name.
func ParseTopicSubnet(topicName string) (s Subnet, boole bool, err error) {
	if strings.HasPrefix(topicName, alanTopicPrefix+".") {
		trimmed := strings.TrimPrefix(topicName, alanTopicPrefix+".")
		subnet, err := strconv.ParseUint(trimmed, 10, 64)
		return Subnet(subnet), false, err
	}
	if strings.HasPrefix(topicName, topicRoot+"/") {
		remainder := strings.TrimPrefix(topicName, topicRoot+"/")
		parts := strings.SplitN(remainder, "/", 3)
		if len(parts) != 3 || parts[1] != booleTopicFork || parts[2] == "" {
			return 0, false, fmt.Errorf("invalid topic format: %s", topicName)
		}
		subnet, err := strconv.ParseUint(parts[2], 10, 64)
		return Subnet(subnet), true, err
	}
	return 0, false, fmt.Errorf("invalid topic format: %s", topicName)
}

// BooleCommitteeSubnet returns the subnet for the given committee calculated as (lowestHash % bigIntSubnetsCount).
// It requires committee to be valid and have length >=1.
func BooleCommitteeSubnet(committee []spectypes.OperatorID) Subnet {
	var operatorBytes [8]byte
	binary.LittleEndian.PutUint64(operatorBytes[:], committee[0])

	var lowest, hashNum, result big.Int
	hash := sha256.Sum256(operatorBytes[:])
	lowest.SetBytes(hash[:])

	for i := 1; i < len(committee); i++ {
		binary.LittleEndian.PutUint64(operatorBytes[:], committee[i])
		hash = sha256.Sum256(operatorBytes[:])
		hashNum.SetBytes(hash[:])

		if hashNum.Cmp(&lowest) < 0 {
			lowest.Set(&hashNum)
		}
	}

	result.Mod(&lowest, bigIntSubnetsCount)
	return Subnet(result.Uint64())
}

// AlanCommitteeSubnet returns the subnet for the given committee for Alan fork
func AlanCommitteeSubnet(cid spectypes.CommitteeID) Subnet {
	var bi big.Int
	bi.Mod(bi.SetBytes(cid[:]), bigIntSubnetsCount)
	return Subnet(bi.Uint64())
}

// Topics returns the available Alan and Boole topics for the given network.
func Topics(netCfg *networkconfig.Network) []string {
	topics := make([]string, 0, SubnetsCount*2)
	for subnet := Subnet(0); subnet < Subnet(SubnetsCount); subnet++ {
		if !netCfg.BooleFork() {
			topics = append(topics, subnet.AlanTopic())
		}
		topics = append(topics, subnet.BooleTopic(netCfg.SSV.Name))
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
func (s *Subnets) IsSet(i Subnet) bool {
	if uint64(i) >= SubnetsCount {
		return false
	}
	byteIndex := uint64(i) / 8
	bitIndex := uint64(i) % 8
	return (s.v[byteIndex] & (1 << bitIndex)) != 0
}

// Set marks the i-th subnet as set.
func (s *Subnets) Set(i Subnet) {
	if uint64(i) >= SubnetsCount {
		return
	}
	byteIndex := uint64(i) / 8
	bitIndex := uint64(i) % 8
	s.v[byteIndex] |= 1 << bitIndex
}

// Clear marks the i-th subnet as not set.
func (s *Subnets) Clear(i Subnet) {
	if uint64(i) >= SubnetsCount {
		return
	}
	byteIndex := uint64(i) / 8
	bitIndex := uint64(i) % 8
	s.v[byteIndex] &^= 1 << bitIndex
}

// StringHumanReadable returns human-readable subnets representation.
func (s *Subnets) StringHumanReadable() string {
	return fmt.Sprint(s.SubnetList())
}

// StringHex returns subnets as a hex-encoded string.
func (s *Subnets) StringHex() string {
	return hex.EncodeToString(s.v[:])
}

// String returns subnets as a hex-encoded string.
// DEPRECATED, use StringHex or StringHumanReadable instead, this method is preserved for
// backward-compatibility since it's used for some p2p-message encoding/decoding.
func (s *Subnets) String() string {
	return s.StringHex()
}

// SubnetList returns a list of subnets; each subnet is in the range [0, SubnetsCount).
func (s *Subnets) SubnetList() []Subnet {
	subnetNumbers := make([]Subnet, 0)
	for byteIdx, b := range s.v {
		if byteIdx >= SubnetsCount {
			break
		}
		for bitIdx := uint64(0); bitIdx < 8; bitIdx++ {
			bit := byte(1 << uint(bitIdx)) // #nosec G115 -- subnets has a constant max len of SubnetsCount
			if b&bit == bit {
				subnet := Subnet(byteIdx)*8 + Subnet(bitIdx)
				subnetNumbers = append(subnetNumbers, subnet)
			}
		}
	}
	return subnetNumbers
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

// SharedSubnets returns the shared subnets
func (s *Subnets) SharedSubnets(other Subnets) []Subnet {
	return s.SharedSubnetsN(other, 0)
}

// SharedSubnetsN returns up to n shared subnets (all if n <= 0).
func (s *Subnets) SharedSubnetsN(other Subnets, n int) []Subnet {
	var shared []Subnet
	if n <= 0 {
		n = SubnetsCount
	}
	for subnet := Subnet(0); subnet < Subnet(SubnetsCount); subnet++ {
		if s.IsSet(subnet) && other.IsSet(subnet) {
			shared = append(shared, subnet)
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
