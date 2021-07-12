package metrics

import (
	"fmt"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestCollect(t *testing.T) {
	var records1 []string
	for i := 0; i < 10; i++ {
		records1 = append(records1, fmt.Sprintf("mc1{} %d", i))
	}
	mc1 := mockCollector{records1, "mockCollector1"}
	var records2 []string
	for i := 0; i < 10; i++ {
		records2 = append(records2, fmt.Sprintf("mc2{} %d", i))
	}
	mc2 := mockCollector{records2, "mockCollector2"}

	Register(&mc1)
	defer Deregister(&mc1)
	Register(&mc2)
	defer Deregister(&mc2)

	results, errs := Collect()
	require.Equal(t, 0, len(errs))
	require.Equal(t, 20, len(results))
}

func TestCollect_FailedCollectors(t *testing.T) {
	var records1 []string
	for i := 0; i < 10; i++ {
		records1 = append(records1, fmt.Sprintf("mc1{} %d", i))
	}
	mc := mockCollector{records1, "mockCollector1"}
	expectedErr := errors.New("failedCollectorErr")
	fc := failedCollector{expectedErr}
	Register(&mc)
	defer Deregister(&mc)
	Register(&fc)
	defer Deregister(&fc)

	results, errs := Collect()
	require.Equal(t, 1, len(errs))
	require.Equal(t, expectedErr, errs[0])
	require.Equal(t, 10, len(results))
}

type mockCollector struct {
	results []string
	id      string
}

func (c *mockCollector) ID() string {
	return c.id
}

func (c *mockCollector) Collect() ([]string, error) {
	return c.results, nil
}

type failedCollector struct {
	err error
}

func (c *failedCollector) ID() string {
	return "failedCollector"
}

func (c *failedCollector) Collect() ([]string, error) {
	if c.err != nil {
		return nil, c.err
	}
	return []string{}, nil
}
