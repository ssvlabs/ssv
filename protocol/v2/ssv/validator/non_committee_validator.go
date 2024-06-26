package validator

import (
	"fmt"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/herumi/bls-eth-go-binary/bls"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	specssv "github.com/ssvlabs/ssv-spec/ssv"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv/exporter/exporter_message"
	"github.com/ssvlabs/ssv/ibft/storage"
	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/protocol/v2/qbft"
	qbftcontroller "github.com/ssvlabs/ssv/protocol/v2/qbft/controller"
	qbftctrl "github.com/ssvlabs/ssv/protocol/v2/qbft/controller"
	qbftstorage "github.com/ssvlabs/ssv/protocol/v2/qbft/storage"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	"github.com/ssvlabs/ssv/protocol/v2/types"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
	"go.uber.org/zap"
	"golang.org/x/exp/slices"
	"strconv"
	"strings"
)

type CommitteeObserver struct {
	logger                 *zap.Logger
	Storage                *storage.QBFTStores
	qbftController         *qbftcontroller.Controller
	ValidatorStore         registrystorage.ValidatorStore
	newDecidedHandler      qbftcontroller.NewDecidedHandler
	Roots                  map[[32]byte]spectypes.BeaconRole
	postConsensusContainer map[phase0.ValidatorIndex]*specssv.PartialSigContainer
}

type NonCommitteeOptions struct {
	FullNode          bool
	Logger            *zap.Logger
	Network           specqbft.Network
	Storage           *storage.QBFTStores
	Operator          *spectypes.Operator
	NewDecidedHandler qbftctrl.NewDecidedHandler
	ValidatorStore    registrystorage.ValidatorStore
}

func NewNonCommitteeValidator(identifier exporter_message.MessageID, opts NonCommitteeOptions) *CommitteeObserver {
	// currently, only need domain & storage
	config := &qbft.Config{
		Domain:                types.GetDefaultDomain(),
		Storage:               opts.Storage.Get(identifier.GetRoleType()),
		Network:               opts.Network,
		SignatureVerification: true,
	}

	// TODO: does the specific operator matters?

	ctrl := qbftcontroller.NewController(identifier[:], opts.Operator, config, opts.FullNode)
	ctrl.StoredInstances = make(qbftcontroller.InstanceContainer, 0, nonCommitteeInstanceContainerCapacity(opts.FullNode))
	if _, err := ctrl.LoadHighestInstance(identifier[:]); err != nil {
		opts.Logger.Debug("❗ failed to load highest instance", zap.Error(err))
	}

	return &CommitteeObserver{
		qbftController:         ctrl,
		logger:                 opts.Logger,
		Storage:                opts.Storage,
		ValidatorStore:         opts.ValidatorStore,
		newDecidedHandler:      opts.NewDecidedHandler,
		Roots:                  make(map[[32]byte]spectypes.BeaconRole),
		postConsensusContainer: make(map[phase0.ValidatorIndex]*specssv.PartialSigContainer),
	}
}

func (ncv *CommitteeObserver) ProcessMessage(msg *queue.DecodedSSVMessage) {
	cid := spectypes.CommitteeID(msg.GetID().GetDutyExecutorID()[16:])
	logger := ncv.logger.With(fields.CommitteeID(cid), fields.Role(msg.MsgID.GetRoleType()))

	partialSigMessages := &spectypes.PartialSignatureMessages{}
	if err := partialSigMessages.Decode(msg.SSVMessage.GetData()); err != nil {
		logger.Debug("❗ failed to get partial signature message from network message", zap.Error(err))
		return
	}
	if partialSigMessages.Type != spectypes.PostConsensusPartialSig {
		return
	}

	slot := partialSigMessages.Slot
	logger = logger.With(fields.Slot(slot))

	if err := partialSigMessages.Validate(); err != nil {
		logger.Debug("❌ got invalid message", zap.Error(err))
	}

	quorums, err := ncv.processMessage(partialSigMessages)
	if err != nil {
		logger.Debug("❌ could not process SignedPartialSignatureMessage",
			zap.Error(err))
		return
	}

	if len(quorums) == 0 {
		return
	}

	for key, quorum := range quorums {
		role := ncv.getRole(msg, key.Root)
		validator := ncv.ValidatorStore.ValidatorByIndex(key.ValidatorIndex)
		MsgID := exporter_message.NewMsgID(types.GetDefaultDomain(), validator.ValidatorPubKey[:], role)
		if err := ncv.Storage.Get(MsgID.GetRoleType()).SaveParticipants(MsgID, slot, quorum); err != nil {
			logger.Error("❌ could not save participants", zap.Error(err))
			return
		} else {
			var operatorIDs []string
			for _, share := range quorum {
				operatorIDs = append(operatorIDs, strconv.FormatUint(share, 10))
			}
			logger.Info("✅ saved participants",
				zap.String("converted_role", role.ToBeaconRole()),
				zap.String("validator_index", strconv.FormatUint(uint64(key.ValidatorIndex), 10)),
				zap.String("signers", strings.Join(operatorIDs, ", ")),
			)
		}

		if ncv.newDecidedHandler != nil {
			ncv.newDecidedHandler(qbftstorage.ParticipantsRangeEntry{
				Slot:       slot,
				Signers:    quorum,
				Identifier: MsgID,
			})
		}
	}
}

func (ncv *CommitteeObserver) getRole(msg *queue.DecodedSSVMessage, root [32]byte) exporter_message.RunnerRole {
	if msg.MsgID.GetRoleType() == spectypes.RoleCommittee {
		_, found := ncv.Roots[root]
		if !found {
			return exporter_message.RoleAttester
		}
		return exporter_message.RoleSyncCommittee
	}
	return exporter_message.RunnerRole(msg.MsgID.GetRoleType())
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
		validator := ncv.ValidatorStore.ValidatorByIndex(msg.ValidatorIndex)
		if validator == nil {
			panic("fuck my life")
		}
		container, ok := ncv.postConsensusContainer[msg.ValidatorIndex]
		if !ok {
			container = specssv.NewPartialSigContainer(validator.Quorum)
			ncv.postConsensusContainer[msg.ValidatorIndex] = container
		}
		if container.HasSigner(msg.ValidatorIndex, msg.Signer, msg.SigningRoot) {
			ncv.resolveDuplicateSignature(container, msg, validator)
		} else {
			container.AddSignature(msg)
		}

		rootSignatures := container.GetSignatures(msg.ValidatorIndex, msg.SigningRoot)
		if uint64(len(rootSignatures)) >= validator.Quorum {
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
func (ncv *CommitteeObserver) resolveDuplicateSignature(container *specssv.PartialSigContainer, msg *spectypes.PartialSignatureMessage, share *types.SSVShare) {
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
func (ncv *CommitteeObserver) verifyBeaconPartialSignature(signer uint64, signature spectypes.Signature, root [32]byte, share *types.SSVShare) error {
	types.MetricsSignaturesVerifications.WithLabelValues().Inc()

	for _, n := range share.Committee {
		if n.Signer == signer {
			pk, err := types.DeserializeBLSPublicKey(n.SharePubKey)
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

func (ncv *CommitteeObserver) OnProposalMsg(msg *queue.DecodedSSVMessage) {
	mssg := &specqbft.Message{}
	if err := mssg.Decode(msg.SSVMessage.GetData()); err != nil {
		ncv.logger.Debug("❗ failed to get decode ssv message", zap.Error(err))
		return
	}

	// decode consensus data
	beaconVote := &spectypes.BeaconVote{}
	if err := beaconVote.Decode(msg.SignedSSVMessage.FullData); err != nil {
		ncv.logger.Debug("❗ failed to get beacon vote data", zap.Error(err))
		return
	}
	cid := spectypes.CommitteeID(msg.GetID().GetDutyExecutorID()[16:])

	ncv.logger.Info("✅ Got proposal message", fields.CommitteeID(cid))
	ncv.Roots[beaconVote.BlockRoot] = spectypes.BNRoleSyncCommittee
}

//func (ncv *NonCommitteeValidator) onPostProposalMsg(msg *queue.DecodedSSVMessage) {
//	role := ncvroots_to_roles[msg.root]
//	if role == 'attester':
//	att = Attestation(msg)
//	attesters[att.index] = att
//	elif role == 'sync':
//	sync = Sync(msg)
//	syncs[sync.index] = sync
//	else:
//	raise Exception('Unknown role')
//}
