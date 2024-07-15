package spectest

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
)

var (
	DebugDumpState = false
	dumpDir        = "./debug"
)

func dumpState(t *testing.T,
	name string,
	expect spectypes.Root,
	actual spectypes.Root,
) string {
	a, err := json.Marshal(expect)
	require.NoError(t, err)
	b, err := json.Marshal(actual)
	require.NoError(t, err)

	// comparing jsons
	diff := cmp.Diff(a, b,
		cmp.FilterValues(func(x, y []byte) bool {
			return json.Valid(x) && json.Valid(y)
		}, cmp.Transformer("ParseJSON", func(in []byte) (out interface{}) {
			if err := json.Unmarshal(in, &out); err != nil {
				panic(err) // should never occur given previous filter to ensure valid JSON
			}
			return out
		}),
		),
	)

	if len(diff) > 0 && DebugDumpState {
		logJSON(t, fmt.Sprintf("test_%s_EXPECT", name), expect)
		logJSON(t, fmt.Sprintf("test_%s_ACTUAL", name), actual)
	}
	return diff
}

func logJSON(t *testing.T, name string, value interface{}) {
	bytes, err := json.Marshal(value)
	require.NoError(t, err)
	err = os.WriteFile(fmt.Sprintf("%s/%s_test_serialized.json", dumpDir, name), bytes, 0644)
	require.NoError(t, err)
}
