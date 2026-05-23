package obft

// Wire-level size and count caps shared by both OBFT-family wire codecs
// (base/wire and twoab/wire). Reconciled to the tighter, per-field values:
// each cap is sized to the realistic maximum of its field type with a healthy
// multiplicative margin, and the decoder rejects any field exceeding its cap
// before allocating — bounding the unbounded-allocation surface a malformed
// message can exercise before the protocol layer ever sees it.
//
// Choosing per-field bounds (rather than one coarse global cap) tightens the
// realistic upper bound on a retained message's size — relevant because a
// byzantine's first distinct Commit is deep-copied and retained for the slot.

// MaxLayers caps the number of layers a message can declare on the wire. Real
// OBFT-family configs use K ≤ n ≤ 13 in SSV; anything past 32 is almost
// certainly malformed/malicious. Valid layer indices are [0, MaxLayers).
const MaxLayers = 32

const (
	// MaxValueSize caps proposer-duty candidate values (Phase1Bundle.Value,
	// EncryptedLayer.Value, Certificate.Value). Real beacon-block-V3 with
	// EIP-4844 blob commitments lands ~1–2 KB; future scaling (PeerDAS, larger
	// blocks) may grow this. 1 MiB gives ~500× margin.
	MaxValueSize = 1 * 1024 * 1024

	// MaxSignatureSize caps BLS partial / aggregate signatures (leader σ
	// witnesses, NR partials, certificate signatures). Real BLS12-381
	// signatures are 96 B; 1 KiB is ~10× margin against future scheme variants.
	MaxSignatureSize = 1 * 1024

	// MaxCiphertextSize caps chained-IBE-wrapped σ partials in commit-layer
	// onions. Inner plaintext is a ~96 B BLS partial; chained-IBE wrapping at
	// layer k applies k encryptions (~300 B overhead each), so at K ≤ MaxLayers
	// the worst case is ≈ 10 KiB; 64 KiB gives ~6× margin.
	MaxCiphertextSize = 64 * 1024
)
