package validator

import (
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/drand/kyber"
	bls12381 "github.com/drand/kyber-bls12381"
	"github.com/drand/kyber/share"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/ssvsigner/ekm"

	obftcore "github.com/ssvlabs/ssv/protocol/v2/obft/base"
	"github.com/ssvlabs/ssv/protocol/v2/obft/blsbackend"
	obftadapter "github.com/ssvlabs/ssv/protocol/v2/ssv/runner/obft"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/validator"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

// buildOBFTControllerForProposer constructs a OBFT Controller for the
// proposer duty of `share`. The IBE wiring is selected by the
// IBEUseOptionB toggle (see ibe_option.go):
//
//   - Option A (default): the validator's existing herumi BLS share
//     serves as the IBE source via the DST trick. KyberSigner consumes
//     the validator share bytes; ClusterPubKey is the validator
//     pubkey. No DKG runs; IBEPubKeyShares is nil (per-operator NR
//     verification is not applicable — there's no separate IBE
//     polynomial to evaluate per operator).
//   - Option B: the cluster's per-DKG IBE share material established
//     by the orchestrator. KyberSigner consumes the
//     IBE share; ClusterPubKey is the cluster IBE pubkey;
//     IBEPubKeyShares is computed from polyCommits for observe-time
//     verification.
//
// Returns (nil, nil) if the signer does not expose the share material
// the active mode requires (typical for remote-signing setups). The
// caller skips the proposer runner.
//
// Returns a non-nil error only on Controller construction failure
// (malformed inputs, etc.).
func buildOBFTControllerForProposer(
	ssvShare *ssvtypes.SSVShare,
	operator *spectypes.CommitteeMember,
	options *validator.CommonOptions,
) (*obftadapter.Controller, error) {
	shareProvider, ok := options.Signer.(ekm.ShareBytesProvider)
	if !ok {
		return nil, nil
	}
	shareBytes, err := shareProvider.GetShareBytes(phase0.BLSPubKey(ssvShare.SharePubKey))
	if err != nil {
		return nil, nil
	}

	clusterID := ssvShare.CommitteeID()
	pubKeyShares := make(map[obftcore.OperatorID][]byte, len(ssvShare.Committee))
	committee := make([]spectypes.OperatorID, 0, len(ssvShare.Committee))
	for _, m := range ssvShare.Committee {
		pubKeyShares[obftcore.OperatorID(m.Signer)] = append([]byte(nil), m.SharePubKey...)
		committee = append(committee, m.Signer)
	}

	// Production proposer-duty config: K = f+1 (BFT-liveness minimum) per
	// spec §Setting / §Application — the recommended default post-K=f+1
	// flip. At n=4 (f=1) this resolves to K=2 (matches DefaultK=2); at
	// larger n it scales: n=7 → K=3, n=10 → K=4, n=13 → K=5. Trade-off:
	// no late-leader-resilience backstop at L_{K-1} (a single late-broadcasting
	// honest leader can foreclose the slot); deployments preferring deeper
	// fall-through can override DefaultK or pass ConfigOverrides.K explicitly.
	// BroadcastBudget must be built with the SAME K so its length matches
	// ConfigForCluster's expectation.
	//
	// Primary-vs-backup schedule (spec §Setting): B_0 = 2·BTT + RefloodDelay
	// for the MEV-fresh primary; B_1..B_{K-1} = T_commit for backups
	// (broadcast at BFT_start with deepest-confirmed-parent fetch). At
	// production defaults (BTT=200ms, RefloodDelay=700ms, T_commit=3400ms),
	// K=2: [1100, 3400]ms. Larger K extends backups uniformly:
	// K=3: [1100, 3400, 3400]ms; K=4: [1100, 3400, 3400, 3400]ms.
	n := len(ssvShare.Committee)
	f := (n - 1) / 3
	K := obftadapter.DefaultK
	if minK := f + 1; K < minK {
		K = minK
	}
	broadcastBudget, err := obftadapter.DefaultBroadcastBudgetSchedule(K, obftadapter.DefaultBTT, obftadapter.DefaultRefloodDelay, obftadapter.DefaultTCommit)
	if err != nil {
		return nil, fmt.Errorf("build OBFT broadcast budget: %w", err)
	}
	overrides := &obftadapter.ConfigOverrides{
		K:               K,
		BroadcastBudget: broadcastBudget,
	}

	// Wrap the V-side BLS signer so OBFT partials are computed over the
	// block's proposer-domain signing root (the OBFT-aggregated signature
	// is then directly usable as a beacon-block proposer signature). The
	// IBE TagSigner stays unwrapped — it signs raw nr_tag bytes, not blocks.
	vSigner, err := obftadapter.NewProposerSigner(blsbackend.New(shareBytes), options.NetworkConfig.Beacon)
	if err != nil {
		return nil, fmt.Errorf("build proposer signing-root signer: %w", err)
	}

	opts := obftadapter.ControllerOptions{
		OperatorID:       operator.OperatorID,
		Committee:        committee,
		ClusterID:        clusterID,
		PubKeyShares:     pubKeyShares,
		Signer:           vSigner,
		IBE:              blsbackend.NewTLockIBE(),
		Overrides:        overrides,
		EvidenceObserver: makeOBFTEvidenceObserver(operator.OperatorID, clusterID),
	}

	if IBEUseOptionB {
		ibeProvider, ok := options.Signer.(ekm.IBEShareBytesProvider)
		if !ok {
			return nil, nil
		}
		ibeShareBytes, err := ibeProvider.GetIBEShareBytes(clusterID)
		if err != nil {
			// DKG hasn't established an IBE share for this cluster yet
			// (the orchestrator's EnsureClusterIBE hook in onShareInit
			// should have run before this point — bail out rather than
			// building a half-wired runner).
			return nil, nil
		}
		clusterIBEPubKey, err := ibeProvider.GetClusterIBEPubKey(clusterID)
		if err != nil {
			return nil, nil
		}
		polyCommits, err := ibeProvider.GetClusterIBEPolyCommits(clusterID)
		if err != nil {
			return nil, nil
		}
		ibePubKeyShares, err := computeIBEPubKeyShares(polyCommits, committee)
		if err != nil {
			return nil, fmt.Errorf("compute IBE pubkey shares: %w", err)
		}
		opts.ClusterPubKey = clusterIBEPubKey
		opts.TagSigner = blsbackend.NewKyberSigner(ibeShareBytes)
		opts.IBEPubKeyShares = ibePubKeyShares
	} else {
		// Option A: validator share doubles as IBE share via the DST
		// trick. The validator pubkey is the
		// IBE trust anchor; per-NR-partial verification is not wired
		// up (no separate IBE polynomial to evaluate).
		opts.ClusterPubKey = append([]byte(nil), ssvShare.ValidatorPubKey[:]...)
		opts.TagSigner = blsbackend.NewKyberSigner(shareBytes)
	}

	ctrl, err := obftadapter.NewController(opts)
	if err != nil {
		return nil, fmt.Errorf("build OBFT controller: %w", err)
	}
	return ctrl, nil
}

// computeIBEPubKeyShares evaluates the cluster's IBE polynomial at each
// operator's index to produce per-operator IBE pubkey shares used for
// observe-time NonReceipt-attestation verification.
//
// Kyber's PubPoly.Eval(idx) computes the polynomial at x = 1 + idx, so
// using idx = opID - 1 places each operator's pubkey share at x = opID
// — matching the Lagrange x-coordinates KyberSigner uses (operator
// IDs). Aligns with protocol/v2/dkg/coordinator.go's buildNodes choice.
//
// Used only under Option B (see IBEUseOptionB).
func computeIBEPubKeyShares(polyCommits [][]byte, committee []spectypes.OperatorID) (map[obftcore.OperatorID][]byte, error) {
	if len(polyCommits) == 0 {
		return nil, fmt.Errorf("empty polyCommits")
	}
	g1 := bls12381.NewBLS12381Suite().G1()
	commits := make([]kyber.Point, len(polyCommits))
	for i, b := range polyCommits {
		pt := g1.Point()
		if err := pt.UnmarshalBinary(b); err != nil {
			return nil, fmt.Errorf("unmarshal polyCommits[%d]: %w", i, err)
		}
		commits[i] = pt
	}
	pp := share.NewPubPoly(g1, nil, commits)

	out := make(map[obftcore.OperatorID][]byte, len(committee))
	for _, opID := range committee {
		if opID == 0 {
			return nil, fmt.Errorf("operator id 0 not supported")
		}
		// idx = opID - 1; PubPoly.Eval(idx) returns share at x = 1 + idx = opID.
		s := pp.Eval(int(opID) - 1) //nolint:gosec // bounded by SSV committee sizes
		b, err := s.V.MarshalBinary()
		if err != nil {
			return nil, fmt.Errorf("marshal IBE pubkey share for op %d: %w", opID, err)
		}
		out[obftcore.OperatorID(opID)] = b
	}
	return out, nil
}

// makeOBFTEvidenceObserver returns an EvidenceObserver that logs each
// FIRST observation per (Rule, OperatorID, Layer) tuple at WARN level.
//
// Per spec §Slashing evidence, honest operators MUST log observed
// evidence per-rule for later out-of-band aggregation; log format is
// implementation-defined. The manual-blacklist mechanism (planned
// protocol extension) is the canonical consumer; operators monitor logs
// out-of-band until that lands.
//
// Uses zap.L() (the global logger) — matching the pattern at
// proposer_obft.go pending-envelope replay error logging. The observer
// is wired once per Controller (i.e., once per validator's proposer
// duty) and reused for every per-slot Instance.
func makeOBFTEvidenceObserver(opID spectypes.OperatorID, clusterID [32]byte) obftcore.EvidenceObserver {
	return func(e obftcore.Evidence) {
		zap.L().Warn("OBFT slashing-evidence observed (spec §Slashing-evidence MUST-log rule)",
			zap.String("rule", e.Rule.String()),
			zap.Uint64("local_operator_id", uint64(opID)),
			zap.String("cluster_id", fmt.Sprintf("%x", clusterID)),
			zap.Uint64("byz_operator_id", uint64(e.OperatorID)),
			zap.Int("layer", e.Layer),
		)
	}
}
