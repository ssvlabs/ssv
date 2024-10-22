package records

import (
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/go-bitfield"

	"github.com/ssvlabs/ssv/network/commons"
)

const (
	// ZeroSubnets is the representation of no subnets
	ZeroSubnets = "00000000000000000000000000000000"
	// AllSubnets is the representation of all subnets
	AllSubnets = "ffffffffffffffffffffffffffffffff"
)

// UpdateSubnets updates subnets entry according to the given changes.
// count is the amount of subnets, in case that the entry doesn't exist as we want to initialize it
func UpdateSubnets(node *enode.LocalNode, added []uint64, removed []uint64) (Subnets, bool, error) {
	subnets, err := GetSubnetsEntry(node.Node().Record())
	if err != nil && !errors.Is(err, ErrEntryNotFound) {
		return Subnets{}, false, errors.Wrap(err, "could not read subnets entry")
	}

	orig := subnets
	for _, i := range added {
		subnets[i] = 1
	}
	for _, i := range removed {
		subnets[i] = 0
	}
	if orig == subnets {
		return Subnets{}, false, nil
	}
	if err := SetSubnetsEntry(node, subnets); err != nil {
		return Subnets{}, false, errors.Wrap(err, "could not update subnets entry")
	}
	return subnets, true, nil
}

// Subnets holds all the subscribed subnets of a specific node.
// The array index represents a subnet number,
// the value holds either 0 or 1 representing if the node is subscribed to the subnet number.
type Subnets [commons.SubnetsCount]byte

func (s Subnets) String() string {
	subnetsVec := bitfield.NewBitvector128()
	subnet := uint64(0)
	for _, val := range s {
		subnetsVec.SetBitAt(subnet, val > uint8(0))
		subnet++
	}
	return hex.EncodeToString(subnetsVec.Bytes())
}

// FromString parses a given subnet string
func (s Subnets) FromString(subnetsStr string) (Subnets, error) {
	subnetsStr = strings.Replace(subnetsStr, "0x", "", 1)
	var data []byte
	for i := 0; i+1 < len(subnetsStr); i += 2 {
		maskData1, err := getCharMask(string(subnetsStr[i]))
		if err != nil {
			return Subnets{}, err
		}
		maskData2, err := getCharMask(string(subnetsStr[i+1]))
		if err != nil {
			return Subnets{}, err
		}
		data = append(data, maskData2...)
		data = append(data, maskData1...)
	}

	if len(data) != commons.Subnets() {
		return Subnets{}, fmt.Errorf("invalid subnets length %d", len(data))
	}

	return Subnets(data), nil
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

// ToMap returns a map with all subnets and their values
func (s Subnets) ToMap() map[int]byte {
	m := make(map[int]byte, len(s))
	for subnet, v := range s {
		m[subnet] = v
	}
	return m
}

// SharedSubnets returns the shared subnets
func (s Subnets) SharedSubnets(other Subnets) []int {
	return s.SharedSubnetsN(other, 0)
}

func (s Subnets) SharedSubnetsN(other Subnets, n int) []int {
	var shared []int
	if n == 0 {
		n = len(s)
	}
	for subnet, v := range s {
		if v == 0 {
			continue
		}
		if other[subnet] == 0 {
			continue
		}
		shared = append(shared, subnet)
		if len(shared) == n {
			break
		}
	}
	return shared
}

// DiffSubnets returns a diff of the two given subnets.
// returns a map with all the different entries and their post change value
func (s Subnets) DiffSubnets(other Subnets) map[int]byte {
	diff := make(map[int]byte)
	for subnet, bval := range other {
		if aval := s[subnet]; aval != bval {
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
	var maskData []byte
	for j := 0; j < len(mask); j++ {
		val, err := strconv.ParseUint(string(mask[len(mask)-1-j]), 2, 8)
		if err != nil {
			return nil, err
		}
		maskData = append(maskData, uint8(val)) // nolint:gosec
	}
	return maskData, nil
}
