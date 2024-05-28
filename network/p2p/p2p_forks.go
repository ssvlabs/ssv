package p2pv1

import (
	"encoding/hex"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ssvlabs/ssv/protocol/v2/types"
	"go.uber.org/zap"
)

// type forkPhase int

// const (
// 	forkPhaseBefore forkPhase = iota
// 	forkPhasePrepare
// 	forkPhaseAfter
// )

const (
	// committeeSubnetPresubscriptionDistance is the distance from the fork slot
	// to start subscribing to committee subnets. During this phase, the node
	// subscribes to both committee and validator subnets.
	committeeSubnetPresubscriptionDistance phase0.Slot = 2
)

// committeeSubnetSubscriptions returns whether the node should subscribe to committee subnets,
// which is true during the presubscription phase and after the committee subnets fork.
func (n *p2pNetwork) committeeSubnetSubscriptions() bool {
	if n.cfg.Network.CommitteeSubnetsFork() {
		return true
	}
	currentSlot := n.cfg.Network.Beacon.EstimatedCurrentSlot()
	forkSlot := phase0.Slot(uint64(n.cfg.Network.CommitteeSubnetForkEpoch) * n.cfg.Network.SlotsPerEpoch())
	presubscriptionSlot := forkSlot - committeeSubnetPresubscriptionDistance
	return currentSlot >= presubscriptionSlot
}

// validatorSubnetSubscriptions returns whether the node should subscribe to validator subnets,
// which is true before the committee subnets fork.
func (n *p2pNetwork) validatorSubnetSubscriptions() bool {
	return !n.cfg.Network.CommitteeSubnetsFork()
}

// presubscribeToCommitteeSubnets subscribes to committee subnets according to the active validators.
// This should only be called once at the beginning of the presubscription phase.
func (n *p2pNetwork) presubscribeToCommitteeSubnets(logger *zap.Logger) error {
	start := time.Now()

	// Compute activeCommittees according to the active validators.
	n.activeValidators.Range(func(pkHex string, status validatorStatus) bool {
		pk, err := hex.DecodeString(pkHex)
		if err != nil {
			logger.Warn("could not decode public key", zap.String("pk", pkHex))
			return true
		}
		share := n.nodeStorage.Shares().Get(nil, pk)
		if share == nil {
			logger.Warn("could not find share to subscribe to committee", zap.String("pk", pkHex))
			return true
		}
		cid := share.CommitteeID()
		n.activeCommittees.Set(string(cid[:]), status)
		return true
	})

	// Subscribe to committee subnets.
	hexCommitteeIDs := make([]string, 0, n.activeCommittees.Len())
	n.activeCommittees.Range(func(cid string, status validatorStatus) bool {
		if err := n.subscribeCommittee(types.CommitteeID([]byte(cid))); err != nil {
			logger.Warn("could not subscribe to committee", zap.Error(err))
		}
		hexCommitteeIDs = append(hexCommitteeIDs, hex.EncodeToString([]byte(cid)))
		return true
	})

	logger.Debug("presubscribed to committee subnets",
		zap.Strings("committee_ids", hexCommitteeIDs),
		zap.Int("total_committees", len(hexCommitteeIDs)),
		zap.Duration("took", time.Since(start)))

	go func() {
		// Wait for the fork slot to update subnet score params.
		forkTime := n.cfg.Network.Beacon.EpochStartTime(n.cfg.Network.CommitteeSubnetForkEpoch)
		time.Sleep(time.Until(forkTime))

		if err := n.topicsCtrl.UpdateScoreParams(logger); err != nil {
			logger.Warn("could not update score params", zap.Error(err))
			return
		}
		logger.Debug("updated score params for committee subnets")
	}()

	return nil
}
