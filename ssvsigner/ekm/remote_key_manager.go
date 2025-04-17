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
	"github.com/ssvlabs/eth2-key-manager/core"
	"github.com/ssvlabs/eth2-key-manager/signer"
	slashingprotection "github.com/ssvlabs/eth2-key-manager/slashing_protection"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

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
	logger          *zap.Logger
	netCfg          networkconfig.NetworkConfig
	signerClient    signerClient
	consensusClient consensusClient
	getOperatorId   func() spectypes.OperatorID
	operatorPubKey  keys.OperatorPublicKey
	signLocksMu     sync.RWMutex
	signLocks       map[signKey]*sync.RWMutex
	slashingProtector
}

type signerClient interface {
	AddValidators(ctx context.Context, shares ...ssvsigner.ShareKeys) error
	RemoveValidators(ctx context.Context, sharePubKeys ...phase0.BLSPubKey) error
	Sign(ctx context.Context, sharePubKey phase0.BLSPubKey, payload web3signer.SignRequest) (phase0.BLSSignature, error)
	OperatorIdentity(ctx context.Context) (string, error)
	OperatorSign(ctx context.Context, payload []byte) ([]byte, error)
}

type consensusClient interface {
	ForkAtEpoch(ctx context.Context, epoch phase0.Epoch) (*phase0.Fork, error)
	Genesis(ctx context.Context) (*eth2apiv1.Genesis, error)
}

// NewRemoteKeyManager returns a RemoteKeyManager that fetches the operator's public
// identity from the signerClient, sets up local slashing protection, and uses
// the provided consensusClient to get the current fork/genesis for sign requests.
func NewRemoteKeyManager(
	logger *zap.Logger,
	netCfg networkconfig.NetworkConfig,
	signerClient signerClient,
	consensusClient consensusClient,
	db basedb.Database,
	networkConfig networkconfig.NetworkConfig,
	getOperatorId func() spectypes.OperatorID,
) (*RemoteKeyManager, error) {
	signerStore := NewSignerStorage(db, networkConfig.Beacon, logger)
	protection := slashingprotection.NewNormalProtection(signerStore)

	operatorPubKeyString, err := signerClient.OperatorIdentity(context.Background()) // TODO: use context
	if err != nil {
		return nil, fmt.Errorf("get operator identity: %w", err)
	}

	operatorPubKey, err := keys.PublicKeyFromString(operatorPubKeyString)
	if err != nil {
		return nil, fmt.Errorf("extract operator public key: %w", err)
	}

	return &RemoteKeyManager{
		logger:            logger,
		netCfg:            netCfg,
		signerClient:      signerClient,
		consensusClient:   consensusClient,
		slashingProtector: NewSlashingProtector(logger, signerStore, protection),
		getOperatorId:     getOperatorId,
		operatorPubKey:    operatorPubKey,
		signLocks:         map[signKey]*sync.RWMutex{},
	}, nil
}

// AddShare registers a validator share with the remote service via signerClient.AddValidators
// and then calls BumpSlashingProtection on the local store. If remote or local operations
// fail, returns an error.
func (km *RemoteKeyManager) AddShare(
	ctx context.Context,
	encryptedPrivKey []byte,
	pubKey phase0.BLSPubKey,
) error {
	shareKeys := ssvsigner.ShareKeys{
		EncryptedPrivKey: hexutil.Bytes(encryptedPrivKey),
		PubKey:           pubKey,
	}

	if err := km.signerClient.AddValidators(ctx, shareKeys); err != nil {
		return fmt.Errorf("add validator: %w", err)
	}

	if err := km.BumpSlashingProtection(pubKey); err != nil {
		return fmt.Errorf("could not bump slashing protection: %w", err)
	}

	return nil
}

// RemoveShare unregisters a validator share with the remote service and removes
// its highest attestation/proposal data locally. If the remote or local operations
// fail, returns an error.
func (km *RemoteKeyManager) RemoveShare(ctx context.Context, pubKey phase0.BLSPubKey) error {
	if err := km.signerClient.RemoveValidators(ctx, pubKey); err != nil {
		return fmt.Errorf("remove validator: %w", err)
	}

	if err := km.RemoveHighestAttestation(pubKey); err != nil {
		return fmt.Errorf("could not remove highest attestation: %w", err)
	}

	if err := km.RemoveHighestProposal(pubKey); err != nil {
		return fmt.Errorf("could not remove highest proposal: %w", err)
	}

	return nil
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
	epoch := km.netCfg.Beacon.EstimatedEpochAtSlot(slot)

	forkInfo, err := km.getForkInfo(ctx, epoch)
	if err != nil {
		return spectypes.Signature{}, phase0.Root{}, fmt.Errorf("get fork info: %w", err)
	}

	req := web3signer.SignRequest{
		ForkInfo: forkInfo,
	}

	switch signatureDomain {
	case spectypes.DomainAttester:
		val := km.lock(sharePubkey, "attestation")
		val.Lock()
		defer val.Unlock()

		data, err := km.handleDomainAttester(obj, sharePubkey)
		if err != nil {
			return spectypes.Signature{}, phase0.Root{}, err
		}

		req.Type = web3signer.TypeAttestation
		req.Attestation = data

	case spectypes.DomainProposer:
		val := km.lock(sharePubkey, "proposal")
		val.Lock()
		defer val.Unlock()

		block, err := km.handleDomainProposer(obj, sharePubkey)
		if err != nil {
			return spectypes.Signature{}, phase0.Root{}, err
		}

		req.Type = web3signer.TypeBlockV2
		req.BeaconBlock = block

	case spectypes.DomainVoluntaryExit:
		data, ok := obj.(*phase0.VoluntaryExit)
		if !ok {
			return nil, phase0.Root{}, errors.New("could not cast obj to VoluntaryExit")
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
			return nil, phase0.Root{}, fmt.Errorf("obj type is unknown: %T", obj)
		}

	case spectypes.DomainSelectionProof:
		data, ok := obj.(spectypes.SSZUint64)
		if !ok {
			return nil, phase0.Root{}, errors.New("could not cast obj to SSZUint64")
		}

		req.Type = web3signer.TypeAggregationSlot
		req.AggregationSlot = &web3signer.AggregationSlot{Slot: phase0.Slot(data)}

	case spectypes.DomainRandao:
		data, ok := obj.(spectypes.SSZUint64)
		if !ok {
			return nil, phase0.Root{}, errors.New("could not cast obj to SSZUint64")
		}

		req.Type = web3signer.TypeRandaoReveal
		req.RandaoReveal = &web3signer.RandaoReveal{Epoch: phase0.Epoch(data)}

	case spectypes.DomainSyncCommittee:
		val := km.lock(sharePubkey, "sync_committee")
		val.Lock()
		defer val.Unlock()

		data, ok := obj.(spectypes.SSZBytes)
		if !ok {
			return nil, phase0.Root{}, errors.New("could not cast obj to SSZBytes")
		}

		req.Type = web3signer.TypeSyncCommitteeMessage
		req.SyncCommitteeMessage = &web3signer.SyncCommitteeMessage{
			BeaconBlockRoot: phase0.Root(data),
			Slot:            slot,
		}

	case spectypes.DomainSyncCommitteeSelectionProof:
		val := km.lock(sharePubkey, "sync_committee_selection_data")
		val.Lock()
		defer val.Unlock()

		data, ok := obj.(*altair.SyncAggregatorSelectionData)
		if !ok {
			return nil, phase0.Root{}, errors.New("could not cast obj to SyncAggregatorSelectionData")
		}

		req.Type = web3signer.TypeSyncCommitteeSelectionProof
		req.SyncAggregatorSelectionData = &web3signer.SyncCommitteeAggregatorSelection{
			Slot:              data.Slot,
			SubcommitteeIndex: phase0.CommitteeIndex(data.SubcommitteeIndex),
		}

	case spectypes.DomainContributionAndProof:
		val := km.lock(sharePubkey, "sync_committee_selection_and_proof")
		val.Lock()
		defer val.Unlock()

		data, ok := obj.(*altair.ContributionAndProof)
		if !ok {
			return nil, phase0.Root{}, errors.New("could not cast obj to ContributionAndProof")
		}

		req.Type = web3signer.TypeSyncCommitteeContributionAndProof
		req.ContributionAndProof = data

	case spectypes.DomainApplicationBuilder:
		data, ok := obj.(*eth2apiv1.ValidatorRegistration)
		if !ok {
			return nil, phase0.Root{}, errors.New("could not cast obj to ValidatorRegistration")
		}

		req.Type = web3signer.TypeValidatorRegistration
		req.ValidatorRegistration = data
	default:
		return nil, phase0.Root{}, errors.New("domain unknown")
	}

	root, err := spectypes.ComputeETHSigningRoot(obj, domain)
	if err != nil {
		return nil, phase0.Root{}, fmt.Errorf("compute root: %w", err)
	}

	req.SigningRoot = root

	sig, err := km.signerClient.Sign(ctx, sharePubkey, req)
	if err != nil {
		return spectypes.Signature{}, phase0.Root{}, fmt.Errorf("remote signer: %w", err)
	}

	return sig[:], root, nil
}

func (km *RemoteKeyManager) handleDomainAttester(
	obj ssz.HashRoot,
	sharePubkey phase0.BLSPubKey,
) (*phase0.AttestationData, error) {
	data, ok := obj.(*phase0.AttestationData)
	if !ok {
		return nil, errors.New("could not cast obj to AttestationData")
	}

	network := core.Network(km.netCfg.Beacon.GetBeaconNetwork())
	if !signer.IsValidFarFutureEpoch(network, data.Target.Epoch) {
		return nil, fmt.Errorf("target epoch too far into the future")
	}
	if !signer.IsValidFarFutureEpoch(network, data.Source.Epoch) {
		return nil, fmt.Errorf("source epoch too far into the future")
	}

	if err := km.IsAttestationSlashable(sharePubkey, data); err != nil {
		return nil, err
	}

	if err := km.UpdateHighestAttestation(sharePubkey, data); err != nil {
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

	network := core.Network(km.netCfg.Beacon.GetBeaconNetwork())
	if !signer.IsValidFarFutureSlot(network, blockSlot) {
		return nil, fmt.Errorf("proposed block slot too far into the future")
	}

	if err := km.IsBeaconBlockSlashable(sharePubkey, blockSlot); err != nil {
		return nil, err
	}

	if err := km.UpdateHighestProposal(sharePubkey, blockSlot); err != nil {
		return nil, err
	}

	return ret, nil
}

func (km *RemoteKeyManager) getForkInfo(ctx context.Context, epoch phase0.Epoch) (web3signer.ForkInfo, error) {
	currentFork, err := km.consensusClient.ForkAtEpoch(ctx, epoch)
	if err != nil {
		return web3signer.ForkInfo{}, fmt.Errorf("get current fork: %w", err)
	}

	genesis, err := km.consensusClient.Genesis(ctx)
	if err != nil {
		return web3signer.ForkInfo{}, fmt.Errorf("get genesis: %w", err)
	}

	return web3signer.ForkInfo{
		Fork:                  currentFork,
		GenesisValidatorsRoot: genesis.GenesisValidatorsRoot,
	}, nil
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

type signKey struct {
	pubkey    phase0.BLSPubKey
	operation string
}

func (km *RemoteKeyManager) lock(sharePubkey phase0.BLSPubKey, operation string) *sync.RWMutex {
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
