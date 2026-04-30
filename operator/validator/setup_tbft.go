package validator

import (
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/ssvsigner/ekm"

	tbftadapter "github.com/ssvlabs/ssv/protocol/v2/ssv/runner/tbft"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/validator"
	tbftcore "github.com/ssvlabs/ssv/protocol/v2/tbft"
	"github.com/ssvlabs/ssv/protocol/v2/tbft/blsbackend"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

// buildTBFTControllerForProposer constructs a TBFT Controller for the
// proposer duty of `share`, using `options.Signer` as the source of
// raw share material (Option A — see docs/IBE-INTEGRATION.md and
// docs/TASKS.md).
//
// Returns (nil, nil) if the signer does not expose share bytes (typical
// for remote-signing setups) or if the share is not yet registered with
// the local signer. The caller treats this as "TBFT not available for
// this validator on this node" and falls back to QBFT.
//
// Returns a non-nil error only on Controller construction failure
// (malformed inputs, etc.).
func buildTBFTControllerForProposer(
	share *ssvtypes.SSVShare,
	operator *spectypes.CommitteeMember,
	options *validator.CommonOptions,
) (*tbftadapter.Controller, error) {
	provider, ok := options.Signer.(ekm.ShareBytesProvider)
	if !ok {
		// Remote signer: share material isn't on this node. The TBFT
		// path needs raw share bytes for the BLSSigner / KyberSigner
		// constructors, so the validator runs on QBFT instead.
		return nil, nil
	}

	shareBytes, err := provider.GetShareBytes(phase0.BLSPubKey(share.SharePubKey))
	if err != nil {
		// Share not registered with the local signer (e.g. node was
		// started after the share was added in a previous process and
		// AddShare hasn't been replayed in this process). Treat as
		// "TBFT not available for this validator" rather than fatal —
		// the QBFT path still works.
		return nil, nil
	}

	// Build the cluster's pubkey-shares map (operator ID → share public
	// key bytes). Each share member's SharePubKey IS its operator
	// share's BLS public key.
	pubKeyShares := make(map[tbftcore.OperatorID][]byte, len(share.Committee))
	committee := make([]spectypes.OperatorID, 0, len(share.Committee))
	for _, m := range share.Committee {
		pubKeyShares[tbftcore.OperatorID(m.Signer)] = append([]byte(nil), m.SharePubKey...)
		committee = append(committee, m.Signer)
	}

	clusterID := share.CommitteeID()

	ctrl, err := tbftadapter.NewController(tbftadapter.ControllerOptions{
		OperatorID:    operator.OperatorID,
		Committee:     committee,
		ClusterID:     clusterID, // [32]byte by definition
		ClusterPubKey: append([]byte(nil), share.ValidatorPubKey[:]...),
		PubKeyShares:  pubKeyShares,
		// Two share-bound signers consuming the same share — see the
		// DST-trick rationale in docs/IBE-INTEGRATION.md.
		Signer:    blsbackend.New(shareBytes),
		TagSigner: blsbackend.NewKyberSigner(shareBytes),
		IBE:       blsbackend.NewTLockIBE(),
	})
	if err != nil {
		return nil, fmt.Errorf("build TBFT controller: %w", err)
	}
	return ctrl, nil
}
