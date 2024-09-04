package spectest

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv-spec/types/testingutils"
	"github.com/stretchr/testify/require"
)

type RunnerConstructionSpecTest struct {
	Name      string
	Shares    map[phase0.ValidatorIndex]*types.Share
	RoleError map[types.RunnerRole]string
}

func (test *RunnerConstructionSpecTest) TestName() string {
	return "RunnerConstruction " + test.Name
}

func (test *RunnerConstructionSpecTest) Run(t *testing.T) {

	if len(test.RoleError) == 0 {
		panic("no roles")
	}

	for role, expectedError := range test.RoleError {
		// Construct runner and get construction error
		_, err := testingutils.ConstructBaseRunnerWithShareMap(role, test.Shares)

		// Check error
		if len(expectedError) > 0 {
			require.Error(t, err)
			require.Contains(t, err.Error(), expectedError)
		} else {
			require.NoError(t, err)
		}
	}
}

func (test *RunnerConstructionSpecTest) GetPostState() (interface{}, error) {
	return nil, nil
}
