package goclient

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"time"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
	ssz "github.com/ferranbt/fastssz"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
)

// IsSyncCommitteeAggregator returns tru if aggregator
func (gc *goClient) IsSyncCommitteeAggregator(proof []byte) (bool, error) {
	// Hash the signature.
	hash := sha256.Sum256(proof)

	// Keep the signature if it's an aggregator.
	modulo := SyncCommitteeSize / SyncCommitteeSubnetCount / TargetAggregatorsPerSyncSubcommittee
	if modulo == uint64(0) {
		// Modulo must be at least 1.
		modulo = 1
	}
	return binary.LittleEndian.Uint64(hash[:8])%modulo == 0, nil
}

// SyncCommitteeSubnetID returns sync committee subnet ID from subcommittee index
func (gc *goClient) SyncCommitteeSubnetID(index phase0.CommitteeIndex) (uint64, error) {
	return uint64(index) / (SyncCommitteeSize / SyncCommitteeSubnetCount), nil
}

// GetSyncCommitteeContribution returns
func (gc *goClient) GetSyncCommitteeContribution(slot phase0.Slot, selectionProofs []phase0.BLSSignature, subnetIDs []uint64) (ssz.Marshaler, spec.DataVersion, error) {
	if len(selectionProofs) != len(subnetIDs) {
		return nil, DataVersionNil, errors.New("mismatching number of selection proofs and subnet IDs")
	}

	gc.waitForOneThirdSlotDuration(slot)

	scDataReqStart := time.Now()
	blockRoot, err := gc.client.BeaconBlockRoot(gc.ctx, fmt.Sprint(slot))
	if err != nil {
		return nil, DataVersionNil, err
	}
	if blockRoot == nil {
		return nil, DataVersionNil, errors.New("block root is nil")
	}

	gc.metrics.SyncCommitteeDataRequest(time.Since(scDataReqStart))

	gc.waitToSlotTwoThirds(slot)

	// Fetch sync committee contributions for each subnet in parallel.
	var (
		sccDataReqStart = time.Now()
		contributions   = make(spectypes.Contributions, 0, len(subnetIDs))
		g               errgroup.Group
	)
	for i := range subnetIDs {
		index := i
		g.Go(func() error {
			contribution, err := gc.client.SyncCommitteeContribution(gc.ctx, slot, subnetIDs[index], *blockRoot)
			if err != nil {
				return err
			}
			if contribution == nil {
				return errors.New("contribution is nil")
			}
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

	gc.metrics.SyncCommitteeContributionDataRequest(time.Since(sccDataReqStart))

	return &contributions, spec.DataVersionAltair, nil
}

// SubmitSignedContributionAndProof broadcasts to the network
func (gc *goClient) SubmitSignedContributionAndProof(contribution *altair.SignedContributionAndProof) error {
	return gc.client.SubmitSyncCommitteeContributions(gc.ctx, []*altair.SignedContributionAndProof{contribution})
}

// waitForOneThirdSlotDuration waits until one-third of the slot has transpired (SECONDS_PER_SLOT / 3 seconds after the start of slot)
func (gc *goClient) waitForOneThirdSlotDuration(slot phase0.Slot) {
	delay := gc.network.SlotDurationSec() / 3 /* a third of the slot duration */
	finalTime := gc.slotStartTime(slot).Add(delay)
	wait := time.Until(finalTime)
	if wait <= 0 {
		return
	}
	time.Sleep(wait)
}
