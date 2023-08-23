package discovery

import (
	"fmt"
	"strconv"

	"github.com/bloxapp/ssv/network/commons"
	"github.com/bloxapp/ssv/utils/format"
)

var (
	regPool            = format.NewRegexpPool("\\w+:bloxstaking\\.ssv\\.(\\d+)")
	errPatternMismatch = fmt.Errorf("pattern mismatch")
	errValueOutOfRange = fmt.Errorf("value out of range")
)

// nsToSubnet converts the given topic to subnet
func (dvs *DiscV5Service) nsToSubnet(ns string) (int, error) {
	r, done := regPool.Get()
	defer done()

	found := r.FindStringSubmatch(ns)
	if len(found) != 2 {
		return 0, errPatternMismatch
	}

	val, err := strconv.ParseUint(found[1], 10, 64)
	if err != nil {
		return 0, err
	}

	if val >= uint64(commons.Subnets()) {
		return 0, errValueOutOfRange
	}

	return int(val), nil
}

// isSubnet checks if the given string is a subnet string
func isSubnet(ns string) bool {
	r, done := regPool.Get()
	defer done()
	return r.MatchString(ns)
}
