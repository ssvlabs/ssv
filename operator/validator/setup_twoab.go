package validator

import (
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/ssvsigner/ekm"

	"github.com/ssvlabs/ssv/protocol/v2/obft/blsbackend"
	twoabcore "github.com/ssvlabs/ssv/protocol/v2/obft/twoab"
	twoabadapter "github.com/ssvlabs/ssv/protocol/v2/ssv/runner/obft/twoab"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/validator"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

// buildTwoabControllerForProposer constructs a 2abOBFT Controller for the
// proposer duty of `share`. It is the sibling of buildOBFTControllerForProposer
// (setup_obft.go), selected when UseTwoabOBFT is set; the crypto wiring (share
// extraction, Option-A/B IBE, evidence logging) is identical, only the adapter
// types and the timing-override shape differ — 2abOBFT derives its
// per-layer BroadcastBudget / FetchAt internally from BTT + TPhase2a, so the
// overrides carry only K.
//
// Returns (nil, nil) if the signer doesn't expose the share material the active
// IBE mode requires (typical for remote-signing setups); the caller then skips
// the proposer runner. Returns a non-nil error only on construction failure.
func buildTwoabControllerForProposer(
	ssvShare *ssvtypes.SSVShare,
	operator *spectypes.CommitteeMember,
	options *validator.CommonOptions,
) (*twoabadapter.Controller, error) {
	shareProvider, ok := options.Signer.(ekm.ShareBytesProvider)
	if !ok {
		return nil, nil
	}
	shareBytes, err := shareProvider.GetShareBytes(phase0.BLSPubKey(ssvShare.SharePubKey))
	if err != nil {
		return nil, nil
	}

	clusterID := ssvShare.CommitteeID()
	pubKeyShares := make(map[twoabcore.OperatorID][]byte, len(ssvShare.Committee))
	committee := make([]spectypes.OperatorID, 0, len(ssvShare.Committee))
	for _, m := range ssvShare.Committee {
		pubKeyShares[twoabcore.OperatorID(m.Signer)] = append([]byte(nil), m.SharePubKey...)
		committee = append(committee, m.Signer)
	}

	// Production proposer-duty K = f+1 (BFT-liveness minimum), matching the OBFT
	// builder. 2abOBFT auto-derives BroadcastBudget / FetchAt from BTT + the
	// derived TPhase2a (ConfigForCluster), so unlike OBFT no explicit budget
	// schedule is passed — just K.
	n := len(ssvShare.Committee)
	f := (n - 1) / 3
	K := twoabadapter.DefaultK
	if minK := f + 1; K < minK {
		K = minK
	}
	overrides := &twoabadapter.ConfigOverrides{K: K}

	// Wrap the V-side BLS signer so 2abOBFT σ partials are computed over the
	// block's proposer-domain signing root. The IBE TagSigner stays unwrapped —
	// it signs raw nr_tag bytes, not blocks.
	vSigner, err := twoabadapter.NewProposerSigner(blsbackend.New(shareBytes), options.NetworkConfig.Beacon)
	if err != nil {
		return nil, fmt.Errorf("build 2abOBFT proposer signing-root signer: %w", err)
	}

	opts := twoabadapter.ControllerOptions{
		OperatorID:       operator.OperatorID,
		Committee:        committee,
		ClusterID:        clusterID,
		PubKeyShares:     pubKeyShares,
		Signer:           vSigner,
		IBE:              blsbackend.NewTLockIBE(),
		Overrides:        overrides,
		EvidenceObserver: makeTwoabEvidenceObserver(operator.OperatorID, clusterID),
	}

	if IBEUseOptionB {
		ibeProvider, ok := options.Signer.(ekm.IBEShareBytesProvider)
		if !ok {
			return nil, nil
		}
		ibeShareBytes, err := ibeProvider.GetIBEShareBytes(clusterID)
		if err != nil {
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
		// computeIBEPubKeyShares returns map[obftcore.OperatorID][]byte, which is
		// the same aliased type as map[twoabcore.OperatorID][]byte (both
		// obft.OperatorID), so it is reused directly across variants.
		ibePubKeyShares, err := computeIBEPubKeyShares(polyCommits, committee)
		if err != nil {
			return nil, fmt.Errorf("compute IBE pubkey shares: %w", err)
		}
		opts.ClusterPubKey = clusterIBEPubKey
		opts.TagSigner = blsbackend.NewKyberSigner(ibeShareBytes)
		opts.IBEPubKeyShares = ibePubKeyShares
	} else {
		// Option A: validator share doubles as IBE share via the DST trick.
		opts.ClusterPubKey = append([]byte(nil), ssvShare.ValidatorPubKey[:]...)
		opts.TagSigner = blsbackend.NewKyberSigner(shareBytes)
	}

	ctrl, err := twoabadapter.NewController(opts)
	if err != nil {
		return nil, fmt.Errorf("build 2abOBFT controller: %w", err)
	}
	return ctrl, nil
}

// makeTwoabEvidenceObserver mirrors makeOBFTEvidenceObserver for 2abOBFT's
// evidence type: logs each first observation per (Rule, OperatorID, Layer) at
// WARN level for out-of-band aggregation (spec §Slashing evidence MUST-log).
func makeTwoabEvidenceObserver(opID spectypes.OperatorID, clusterID [32]byte) twoabcore.EvidenceObserver {
	return func(e twoabcore.Evidence) {
		zap.L().Warn("2abOBFT slashing-evidence observed (spec §Slashing-evidence MUST-log rule)",
			zap.String("rule", e.Rule.String()),
			zap.Uint64("local_operator_id", uint64(opID)),
			zap.String("cluster_id", fmt.Sprintf("%x", clusterID)),
			zap.Uint64("byz_operator_id", uint64(e.OperatorID)),
			zap.Int("layer", e.Layer),
		)
	}
}
