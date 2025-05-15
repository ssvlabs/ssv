package observability

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/brianvoe/gofakeit/v7"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"
)

func TestShouldBuildDeterministicTraceIDWhenRandomInputProvided(t *testing.T) {
	gofakeit.Seed(0)
	traceIDInput := gofakeit.LetterN(gofakeit.UintN(128))

	traceContext := TraceContext(t.Context(), traceIDInput)

	expectedSha := sha256.Sum256([]byte(traceIDInput))
	expectedTraceIDHex := hex.EncodeToString(expectedSha[:traceIDByteLen])
	require.Equal(t, expectedTraceIDHex, trace.SpanContextFromContext(traceContext).TraceID().String(), "TraceID mismatch for input: %s", traceIDInput)
}
