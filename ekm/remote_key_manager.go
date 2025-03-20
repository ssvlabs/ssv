package ekm

import (
	"context"
	"errors"
	"fmt"

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
	slashingprotection "github.com/ssvlabs/eth2-key-manager/slashing_protection"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/operator/keys"
	"github.com/ssvlabs/ssv/ssvsigner"
	"github.com/ssvlabs/ssv/ssvsigner/web3signer"
	"github.com/ssvlabs/ssv/storage/basedb"
)

type RemoteKeyManager struct {
	logger          *zap.Logger
	netCfg          networkconfig.NetworkConfig
	remoteSigner    RemoteSigner
	consensusClient ConsensusClient
	getOperatorId   func() spectypes.OperatorID
	operatorPubKey  keys.OperatorPublicKey
	SlashingProtector
}

type RemoteSigner interface {
	AddValidators(ctx context.Context, shares ...ssvsigner.ShareKeys) ([]web3signer.Status, error)
	RemoveValidators(ctx context.Context, sharePubKeys ...phase0.BLSPubKey) ([]web3signer.Status, error)
	Sign(ctx context.Context, sharePubKey phase0.BLSPubKey, payload web3signer.SignRequest) (phase0.BLSSignature, error)
	OperatorIdentity(ctx context.Context) (string, error)
	OperatorSign(ctx context.Context, payload []byte) ([]byte, error)
}

type ConsensusClient interface {
	ForkAtEpoch(ctx context.Context, epoch phase0.Epoch) (*phase0.Fork, error)
	Genesis(ctx context.Context) (*eth2apiv1.Genesis, error)
}

func NewRemoteKeyManager(
	logger *zap.Logger,
	netCfg networkconfig.NetworkConfig,
	remoteSigner RemoteSigner,
	consensusClient ConsensusClient,
	db basedb.Database,
	networkConfig networkconfig.NetworkConfig,
	getOperatorId func() spectypes.OperatorID,
) (*RemoteKeyManager, error) {
	signerStore := NewSignerStorage(db, networkConfig.Beacon, logger)
	protection := slashingprotection.NewNormalProtection(signerStore)

	operatorPubKeyString, err := remoteSigner.OperatorIdentity(context.Background()) // TODO: use context
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
		remoteSigner:      remoteSigner,
		consensusClient:   consensusClient,
		SlashingProtector: NewSlashingProtector(logger, signerStore, protection),
		getOperatorId:     getOperatorId,
		operatorPubKey:    operatorPubKey,
	}, nil
}

func (km *RemoteKeyManager) AddShare(
	ctx context.Context,
	encryptedSharePrivKey []byte,
	sharePubKey phase0.BLSPubKey,
) error {
	shareKeys := ssvsigner.ShareKeys{
		EncryptedPrivKey: hexutil.Bytes(encryptedSharePrivKey),
		PublicKey:        sharePubKey,
	}

	statuses, err := km.remoteSigner.AddValidators(ctx, shareKeys)
	if err != nil {
		return fmt.Errorf("add validator: %w", err)
	}

	// AddValidators validates response length.
	if statuses[0] != web3signer.StatusImported {
		return fmt.Errorf("unexpected status %s", statuses[0])
	}

	if err := km.BumpSlashingProtection(sharePubKey); err != nil {
		return fmt.Errorf("could not bump slashing protection: %w", err)
	}

	return nil
}

func (km *RemoteKeyManager) RemoveShare(ctx context.Context, pubKey phase0.BLSPubKey) error {
	statuses, err := km.remoteSigner.RemoveValidators(ctx, pubKey)
	if err != nil {
		return fmt.Errorf("remove validator: %w", err)
	}

	// RemoveValidators validates response length.
	if statuses[0] != web3signer.StatusDeleted {
		return fmt.Errorf("received status %s", statuses[0])
	}

	if err := km.RemoveHighestAttestation(pubKey); err != nil {
		return fmt.Errorf("could not remove highest attestation: %w", err)
	}
	if err := km.RemoveHighestProposal(pubKey); err != nil {
		return fmt.Errorf("could not remove highest proposal: %w", err)
	}

	return nil
}

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
		data, ok := obj.(*phase0.AttestationData)
		if !ok {
			return nil, phase0.Root{}, errors.New("could not cast obj to AttestationData")
		}

		if err := km.IsAttestationSlashable(sharePubkey, data); err != nil {
			return nil, phase0.Root{}, err
		}

		if err := km.UpdateHighestAttestation(sharePubkey, data); err != nil {
			return nil, phase0.Root{}, err
		}

		req.Type = web3signer.TypeAttestation
		req.Attestation = data

	case spectypes.DomainProposer:
		switch v := obj.(type) {
		case *capella.BeaconBlock, *deneb.BeaconBlock, *electra.BeaconBlock:
			return nil, phase0.Root{}, fmt.Errorf("web3signer supports only blinded blocks since bellatrix") // https://github.com/Consensys/web3signer/blob/85ed009955d4a5bbccba5d5248226093987e7f6f/core/src/main/java/tech/pegasys/web3signer/core/service/http/handlers/signing/eth2/BlockRequest.java#L29

		case *apiv1capella.BlindedBeaconBlock:
			req.Type = web3signer.TypeBlockV2
			bodyRoot, err := v.Body.HashTreeRoot()
			if err != nil {
				return nil, phase0.Root{}, fmt.Errorf("could not hash beacon block (capella): %w", err)
			}

			if err := km.IsBeaconBlockSlashable(sharePubkey, v.Slot); err != nil {
				return nil, phase0.Root{}, err
			}

			if err = km.UpdateHighestProposal(sharePubkey, v.Slot); err != nil {
				return nil, phase0.Root{}, err
			}

			req.BeaconBlock = &web3signer.BeaconBlockData{
				Version: spec.DataVersionCapella,
				BlockHeader: &phase0.BeaconBlockHeader{
					Slot:          v.Slot,
					ProposerIndex: v.ProposerIndex,
					ParentRoot:    v.ParentRoot,
					StateRoot:     v.StateRoot,
					BodyRoot:      bodyRoot,
				},
			}

		case *apiv1deneb.BlindedBeaconBlock:
			req.Type = web3signer.TypeBlockV2
			bodyRoot, err := v.Body.HashTreeRoot()
			if err != nil {
				return nil, phase0.Root{}, fmt.Errorf("could not hash beacon block (deneb): %w", err)
			}

			if err := km.IsBeaconBlockSlashable(sharePubkey, v.Slot); err != nil {
				return nil, phase0.Root{}, err
			}

			if err = km.UpdateHighestProposal(sharePubkey, v.Slot); err != nil {
				return nil, phase0.Root{}, err
			}

			req.BeaconBlock = &web3signer.BeaconBlockData{
				Version: spec.DataVersionDeneb,
				BlockHeader: &phase0.BeaconBlockHeader{
					Slot:          v.Slot,
					ProposerIndex: v.ProposerIndex,
					ParentRoot:    v.ParentRoot,
					StateRoot:     v.StateRoot,
					BodyRoot:      bodyRoot,
				},
			}

		case *apiv1electra.BlindedBeaconBlock:
			req.Type = web3signer.TypeBlockV2
			bodyRoot, err := v.Body.HashTreeRoot()
			if err != nil {
				return nil, phase0.Root{}, fmt.Errorf("could not hash beacon block (electra): %w", err)
			}

			if err := km.IsBeaconBlockSlashable(sharePubkey, v.Slot); err != nil {
				return nil, phase0.Root{}, err
			}

			if err = km.UpdateHighestProposal(sharePubkey, v.Slot); err != nil {
				return nil, phase0.Root{}, err
			}

			req.BeaconBlock = &web3signer.BeaconBlockData{
				Version: spec.DataVersionElectra,
				BlockHeader: &phase0.BeaconBlockHeader{
					Slot:          v.Slot,
					ProposerIndex: v.ProposerIndex,
					ParentRoot:    v.ParentRoot,
					StateRoot:     v.StateRoot,
					BodyRoot:      bodyRoot,
				},
			}

		default:
			return nil, phase0.Root{}, fmt.Errorf("obj type is unknown: %T", obj)
		}

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

	sig, err := km.remoteSigner.Sign(ctx, sharePubkey, req)
	if err != nil {
		return spectypes.Signature{}, phase0.Root{}, fmt.Errorf("remote signer: %w", err)
	}
	return sig[:], root, nil
}

func (km *RemoteKeyManager) getForkInfo(ctx context.Context, epoch phase0.Epoch) (web3signer.ForkInfo, error) {
	// ForkSchedule result is cached in the client and updated once in a while.
	currentFork, err := km.consensusClient.ForkAtEpoch(ctx, epoch)
	if err != nil {
		return web3signer.ForkInfo{}, fmt.Errorf("get current fork: %w", err)
	}

	// Genesis result is cached in the client and updated once in a while.
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
	return km.remoteSigner.OperatorSign(context.Background(), payload) // TODO: use context
}

func (km *RemoteKeyManager) Public() keys.OperatorPublicKey {
	return km.operatorPubKey
}

func (km *RemoteKeyManager) SignSSVMessage(ssvMsg *spectypes.SSVMessage) ([]byte, error) {
	encodedMsg, err := ssvMsg.Encode()
	if err != nil {
		return nil, err
	}

	return km.remoteSigner.OperatorSign(context.Background(), encodedMsg) // TODO: use context
}

func (km *RemoteKeyManager) GetOperatorID() spectypes.OperatorID {
	return km.getOperatorId()
}
