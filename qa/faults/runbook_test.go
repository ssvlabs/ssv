package faults

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The runbook is what the tester actually reads during pass M3. A value in the code but not in the
// runbook is a fault nobody will run; a value in the runbook but not in the code fails startup.
func TestRunbookCoversEveryFault(t *testing.T) {
	body, err := os.ReadFile("../FAULTS.md")
	require.NoError(t, err)
	text := string(body)

	for _, e := range Menu() {
		require.Contains(t, text, "`"+string(e.Fault)+"`", "fault %q is missing from qa/FAULTS.md", e.Fault)
		require.Contains(t, text, e.Scenarios, "scenario IDs for %q are missing from qa/FAULTS.md", e.Fault)
	}
	require.Contains(t, strings.ToLower(text), "never merged")
}
