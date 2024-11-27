package validator

import (
	"encoding/hex"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/jellydator/ttlcache/v3"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/exporter/convert"
	"github.com/ssvlabs/ssv/ibft/storage"
	"github.com/ssvlabs/ssv/logging/fields"
	msgvalidation "github.com/ssvlabs/ssv/message/validation"
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
	logger            *zap.Logger
	netCfg            networkconfig.NetworkConfig
	Storage           *storage.QBFTStores
	beaconNetwork     beacon.BeaconNetwork
	qbftController    *qbftcontroller.Controller
	ValidatorStore    registrystorage.ValidatorStore
	newDecidedHandler qbftcontroller.NewDecidedHandler
	attesterRoots     *ttlcache.Cache[phase0.Root, struct{}]
	syncCommRoots     *ttlcache.Cache[phase0.Root, struct{}]
	domainCache       *DomainCache
	// TODO: consider using round-robin container as []map[phase0.ValidatorIndex]*ssv.PartialSigContainer similar to what is used in OperatorState
	postConsensusContainer map[phase0.Slot]map[phase0.ValidatorIndex]*ssv.PartialSigContainer
}

type CommitteeObserverOptions struct {
	FullNode          bool
	Logger            *zap.Logger
	NetworkConfig     networkconfig.NetworkConfig
	Network           specqbft.Network
	Storage           *storage.QBFTStores
	Operator          *spectypes.CommitteeMember
	OperatorSigner    ssvtypes.OperatorSigner
	NewDecidedHandler qbftctrl.NewDecidedHandler
	ValidatorStore    registrystorage.ValidatorStore
	AttesterRoots     *ttlcache.Cache[phase0.Root, struct{}]
	SyncCommRoots     *ttlcache.Cache[phase0.Root, struct{}]
	DomainCache       *DomainCache
}

func NewCommitteeObserver(identifier convert.MessageID, opts CommitteeObserverOptions) *CommitteeObserver {
	// currently, only need domain & storage
	config := &qbft.Config{
		Domain:      opts.NetworkConfig.DomainType(),
		Network:     opts.Network,
		CutOffRound: roundtimer.CutOffRound,
	}

	// TODO: does the specific operator matters?

	ctrl := qbftcontroller.NewController(identifier[:], opts.Operator, config, opts.OperatorSigner, opts.FullNode)
	ctrl.StoredInstances = make(qbftcontroller.InstanceContainer, 0, nonCommitteeInstanceContainerCapacity(opts.FullNode))

	co := &CommitteeObserver{
		qbftController:    ctrl,
		logger:            opts.Logger,
		netCfg:            opts.NetworkConfig,
		Storage:           opts.Storage,
		beaconNetwork:     opts.NetworkConfig.Beacon,
		ValidatorStore:    opts.ValidatorStore,
		newDecidedHandler: opts.NewDecidedHandler,
		attesterRoots:     opts.AttesterRoots,
		syncCommRoots:     opts.SyncCommRoots,
		domainCache:       opts.DomainCache,
	}
	co.postConsensusContainer = make(map[phase0.Slot]map[phase0.ValidatorIndex]*ssv.PartialSigContainer, co.postConsensusContainerCapacity())

	return co
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
		var operatorIDs []string
		for _, share := range quorum {
			operatorIDs = append(operatorIDs, strconv.FormatUint(share, 10))
		}

		validator, exists := ncv.ValidatorStore.ValidatorByIndex(key.ValidatorIndex)
		if !exists {
			return fmt.Errorf("could not find share for validator with index %d", key.ValidatorIndex)
		}

		beaconRoles := ncv.getBeaconRoles(msg, key.Root)
		if len(beaconRoles) == 0 {
			logger.Warn("no roles found for quorum root",
				zap.Uint64("validator_index", uint64(key.ValidatorIndex)),
				fields.Validator(validator.ValidatorPubKey[:]),
				zap.String("signers", strings.Join(operatorIDs, ", ")),
				fields.BlockRoot(key.Root),
				zap.String("qbft_ctrl_identifier", hex.EncodeToString(ncv.qbftController.Identifier)),
			)
		}

		for _, beaconRole := range beaconRoles {
			msgID := convert.NewMsgID(ncv.qbftController.GetConfig().GetSignatureDomainType(), validator.ValidatorPubKey[:], beaconRole)
			roleStorage := ncv.Storage.Get(msgID.GetRoleType())
			if roleStorage == nil {
				return fmt.Errorf("role storage doesn't exist: %v", beaconRole)
			}

			updated, err := roleStorage.UpdateParticipants(msgID, slot, quorum)
			if err != nil {
				return fmt.Errorf("update participants: %w", err)
			}

			if !updated {
				continue
			}

			logger.Info("✅ saved participants",
				zap.String("converted_role", beaconRole.ToBeaconRole()),
				zap.Uint64("validator_index", uint64(key.ValidatorIndex)),
				fields.Validator(validator.ValidatorPubKey[:]),
				zap.String("signers", strings.Join(operatorIDs, ", ")),
				zap.String("msg_id", hex.EncodeToString(msgID[:])),
				fields.BlockRoot(key.Root),
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

func (ncv *CommitteeObserver) getBeaconRoles(msg *queue.SSVMessage, root phase0.Root) []convert.RunnerRole {
	if msg.MsgID.GetRoleType() == spectypes.RoleCommittee {
		attester := ncv.attesterRoots.Get(root)
		syncCommittee := ncv.syncCommRoots.Get(root)

		switch {
		case attester != nil && syncCommittee != nil:
			return []convert.RunnerRole{convert.RoleAttester, convert.RoleSyncCommittee}
		case attester != nil:
			return []convert.RunnerRole{convert.RoleAttester}
		case syncCommittee != nil:
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
	Root           phase0.Root
}

func (ncv *CommitteeObserver) processMessage(
	signedMsg *spectypes.PartialSignatureMessages,
) (map[validatorIndexAndRoot][]spectypes.OperatorID, error) {
	quorums := make(map[validatorIndexAndRoot][]spectypes.OperatorID)

	currentSlot := signedMsg.Slot
	slotValidators, exist := ncv.postConsensusContainer[currentSlot]
	if !exist {
		slotValidators = make(map[phase0.ValidatorIndex]*ssv.PartialSigContainer)
		ncv.postConsensusContainer[signedMsg.Slot] = slotValidators
	}

	for _, msg := range signedMsg.Messages {
		validator, exists := ncv.ValidatorStore.ValidatorByIndex(msg.ValidatorIndex)
		if !exists {
			return nil, fmt.Errorf("could not find share for validator with index %d", msg.ValidatorIndex)
		}
		container, ok := slotValidators[msg.ValidatorIndex]
		if !ok {
			container = ssv.NewPartialSigContainer(validator.Quorum())
			slotValidators[msg.ValidatorIndex] = container
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

	// Remove older slots container
	if len(ncv.postConsensusContainer) >= ncv.postConsensusContainerCapacity() {
		// #nosec G115 -- capacity must be low epoch not to cause overflow
		thresholdSlot := currentSlot - phase0.Slot(ncv.postConsensusContainerCapacity())
		for slot := range ncv.postConsensusContainer {
			if slot < thresholdSlot {
				delete(ncv.postConsensusContainer, slot)
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
func (ncv *CommitteeObserver) verifyBeaconPartialSignature(signer uint64, signature spectypes.Signature, root phase0.Root, share *ssvtypes.SSVShare) error {
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
	beaconVote := &spectypes.BeaconVote{}
	if err := beaconVote.Decode(msg.SignedSSVMessage.FullData); err != nil {
		ncv.logger.Debug("❗ failed to get beacon vote data", zap.Error(err))
		return err
	}

	qbftMsg, ok := msg.Body.(*specqbft.Message)
	if !ok {
		ncv.logger.Fatal("unreachable: OnProposalMsg must be called only on qbft messages")
	}

	epoch := ncv.beaconNetwork.EstimatedEpochAtSlot(phase0.Slot(qbftMsg.Height))

	if err := ncv.saveAttesterRoots(epoch, beaconVote, qbftMsg); err != nil {
		return err
	}

	if err := ncv.saveSyncCommRoots(epoch, beaconVote); err != nil {
		return err
	}

	return nil
}

func (ncv *CommitteeObserver) saveAttesterRoots(epoch phase0.Epoch, beaconVote *spectypes.BeaconVote, qbftMsg *specqbft.Message) error {
	attesterDomain, err := ncv.domainCache.Get(epoch, spectypes.DomainAttester)
	if err != nil {
		return err
	}

	for committeeIndex := phase0.CommitteeIndex(0); committeeIndex < 64; committeeIndex++ {
		attestationData := constructAttestationData(beaconVote, phase0.Slot(qbftMsg.Height), committeeIndex)
		attesterRoot, err := spectypes.ComputeETHSigningRoot(attestationData, attesterDomain)
		if err != nil {
			return err
		}

		ncv.attesterRoots.Set(attesterRoot, struct{}{}, ttlcache.DefaultTTL)
	}

	return nil
}

func (ncv *CommitteeObserver) saveSyncCommRoots(epoch phase0.Epoch, beaconVote *spectypes.BeaconVote) error {
	syncCommDomain, err := ncv.domainCache.Get(epoch, spectypes.DomainSyncCommittee)
	if err != nil {
		return err
	}

	blockRoot := spectypes.SSZBytes(beaconVote.BlockRoot[:])
	syncCommitteeRoot, err := spectypes.ComputeETHSigningRoot(blockRoot, syncCommDomain)
	if err != nil {
		return err
	}

	ncv.syncCommRoots.Set(syncCommitteeRoot, struct{}{}, ttlcache.DefaultTTL)

	return nil
}

func (ncv *CommitteeObserver) postConsensusContainerCapacity() int {
	// #nosec G115 -- slots per epoch must be low epoch not to cause overflow
	return int(ncv.netCfg.SlotsPerEpoch()) + msgvalidation.LateSlotAllowance
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
