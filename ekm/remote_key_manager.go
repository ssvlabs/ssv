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
	"github.com/attestantio/go-eth2-client/spec/electra"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	slashingprotection "github.com/ssvlabs/eth2-key-manager/slashing_protection"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	ssvsignerclient "github.com/ssvlabs/ssv/ssvsigner/client"
	"github.com/ssvlabs/ssv/ssvsigner/web3signer"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/operator/keys"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/storage/basedb"
)

type RemoteKeyManager struct {
	logger          *zap.Logger
	remoteSigner    RemoteSigner
	consensusClient ConsensusClient
	getOperatorId   func() spectypes.OperatorID
	retryCount      int
	operatorPubKey  keys.OperatorPublicKey
	SlashingProtector
}

type RemoteSigner interface {
	AddValidators(ctx context.Context, shares ...ssvsignerclient.ShareKeys) ([]web3signer.Status, error)
	RemoveValidators(ctx context.Context, sharePubKeys ...[]byte) ([]web3signer.Status, error)
	Sign(ctx context.Context, sharePubKey []byte, payload web3signer.SignRequest) ([]byte, error)
	OperatorIdentity(ctx context.Context) (string, error)
	OperatorSign(ctx context.Context, payload []byte) ([]byte, error)
}

type ConsensusClient interface {
	CurrentFork(ctx context.Context) (*phase0.Fork, error)
	Genesis(ctx context.Context) (*eth2apiv1.Genesis, error)
}

func NewRemoteKeyManager(
	logger *zap.Logger,
	remoteSigner RemoteSigner,
	consensusClient ConsensusClient,
	db basedb.Database,
	networkConfig networkconfig.NetworkConfig,
	getOperatorId func() spectypes.OperatorID,
	options ...Option,
) (*RemoteKeyManager, error) {
	signerStore := NewSignerStorage(db, networkConfig.Beacon, logger)
	protection := slashingprotection.NewNormalProtection(signerStore)

	sp, err := NewSlashingProtector(logger, signerStore, protection)
	if err != nil {
		logger.Fatal("could not create new slashing protector", zap.Error(err))
	}

	operatorPubKeyString, err := remoteSigner.OperatorIdentity(context.Background()) // TODO: use context
	if err != nil {
		return nil, fmt.Errorf("get operator identity: %w", err)
	}

	operatorPubKey, err := keys.PublicKeyFromString(operatorPubKeyString)
	if err != nil {
		return nil, fmt.Errorf("extract operator public key: %w", err)
	}

	s := &RemoteKeyManager{
		logger:            logger,
		remoteSigner:      remoteSigner,
		consensusClient:   consensusClient,
		SlashingProtector: sp,
		getOperatorId:     getOperatorId,
		operatorPubKey:    operatorPubKey,
	}

	for _, option := range options {
		option(s)
	}

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

func (km *RemoteKeyManager) AddShare(encryptedSharePrivKey, sharePubKey []byte) error {
	shareKeys := ssvsignerclient.ShareKeys{
		EncryptedPrivKey: encryptedSharePrivKey,
		PublicKey:        sharePubKey,
	}

	f := func(arg any) (any, error) {
		return km.remoteSigner.AddValidators(context.Background(), arg.(ssvsignerclient.ShareKeys)) // TODO: use context
	}

	res, err := km.retryFunc(f, shareKeys, "AddValidators")
	if err != nil {
		return fmt.Errorf("add validator: %w", err)
	}

	statuses, ok := res.([]web3signer.Status)
	if !ok {
		return fmt.Errorf("bug: expected []Status, got %T", res)
	}

	if len(statuses) != 1 {
		return fmt.Errorf("bug: expected 1 status, got %d", len(statuses))
	}

	if statuses[0] != web3signer.StatusImported {
		return fmt.Errorf("unexpected status %s", statuses[0])
	}

	if err := km.BumpSlashingProtection(sharePubKey); err != nil {
		return fmt.Errorf("could not bump slashing protection: %w", err)
	}

	return nil
}

func (km *RemoteKeyManager) RemoveShare(pubKey []byte) error {
	statuses, err := km.remoteSigner.RemoveValidators(context.Background(), pubKey) // TODO: use context
	if err != nil {
		return fmt.Errorf("remove validator: %w", err)
	}

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

func (km *RemoteKeyManager) retryFunc(f func(arg any) (any, error), arg any, funcName string) (any, error) {
	if km.retryCount < 2 {
		return f(arg)
	}

	var multiErr error
	for i := 1; i <= km.retryCount; i++ {
		v, err := f(arg)
		if err == nil {
			return v, nil
		}
		var shareDecryptionError ssvsignerclient.ShareDecryptionError
		if errors.As(err, &shareDecryptionError) {
			return nil, ShareDecryptionError(err)
		}
		multiErr = errors.Join(multiErr, err)
		km.logger.Warn("call failed", zap.Error(err), zap.Int("attempt", i), zap.String("func", funcName))
	}

	return nil, fmt.Errorf("no successful result after %d attempts: %w", km.retryCount, multiErr)
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
		case *capella.BeaconBlock, *deneb.BeaconBlock, *electra.BeaconBlock:
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
		req.Type = web3signer.AggregateAndProof

		switch v := obj.(type) {
		case *phase0.AggregateAndProof:
			req.AggregateAndProof = &web3signer.AggregateAndProofData{
				AggregatorIndex: v.AggregatorIndex,
				SelectionProof:  v.SelectionProof,
			}
			if v.Aggregate != nil {
				req.AggregateAndProof.Aggregate = &web3signer.AttestationData{
					AggregationBits: fmt.Sprintf("%#x", v.Aggregate.AggregationBits),
					Data:            v.Aggregate.Data,
					Signature:       v.Aggregate.Signature,
				}
			}
		case *electra.AggregateAndProof:
			req.AggregateAndProof = &web3signer.AggregateAndProofData{
				AggregatorIndex: v.AggregatorIndex,
				SelectionProof:  v.SelectionProof,
			}
			if v.Aggregate != nil {
				req.AggregateAndProof.Aggregate = &web3signer.AttestationData{
					AggregationBits: fmt.Sprintf("%#x", []byte(v.Aggregate.AggregationBits)),
					Data:            v.Aggregate.Data,
					Signature:       v.Aggregate.Signature,
					CommitteeBits:   fmt.Sprintf("%#x", v.Aggregate.CommitteeBits),
				}
			}
		default:
			return nil, [32]byte{}, fmt.Errorf("obj type is unknown: %T", obj)
		}

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

	sig, err := km.remoteSigner.Sign(context.Background(), sharePubkey, req) // TODO: use context
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
