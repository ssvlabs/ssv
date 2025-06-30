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
	"go.uber.org/zap"
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

	gc.waitForOneThirdSlotDuration(slot)

	scDataReqStart := time.Now()
	beaconBlockRootResp, err := gc.multiClient.BeaconBlockRoot(ctx, &api.BeaconBlockRootOpts{
		Block: fmt.Sprint(slot),
	})
	recordRequestDuration(ctx, "BeaconBlockRoot", gc.multiClient.Address(), http.MethodGet, time.Since(scDataReqStart), err)
	if err != nil {
		gc.log.Error(clResponseErrMsg,
			zap.String("api", "BeaconBlockRoot"),
			zap.Error(err),
		)
		return nil, DataVersionNil, fmt.Errorf("failed to obtain beacon block root: %w", err)
	}
	if beaconBlockRootResp == nil {
		gc.log.Error(clNilResponseErrMsg,
			zap.String("api", "BeaconBlockRoot"),
		)
		return nil, DataVersionNil, fmt.Errorf("beacon block root response is nil")
	}
	if beaconBlockRootResp.Data == nil {
		gc.log.Error(clNilResponseDataErrMsg,
			zap.String("api", "BeaconBlockRoot"),
		)
		return nil, DataVersionNil, fmt.Errorf("beacon block root data is nil")
	}

	blockRoot := beaconBlockRootResp.Data

	gc.waitToSlotTwoThirds(slot)

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
			recordRequestDuration(ctx, "SyncCommitteeContribution", gc.multiClient.Address(), http.MethodGet, time.Since(start), err)
			if err != nil {
				gc.log.Error(clResponseErrMsg,
					zap.String("api", "SyncCommitteeContribution"),
					zap.Error(err),
				)
				return fmt.Errorf("failed to obtain sync committee contribution: %w", err)
			}
			if syncCommitteeContrResp == nil {
				gc.log.Error(clNilResponseErrMsg,
					zap.String("api", "SyncCommitteeContribution"),
				)
				return fmt.Errorf("sync committee contribution response is nil")
			}
			if syncCommitteeContrResp.Data == nil {
				gc.log.Error(clNilResponseDataErrMsg,
					zap.String("api", "SyncCommitteeContribution"),
				)
				return fmt.Errorf("sync committee contribution data is nil")
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
	clientAddress := gc.multiClient.Address()
	logger := gc.log.With(
		zap.String("api", "SubmitSyncCommitteeContributions"),
		zap.String("client_addr", clientAddress))

	start := time.Now()
	err := gc.multiClient.SubmitSyncCommitteeContributions(ctx, []*altair.SignedContributionAndProof{contribution})
	recordRequestDuration(ctx, "SubmitSyncCommitteeContributions", clientAddress, http.MethodPost, time.Since(start), err)
	if err != nil {
		logger.Error(clResponseErrMsg, zap.Error(err))
		return err
	}

	logger.Debug("consensus client submitted signed contribution and proof")
	return nil
}

// waitForOneThirdSlotDuration waits until one-third of the slot has transpired (SECONDS_PER_SLOT / 3 seconds after slot start time)
func (gc *GoClient) waitForOneThirdSlotDuration(slot phase0.Slot) {
	config := gc.getBeaconConfig()
	delay := config.IntervalDuration()
	finalTime := config.GetSlotStartTime(slot).Add(delay)
	wait := time.Until(finalTime)
	if wait <= 0 {
		return
	}
	time.Sleep(wait)
}
