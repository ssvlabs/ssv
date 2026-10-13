package goclient

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"net/http"
	"time"

	"github.com/attestantio/go-eth2-client/api"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	"golang.org/x/sync/errgroup"

	spectypes "github.com/ssvlabs/ssv-spec/types"
)

// IsSyncCommitteeAggregator reports whether the given selection proof makes the validator
// a sync-committee aggregator.
func (gc *GoClient) IsSyncCommitteeAggregator(proof []byte) bool {
	hash := sha256.Sum256(proof)

	cfg := gc.BeaconConfig()

	// as per spec: https://github.com/ethereum/consensus-specs/blob/dev/specs/altair/validator.md#aggregation-selection
	modulo := cfg.SyncCommitteeSize / cfg.SyncCommitteeSubnetCount / cfg.TargetAggregatorsPerSyncSubcommittee
	if modulo == uint64(0) {
		// Modulo must be at least 1.
		modulo = 1
	}
	return binary.LittleEndian.Uint64(hash[:8])%modulo == 0
}

// SyncCommitteeSubnetID returns the sync-committee subnet ID for the given subcommittee index.
func (gc *GoClient) SyncCommitteeSubnetID(index phase0.CommitteeIndex) uint64 {
	return uint64(index) / (gc.BeaconConfig().SyncCommitteeSize / gc.BeaconConfig().SyncCommitteeSubnetCount)
}

// GetSyncCommitteeContribution fetches one contribution per requested subnet, each paired
// with its selection proof, all for the head block root as of the duty slot.
func (gc *GoClient) GetSyncCommitteeContribution(
	ctx context.Context,
	slot phase0.Slot,
	selectionProofs []phase0.BLSSignature,
	subnetIDs []uint64,
) (ssz.Marshaler, spec.DataVersion, error) {
	if len(selectionProofs) != len(subnetIDs) {
		return nil, DataVersionNil, fmt.Errorf("mismatching number of selection proofs and subnet IDs")
	}

	if err := gc.waitIntoSlot(ctx, slot, 1); err != nil {
		return nil, DataVersionNil, fmt.Errorf("wait for sync message deadline: %w", err)
	}

	// Resolve the contribution root from head rather than by slot: sync-committee messages
	// sign the head root as seen during the duty slot, which for a missed or late proposal
	// is an earlier block's root — a by-slot lookup would 404 there and fail the duty.
	// In the common case head matches the root the cluster's own messages signed; those
	// sign the QBFT-decided head vote, which can diverge from a freshly-queried head under
	// a reorg or a lagging node.
	start := time.Now()
	beaconBlockRootResp, err := gc.multiClient.BeaconBlockRoot(ctx, &api.BeaconBlockRootOpts{
		Block: "head",
	})
	recordRequest(ctx, gc.log, "BeaconBlockRoot", gc.multiClient, http.MethodGet, true, time.Since(start), err)
	if err != nil {
		return nil, DataVersionNil, errMultiClient(fmt.Errorf("fetch beacon block root: %w", err), "BeaconBlockRoot")
	}
	if err := checkPtrResponse(beaconBlockRootResp, "beacon block root"); err != nil {
		return nil, DataVersionNil, errMultiClient(err, "BeaconBlockRoot")
	}

	blockRoot := beaconBlockRootResp.Data

	if err := gc.waitIntoSlot(ctx, slot, 2); err != nil {
		return nil, DataVersionNil, fmt.Errorf("wait for contribution deadline: %w", err)
	}

	// Fetch sync committee contributions for each subnet in parallel.
	var (
		contributions = make(spectypes.Contributions, len(subnetIDs))
		g             errgroup.Group
	)
	for i := range subnetIDs {
		g.Go(func() error {
			start := time.Now()
			syncCommitteeContrResp, err := gc.multiClient.SyncCommitteeContribution(ctx, &api.SyncCommitteeContributionOpts{
				Slot:              slot,
				SubcommitteeIndex: subnetIDs[i],
				BeaconBlockRoot:   *blockRoot,
			})
			recordRequest(ctx, gc.log, "SyncCommitteeContribution", gc.multiClient, http.MethodGet, true, time.Since(start), err)
			if err != nil {
				return errMultiClient(fmt.Errorf("fetch sync committee contribution: %w", err), "SyncCommitteeContribution")
			}
			if err := checkPtrResponse(syncCommitteeContrResp, "sync committee contribution"); err != nil {
				return errMultiClient(err, "SyncCommitteeContribution")
			}

			contribution := syncCommitteeContrResp.Data
			contributions[i] = &spectypes.Contribution{
				SelectionProofSig: selectionProofs[i],
				Contribution:      *contribution,
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, DataVersionNil, err
	}

	return &contributions, spec.DataVersionAltair, nil
}

// SubmitSignedContributionAndProof submits the signed contribution and proof to the beacon node.
func (gc *GoClient) SubmitSignedContributionAndProof(
	ctx context.Context,
	contribution *altair.SignedContributionAndProof,
) error {
	start := time.Now()
	err := gc.multiClient.SubmitSyncCommitteeContributions(ctx, []*altair.SignedContributionAndProof{contribution})
	recordRequest(ctx, gc.log, "SubmitSyncCommitteeContributions", gc.multiClient, http.MethodPost, true, time.Since(start), err)
	if err != nil {
		return errMultiClient(fmt.Errorf("submit sync committee contributions: %w", err), "SubmitSyncCommitteeContributions")
	}

	return nil
}
