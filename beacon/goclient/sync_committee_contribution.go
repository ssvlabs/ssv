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

// IsSyncCommitteeAggregator returns tru if aggregator
func (gc *GoClient) IsSyncCommitteeAggregator(proof []byte) bool {
	// Hash the signature.
	hash := sha256.Sum256(proof)

	// Keep the signature if it's an aggregator.
	cfg := gc.BeaconConfig()

	// as per spec: https://github.com/ethereum/consensus-specs/blob/dev/specs/altair/validator.md#aggregation-selection
	modulo := cfg.SyncCommitteeSize / cfg.SyncCommitteeSubnetCount / cfg.TargetAggregatorsPerSyncSubcommittee
	if modulo == uint64(0) {
		// Modulo must be at least 1.
		modulo = 1
	}
	return binary.LittleEndian.Uint64(hash[:8])%modulo == 0
}

// SyncCommitteeSubnetID returns sync committee subnet ID from subcommittee index
func (gc *GoClient) SyncCommitteeSubnetID(index phase0.CommitteeIndex) uint64 {
	return uint64(index) / (gc.BeaconConfig().SyncCommitteeSize / gc.BeaconConfig().SyncCommitteeSubnetCount)
}

// GetSyncCommitteeContribution returns
func (gc *GoClient) GetSyncCommitteeContribution(
	ctx context.Context,
	slot phase0.Slot,
	selectionProofs []phase0.BLSSignature,
	subnetIDs []uint64,
) (ssz.Marshaler, spec.DataVersion, error) {
	if len(selectionProofs) != len(subnetIDs) {
		return nil, DataVersionNil, fmt.Errorf("mismatching number of selection proofs and subnet IDs")
	}

	gc.waitOneThirdIntoSlot(ctx, slot)

	scDataReqStart := time.Now()
	beaconBlockRootResp, err := gc.multiClient.BeaconBlockRoot(ctx, &api.BeaconBlockRootOpts{
		Block: fmt.Sprint(slot),
	})
	recordRequest(ctx, gc.log, "BeaconBlockRoot", gc.multiClient, http.MethodGet, true, time.Since(scDataReqStart), err)
	if err != nil {
		return nil, DataVersionNil, errMultiClient(fmt.Errorf("fetch beacon block root: %w", err), "BeaconBlockRoot")
	}
	if beaconBlockRootResp == nil {
		return nil, DataVersionNil, errMultiClient(fmt.Errorf("beacon block root response is nil"), "BeaconBlockRoot")
	}
	if beaconBlockRootResp.Data == nil {
		return nil, DataVersionNil, errMultiClient(fmt.Errorf("beacon block root response data is nil"), "BeaconBlockRoot")
	}

	blockRoot := beaconBlockRootResp.Data

	if err := gc.waitTwoThirdsIntoSlot(ctx, slot); err != nil {
		return nil, 0, fmt.Errorf("wait for 2/3 of slot: %w", err)
	}

	// Fetch sync committee contributions for each subnet in parallel.
	var (
		contributions = make(spectypes.Contributions, 0, len(subnetIDs))
		g             errgroup.Group
	)
	for i := range subnetIDs {
		index := i
		g.Go(func() error {
			start := time.Now()
			syncCommitteeContrResp, err := gc.multiClient.SyncCommitteeContribution(ctx, &api.SyncCommitteeContributionOpts{
				Slot:              slot,
				SubcommitteeIndex: subnetIDs[index],
				BeaconBlockRoot:   *blockRoot,
			})
			recordRequest(ctx, gc.log, "SyncCommitteeContribution", gc.multiClient, http.MethodGet, true, time.Since(start), err)
			if err != nil {
				return errMultiClient(fmt.Errorf("fetch sync committee contribution: %w", err), "SyncCommitteeContribution")
			}
			if syncCommitteeContrResp == nil {
				return errMultiClient(fmt.Errorf("sync committee contribution response is nil"), "SyncCommitteeContribution")
			}
			if syncCommitteeContrResp.Data == nil {
				return errMultiClient(fmt.Errorf("sync committee contribution response data is nil"), "SyncCommitteeContribution")
			}

			contribution := syncCommitteeContrResp.Data
			contributions = append(contributions, &spectypes.Contribution{
				SelectionProofSig: selectionProofs[index],
				Contribution:      *contribution,
			})
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, DataVersionNil, err
	}

	return &contributions, spec.DataVersionAltair, nil
}

// SubmitSignedContributionAndProof broadcasts to the network
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

// waitOneThirdIntoSlot waits until one-third of the slot has transpired (SECONDS_PER_SLOT / 3 seconds after slot start time)
func (gc *GoClient) waitOneThirdIntoSlot(ctx context.Context, slot phase0.Slot) {
	config := gc.getBeaconConfig()
	delay := config.IntervalDuration()
	finalTime := config.SlotStartTime(slot).Add(delay)
	wait := time.Until(finalTime)
	if wait <= 0 {
		return
	}

	select {
	case <-ctx.Done():
		return
	case <-time.After(wait):
	}
}
