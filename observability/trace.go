package observability

import (
	"crypto/sha256"
	"encoding/hex"

	"go.opentelemetry.io/otel/trace"
)

const traceIDByteLen = 16

func TraceID(str string) (trace.TraceID, error) {
	traceStrSha := sha256.Sum256([]byte(str))

	var traceID trace.TraceID
	traceID, err := trace.TraceIDFromHex(hex.EncodeToString(traceStrSha[:traceIDByteLen]))
	if err != nil {
		return trace.TraceID{}, err
	}

	return traceID, nil
}
