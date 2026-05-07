package consensustest

// BLS-realistic byte sizes for the herumi/bls-eth-go-binary library used by
// SSV (BLS12-381 G1 sigs, G2 pubkeys; spec/eth2 conventions). Stub signatures
// in adapters use these sizes so bandwidth measurements in stub mode match
// real-BLS mode — only signing/verification CPU cost differs, not wire size.
//
// Phase 8 (bandwidth instrumentation) will reference these when accounting
// for per-message byte counts; only StubSignatureSize is currently consumed
// (by the OBFT byz patterns that forge σ partial bytes).
const (
	StubSignatureSize = 96 // BLS12-381 G2 signature (compressed)
	StubPublicKeySize = 48 // BLS12-381 G1 public key (compressed)

	// IBE primitive sizes (tlock-style timelock encryption). Production uses
	// drand's age-encrypt-go IBE; the stub matches its envelope.
	StubIBECiphertextOverhead = 48 // IBE element + AEAD overhead per encrypted onion entry
)
