package records

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/go-bitfield"
)

const (
	// ZeroSubnets is the representation of no subnets
	ZeroSubnets = "00000000000000000000000000000000"
	// AllSubnets is the representation of all subnets
	AllSubnets = "ffffffffffffffffffffffffffffffff"
)

// UpdateSubnets updates subnets entry according to the given changes.
// count is the amount of subnets, in case that the entry doesn't exist as we want to initialize it
func UpdateSubnets(node *enode.LocalNode, count int, added []int, removed []int) ([]byte, error) {
	subnets, err := GetSubnetsEntry(node.Node().Record())
	if err != nil && !errors.Is(err, ErrEntryNotFound) {
		return nil, errors.Wrap(err, "could not read subnets entry")
	}
	orig := make([]byte, len(subnets))
	if len(subnets) == 0 { // not exist, creating slice
		subnets = make([]byte, count)
	} else {
		copy(orig, subnets)
	}
	for _, i := range added {
		subnets[i] = 1
	}
	for _, i := range removed {
		subnets[i] = 0
	}
	if bytes.Equal(orig, subnets) {
		return nil, nil
	}
	if err := SetSubnetsEntry(node, subnets); err != nil {
		return nil, errors.Wrap(err, "could not update subnets entry")
	}
	return subnets, nil
}

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
	for subnet, val := range s {
		subnetsVec.SetBitAt(uint64(subnet), val > uint8(0))
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

func (s Subnets) Active() int {
	var active int
	for _, val := range s {
		if val > 0 {
			active++
		}
	}
	return active
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
	var maskData []byte
	for j := 0; j < len(mask); j++ {
		val, err := strconv.ParseUint(string(mask[len(mask)-1-j]), 2, 8)
		if err != nil {
			return nil, err
		}
		maskData = append(maskData, uint8(val))
	}
	return maskData, nil
}
