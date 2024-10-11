package validator

import (
	"encoding/hex"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/herumi/bls-eth-go-binary/bls"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/exporter/convert"
	"github.com/ssvlabs/ssv/ibft/storage"
	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/ssvlabs/ssv/protocol/v2/qbft"
	qbftcontroller "github.com/ssvlabs/ssv/protocol/v2/qbft/controller"
	qbftctrl "github.com/ssvlabs/ssv/protocol/v2/qbft/controller"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/roundtimer"
	qbftstorage "github.com/ssvlabs/ssv/protocol/v2/qbft/storage"
	"github.com/ssvlabs/ssv/protocol/v2/ssv"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
	"github.com/ssvlabs/ssv/utils/casts"
)

type CommitteeObserver struct {
	logger                 *zap.Logger
	Storage                *storage.QBFTStores
	beaconNode             beacon.BeaconNode
	beaconNetwork          beacon.BeaconNetwork
	signer                 spectypes.BeaconSigner
	qbftController         *qbftcontroller.Controller
	ValidatorStore         registrystorage.ValidatorStore
	newDecidedHandler      qbftcontroller.NewDecidedHandler
	attesterRoots          map[[32]byte]struct{}
	syncCommitteeRoots     map[[32]byte]struct{}
	postConsensusContainer map[phase0.ValidatorIndex]*ssv.PartialSigContainer
}

type CommitteeObserverOptions struct {
	FullNode          bool
	Logger            *zap.Logger
	Network           specqbft.Network
	BeaconNetwork     beacon.BeaconNetwork
	BeaconNode        beacon.BeaconNode
	Signer            spectypes.BeaconSigner
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
		CutOffRound: roundtimer.CutOffRound,
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
		Storage:                opts.Storage,
		beaconNode:             opts.BeaconNode,
		beaconNetwork:          opts.BeaconNetwork,
		signer:                 opts.Signer,
		ValidatorStore:         opts.ValidatorStore,
		newDecidedHandler:      opts.NewDecidedHandler,
		attesterRoots:          make(map[[32]byte]struct{}),
		syncCommitteeRoots:     make(map[[32]byte]struct{}),
		postConsensusContainer: make(map[phase0.ValidatorIndex]*ssv.PartialSigContainer),
	}
}

func (ncv *CommitteeObserver) ProcessMessage(msg *queue.SSVMessage) error {
	role := msg.MsgID.GetRoleType()

	logger := ncv.logger.With(fields.Role(role))
	if role == spectypes.RoleCommittee {
		cid := spectypes.CommitteeID(msg.GetID().GetDutyExecutorID()[16:])
		logger = logger.With(fields.CommitteeID(cid))
	} else {
		validatorPK := msg.GetID().GetDutyExecutorID()
		logger = logger.With(fields.Validator(validatorPK))
	}

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
		roles := ncv.getRoles(msg, key.Root)

		if len(roles) == 0 {
			logger.Warn("NOT saved participants, roles not found",
				zap.Uint64("validator_index", uint64(key.ValidatorIndex)),
				zap.String("msg_id", hex.EncodeToString(msg.MsgID[:])),
				fields.BlockRoot(key.Root),
			)
		}

		for _, role := range roles {
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
				zap.String("msg_id", hex.EncodeToString(msgID[:])),
			)

			if ncv.newDecidedHandler != nil {
				ncv.newDecidedHandler(qbftstorage.ParticipantsRangeEntry{
					Slot:       slot,
					Signers:    quorum,
					Identifier: msgID,
				})
			}
		}
	}

	return nil
}

func (ncv *CommitteeObserver) getRoles(msg *queue.SSVMessage, root [32]byte) []convert.RunnerRole {
	if msg.MsgID.GetRoleType() == spectypes.RoleCommittee {
		_, foundAttester := ncv.attesterRoots[root]
		_, foundSyncCommittee := ncv.syncCommitteeRoots[root]

		switch {
		case foundAttester && foundSyncCommittee:
			return []convert.RunnerRole{convert.RoleAttester, convert.RoleSyncCommittee}
		case foundAttester:
			return []convert.RunnerRole{convert.RoleAttester}
		case foundSyncCommittee:
			return []convert.RunnerRole{convert.RoleSyncCommittee}
		default:
			return nil
		}
	}
	return []convert.RunnerRole{casts.RunnerRoleToConvertRole(msg.MsgID.GetRoleType())}
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
			key := validatorIndexAndRoot{ValidatorIndex: msg.ValidatorIndex, Root: msg.SigningRoot}
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

	qbftMsg, ok := msg.Body.(*specqbft.Message)
	if !ok {
		ncv.logger.Fatal("unreachable: OnProposalMsg must be called only on qbft messages")
	}

	epoch := ncv.beaconNetwork.EstimatedEpochAtSlot(phase0.Slot(qbftMsg.Height))
	committeeIndex := phase0.CommitteeIndex(0) //TODO committeeIndex is 0, is this correct? this is copied from ssv/runner/committee.go
	attestationData := constructAttestationData(beaconVote, phase0.Slot(qbftMsg.Height), committeeIndex)

	attesterDomain, err := ncv.beaconNode.DomainData(epoch, spectypes.DomainAttester)
	if err != nil {
		return err
	}

	syncCommitteeDomain, err := ncv.beaconNode.DomainData(epoch, spectypes.DomainSyncCommittee)
	if err != nil {
		return err
	}

	attesterRoot, err := spectypes.ComputeETHSigningRoot(attestationData, attesterDomain)
	if err != nil {
		return err
	}

	blockRoot := spectypes.SSZBytes(beaconVote.BlockRoot[:])
	syncCommitteeRoot, err := spectypes.ComputeETHSigningRoot(blockRoot, syncCommitteeDomain)
	if err != nil {
		return err
	}

	ncv.attesterRoots[attesterRoot] = struct{}{}
	ncv.logger.Info("saved attester block root", fields.BlockRoot(attesterRoot)) // TODO: remove or make debug

	ncv.syncCommitteeRoots[syncCommitteeRoot] = struct{}{}
	ncv.logger.Info("saved sync committee block root", fields.BlockRoot(syncCommitteeRoot)) // TODO: remove or make debug

	return nil
}

func constructAttestationData(vote *spectypes.BeaconVote, slot phase0.Slot, committeeIndex phase0.CommitteeIndex) *phase0.AttestationData {
	return &phase0.AttestationData{
		Slot:            slot,
		Index:           committeeIndex,
		BeaconBlockRoot: vote.BlockRoot,
		Source:          vote.Source,
		Target:          vote.Target,
	}
}
