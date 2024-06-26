package validator

import (
	"bytes"
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
	qbftstorage "github.com/ssvlabs/ssv/protocol/v2/qbft/storage"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	"github.com/ssvlabs/ssv/protocol/v2/types"
	"go.uber.org/zap"
	"golang.org/x/exp/slices"
	"strconv"
	"strings"
)

type NonCommitteeValidator struct {
	logger                 *zap.Logger
	Storage                *storage.QBFTStores
	Quorum                 uint64
	qbftController         *qbftcontroller.Controller
	postConsensusContainer map[phase0.ValidatorIndex]*specssv.PartialSigContainer
	newDecidedHandler      qbftcontroller.NewDecidedHandler
	Roots                  map[[32]byte]spectypes.BeaconRole
}

func NewNonCommitteeValidator(logger *zap.Logger, identifier exporter_message.MessageID, opts Options, quorum uint64) *NonCommitteeValidator {
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
		logger.Debug("❗ failed to load highest instance", zap.Error(err))
	}

	return &NonCommitteeValidator{
		qbftController:         ctrl,
		Quorum:                 quorum,
		logger:                 logger,
		Storage:                opts.Storage,
		newDecidedHandler:      opts.NewDecidedHandler,
		Roots:                  make(map[[32]byte]spectypes.BeaconRole),
		postConsensusContainer: make(map[phase0.ValidatorIndex]*specssv.PartialSigContainer),
	}
}

func (ncv *NonCommitteeValidator) ProcessMessage(msg *queue.DecodedSSVMessage) {
	logger := ncv.logger.With(fields.PubKey(ncv.Share.ValidatorPubKey[:]), fields.Role(msg.MsgID.GetRoleType()))

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

	if msg.MsgID.GetRoleType() == spectypes.RoleCommittee {
		if err := partialSigMessages.ValidateForSigner(msg.SignedSSVMessage.OperatorIDs[0]); err != nil {
			logger.Debug("❌ got invalid message for role committee", zap.Error(err))
		}
	} else {
		if err := validateMessage(ncv.Share.Share, msg); err != nil {
			logger.Debug("❌ got invalid message", zap.Error(err))
			return
		}
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
		validatorPublicKey := ncv.
		MsgID := exporter_message.NewMsgID(ncv.Share.DomainType, ncv.Share.ValidatorPubKey[:], role)
		if err := ncv.Storage.Get(MsgID.GetRoleType()).SaveParticipants(MsgID, slot, quorum); err != nil {
			logger.Error("❌ could not save participants", zap.Error(err))
			return
		} else {
			var operatorIDs []string
			for _, share := range quorum {
				operatorIDs = append(operatorIDs, strconv.FormatUint(share, 10))
			}
			logger.Info("✅saved participants",
				zap.String("converted_role", role.ToBeaconRole()),
				zap.String("validator_index", strconv.FormatUint(uint64(ncv.Share.ValidatorIndex), 10)),
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

func (ncv *NonCommitteeValidator) getRole(msg *queue.DecodedSSVMessage, root [32]byte) exporter_message.RunnerRole {
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

type validatorAndRoot struct {
	Validator phase0.ValidatorIndex
	Root [32]byte
}

func (ncv *NonCommitteeValidator) processMessage(
	signedMsg *spectypes.PartialSignatureMessages,
) (map[validatorAndRoot][]spectypes.OperatorID, error) {
	quorums := make(map[validatorAndRoot][]spectypes.OperatorID)

	for _, msg := range signedMsg.Messages {
		container, ok := ncv.postConsensusContainer[msg.ValidatorIndex]
		if !ok {
			container = specssv.NewPartialSigContainer(ncv.Quorum)
			ncv.postConsensusContainer[msg.ValidatorIndex] = container
		}
		//println("<<<<<<<<<<<<<<<<<<<<<<<<,fuck my life>>>>>>>>>>>>>>>>>>>>>>>>>>>>")
		//println(msg.ValidatorIndex)
		//println(signedMsg.Slot)
		//println("<<<<<<<<<<<<<<<<<<<<<<<<,fuck my life>>>>>>>>>>>>>>>>>>>>>>>>>>>>")
		if container.HasSigner(msg.ValidatorIndex, msg.Signer, msg.SigningRoot) {
			ncv.resolveDuplicateSignature(container, msg)
		} else {
			container.AddSignature(msg)
		}

		rootSignatures := container.GetSignatures(msg.ValidatorIndex, msg.SigningRoot)
		if uint64(len(rootSignatures)) >= ncv.Quorum {
			key := validatorAndRoot{msg.ValidatorIndex, msg.SigningRoot}
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
func (ncv *NonCommitteeValidator) resolveDuplicateSignature(container *specssv.PartialSigContainer, msg *spectypes.PartialSignatureMessage) {
	// Check previous signature validity
	previousSignature, err := container.GetSignature(msg.ValidatorIndex, msg.Signer, msg.SigningRoot)
	if err == nil {
		err = ncv.verifyBeaconPartialSignature(msg.Signer, previousSignature, msg.SigningRoot)
		if err == nil {
			// Keep the previous sigature since it's correct
			return
		}
	}

	// Previous signature is incorrect or doesn't exist
	container.Remove(msg.ValidatorIndex, msg.Signer, msg.SigningRoot)

	// Hold the new signature, if correct
	err = ncv.verifyBeaconPartialSignature(msg.Signer, msg.PartialSignature, msg.SigningRoot)
	if err == nil {
		container.AddSignature(msg)
	}
}

// copied from BaseRunner
func (ncv *NonCommitteeValidator) verifyBeaconPartialSignature(signer uint64, signature spectypes.Signature, root [32]byte) error {
	types.MetricsSignaturesVerifications.WithLabelValues().Inc()

	for _, n := range ncv.Share.Committee {
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

func (ncv *NonCommitteeValidator) OnProposalMsg(msg *queue.DecodedSSVMessage) {
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
