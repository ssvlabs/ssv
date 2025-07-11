package ekm

import (
	"context"
	"errors"
	"fmt"
	"sync"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	apiv1capella "github.com/attestantio/go-eth2-client/api/v1/capella"
	apiv1deneb "github.com/attestantio/go-eth2-client/api/v1/deneb"
	apiv1electra "github.com/attestantio/go-eth2-client/api/v1/electra"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/capella"
	"github.com/attestantio/go-eth2-client/spec/deneb"
	"github.com/attestantio/go-eth2-client/spec/electra"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ssz "github.com/ferranbt/fastssz"
	"github.com/ssvlabs/eth2-key-manager/signer"
	slashingprotection "github.com/ssvlabs/eth2-key-manager/slashing_protection"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/storage/basedb"

	"github.com/ssvlabs/ssv/ssvsigner"
	"github.com/ssvlabs/ssv/ssvsigner/keys"
	"github.com/ssvlabs/ssv/ssvsigner/web3signer"
)

// RemoteKeyManager implements KeyManager by delegating signing operations to
// a remote signing service (via signerClient). Validator shares are
// registered or removed on the remote side, while minimal slashing protection
// data is still maintained locally to prevent slashable requests.
//
// RemoteKeyManager doesn't use operator private key as it's stored externally in the remote signer.
type RemoteKeyManager struct {
	logger       *zap.Logger
	beaconConfig *networkconfig.Beacon
	signerClient signerClient

	getOperatorId     func() spectypes.OperatorID
	operatorPubKey    keys.OperatorPublicKey
	signLocksMu       sync.RWMutex
	signLocks         map[signKey]*sync.RWMutex
	slashingProtector slashingProtector
}

type signerClient interface {
	AddValidators(ctx context.Context, shares ...ssvsigner.ShareKeys) ([]web3signer.Status, error)
	RemoveValidators(ctx context.Context, pubKeys ...phase0.BLSPubKey) (statuses []web3signer.Status, err error)
	Sign(ctx context.Context, sharePubKey phase0.BLSPubKey, payload web3signer.SignRequest) (phase0.BLSSignature, error)
	OperatorIdentity(ctx context.Context) (string, error)
	OperatorSign(ctx context.Context, payload []byte) ([]byte, error)
}

// NewRemoteKeyManager returns a RemoteKeyManager that fetches the operator's public
// identity from the signerClient, sets up local slashing protection, and uses
// the provided consensusClient to get the current fork/genesis for sign requests.
func NewRemoteKeyManager(
	ctx context.Context,
	logger *zap.Logger,
	beaconConfig *networkconfig.Beacon,
	signerClient signerClient,
	db basedb.Database,
	getOperatorId func() spectypes.OperatorID,
) (*RemoteKeyManager, error) {
	signerStore := NewSignerStorage(db, beaconConfig, logger)
	protection := slashingprotection.NewNormalProtection(signerStore)

	operatorPubKeyString, err := signerClient.OperatorIdentity(ctx)
	if err != nil {
		return nil, fmt.Errorf("get operator identity: %w", err)
	}

	operatorPubKey, err := keys.PublicKeyFromString(operatorPubKeyString)
	if err != nil {
		return nil, fmt.Errorf("extract operator public key: %w", err)
	}

	return &RemoteKeyManager{
		logger:            logger,
		beaconConfig:      beaconConfig,
		signerClient:      signerClient,
		slashingProtector: NewSlashingProtector(logger, beaconConfig, signerStore, protection),
		getOperatorId:     getOperatorId,
		operatorPubKey:    operatorPubKey,
		signLocks:         map[signKey]*sync.RWMutex{},
	}, nil
}

// AddShare registers a validator share with the remote service via signerClient.AddValidators
// and then calls BumpSlashingProtectionTxn on the local store. If remote or local operations
// fail, returns an error.
func (km *RemoteKeyManager) AddShare(
	ctx context.Context,
	txn basedb.Txn,
	encryptedPrivKey []byte,
	pubKey phase0.BLSPubKey,
) error {
	if err := km.slashingProtector.BumpSlashingProtectionTxn(txn, pubKey); err != nil {
		return fmt.Errorf("could not bump slashing protection: %w", err)
	}

	shareKeys := ssvsigner.ShareKeys{
		EncryptedPrivKey: hexutil.Bytes(encryptedPrivKey),
		PubKey:           pubKey,
	}

	// If txn gets rolled back after share is saved,
	// there will be some inconsistency between syncer state and remote signer.
	// However, syncer crashes node on an error and restarts the sync process from the failing block,
	// so it will attempt to save the same share again, which won't be an issue
	// because AddValidators doesn't fail if the same share exists.
	statuses, err := km.signerClient.AddValidators(ctx, shareKeys)
	if err != nil {
		return fmt.Errorf("add validator: %w", err)
	}

	for _, status := range statuses {
		switch status {
		case web3signer.StatusImported:

		case web3signer.StatusDuplicated:
			// A failed request does not guarantee that the keys were not added.
			// It's possible that the ssv-signer successfully added the keys,
			// but a network error occurred before a response could be received.
			// Or, if ssv-signer is behind a load balancer, the load balancer may return an error.
			// In such cases, the node would crash and, upon restarting, encounter a duplicate key error.
			// To handle this gracefully, we allow returning a duplicate key error without treating it as a failure.
			km.logger.Warn("Attempted to add already existing share to the remote signer. " +
				"This is expected in the first block after failed sync")

		default:
			return fmt.Errorf("unexpected status %s", status)
		}
	}

	return nil
}

// RemoveShare unregisters a validator share with the remote service and removes
// its highest attestation/proposal data locally. If the remote or local operations
// fail, returns an error.
func (km *RemoteKeyManager) RemoveShare(ctx context.Context, txn basedb.Txn, pubKey phase0.BLSPubKey) error {
	// Similarly to addition, if txn gets rolled back after share is removed,
	// there will be some inconsistency between syncer state and remote signer.
	// After restart, it will attempt to delete the same share again, which won't be an issue
	// because RemoveValidators doesn't fail if the share doesn't exist.
	statuses, err := km.signerClient.RemoveValidators(ctx, pubKey)
	if err != nil {
		return fmt.Errorf("remove validator: %w", err)
	}

	for _, status := range statuses {
		switch status {
		case web3signer.StatusDeleted:

		case web3signer.StatusNotFound:
			// A failed request does not guarantee that the keys were not deleted.
			// It's possible that the ssv-signer successfully deleted the keys,
			// but a network error occurred before a response could be received.
			// Or, if ssv-signer is behind a load balancer, the load balancer may return an error.
			// In such cases, the node would crash and, upon restarting, encounter a not found key error.
			// To handle this gracefully, we allow returning a not found key error without treating it as a failure.
			km.logger.Warn("Attempted to delete non-existing share from the remote signer. " +
				"This is expected in the first block after failed sync")

		default:
			return fmt.Errorf("unexpected status %s", status)
		}
	}

	if err := km.slashingProtector.RemoveHighestAttestationTxn(txn, pubKey); err != nil {
		return fmt.Errorf("could not remove highest attestation: %w", err)
	}

	if err := km.slashingProtector.RemoveHighestProposalTxn(txn, pubKey); err != nil {
		return fmt.Errorf("could not remove highest proposal: %w", err)
	}

	return nil
}

func (km *RemoteKeyManager) IsAttestationSlashable(pubKey phase0.BLSPubKey, attData *phase0.AttestationData) error {
	return km.slashingProtector.IsAttestationSlashable(pubKey, attData)
}

func (km *RemoteKeyManager) IsBeaconBlockSlashable(pubKey phase0.BLSPubKey, slot phase0.Slot) error {
	return km.slashingProtector.IsBeaconBlockSlashable(pubKey, slot)
}

func (km *RemoteKeyManager) BumpSlashingProtection(txn basedb.Txn, pubKey phase0.BLSPubKey) error {
	attLock := km.lock(pubKey, lockAttestation)
	attLock.Lock()
	defer attLock.Unlock()

	propLock := km.lock(pubKey, lockProposal)
	propLock.Lock()
	defer propLock.Unlock()

	return km.slashingProtector.BumpSlashingProtectionTxn(txn, pubKey)
}

// SignBeaconObject checks slashing conditions locally for attestation and beacon block,
// then constructs a SignRequest for the remote signerClient. If slashable, returns an error immediately.
// Otherwise, forwards to the remote service. It returns signature as well as the computed signing root.
func (km *RemoteKeyManager) SignBeaconObject(
	ctx context.Context,
	obj ssz.HashRoot,
	domain phase0.Domain,
	sharePubkey phase0.BLSPubKey,
	slot phase0.Slot,
	signatureDomain phase0.DomainType,
) (spectypes.Signature, phase0.Root, error) {
	req, root, err := km.prepareSignRequest(obj, domain, sharePubkey, slot, signatureDomain)
	if err != nil {
		return nil, phase0.Root{}, err
	}

	sig, err := km.signerClient.Sign(ctx, sharePubkey, req)
	if err != nil {
		return nil, phase0.Root{}, fmt.Errorf("remote signer: %w", err)
	}

	return sig[:], root, nil
}

func (km *RemoteKeyManager) prepareSignRequest(
	obj ssz.HashRoot,
	domain phase0.Domain,
	sharePubkey phase0.BLSPubKey,
	slot phase0.Slot,
	signatureDomain phase0.DomainType,
) (web3signer.SignRequest, phase0.Root, error) {
	epoch := km.beaconConfig.EstimatedEpochAtSlot(slot)

	req := web3signer.SignRequest{
		ForkInfo: km.getForkInfo(epoch),
	}

	switch signatureDomain {
	case spectypes.DomainAttester:
		val := km.lock(sharePubkey, lockAttestation)
		val.Lock()
		defer val.Unlock()

		data, err := km.handleDomainAttester(obj, sharePubkey)
		if err != nil {
			return web3signer.SignRequest{}, phase0.Root{}, err
		}

		req.Type = web3signer.TypeAttestation
		req.Attestation = data

	case spectypes.DomainProposer:
		val := km.lock(sharePubkey, lockProposal)
		val.Lock()
		defer val.Unlock()

		block, err := km.handleDomainProposer(obj, sharePubkey)
		if err != nil {
			return web3signer.SignRequest{}, phase0.Root{}, err
		}

		req.Type = web3signer.TypeBlockV2
		req.BeaconBlock = block

	case spectypes.DomainVoluntaryExit:
		data, ok := obj.(*phase0.VoluntaryExit)
		if !ok {
			return web3signer.SignRequest{}, phase0.Root{}, errors.New("could not cast obj to VoluntaryExit")
		}

		req.Type = web3signer.TypeVoluntaryExit
		req.VoluntaryExit = data

	case spectypes.DomainAggregateAndProof:
		req.Type = web3signer.TypeAggregateAndProof

		switch v := obj.(type) {
		case *phase0.AggregateAndProof:
			req.AggregateAndProof = &web3signer.AggregateAndProof{
				Phase0: v,
			}
		case *electra.AggregateAndProof:
			req.AggregateAndProof = &web3signer.AggregateAndProof{
				Electra: v,
			}
		default:
			return web3signer.SignRequest{}, phase0.Root{}, fmt.Errorf("obj type is unknown: %T", obj)
		}

	case spectypes.DomainSelectionProof:
		data, ok := obj.(spectypes.SSZUint64)
		if !ok {
			return web3signer.SignRequest{}, phase0.Root{}, errors.New("could not cast obj to SSZUint64")
		}

		req.Type = web3signer.TypeAggregationSlot
		req.AggregationSlot = &web3signer.AggregationSlot{Slot: phase0.Slot(data)}

	case spectypes.DomainRandao:
		data, ok := obj.(spectypes.SSZUint64)
		if !ok {
			return web3signer.SignRequest{}, phase0.Root{}, errors.New("could not cast obj to SSZUint64")
		}

		req.Type = web3signer.TypeRandaoReveal
		req.RandaoReveal = &web3signer.RandaoReveal{Epoch: phase0.Epoch(data)}

	case spectypes.DomainSyncCommittee:
		val := km.lock(sharePubkey, lockSyncCommittee)
		val.Lock()
		defer val.Unlock()

		data, ok := obj.(spectypes.SSZBytes)
		if !ok {
			return web3signer.SignRequest{}, phase0.Root{}, errors.New("could not cast obj to SSZBytes")
		}

		req.Type = web3signer.TypeSyncCommitteeMessage
		req.SyncCommitteeMessage = &web3signer.SyncCommitteeMessage{
			BeaconBlockRoot: phase0.Root(data),
			Slot:            slot,
		}

	case spectypes.DomainSyncCommitteeSelectionProof:
		val := km.lock(sharePubkey, lockSyncCommitteeSelectionData)
		val.Lock()
		defer val.Unlock()

		data, ok := obj.(*altair.SyncAggregatorSelectionData)
		if !ok {
			return web3signer.SignRequest{}, phase0.Root{}, errors.New("could not cast obj to SyncAggregatorSelectionData")
		}

		req.Type = web3signer.TypeSyncCommitteeSelectionProof
		req.SyncAggregatorSelectionData = &web3signer.SyncCommitteeAggregatorSelection{
			Slot:              data.Slot,
			SubcommitteeIndex: phase0.CommitteeIndex(data.SubcommitteeIndex),
		}

	case spectypes.DomainContributionAndProof:
		val := km.lock(sharePubkey, lockSyncCommitteeSelectionAndProof)
		val.Lock()
		defer val.Unlock()

		data, ok := obj.(*altair.ContributionAndProof)
		if !ok {
			return web3signer.SignRequest{}, phase0.Root{}, errors.New("could not cast obj to ContributionAndProof")
		}

		req.Type = web3signer.TypeSyncCommitteeContributionAndProof
		req.ContributionAndProof = data

	case spectypes.DomainApplicationBuilder:
		data, ok := obj.(*eth2apiv1.ValidatorRegistration)
		if !ok {
			return web3signer.SignRequest{}, phase0.Root{}, errors.New("could not cast obj to ValidatorRegistration")
		}

		req.Type = web3signer.TypeValidatorRegistration
		req.ValidatorRegistration = data
	default:
		return web3signer.SignRequest{}, phase0.Root{}, errors.New("domain unknown")
	}

	root, err := spectypes.ComputeETHSigningRoot(obj, domain)
	if err != nil {
		return web3signer.SignRequest{}, phase0.Root{}, fmt.Errorf("compute root: %w", err)
	}
	req.SigningRoot = root

	return req, root, nil
}

func (km *RemoteKeyManager) handleDomainAttester(
	obj ssz.HashRoot,
	sharePubkey phase0.BLSPubKey,
) (*phase0.AttestationData, error) {
	data, ok := obj.(*phase0.AttestationData)
	if !ok {
		return nil, errors.New("could not cast obj to AttestationData")
	}

	if !signer.IsValidFarFutureEpoch(km.beaconConfig, data.Target.Epoch) {
		return nil, fmt.Errorf("target epoch too far into the future")
	}
	if !signer.IsValidFarFutureEpoch(km.beaconConfig, data.Source.Epoch) {
		return nil, fmt.Errorf("source epoch too far into the future")
	}

	if err := km.slashingProtector.IsAttestationSlashable(sharePubkey, data); err != nil {
		return nil, err
	}

	if err := km.slashingProtector.UpdateHighestAttestation(sharePubkey, data); err != nil {
		return nil, err
	}

	return data, nil
}

func (km *RemoteKeyManager) handleDomainProposer(
	obj ssz.HashRoot,
	sharePubkey phase0.BLSPubKey,
) (*web3signer.BeaconBlockData, error) {
	var ret *web3signer.BeaconBlockData

	switch v := obj.(type) {
	case *capella.BeaconBlock:
		bodyRoot, err := v.Body.HashTreeRoot()
		if err != nil {
			return nil, fmt.Errorf("could not hash beacon block (capella): %w", err)
		}

		ret = &web3signer.BeaconBlockData{
			Version: web3signer.DataVersion(spec.DataVersionCapella),
			BlockHeader: &phase0.BeaconBlockHeader{
				Slot:          v.Slot,
				ProposerIndex: v.ProposerIndex,
				ParentRoot:    v.ParentRoot,
				StateRoot:     v.StateRoot,
				BodyRoot:      bodyRoot,
			},
		}
	case *deneb.BeaconBlock:
		bodyRoot, err := v.Body.HashTreeRoot()
		if err != nil {
			return nil, fmt.Errorf("could not hash beacon block (deneb): %w", err)
		}

		ret = &web3signer.BeaconBlockData{
			Version: web3signer.DataVersion(spec.DataVersionDeneb),
			BlockHeader: &phase0.BeaconBlockHeader{
				Slot:          v.Slot,
				ProposerIndex: v.ProposerIndex,
				ParentRoot:    v.ParentRoot,
				StateRoot:     v.StateRoot,
				BodyRoot:      bodyRoot,
			},
		}

	case *electra.BeaconBlock:
		bodyRoot, err := v.Body.HashTreeRoot()
		if err != nil {
			return nil, fmt.Errorf("could not hash beacon block (electra): %w", err)
		}

		ret = &web3signer.BeaconBlockData{
			Version: web3signer.DataVersion(spec.DataVersionElectra),
			BlockHeader: &phase0.BeaconBlockHeader{
				Slot:          v.Slot,
				ProposerIndex: v.ProposerIndex,
				ParentRoot:    v.ParentRoot,
				StateRoot:     v.StateRoot,
				BodyRoot:      bodyRoot,
			},
		}

	case *apiv1capella.BlindedBeaconBlock:
		bodyRoot, err := v.Body.HashTreeRoot()
		if err != nil {
			return nil, fmt.Errorf("could not hash blinded beacon block (capella): %w", err)
		}

		ret = &web3signer.BeaconBlockData{
			Version: web3signer.DataVersion(spec.DataVersionCapella),
			BlockHeader: &phase0.BeaconBlockHeader{
				Slot:          v.Slot,
				ProposerIndex: v.ProposerIndex,
				ParentRoot:    v.ParentRoot,
				StateRoot:     v.StateRoot,
				BodyRoot:      bodyRoot,
			},
		}

	case *apiv1deneb.BlindedBeaconBlock:
		bodyRoot, err := v.Body.HashTreeRoot()
		if err != nil {
			return nil, fmt.Errorf("could not hash blinded beacon block (deneb): %w", err)
		}

		ret = &web3signer.BeaconBlockData{
			Version: web3signer.DataVersion(spec.DataVersionDeneb),
			BlockHeader: &phase0.BeaconBlockHeader{
				Slot:          v.Slot,
				ProposerIndex: v.ProposerIndex,
				ParentRoot:    v.ParentRoot,
				StateRoot:     v.StateRoot,
				BodyRoot:      bodyRoot,
			},
		}

	case *apiv1electra.BlindedBeaconBlock:
		bodyRoot, err := v.Body.HashTreeRoot()
		if err != nil {
			return nil, fmt.Errorf("could not hash blinded beacon block (electra): %w", err)
		}

		ret = &web3signer.BeaconBlockData{
			Version: web3signer.DataVersion(spec.DataVersionElectra),
			BlockHeader: &phase0.BeaconBlockHeader{
				Slot:          v.Slot,
				ProposerIndex: v.ProposerIndex,
				ParentRoot:    v.ParentRoot,
				StateRoot:     v.StateRoot,
				BodyRoot:      bodyRoot,
			},
		}

	default:
		return nil, fmt.Errorf("obj type is unknown: %T", obj)
	}

	blockSlot := ret.BlockHeader.Slot

	if !signer.IsValidFarFutureSlot(km.beaconConfig, blockSlot) {
		return nil, fmt.Errorf("proposed block slot too far into the future")
	}

	if err := km.slashingProtector.IsBeaconBlockSlashable(sharePubkey, blockSlot); err != nil {
		return nil, err
	}

	if err := km.slashingProtector.UpdateHighestProposal(sharePubkey, blockSlot); err != nil {
		return nil, err
	}

	return ret, nil
}

func (km *RemoteKeyManager) getForkInfo(epoch phase0.Epoch) web3signer.ForkInfo {
	_, currentFork := km.beaconConfig.ForkAtEpoch(epoch)

	return web3signer.ForkInfo{
		Fork:                  currentFork,
		GenesisValidatorsRoot: km.beaconConfig.GenesisValidatorsRoot,
	}
}

func (km *RemoteKeyManager) Sign(payload []byte) ([]byte, error) {
	return km.signerClient.OperatorSign(context.Background(), payload) // TODO: use context
}

func (km *RemoteKeyManager) Public() keys.OperatorPublicKey {
	return km.operatorPubKey
}

func (km *RemoteKeyManager) SignSSVMessage(ssvMsg *spectypes.SSVMessage) ([]byte, error) {
	encodedMsg, err := ssvMsg.Encode()
	if err != nil {
		return nil, err
	}

	return km.signerClient.OperatorSign(context.Background(), encodedMsg) // TODO: use context
}

func (km *RemoteKeyManager) GetOperatorID() spectypes.OperatorID {
	return km.getOperatorId()
}

type lockOperation int

const (
	lockAttestation lockOperation = iota
	lockProposal
	lockSyncCommittee
	lockSyncCommitteeSelectionData
	lockSyncCommitteeSelectionAndProof
)

type signKey struct {
	pubkey    phase0.BLSPubKey
	operation lockOperation
}

func (km *RemoteKeyManager) lock(sharePubkey phase0.BLSPubKey, operation lockOperation) *sync.RWMutex {
	km.signLocksMu.Lock()
	defer km.signLocksMu.Unlock()

	key := signKey{
		pubkey:    sharePubkey,
		operation: operation,
	}
	if val, ok := km.signLocks[key]; ok {
		return val
	}

	km.signLocks[key] = &sync.RWMutex{}
	return km.signLocks[key]
}
