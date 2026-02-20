//go:build !alan_spec

package qbft

import (
	"testing"

	spectests "github.com/ssvlabs/ssv-spec/qbft/spectest/tests"
)

func runRoundRobinSpecTest(t *testing.T, test *spectests.RoundRobinSpecTest) {
	test.Run(t)
}
