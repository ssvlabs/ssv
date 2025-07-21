package traces

import (
	"crypto/sha256"
	"encoding/hex"
	"log"
	"os"
	"testing"

	"github.com/brianvoe/gofakeit/v7"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"
)

func TestMain(m *testing.M) {
	if err := gofakeit.Seed(0); err != nil {
		log.Fatalf("failed to seed gofakeit: %v", err)
	}
	os.Exit(m.Run())
}

func TestShouldBuildDeterministicTraceIDWhenRandomInputProvided(t *testing.T) {
	traceIDInput := gofakeit.LetterN(gofakeit.UintN(128))

	traceContext := Context(t.Context(), traceIDInput)

	expectedSha := sha256.Sum256([]byte(traceIDInput))
	expectedTraceIDHex := hex.EncodeToString(expectedSha[:traceIDByteLen])
	require.Equal(t, expectedTraceIDHex, trace.SpanContextFromContext(traceContext).TraceID().String(), "TraceID mismatch for input: %s", traceIDInput)
}

func TestShouldBuildEqualTraceIDsWhenTwoIdenticalInputsProvided(t *testing.T) {
	traceIDInput := gofakeit.LetterN(gofakeit.UintN(128))

	traceContextOne := Context(t.Context(), traceIDInput)
	traceContextTwo := Context(t.Context(), traceIDInput)

	require.Equal(t,
		trace.SpanContextFromContext(traceContextOne).TraceID().String(),
		trace.SpanContextFromContext(traceContextTwo).TraceID().String())
}
