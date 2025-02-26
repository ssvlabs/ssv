package commons

import (
	"encoding/hex"
	"fmt"
	"github.com/prysmaticlabs/go-bitfield"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"math"
	"math/big"
	"strconv"
	"strings"
)

const (
	// SubnetsCount returns the subnet count for this fork
	SubnetsCount = 128

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

func CommitteeTopicID(cid spectypes.CommitteeID) []string {
	return []string{fmt.Sprintf("%d", CommitteeSubnet(cid))}
}

// GetTopicFullName returns the topic full name, including prefix
func GetTopicFullName(baseName string) string {
	return fmt.Sprintf("%s.%s", topicPrefix, baseName)
}

// GetTopicBaseName return the base topic name of the topic, w/o ssv prefix
func GetTopicBaseName(topicName string) string {
	return strings.TrimPrefix(topicName, topicPrefix+".")
}

// CommitteeSubnet returns the subnet for the given committee
func CommitteeSubnet(cid spectypes.CommitteeID) uint64 {
	subnet := new(big.Int).Mod(new(big.Int).SetBytes(cid[:]), bigIntSubnetsCount)
	return subnet.Uint64()
}

// SetCommitteeSubnet returns the subnet for the given committee, it doesn't allocate memory but uses the passed in big.Int
func SetCommitteeSubnet(bigInt *big.Int, cid spectypes.CommitteeID) {
	bigInt.SetBytes(cid[:])
	bigInt.Mod(bigInt, bigIntSubnetsCount)
}

// Topics returns the available topics for this fork.
func Topics() []string {
	topics := make([]string, SubnetsCount)
	for i := uint64(0); i < SubnetsCount; i++ {
		topics[i] = GetTopicFullName(SubnetTopicID(i))
	}
	return topics
}

const (
	// ZeroSubnets is the representation of no subnets
	ZeroSubnets = "00000000000000000000000000000000"
	// AllSubnets is the representation of all subnets
	AllSubnets = "ffffffffffffffffffffffffffffffff"
)

// Subnets holds all the subscribed subnets of a specific node
type Subnets []byte

// Clone clones the independent byte slice
func (s Subnets) Clone() Subnets {
	cp := make([]byte, len(s))
	copy(cp, s)
	return cp
}

func (s Subnets) String() string {
	subnetsVec := bitfield.NewBitvector128()
	subnet := uint64(0)
	for _, val := range s {
		subnetsVec.SetBitAt(subnet, val > uint8(0))
		subnet++
	}
	return hex.EncodeToString(subnetsVec.Bytes())
}

func (s Subnets) Active() int {
	var active int
	for _, val := range s {
		if val > 0 {
			active++
		}
	}
	return active
}

// FromString parses a given subnet string
func FromString(subnetsStr string) (Subnets, error) {
	subnetsStr = strings.Replace(subnetsStr, "0x", "", 1)
	var data []byte
	for i := 0; i+1 < len(subnetsStr); i += 2 {
		maskData1, err := getCharMask(string(subnetsStr[i]))
		if err != nil {
			return nil, err
		}
		maskData2, err := getCharMask(string(subnetsStr[i+1]))
		if err != nil {
			return nil, err
		}
		data = append(data, maskData2...)
		data = append(data, maskData1...)
	}
	return data, nil
}

// SharedSubnets returns the shared subnets
func SharedSubnets(a, b []byte, maxLen int) []int {
	var shared []int
	if maxLen == 0 {
		maxLen = len(a)
	}
	if len(a) == 0 || len(b) == 0 {
		return shared
	}
	for subnet, aval := range a {
		if aval == 0 {
			continue
		}
		if b[subnet] == 0 {
			continue
		}
		shared = append(shared, subnet)
		if len(shared) == maxLen {
			break
		}
	}
	return shared
}

// DiffSubnets returns a diff of the two given subnets.
// returns a map with all the different entries and their post change value
func DiffSubnets(a, b []byte) map[int]byte {
	diff := make(map[int]byte)
	for subnet, bval := range b {
		if subnet >= len(a) {
			diff[subnet] = bval
		} else if aval := a[subnet]; aval != bval {
			diff[subnet] = bval
		}
	}
	return diff
}

func getCharMask(str string) ([]byte, error) {
	val, err := strconv.ParseUint(str, 16, 8)
	if err != nil {
		return nil, err
	}
	mask := fmt.Sprintf("%04b", val)
	maskData := make([]byte, 0, 4)
	for j := 0; j < len(mask); j++ {
		val, err := strconv.ParseUint(string(mask[len(mask)-1-j]), 2, 8)
		if err != nil {
			return nil, err
		}
		maskData = append(maskData, uint8(val)) // nolint:gosec
	}
	return maskData, nil
}
