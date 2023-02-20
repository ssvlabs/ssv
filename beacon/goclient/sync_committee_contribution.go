package goclient

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"time"

	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/pkg/errors"
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
func (gc *goClient) GetSyncCommitteeContribution(slot phase0.Slot, subnetID uint64) (*altair.SyncCommitteeContribution, error) {
	gc.waitOneThirdOrValidBlock(slot)

	scDataReqStart := time.Now()
	blockRoot, err := gc.client.BeaconBlockRoot(gc.ctx, fmt.Sprint(slot))
	if err != nil {
		return nil, err
	}
	if blockRoot == nil {
		return nil, errors.New("block root is nil")
	}

	metricsSyncCommitteeDataRequest.Observe(time.Since(scDataReqStart).Seconds())

	gc.waitToSlotTwoThirds(slot)

	sccDataReqStart := time.Now()
	contribution, err := gc.client.SyncCommitteeContribution(gc.ctx, slot, subnetID, *blockRoot)
	if err != nil {
		return nil, err
	}

	metricsSyncCommitteeContributionDataRequest.Observe(time.Since(sccDataReqStart).Seconds())

	return contribution, nil
}

// SubmitSignedContributionAndProof broadcasts to the network
func (gc *goClient) SubmitSignedContributionAndProof(contribution *altair.SignedContributionAndProof) error {
	return gc.client.SubmitSyncCommitteeContributions(gc.ctx, []*altair.SignedContributionAndProof{contribution})
}
