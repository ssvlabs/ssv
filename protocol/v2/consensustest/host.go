package consensustest

// HostPattern returns the host's valid/not-valid verdict for (operator,
// layer/round, V). Layer is OBFT's 0..K-1 / QBFT's 1.. round number.
type HostPattern interface {
	Validate(op OperatorID, layer int, v []byte) bool
}

// HostAllValid is the trivial all-valid host.
type HostAllValid struct{}

func (HostAllValid) Validate(_ OperatorID, _ int, _ []byte) bool { return true }

// HostInvalidForOperators returns false at the configured Layer for any
// op in Operators; useful for validity-divergence scenarios.
type HostInvalidForOperators struct {
	Layer     int
	Operators map[OperatorID]bool
}

func (h HostInvalidForOperators) Validate(op OperatorID, layer int, _ []byte) bool {
	if layer != h.Layer {
		return true
	}
	return !h.Operators[op]
}
