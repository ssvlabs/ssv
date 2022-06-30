package discovery

import (
	"github.com/bloxapp/ssv/utils/format"
	"strconv"
)

var regPool = format.NewRegexpPool("\\w+:bloxstaking\\.ssv\\.(\\d+)")

// nsToSubnet converts the given topic to subnet
// TODO: return other value than zero upon failure?
func nsToSubnet(ns string) int {
	r, done := regPool.Get()
	defer done()
	found := r.FindStringSubmatch(ns)
	if len(found) != 2 {
		return -1
	}
	val, err := strconv.ParseUint(found[1], 10, 64)
	if err != nil {
		return -1
	}
	return int(val)
}

// isSubnet checks if the given string is a subnet string
func isSubnet(ns string) bool {
	r, done := regPool.Get()
	defer done()
	return r.MatchString(ns)
}
