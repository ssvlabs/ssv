package cliflag

import (
	"fmt"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAddPersistentStringFlag verifies that AddPersistentStringFlag correctly adds string flags
// to a cobra.Command with the proper default value, description format, and required status.
func TestAddPersistentStringFlag(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		flag        string
		value       string
		description string
		isRequired  bool
	}{
		{
			name:        "optional flag",
			flag:        "test-flag",
			value:       "default",
			description: "test description",
			isRequired:  false,
		},
		{
			name:        "required flag",
			flag:        "required-flag",
			value:       "default",
			description: "required description",
			isRequired:  true,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cmd := &cobra.Command{}
			AddPersistentStringFlag(cmd, tt.flag, tt.value, tt.description, tt.isRequired)
			flag := cmd.PersistentFlags().Lookup(tt.flag)

			require.NotNil(t, flag)
			assert.Equal(t, tt.value, flag.DefValue)

			expectedUsage := tt.description
			if tt.isRequired {
				expectedUsage += " (required)"
			}

			assert.Equal(t, expectedUsage, flag.Usage)
		})
	}
}

// TestAddPersistentIntFlag verifies that AddPersistentIntFlag correctly adds uint64 flags
// to a cobra.Command with the proper default value, description format, and required status.
func TestAddPersistentIntFlag(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		flag        string
		value       uint64
		description string
		isRequired  bool
	}{
		{
			name:        "optional flag",
			flag:        "test-flag",
			value:       42,
			description: "test description",
			isRequired:  false,
		},
		{
			name:        "required flag",
			flag:        "required-flag",
			value:       100,
			description: "required description",
			isRequired:  true,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cmd := &cobra.Command{}
			AddPersistentIntFlag(cmd, tt.flag, tt.value, tt.description, tt.isRequired)
			flag := cmd.PersistentFlags().Lookup(tt.flag)

			require.NotNil(t, flag)
			assert.Equal(t, fmt.Sprintf("%d", tt.value), flag.DefValue)

			expectedUsage := tt.description
			if tt.isRequired {
				expectedUsage += " (required)"
			}

			assert.Equal(t, expectedUsage, flag.Usage)
		})
	}
}
