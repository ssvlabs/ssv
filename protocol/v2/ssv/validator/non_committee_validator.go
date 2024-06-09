package validator

import (
	"fmt"
	"github.com/herumi/bls-eth-go-binary/bls"
	specssv "github.com/ssvlabs/ssv-spec/ssv"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv/exporter/exporter_message"
	qbftstorage "github.com/ssvlabs/ssv/protocol/v2/qbft/storage"
	"go.uber.org/zap"
	"golang.org/x/exp/slices"

	"github.com/ssvlabs/ssv/ibft/storage"
	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/protocol/v2/qbft"
	qbftcontroller "github.com/ssvlabs/ssv/protocol/v2/qbft/controller"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	"github.com/ssvlabs/ssv/protocol/v2/types"
)

type NonCommitteeValidator struct {
	logger                 *zap.Logger
	Share                  *types.SSVShare
	Storage                *storage.QBFTStores
	qbftController         *qbftcontroller.Controller
	postConsensusContainer *specssv.PartialSigContainer
	newDecidedHandler      qbftcontroller.NewDecidedHandler
}

func NewNonCommitteeValidator(logger *zap.Logger, identifier exporter_message.MessageID, opts Options) *NonCommitteeValidator {
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
		logger:                 logger,
		Share:                  opts.SSVShare,
		Storage:                opts.Storage,
		qbftController:         ctrl,
		postConsensusContainer: specssv.NewPartialSigContainer(opts.SSVShare.Share.Quorum),
		newDecidedHandler:      opts.NewDecidedHandler,
	}
}

func (ncv *NonCommitteeValidator) ProcessMessage(msg *queue.DecodedSSVMessage) {
	logger := ncv.logger.With(fields.PubKey(msg.MsgID.GetDutyExecutorID()), fields.Role(msg.MsgID.GetRoleType()))

	if msg.GetType() != spectypes.SSVPartialSignatureMsgType {
		return
	}

	spsm := &spectypes.PartialSignatureMessages{}
	if err := spsm.Decode(msg.SSVMessage.GetData()); err != nil {
		logger.Debug("❗ failed to get partial signature message from network message", zap.Error(err))
		return
	}
	if msg.MsgID.GetRoleType() == spectypes.RoleCommittee {
		if err := spsm.ValidateForSigner(msg.SignedSSVMessage.OperatorIDs[0]); err != nil {
			logger.Debug("❌ got invalid message", zap.Error(err))
		}
	} else {
		if err := validateMessage(ncv.Share.Share, msg); err != nil {
			logger.Debug("❌ got invalid message", zap.Error(err))
			return
		}
	}

	if spsm.Type != spectypes.PostConsensusPartialSig {
		return
	}

	logger = logger.With(fields.Slot(spsm.Slot))

	quorums, err := ncv.processMessage(spsm)
	if err != nil {
		logger.Debug("❌ could not process SignedPartialSignatureMessage",
			zap.Error(err))
		return
	}

	if len(quorums) == 0 {
		return
	}

	for _, quorum := range quorums {
		role := getRole(msg.MsgID)
		MsgID := exporter_message.NewMsgID(exporter_message.DomainType(ncv.Share.DomainType), ncv.Share.ValidatorPubKey[:], role)
		println("herehereherehereherehereherehereherehere")
		println(role.String())
		println("herehereherehereherehereherehereherehere")
		if err := ncv.Storage.Get(MsgID.GetRoleType()).SaveParticipants(MsgID, spsm.Slot, quorum); err != nil {
			logger.Error("❌ could not save participants", zap.Error(err))
			return
		}

		if ncv.newDecidedHandler != nil {
			ncv.newDecidedHandler(qbftstorage.ParticipantsRangeEntry{
				Slot:       spsm.Slot,
				Signers:    quorum,
				Identifier: MsgID,
			})
		}
	}
}

func getRole(msgID spectypes.MessageID) exporter_message.RunnerRole {
	if msgID.GetRoleType() == spectypes.RoleCommittee {
		return exporter_message.RoleAttester
	}
	return exporter_message.RunnerRole(msgID.GetRoleType())
}

// nonCommitteeInstanceContainerCapacity returns the capacity of InstanceContainer for non-committee validators
func nonCommitteeInstanceContainerCapacity(fullNode bool) int {
	if fullNode {
		// Helps full nodes reduce
		return 2
	}
	return 1
}

func (ncv *NonCommitteeValidator) processMessage(
	signedMsg *spectypes.PartialSignatureMessages,
) (map[[32]byte][]spectypes.OperatorID, error) {
	quorums := make(map[[32]byte][]spectypes.OperatorID)

	for _, msg := range signedMsg.Messages {
		if ncv.postConsensusContainer.HasSigner(msg.ValidatorIndex, msg.Signer, msg.SigningRoot) {
			ncv.resolveDuplicateSignature(ncv.postConsensusContainer, msg)
		} else {
			ncv.postConsensusContainer.AddSignature(msg)
		}

		rootSignatures := ncv.postConsensusContainer.GetSignatures(msg.ValidatorIndex, msg.SigningRoot)
		if uint64(len(rootSignatures)) >= ncv.Share.Quorum {
			longestSigners := quorums[msg.SigningRoot]
			if newLength := len(rootSignatures); newLength > len(longestSigners) {
				newSigners := make([]spectypes.OperatorID, 0, newLength)
				for signer := range rootSignatures {
					newSigners = append(newSigners, signer)
				}
				slices.Sort(newSigners)
				quorums[msg.SigningRoot] = newSigners
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
