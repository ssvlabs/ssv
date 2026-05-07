package consensustest

// ByzPattern is the framework-level abstract byzantine pattern. Per-protocol
// adapters translate Kind + params to their internal byz model; kinds an
// adapter can't faithfully translate cause Run to return ErrNotApplicable.
type ByzPattern struct {
	Kind       ByzKind
	PrimaryByz OperatorID
	// Recipients carries operator IDs for selective-delivery patterns. For
	// split-delivery (e.g. ByzEquivocateSigmaLockedSplit), index 0 receives V_a
	// and index 1 receives V_b; missing indices fall back to default IDs (2/3).
	Recipients []OperatorID
	K          int // for ByzMultiSilent: how many top leaders are silent
	Layer      int // for layer-targeted patterns (e.g. ByzFakeEncryptedPresence)
}

// ByzKind enumerates the abstract byz behaviors. Add new kinds at the end
// to keep numeric values stable. Per-kind protocol behavior is documented in
// the catalog scenario notes.
type ByzKind int

const (
	ByzNone                       ByzKind = iota // all-honest baseline
	ByzSilentLeader                              // primary leader suppresses their candidate
	ByzMultiSilent                               // top K leaders silent (set ByzPattern.K)
	ByzEquivocate111                             // primary delivers a distinct V to each honest
	ByzEquivocateAllNR                           // primary floods both V's to all honest
	ByzEquivocateSigmaLockedSplit                // primary delivers V_a to one, V_b to another, ∅ to rest
	ByzHV1SelectiveDelivery                      // OBFT-specific: V to exactly one honest near horizon
	ByzFakeEncryptedPresence                     // OBFT-specific: silent at L_0, garbage bytes at L_k>0
	ByzSigmaRefusal                              // byz never contributes σ / never NRs
)

// String returns a stable human-readable name for telemetry.
func (k ByzKind) String() string {
	switch k {
	case ByzNone:
		return "None"
	case ByzSilentLeader:
		return "SilentLeader"
	case ByzMultiSilent:
		return "MultiSilent"
	case ByzEquivocate111:
		return "Equivocate111"
	case ByzEquivocateAllNR:
		return "EquivocateAllNR"
	case ByzEquivocateSigmaLockedSplit:
		return "EquivocateSigmaLockedSplit"
	case ByzHV1SelectiveDelivery:
		return "HV1SelectiveDelivery"
	case ByzFakeEncryptedPresence:
		return "FakeEncryptedPresence"
	case ByzSigmaRefusal:
		return "SigmaRefusal"
	default:
		return "Unknown"
	}
}
