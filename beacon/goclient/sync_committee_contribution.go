package goclient

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	eth2client "github.com/attestantio/go-eth2-client"
	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv-spec/types"
	"github.com/pkg/errors"
)

// IsSyncCommitteeAggregator returns tru if aggregator
func (gc *goClient) IsSyncCommitteeAggregator(proof []byte) (bool, error) {
	// Hash the signature.
	hash := sha256.Sum256(proof)

	// Keep the signature if it's an aggregator.
	modulo := types.SyncCommitteeSize / types.SyncCommitteeSubnetCount / types.TargetAggregatorsPerSyncSubcommittee
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
	provider, isProvider := gc.client.(eth2client.BeaconBlockRootProvider)
	if !isProvider {
		return nil, errors.New("client does not support BeaconBlockRootProvider")
	}

	gc.waitOneThirdOrValidBlock(uint64(slot))

	blockRoot, err := provider.BeaconBlockRoot(gc.ctx, fmt.Sprint(slot))
	if err != nil {
		return nil, err
	}
	if blockRoot == nil {
		return nil, errors.New("block root is nil")
	}

	gc.waitToSlotTwoThirds(uint64(slot))

	if provider, isProvider := gc.client.(eth2client.SyncCommitteeContributionProvider); isProvider {
		contribution, err := provider.SyncCommitteeContribution(gc.ctx, slot, subnetID, *blockRoot)
		if err != nil {
			return nil, err
		}
		return contribution, nil
	}
	return nil, errors.New("client does not support SyncCommitteeContributionProvider")
}

// SubmitSignedContributionAndProof broadcasts to the network
func (gc *goClient) SubmitSignedContributionAndProof(contribution *altair.SignedContributionAndProof) error {
	provider, isProvider := gc.client.(eth2client.SyncCommitteeContributionsSubmitter)
	if !isProvider {
		return errors.New("client does not support SyncCommitteeContributionsSubmitter")
	}
	return provider.SubmitSyncCommitteeContributions(gc.ctx, []*altair.SignedContributionAndProof{contribution})
}
