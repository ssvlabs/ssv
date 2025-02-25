package remotekeymanager

import (
	"encoding/hex"
	"errors"
	"fmt"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	apiv1capella "github.com/attestantio/go-eth2-client/api/v1/capella"
	apiv1deneb "github.com/attestantio/go-eth2-client/api/v1/deneb"
	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/capella"
	"github.com/attestantio/go-eth2-client/spec/deneb"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	"github.com/ssvlabs/eth2-key-manager/core"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/beacon/goclient"
	"github.com/ssvlabs/ssv/ekm"
	"github.com/ssvlabs/ssv/operator/keys"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/ssvsigner/remotesigner/web3signer"
	"github.com/ssvlabs/ssv/ssvsigner/ssvsignerclient"
)

type KeyManager struct {
	logger          *zap.Logger
	client          *ssvsignerclient.SSVSignerClient
	consensusClient *goclient.GoClient
	keyManager      ekm.KeyManager
	getOperatorId   func() spectypes.OperatorID
	retryCount      int
}

func New(
	client *ssvsignerclient.SSVSignerClient,
	consensusClient *goclient.GoClient,
	keyManager ekm.KeyManager,
	getOperatorId func() spectypes.OperatorID,
	options ...Option,
) (*KeyManager, error) {
	s := &KeyManager{
		client:          client,
		consensusClient: consensusClient,
		keyManager:      keyManager,
		getOperatorId:   getOperatorId,
	}

	for _, option := range options {
		option(s)
	}

	return s, nil
}

type Option func(signer *KeyManager)

func WithLogger(logger *zap.Logger) Option {
	return func(s *KeyManager) {
		s.logger = logger.Named("remote_key_manager")
	}
}

func WithRetryCount(n int) Option {
	return func(s *KeyManager) {
		s.retryCount = n
	}
}

func (km *KeyManager) ListAccounts() ([]core.ValidatorAccount, error) {
	return km.keyManager.ListAccounts()
}

func (km *KeyManager) RetrieveHighestAttestation(pubKey []byte) (*phase0.AttestationData, bool, error) {
	return km.keyManager.RetrieveHighestAttestation(pubKey)
}

func (km *KeyManager) RetrieveHighestProposal(pubKey []byte) (phase0.Slot, bool, error) {
	return km.keyManager.RetrieveHighestProposal(pubKey)
}

func (km *KeyManager) BumpSlashingProtection(pubKey []byte) error {
	return km.keyManager.BumpSlashingProtection(pubKey)
}

func (km *KeyManager) RemoveHighestAttestation(pubKey []byte) error {
	return km.keyManager.RemoveHighestAttestation(pubKey)
}

func (km *KeyManager) RemoveHighestProposal(pubKey []byte) error {
	return km.keyManager.RemoveHighestProposal(pubKey)
}

func (km *KeyManager) IsAttestationSlashable(pk spectypes.ShareValidatorPK, data *phase0.AttestationData) error {
	return km.keyManager.IsAttestationSlashable(pk, data)
}

func (km *KeyManager) IsBeaconBlockSlashable(pk []byte, slot phase0.Slot) error {
	return km.keyManager.IsBeaconBlockSlashable(pk, slot)
}

func (km *KeyManager) UpdateHighestAttestation(pk []byte, attestationData *phase0.AttestationData) error {
	return km.keyManager.UpdateHighestAttestation(pk, attestationData)
}

func (km *KeyManager) UpdateHighestProposal(pk []byte, slot phase0.Slot) error {
	return km.keyManager.UpdateHighestProposal(pk, slot)
}

func (km *KeyManager) AddShare(encryptedShare []byte) error {
	var statuses []ssvsignerclient.Status
	var publicKeys [][]byte
	f := func() error {
		var err error
		statuses, publicKeys, err = km.client.AddValidators(encryptedShare)
		return err
	}
	err := km.retryFunc(f)
	if err != nil {
		return fmt.Errorf("add validator: %w", err)
	}

	if statuses[0] == ssvsignerclient.StatusImported || statuses[0] == ssvsignerclient.StatusDuplicated {
		if err := km.keyManager.BumpSlashingProtection(publicKeys[0]); err != nil {
			return fmt.Errorf("could not bump slashing protection: %w", err)
		}
	}

	return nil
}

func (km *KeyManager) RemoveShare(pubKey []byte) error {
	statuses, err := km.client.RemoveValidators(pubKey)
	if err != nil {
		return fmt.Errorf("remove validator: %w", err)
	}

	if statuses[0] == ssvsignerclient.StatusDeleted {
		if err := km.keyManager.RemoveHighestAttestation(pubKey); err != nil {
			return fmt.Errorf("could not remove highest attestation: %w", err)
		}
		if err := km.keyManager.RemoveHighestProposal(pubKey); err != nil {
			return fmt.Errorf("could not remove highest proposal: %w", err)
		}
	}

	return nil
}

func (km *KeyManager) retryFunc(f func() error) error {
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
			return shareDecryptionError
		}
		multiErr = errors.Join(multiErr, err)
	}

	return fmt.Errorf("no successful result after %d attempts: %w", km.retryCount, multiErr)
}

func (km *KeyManager) SignBeaconObject(
	obj ssz.HashRoot,
	domain phase0.Domain,
	sharePubkey []byte,
	signatureDomain phase0.DomainType,
) (spectypes.Signature, [32]byte, error) {
	forkInfo, err := km.getForkInfo()
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

		if err := km.keyManager.IsAttestationSlashable(sharePubkey, data); err != nil {
			return nil, [32]byte{}, err
		}

		if err := km.keyManager.UpdateHighestAttestation(sharePubkey, data); err != nil {
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

			if err := km.keyManager.IsBeaconBlockSlashable(sharePubkey, v.Slot); err != nil {
				return nil, [32]byte{}, err
			}

			if err = km.keyManager.UpdateHighestProposal(sharePubkey, v.Slot); err != nil {
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

			if err := km.keyManager.IsBeaconBlockSlashable(sharePubkey, v.Slot); err != nil {
				return nil, [32]byte{}, err
			}

			if err = km.keyManager.UpdateHighestProposal(sharePubkey, v.Slot); err != nil {
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

func (km *KeyManager) getForkInfo() (web3signer.ForkInfo, error) {
	// TODO: find a better way to manage this
	denebForkHolesky := web3signer.ForkType{ // TODO: electra
		PreviousVersion: "0x04017000",
		CurrentVersion:  "0x05017000",
		Epoch:           29696,
	}

	genesis := km.consensusClient.Genesis()
	if genesis == nil {
		return web3signer.ForkInfo{}, fmt.Errorf("genesis is not ready")
	}
	return web3signer.ForkInfo{
		Fork:                  denebForkHolesky,
		GenesisValidatorsRoot: hex.EncodeToString(genesis.GenesisValidatorsRoot[:]),
	}, nil
}

func (km *KeyManager) Sign(payload []byte) ([]byte, error) {
	return km.client.OperatorSign(payload)
}

func (km *KeyManager) Public() keys.OperatorPublicKey {
	pubkeyString, err := km.client.GetOperatorIdentity() // TODO: cache it
	if err != nil {
		return nil // TODO: handle, consider changing the interface to return error
	}

	pubkey, err := keys.PublicKeyFromString(pubkeyString)
	if err != nil {
		return nil // TODO: handle, consider changing the interface to return error
	}

	return pubkey
}

func (km *KeyManager) SignSSVMessage(ssvMsg *spectypes.SSVMessage) ([]byte, error) {
	encodedMsg, err := ssvMsg.Encode()
	if err != nil {
		return nil, err
	}

	return km.client.OperatorSign(encodedMsg)
}

func (km *KeyManager) GetOperatorID() spectypes.OperatorID {
	return km.getOperatorId()
}
