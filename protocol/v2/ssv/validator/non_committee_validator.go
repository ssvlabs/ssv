package validator

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/ssvlabs/ssv/operator/duties/dutystore"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/roundtimer"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/herumi/bls-eth-go-binary/bls"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/exporter/convert"
	"github.com/ssvlabs/ssv/ibft/storage"
	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/qbft"
	qbftcontroller "github.com/ssvlabs/ssv/protocol/v2/qbft/controller"
	qbftctrl "github.com/ssvlabs/ssv/protocol/v2/qbft/controller"
	qbftstorage "github.com/ssvlabs/ssv/protocol/v2/qbft/storage"
	"github.com/ssvlabs/ssv/protocol/v2/ssv"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
)

type CommitteeObserver struct {
	logger                 *zap.Logger
	networkConfig          networkconfig.NetworkConfig
	Storage                *storage.QBFTStores
	qbftController         *qbftcontroller.Controller
	ValidatorStore         registrystorage.ValidatorStore
	dutyStore              *dutystore.Store
	newDecidedHandler      qbftcontroller.NewDecidedHandler
	postConsensusContainer map[phase0.ValidatorIndex]*ssv.PartialSigContainer
}

type CommitteeObserverOptions struct {
	FullNode          bool
	Logger            *zap.Logger
	Network           specqbft.Network
	Storage           *storage.QBFTStores
	Operator          *spectypes.CommitteeMember
	OperatorSigner    ssvtypes.OperatorSigner
	NetworkConfig     networkconfig.NetworkConfig
	NewDecidedHandler qbftctrl.NewDecidedHandler
	ValidatorStore    registrystorage.ValidatorStore
	DutyStore         *dutystore.Store
}

func NewCommitteeObserver(identifier convert.MessageID, opts CommitteeObserverOptions) *CommitteeObserver {
	// currently, only need domain & storage
	config := &qbft.Config{
		Domain:        opts.NetworkConfig.DomainType(),
		NetworkConfig: opts.NetworkConfig,
		Storage:       opts.Storage.Get(identifier.GetRoleType()),
		Network:       opts.Network,
		CutOffRound:   roundtimer.CutOffRound,
	}

	// TODO: does the specific operator matters?

	ctrl := qbftcontroller.NewController(identifier[:], opts.Operator, config, opts.OperatorSigner, opts.FullNode)
	ctrl.StoredInstances = make(qbftcontroller.InstanceContainer, 0, nonCommitteeInstanceContainerCapacity(opts.FullNode))
	if _, err := ctrl.LoadHighestInstance(identifier[:]); err != nil {
		opts.Logger.Debug("❗ failed to load highest instance", zap.Error(err))
	}

	return &CommitteeObserver{
		qbftController:         ctrl,
		logger:                 opts.Logger,
		networkConfig:          opts.NetworkConfig,
		Storage:                opts.Storage,
		ValidatorStore:         opts.ValidatorStore,
		dutyStore:              opts.DutyStore,
		newDecidedHandler:      opts.NewDecidedHandler,
		postConsensusContainer: make(map[phase0.ValidatorIndex]*ssv.PartialSigContainer),
	}
}

func (ncv *CommitteeObserver) ProcessMessage(msg *queue.SSVMessage) error {
	cid := spectypes.CommitteeID(msg.GetID().GetDutyExecutorID()[16:])
	logger := ncv.logger.With(fields.CommitteeID(cid), fields.Role(msg.MsgID.GetRoleType()))

	partialSigMessages := &spectypes.PartialSignatureMessages{}
	if err := partialSigMessages.Decode(msg.SSVMessage.GetData()); err != nil {
		return fmt.Errorf("failed to get partial signature message from network message %w", err)
	}
	if partialSigMessages.Type != spectypes.PostConsensusPartialSig {
		return fmt.Errorf("not processing message type %d", partialSigMessages.Type)
	}

	slot := partialSigMessages.Slot
	logger = logger.With(fields.Slot(slot))

	if err := partialSigMessages.Validate(); err != nil {
		return fmt.Errorf("got invalid message %w", err)
	}

	quorums, err := ncv.processMessage(partialSigMessages)
	if err != nil {
		return fmt.Errorf("could not process SignedPartialSignatureMessage %w", err)
	}

	if len(quorums) == 0 {
		return nil
	}

	for key, quorum := range quorums {
		role := ncv.getRole(msg, slot, key.ValidatorIndex)

		validator, exists := ncv.ValidatorStore.ValidatorByIndex(key.ValidatorIndex)
		if !exists {
			return fmt.Errorf("could not find share for validator with index %d", key.ValidatorIndex)
		}

		msgID := convert.NewMsgID(ncv.qbftController.GetConfig().GetSignatureDomainType(), validator.ValidatorPubKey[:], role)
		roleStorage := ncv.Storage.Get(msgID.GetRoleType())
		if roleStorage == nil {
			return fmt.Errorf("role storage doesn't exist: %v", role)
		}

		existingQuorum, err := roleStorage.GetParticipants(msgID, slot)
		if err != nil {
			return fmt.Errorf("could not get participants %w", err)
		}

		if len(existingQuorum) > len(quorum) {
			continue
		}

		if err := roleStorage.SaveParticipants(msgID, slot, quorum); err != nil {
			return fmt.Errorf("could not save participants %w", err)
		}

		var operatorIDs []string
		for _, share := range quorum {
			operatorIDs = append(operatorIDs, strconv.FormatUint(share, 10))
		}
		logger.Info("✅ saved participants",
			zap.String("converted_role", role.ToBeaconRole()),
			zap.Uint64("validator_index", uint64(key.ValidatorIndex)),
			zap.String("signers", strings.Join(operatorIDs, ", ")),
		)

		if ncv.newDecidedHandler != nil {
			ncv.newDecidedHandler(qbftstorage.ParticipantsRangeEntry{
				Slot:       slot,
				Signers:    quorum,
				Identifier: msgID,
			})
		}
	}

	return nil
}

func (ncv *CommitteeObserver) getRole(msg *queue.SSVMessage, slot phase0.Slot, validatorIndex phase0.ValidatorIndex) convert.RunnerRole {
	if msg.MsgID.GetRoleType() == spectypes.RoleCommittee {
		beacon := ncv.networkConfig.Beacon
		period := beacon.EstimatedSyncCommitteePeriodAtEpoch(beacon.EstimatedEpochAtSlot(slot))
		d := ncv.dutyStore.SyncCommittee.Duty(period, validatorIndex)
		if d == nil {
			return convert.RoleAttester
		}
		return convert.RoleSyncCommittee
	}
	return convert.RunnerRole(msg.MsgID.GetRoleType())
}

// nonCommitteeInstanceContainerCapacity returns the capacity of InstanceContainer for non-committee validators
func nonCommitteeInstanceContainerCapacity(fullNode bool) int {
	if fullNode {
		// Helps full nodes reduce
		return 2
	}
	return 1
}

type validatorIndexAndRoot struct {
	ValidatorIndex phase0.ValidatorIndex
	Root           [32]byte
}

func (ncv *CommitteeObserver) processMessage(
	signedMsg *spectypes.PartialSignatureMessages,
) (map[validatorIndexAndRoot][]spectypes.OperatorID, error) {
	quorums := make(map[validatorIndexAndRoot][]spectypes.OperatorID)

	for _, msg := range signedMsg.Messages {
		validator, exists := ncv.ValidatorStore.ValidatorByIndex(msg.ValidatorIndex)
		if !exists {
			return nil, fmt.Errorf("could not find share for validator with index %d", msg.ValidatorIndex)
		}
		container, ok := ncv.postConsensusContainer[msg.ValidatorIndex]
		if !ok {
			container = ssv.NewPartialSigContainer(validator.Quorum())
			ncv.postConsensusContainer[msg.ValidatorIndex] = container
		}
		if container.HasSignature(msg.ValidatorIndex, msg.Signer, msg.SigningRoot) {
			ncv.resolveDuplicateSignature(container, msg, validator)
		} else {
			container.AddSignature(msg)
		}

		rootSignatures := container.GetSignatures(msg.ValidatorIndex, msg.SigningRoot)
		if uint64(len(rootSignatures)) >= validator.Quorum() {
			key := validatorIndexAndRoot{msg.ValidatorIndex, msg.SigningRoot}
			longestSigners := quorums[key]
			if newLength := len(rootSignatures); newLength > len(longestSigners) {
				newSigners := make([]spectypes.OperatorID, 0, newLength)
				for signer := range rootSignatures {
					newSigners = append(newSigners, signer)
				}
				slices.Sort(newSigners)
				quorums[key] = newSigners
			}
		}
	}
	return quorums, nil
}

// Stores the container's existing signature or the new one, depending on their validity. If both are invalid, remove the existing one
// copied from BaseRunner
func (ncv *CommitteeObserver) resolveDuplicateSignature(container *ssv.PartialSigContainer, msg *spectypes.PartialSignatureMessage, share *ssvtypes.SSVShare) {
	// Check previous signature validity
	previousSignature, err := container.GetSignature(msg.ValidatorIndex, msg.Signer, msg.SigningRoot)
	if err == nil {
		err = ncv.verifyBeaconPartialSignature(msg.Signer, previousSignature, msg.SigningRoot, share)
		if err == nil {
			// Keep the previous sigature since it's correct
			return
		}
	}

	// Previous signature is incorrect or doesn't exist
	container.Remove(msg.ValidatorIndex, msg.Signer, msg.SigningRoot)

	// Hold the new signature, if correct
	err = ncv.verifyBeaconPartialSignature(msg.Signer, msg.PartialSignature, msg.SigningRoot, share)
	if err == nil {
		container.AddSignature(msg)
	}
}

// copied from BaseRunner
func (ncv *CommitteeObserver) verifyBeaconPartialSignature(signer uint64, signature spectypes.Signature, root [32]byte, share *ssvtypes.SSVShare) error {
	ssvtypes.MetricsSignaturesVerifications.WithLabelValues().Inc()

	for _, n := range share.Committee {
		if n.Signer == signer {
			pk, err := ssvtypes.DeserializeBLSPublicKey(n.SharePubKey)
			if err != nil {
				return fmt.Errorf("could not deserialized pk: %w", err)
			}

			sig := &bls.Sign{}
			if err := sig.Deserialize(signature); err != nil {
				return fmt.Errorf("could not deserialized Signature: %w", err)
			}

			if !sig.VerifyByte(&pk, root[:]) {
				return fmt.Errorf("wrong signature")
			}
			return nil
		}
	}
	return fmt.Errorf("unknown signer")
}

func (ncv *CommitteeObserver) OnProposalMsg(msg *queue.SSVMessage) error {
	mssg := &specqbft.Message{}
	if err := mssg.Decode(msg.SSVMessage.GetData()); err != nil {
		ncv.logger.Debug("❗ failed to get decode ssv message", zap.Error(err))
		return err
	}

	// decode consensus data
	beaconVote := &spectypes.BeaconVote{}
	if err := beaconVote.Decode(msg.SignedSSVMessage.FullData); err != nil {
		ncv.logger.Debug("❗ failed to get beacon vote data", zap.Error(err))
		return err
	}
	cid := spectypes.CommitteeID(msg.GetID().GetDutyExecutorID()[16:])

	ncv.logger.Info("✅ Got proposal message", fields.CommitteeID(cid))
	return nil
}
