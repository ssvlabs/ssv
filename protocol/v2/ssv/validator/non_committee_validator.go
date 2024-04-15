package validator

import (
	"fmt"

	specssv "github.com/bloxapp/ssv-spec/ssv"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/herumi/bls-eth-go-binary/bls"
	"go.uber.org/zap"
	"golang.org/x/exp/slices"

	"github.com/bloxapp/ssv/ibft/storage"
	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/protocol/v2/qbft"
	qbftcontroller "github.com/bloxapp/ssv/protocol/v2/qbft/controller"
	qbftstorage "github.com/bloxapp/ssv/protocol/v2/qbft/storage"
	"github.com/bloxapp/ssv/protocol/v2/ssv/queue"
	"github.com/bloxapp/ssv/protocol/v2/types"
)

type NonCommitteeValidator struct {
	logger                 *zap.Logger
	Share                  *types.SSVShare
	Storage                *storage.QBFTStores
	qbftController         *qbftcontroller.Controller
	postConsensusContainer *specssv.PartialSigContainer
	newParticipantsHandler qbftcontroller.NewParticipantsHandler
}

func NewNonCommitteeValidator(logger *zap.Logger, identifier spectypes.MessageID, opts Options) *NonCommitteeValidator {
	// currently, only need domain & storage
	config := &qbft.Config{
		Domain:                types.GetDefaultDomain(),
		Storage:               opts.Storage.Get(identifier.GetRoleType()),
		Network:               opts.Network,
		SignatureVerification: true,
	}
	ctrl := qbftcontroller.NewController(identifier[:], &opts.SSVShare.Share, config, opts.FullNode)
	ctrl.StoredInstances = make(qbftcontroller.InstanceContainer, 0, nonCommitteeInstanceContainerCapacity(opts.FullNode))
	ctrl.NewDecidedHandler = opts.NewDecidedHandler
	if _, err := ctrl.LoadHighestInstance(identifier[:]); err != nil {
		logger.Debug("❗ failed to load highest instance", zap.Error(err))
	}

	return &NonCommitteeValidator{
		logger:                 logger,
		Share:                  opts.SSVShare,
		Storage:                opts.Storage,
		qbftController:         ctrl,
		postConsensusContainer: specssv.NewPartialSigContainer(opts.SSVShare.Share.Quorum),
		newParticipantsHandler: opts.NewParticipantsHandler,
	}
}

func (ncv *NonCommitteeValidator) ProcessMessage(msg *queue.DecodedSSVMessage) {
	logger := ncv.logger.With(fields.PubKey(msg.MsgID.GetPubKey()), fields.Role(msg.MsgID.GetRoleType()))

	if err := validateMessage(ncv.Share.Share, msg); err != nil {
		logger.Debug("❌ got invalid message", zap.Error(err))
		return
	}

	if msg.GetType() != spectypes.SSVPartialSignatureMsgType {
		return
	}

	spsm := &spectypes.SignedPartialSignatureMessage{}
	if err := spsm.Decode(msg.GetData()); err != nil {
		logger.Debug("❗ failed to get partial signature message from network message", zap.Error(err))
		return
	}

	// only supports post consensus msg's
	if spsm.Message.Type != spectypes.PostConsensusPartialSig {
		return
	}

	logger = logger.With(fields.Slot(spsm.Message.Slot))

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
		if err := ncv.Storage.Get(msg.GetID().GetRoleType()).SaveParticipants(msg.GetID(), spsm.Message.Slot, quorum); err != nil {
			logger.Error("❌ could not save participants", zap.Error(err))
			return
		}

		if ncv.newParticipantsHandler != nil {
			ncv.newParticipantsHandler(qbftstorage.ParticipantsRangeEntry{
				Slot:       spsm.Message.Slot,
				Operators:  quorum,
				Identifier: msg.GetID(),
			})
		}
	}
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
	signedMsg *spectypes.SignedPartialSignatureMessage,
) (map[[32]byte][]spectypes.OperatorID, error) {
	quorums := make(map[[32]byte][]spectypes.OperatorID)

	for _, msg := range signedMsg.Message.Messages {
		if ncv.postConsensusContainer.HasSigner(msg.Signer, msg.SigningRoot) {
			ncv.resolveDuplicateSignature(ncv.postConsensusContainer, msg)
		} else {
			ncv.postConsensusContainer.AddSignature(msg)
		}

		rootSignatures := ncv.postConsensusContainer.GetSignatures(msg.SigningRoot)
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
func (ncv *NonCommitteeValidator) resolveDuplicateSignature(container *specssv.PartialSigContainer, msg *spectypes.PartialSignatureMessage) {
	// Check previous signature validity
	previousSignature, err := container.GetSignature(msg.Signer, msg.SigningRoot)
	if err == nil {
		err = ncv.verifyBeaconPartialSignature(msg.Signer, previousSignature, msg.SigningRoot)
		if err == nil {
			// Keep the previous sigature since it's correct
			return
		}
	}

	// Previous signature is incorrect or doesn't exist
	container.Remove(msg.Signer, msg.SigningRoot)

	// Hold the new signature, if correct
	err = ncv.verifyBeaconPartialSignature(msg.Signer, msg.PartialSignature, msg.SigningRoot)
	if err == nil {
		container.AddSignature(msg)
	}
}

func (ncv *NonCommitteeValidator) verifyBeaconPartialSignature(signer uint64, signature spectypes.Signature, root [32]byte) error {
	types.MetricsSignaturesVerifications.WithLabelValues().Inc()

	for _, n := range ncv.Share.Committee {
		if n.GetID() == signer {
			pk, err := types.DeserializeBLSPublicKey(n.GetPublicKey())
			if err != nil {
				return fmt.Errorf("could not deserialized pk: %w", err)
			}
			sig := &bls.Sign{}
			if err := sig.Deserialize(signature); err != nil {
				return fmt.Errorf("could not deserialized Signature: %w", err)
			}

			// verify
			if !sig.VerifyByte(&pk, root[:]) {
				return fmt.Errorf("wrong signature")
			}
			return nil
		}
	}
	return fmt.Errorf("unknown signer")
}
