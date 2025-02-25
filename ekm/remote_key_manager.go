package ekm

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	apiv1capella "github.com/attestantio/go-eth2-client/api/v1/capella"
	apiv1deneb "github.com/attestantio/go-eth2-client/api/v1/deneb"
	apiv1electra "github.com/attestantio/go-eth2-client/api/v1/electra"
	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/capella"
	"github.com/attestantio/go-eth2-client/spec/deneb"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	slashingprotection "github.com/ssvlabs/eth2-key-manager/slashing_protection"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/beacon/goclient"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/operator/keys"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/ssvsigner/remotesigner/web3signer"
	"github.com/ssvlabs/ssv/ssvsigner/ssvsignerclient"
	"github.com/ssvlabs/ssv/storage/basedb"
)

type RemoteKeyManager struct {
	logger          *zap.Logger
	client          *ssvsignerclient.SSVSignerClient
	consensusClient *goclient.GoClient
	getOperatorId   func() spectypes.OperatorID
	retryCount      int
	operatorPubKey  keys.OperatorPublicKey
	Provider
}

func NewRemoteKeyManager(
	client *ssvsignerclient.SSVSignerClient,
	consensusClient *goclient.GoClient,
	db basedb.Database,
	networkConfig networkconfig.NetworkConfig,
	getOperatorId func() spectypes.OperatorID,
	options ...Option,
) (*RemoteKeyManager, error) {
	s := &RemoteKeyManager{}
	for _, option := range options {
		option(s)
	}

	signerStore := NewSignerStorage(db, networkConfig.Beacon, s.logger)
	protection := slashingprotection.NewNormalProtection(signerStore)

	slashingProtector, err := NewSlashingProtector(s.logger, signerStore, protection)
	if err != nil {
		s.logger.Fatal("could not create new slashing protector", zap.Error(err))
	}

	operatorPubKeyString, err := client.GetOperatorIdentity()
	if err != nil {
		return nil, fmt.Errorf("get operator identity: %w", err)
	}

	operatorPubKey, err := keys.PublicKeyFromString(operatorPubKeyString)
	if err != nil {
		return nil, fmt.Errorf("extract operator public key: %w", err)
	}

	s.client = client
	s.consensusClient = consensusClient
	s.Provider = slashingProtector
	s.getOperatorId = getOperatorId
	s.operatorPubKey = operatorPubKey

	return s, nil
}

type Option func(signer *RemoteKeyManager)

func WithLogger(logger *zap.Logger) Option {
	return func(s *RemoteKeyManager) {
		s.logger = logger.Named("remote_key_manager")
	}
}

func WithRetryCount(n int) Option {
	return func(s *RemoteKeyManager) {
		s.retryCount = n
	}
}

func (km *RemoteKeyManager) AddShare(encryptedShare []byte) error {
	var statuses []ssvsignerclient.Status
	var publicKeys [][]byte
	f := func() error {
		var err error
		statuses, publicKeys, err = km.client.AddValidators(encryptedShare)
		return err
	}
	err := km.retryFunc(f, "AddValidators")
	if err != nil {
		return fmt.Errorf("add validator: %w", err)
	}

	if statuses[0] == ssvsignerclient.StatusImported || statuses[0] == ssvsignerclient.StatusDuplicated {
		if err := km.BumpSlashingProtection(publicKeys[0]); err != nil {
			return fmt.Errorf("could not bump slashing protection: %w", err)
		}
	}

	return nil
}

func (km *RemoteKeyManager) RemoveShare(pubKey []byte) error {
	statuses, err := km.client.RemoveValidators(pubKey)
	if err != nil {
		return fmt.Errorf("remove validator: %w", err)
	}

	if statuses[0] == ssvsignerclient.StatusDeleted {
		if err := km.RemoveHighestAttestation(pubKey); err != nil {
			return fmt.Errorf("could not remove highest attestation: %w", err)
		}
		if err := km.RemoveHighestProposal(pubKey); err != nil {
			return fmt.Errorf("could not remove highest proposal: %w", err)
		}
	}

	return nil
}

func (km *RemoteKeyManager) retryFunc(f func() error, funcName string) error {
	if km.retryCount < 2 {
		return f()
	}

	var multiErr error
	for i := 1; i <= km.retryCount; i++ {
		err := f()
		if err == nil {
			return nil
		}
		var shareDecryptionError ssvsignerclient.ShareDecryptionError
		if errors.As(err, &shareDecryptionError) {
			return ShareDecryptionError(err)
		}
		multiErr = errors.Join(multiErr, err)
		km.logger.Warn("call failed", zap.Error(err), zap.Int("attempt", i), zap.String("func", funcName))
	}

	return fmt.Errorf("no successful result after %d attempts: %w", km.retryCount, multiErr)
}

func (km *RemoteKeyManager) SignBeaconObject(
	obj ssz.HashRoot,
	domain phase0.Domain,
	sharePubkey []byte,
	signatureDomain phase0.DomainType,
) (spectypes.Signature, [32]byte, error) {
	forkInfo, err := km.getForkInfo(context.Background()) // TODO: consider passing context to SignBeaconObject
	if err != nil {
		return spectypes.Signature{}, [32]byte{}, fmt.Errorf("get fork info: %w", err)
	}

	req := web3signer.SignRequest{
		ForkInfo: forkInfo,
	}

	switch signatureDomain {
	case spectypes.DomainAttester:
		data, ok := obj.(*phase0.AttestationData)
		if !ok {
			return nil, [32]byte{}, errors.New("could not cast obj to AttestationData")
		}

		if err := km.IsAttestationSlashable(sharePubkey, data); err != nil {
			return nil, [32]byte{}, err
		}

		if err := km.UpdateHighestAttestation(sharePubkey, data); err != nil {
			return nil, [32]byte{}, err
		}

		req.Type = web3signer.Attestation
		req.Attestation = data

	case spectypes.DomainProposer:
		switch v := obj.(type) {
		case *capella.BeaconBlock, *deneb.BeaconBlock:
			return nil, [32]byte{}, fmt.Errorf("web3signer supports only blinded blocks since bellatrix") // https://github.com/Consensys/web3signer/blob/85ed009955d4a5bbccba5d5248226093987e7f6f/core/src/main/java/tech/pegasys/web3signer/core/service/http/handlers/signing/eth2/BlockRequest.java#L29

		case *apiv1capella.BlindedBeaconBlock:
			req.Type = web3signer.BlockV2
			bodyRoot, err := v.Body.HashTreeRoot()
			if err != nil {
				return nil, [32]byte{}, fmt.Errorf("could not hash beacon block (capella): %w", err)
			}

			if err := km.IsBeaconBlockSlashable(sharePubkey, v.Slot); err != nil {
				return nil, [32]byte{}, err
			}

			if err = km.UpdateHighestProposal(sharePubkey, v.Slot); err != nil {
				return nil, [32]byte{}, err
			}

			req.BeaconBlock = &web3signer.BeaconBlockData{
				Version: "CAPELLA",
				BlockHeader: &phase0.BeaconBlockHeader{
					Slot:          v.Slot,
					ProposerIndex: v.ProposerIndex,
					ParentRoot:    v.ParentRoot,
					StateRoot:     v.StateRoot,
					BodyRoot:      bodyRoot,
				},
			}

		case *apiv1deneb.BlindedBeaconBlock:
			req.Type = web3signer.BlockV2
			bodyRoot, err := v.Body.HashTreeRoot()
			if err != nil {
				return nil, [32]byte{}, fmt.Errorf("could not hash beacon block (deneb): %w", err)
			}

			if err := km.IsBeaconBlockSlashable(sharePubkey, v.Slot); err != nil {
				return nil, [32]byte{}, err
			}

			if err = km.UpdateHighestProposal(sharePubkey, v.Slot); err != nil {
				return nil, [32]byte{}, err
			}

			req.BeaconBlock = &web3signer.BeaconBlockData{
				Version: "DENEB",
				BlockHeader: &phase0.BeaconBlockHeader{
					Slot:          v.Slot,
					ProposerIndex: v.ProposerIndex,
					ParentRoot:    v.ParentRoot,
					StateRoot:     v.StateRoot,
					BodyRoot:      bodyRoot,
				},
			}

		case *apiv1electra.BlindedBeaconBlock:
			req.Type = web3signer.BlockV2
			bodyRoot, err := v.Body.HashTreeRoot()
			if err != nil {
				return nil, [32]byte{}, fmt.Errorf("could not hash beacon block (electra): %w", err)
			}

			if err := km.IsBeaconBlockSlashable(sharePubkey, v.Slot); err != nil {
				return nil, [32]byte{}, err
			}

			if err = km.UpdateHighestProposal(sharePubkey, v.Slot); err != nil {
				return nil, [32]byte{}, err
			}

			req.BeaconBlock = &web3signer.BeaconBlockData{
				Version: "ELECTRA",
				BlockHeader: &phase0.BeaconBlockHeader{
					Slot:          v.Slot,
					ProposerIndex: v.ProposerIndex,
					ParentRoot:    v.ParentRoot,
					StateRoot:     v.StateRoot,
					BodyRoot:      bodyRoot,
				},
			}

		default:
			return nil, [32]byte{}, fmt.Errorf("obj type is unknown: %T", obj)
		}

	case spectypes.DomainVoluntaryExit:
		data, ok := obj.(*phase0.VoluntaryExit)
		if !ok {
			return nil, [32]byte{}, errors.New("could not cast obj to VoluntaryExit")
		}

		req.Type = web3signer.VoluntaryExit
		req.VoluntaryExit = data

	case spectypes.DomainAggregateAndProof:
		data, ok := obj.(*phase0.AggregateAndProof)
		if !ok {
			return nil, [32]byte{}, errors.New("could not cast obj to AggregateAndProof")
		}

		req.Type = web3signer.AggregateAndProof
		req.AggregateAndProof = data

	case spectypes.DomainSelectionProof:
		data, ok := obj.(spectypes.SSZUint64)
		if !ok {
			return nil, [32]byte{}, errors.New("could not cast obj to SSZUint64")
		}

		req.Type = web3signer.AggregationSlot
		req.AggregationSlot = &web3signer.AggregationSlotData{Slot: phase0.Slot(data)}

	case spectypes.DomainRandao:
		data, ok := obj.(spectypes.SSZUint64)
		if !ok {
			return nil, [32]byte{}, errors.New("could not cast obj to SSZUint64")
		}

		req.Type = web3signer.RandaoReveal
		req.RandaoReveal = &web3signer.RandaoRevealData{Epoch: phase0.Epoch(data)}

	case spectypes.DomainSyncCommittee:
		data, ok := obj.(ssvtypes.BlockRootWithSlot)
		if !ok {
			return nil, [32]byte{}, errors.New("could not cast obj to SSZBytes")
		}

		req.Type = web3signer.SyncCommitteeMessage
		req.SyncCommitteeMessage = &web3signer.SyncCommitteeMessageData{
			BeaconBlockRoot: phase0.Root(data.SSZBytes),
			Slot:            data.Slot,
		}

	case spectypes.DomainSyncCommitteeSelectionProof:
		data, ok := obj.(*altair.SyncAggregatorSelectionData)
		if !ok {
			return nil, [32]byte{}, errors.New("could not cast obj to SyncAggregatorSelectionData")
		}

		req.Type = web3signer.SyncCommitteeSelectionProof
		req.SyncAggregatorSelectionData = &web3signer.SyncCommitteeAggregatorSelectionData{
			Slot:              data.Slot,
			SubcommitteeIndex: phase0.CommitteeIndex(data.SubcommitteeIndex),
		}

	case spectypes.DomainContributionAndProof:
		data, ok := obj.(*altair.ContributionAndProof)
		if !ok {
			return nil, [32]byte{}, errors.New("could not cast obj to ContributionAndProof")
		}

		req.Type = web3signer.SyncCommitteeContributionAndProof
		req.ContributionAndProof = data

	case spectypes.DomainApplicationBuilder:
		data, ok := obj.(*eth2apiv1.ValidatorRegistration)
		if !ok {
			return nil, [32]byte{}, errors.New("could not cast obj to ValidatorRegistration")
		}

		req.Type = web3signer.ValidatorRegistration
		req.ValidatorRegistration = data
	default:
		return nil, [32]byte{}, errors.New("domain unknown")
	}

	root, err := spectypes.ComputeETHSigningRoot(obj, domain)
	if err != nil {
		return nil, [32]byte{}, err
	}

	req.SigningRoot = hex.EncodeToString(root[:])

	sig, err := km.client.Sign(sharePubkey, req)
	if err != nil {
		return spectypes.Signature{}, [32]byte{}, err
	}
	return sig, root, nil
}

func (km *RemoteKeyManager) getForkInfo(ctx context.Context) (web3signer.ForkInfo, error) {
	// ForkSchedule result is cached in the client and updated once in a while.
	currentFork, err := km.consensusClient.CurrentFork(ctx)
	if err != nil {
		return web3signer.ForkInfo{}, fmt.Errorf("get current fork: %w", err)
	}

	// Genesis result is cached in the client and updated once in a while.
	genesis, err := km.consensusClient.Genesis(ctx)
	if err != nil {
		return web3signer.ForkInfo{}, fmt.Errorf("get genesis: %w", err)
	}
	if genesis == nil {
		return web3signer.ForkInfo{}, fmt.Errorf("genesis is nil")
	}
	return web3signer.ForkInfo{
		Fork:                  currentFork,
		GenesisValidatorsRoot: genesis.GenesisValidatorsRoot,
	}, nil
}

func (km *RemoteKeyManager) Sign(payload []byte) ([]byte, error) {
	return km.client.OperatorSign(payload)
}

func (km *RemoteKeyManager) Public() keys.OperatorPublicKey {
	return km.operatorPubKey
}

func (km *RemoteKeyManager) SignSSVMessage(ssvMsg *spectypes.SSVMessage) ([]byte, error) {
	encodedMsg, err := ssvMsg.Encode()
	if err != nil {
		return nil, err
	}

	return km.client.OperatorSign(encodedMsg)
}

func (km *RemoteKeyManager) GetOperatorID() spectypes.OperatorID {
	return km.getOperatorId()
}
