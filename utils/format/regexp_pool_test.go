package format

import (
	"fmt"
	"testing"
)

func TestNewRegexpPool(t *testing.T) {
	pool := NewRegexpPool("ssv\\.subnets\\.(\\d)")

	re1, done := pool.Get()
	defer done()
	fmt.Println(re1.FindStringSubmatch("ssv.subnets.1"))
}
