package validator

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/herumi/bls-eth-go-binary/bls"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	specssv "github.com/ssvlabs/ssv-spec/ssv"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"
	"golang.org/x/exp/slices"

	"github.com/ssvlabs/ssv/exporter/convert"
	"github.com/ssvlabs/ssv/ibft/storage"
	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/qbft"
	qbftcontroller "github.com/ssvlabs/ssv/protocol/v2/qbft/controller"
	qbftctrl "github.com/ssvlabs/ssv/protocol/v2/qbft/controller"
	qbftstorage "github.com/ssvlabs/ssv/protocol/v2/qbft/storage"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
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
}

func NewCommitteeObserver(identifier convert.MessageID, opts CommitteeObserverOptions) *CommitteeObserver {
	// currently, only need domain & storage
	config := &qbft.Config{
		Domain:      opts.NetworkConfig.DomainType(),
		Storage:     opts.Storage.Get(identifier.GetRoleType()),
		Network:     opts.Network,
		CutOffRound: specqbft.Round(specqbft.CutoffRound),
	}

	// TODO: does the specific operator matters?

	identifierFunc := func() []byte {
		return identifier[:]
	}
	ctrl := qbftcontroller.NewController(identifierFunc, opts.Operator, config, opts.OperatorSigner, opts.FullNode)
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
		role := ncv.getRole(msg, key.Root)
		validator := ncv.ValidatorStore.ValidatorByIndex(key.ValidatorIndex)
		MsgID := convert.NewMsgID(ncv.qbftController.GetConfig().GetSignatureDomainType(), validator.ValidatorPubKey[:], role)
		if err := ncv.Storage.Get(MsgID.GetRoleType()).SaveParticipants(MsgID, slot, quorum); err != nil {
			return fmt.Errorf("could not save participants %w", err)
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

	return nil
}

func (ncv *CommitteeObserver) getRole(msg *queue.SSVMessage, root [32]byte) convert.RunnerRole {
	if msg.MsgID.GetRoleType() == spectypes.RoleCommittee {
		_, found := ncv.Roots[root]
		if !found {
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
		validator := ncv.ValidatorStore.ValidatorByIndex(msg.ValidatorIndex)
		container, ok := ncv.postConsensusContainer[msg.ValidatorIndex]
		if !ok {
			container = specssv.NewPartialSigContainer(validator.Quorum())
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
func (ncv *CommitteeObserver) resolveDuplicateSignature(container *specssv.PartialSigContainer, msg *spectypes.PartialSignatureMessage, share *ssvtypes.SSVShare) {
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
	ncv.Roots[beaconVote.BlockRoot] = spectypes.BNRoleSyncCommittee
	return nil
}
