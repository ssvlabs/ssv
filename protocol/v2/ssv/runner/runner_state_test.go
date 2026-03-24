package runner

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStateUnmarshalJSONReturnsErrorWhenDutyMissing(t *testing.T) {
	input, err := json.Marshal(map[string]any{
		"Finished": true,
	})
	require.NoError(t, err)

	var state State
	err = state.UnmarshalJSON(input)

	require.EqualError(t, err, "no starting duty in state JSON")
}
