package commons

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestSetBuildData verifies that SetBuildData correctly sets the application name and version,
// and that GetBuildData returns the properly formatted string in the format "app:version".
func TestSetBuildData(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		app      string
		version  string
		expected string
	}{
		{
			name:     "set custom app and version",
			app:      appName,
			version:  version,
			expected: fmt.Sprintf("%s:%s", appName, version),
		},
		{
			name:     "set empty values",
			app:      "",
			version:  "",
			expected: ":",
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			SetBuildData(tt.app, tt.version)
			assert.Equal(t, tt.expected, GetBuildData())
		})
	}
}

// TestGetNodeVersion verifies that GetNodeVersion correctly returns the version portion
// of the build data after it has been set via SetBuildData.
func TestGetNodeVersion(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		version  string
		expected string
	}{
		{
			name:     "get custom version",
			version:  "1.0.0",
			expected: "1.0.0",
		},
		{
			name:     "get empty version",
			version:  "",
			expected: "",
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			SetBuildData("SSV-Node", tt.version)
			assert.Equal(t, tt.expected, GetNodeVersion())
		})
	}
}
