package metrics

import (
	"testing"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestProcessAgents(t *testing.T) {
	a1 := mockAgent{errs: []error{errors.New("dummy error"), errors.New("another error")}}
	a2 := mockAgent{errs: []error{}}
	a3 := mockAgent{errs: []error{errors.New("another error 3")}}

	errs := ProcessAgents([]HealthCheckAgent{&a1, &a2, &a3})
	require.Len(t, errs, 3)
}

type mockAgent struct {
	errs []error
}

func (ma *mockAgent) HealthCheck() []error {
	return ma.errs
}
