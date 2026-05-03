package validator

import (
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/drand/kyber"
	bls12381 "github.com/drand/kyber-bls12381"
	"github.com/drand/kyber/share"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/ssvsigner/ekm"

	tbftadapter "github.com/ssvlabs/ssv/protocol/v2/ssv/runner/tbft"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/validator"
	tbftcore "github.com/ssvlabs/ssv/protocol/v2/tbft"
	"github.com/ssvlabs/ssv/protocol/v2/tbft/blsbackend"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

// buildTBFTControllerForProposer constructs a TBFT Controller for the
// proposer duty of `share`. The IBE wiring is selected by the
// IBEUseOptionB toggle (see ibe_option.go):
//
//   - Option A (default): the validator's existing herumi BLS share
//     serves as the IBE source via the DST trick. KyberSigner consumes
//     the validator share bytes; ClusterPubKey is the validator
//     pubkey. No DKG runs; IBEPubKeyShares is nil (Phase E5
//     verification is not applicable — there's no separate IBE
//     polynomial to evaluate per operator).
//   - Option B: the cluster's per-DKG IBE share material established
//     by the orchestrator (Phases E1-E2). KyberSigner consumes the
//     IBE share; ClusterPubKey is the cluster IBE pubkey;
//     IBEPubKeyShares is computed from polyCommits for observe-time
//     verification.
//
// Returns (nil, nil) if the signer does not expose the share material
// the active mode requires (typical for remote-signing setups; FW1 in
// docs/TBFT-DKG-TASKS.md). The caller skips the proposer runner.
//
// Returns a non-nil error only on Controller construction failure
// (malformed inputs, etc.).
func buildTBFTControllerForProposer(
	ssvShare *ssvtypes.SSVShare,
	operator *spectypes.CommitteeMember,
	options *validator.CommonOptions,
) (*tbftadapter.Controller, error) {
	shareProvider, ok := options.Signer.(ekm.ShareBytesProvider)
	if !ok {
		return nil, nil
	}
	shareBytes, err := shareProvider.GetShareBytes(phase0.BLSPubKey(ssvShare.SharePubKey))
	if err != nil {
		return nil, nil
	}

	clusterID := ssvShare.CommitteeID()
	pubKeyShares := make(map[tbftcore.OperatorID][]byte, len(ssvShare.Committee))
	committee := make([]spectypes.OperatorID, 0, len(ssvShare.Committee))
	for _, m := range ssvShare.Committee {
		pubKeyShares[tbftcore.OperatorID(m.Signer)] = append([]byte(nil), m.SharePubKey...)
		committee = append(committee, m.Signer)
	}

	opts := tbftadapter.ControllerOptions{
		OperatorID:   operator.OperatorID,
		Committee:    committee,
		ClusterID:    clusterID,
		PubKeyShares: pubKeyShares,
		Signer:       blsbackend.New(shareBytes),
		IBE:          blsbackend.NewTLockIBE(),
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
		// trick (docs/IBE-INTEGRATION.md). The validator pubkey is the
		// IBE trust anchor; per-NR-partial verification is not wired
		// up (no separate IBE polynomial to evaluate).
		opts.ClusterPubKey = append([]byte(nil), ssvShare.ValidatorPubKey[:]...)
		opts.TagSigner = blsbackend.NewKyberSigner(shareBytes)
	}

	ctrl, err := tbftadapter.NewController(opts)
	if err != nil {
		return nil, fmt.Errorf("build TBFT controller: %w", err)
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
func computeIBEPubKeyShares(polyCommits [][]byte, committee []spectypes.OperatorID) (map[tbftcore.OperatorID][]byte, error) {
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

	out := make(map[tbftcore.OperatorID][]byte, len(committee))
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
		out[tbftcore.OperatorID(opID)] = b
	}
	return out, nil
}
