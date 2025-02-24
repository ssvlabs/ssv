package ssvsigner

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
	ssvsignerclient "github.com/ssvlabs/ssv-signer/client"
	"github.com/ssvlabs/ssv-signer/web3signer"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/beacon/goclient"
	"github.com/ssvlabs/ssv/ekm"
	"github.com/ssvlabs/ssv/operator/keys"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

type SSVSigner struct {
	logger          *zap.Logger
	client          *ssvsignerclient.SSVSignerClient
	consensusClient *goclient.GoClient
	keyManager      ekm.KeyManager
	getOperatorId   func() spectypes.OperatorID
}

func New(
	logger *zap.Logger,
	client *ssvsignerclient.SSVSignerClient,
	consensusClient *goclient.GoClient,
	keyManager ekm.KeyManager,
	getOperatorId func() spectypes.OperatorID,
) (*SSVSigner, error) {
	return &SSVSigner{
		logger:          logger.Named("SSVSigner"),
		client:          client,
		consensusClient: consensusClient,
		keyManager:      keyManager,
		getOperatorId:   getOperatorId,
	}, nil
}

func (s *SSVSigner) ListAccounts() ([]core.ValidatorAccount, error) {
	return s.keyManager.ListAccounts()
}

func (s *SSVSigner) RetrieveHighestAttestation(pubKey []byte) (*phase0.AttestationData, bool, error) {
	return s.keyManager.RetrieveHighestAttestation(pubKey)
}

func (s *SSVSigner) RetrieveHighestProposal(pubKey []byte) (phase0.Slot, bool, error) {
	return s.keyManager.RetrieveHighestProposal(pubKey)
}

func (s *SSVSigner) BumpSlashingProtection(pubKey []byte) error {
	return s.keyManager.BumpSlashingProtection(pubKey)
}

func (s *SSVSigner) RemoveHighestAttestation(pubKey []byte) error {
	return s.keyManager.RemoveHighestAttestation(pubKey)
}

func (s *SSVSigner) RemoveHighestProposal(pubKey []byte) error {
	return s.keyManager.RemoveHighestProposal(pubKey)
}

func (s *SSVSigner) IsAttestationSlashable(pk spectypes.ShareValidatorPK, data *phase0.AttestationData) error {
	return s.keyManager.IsAttestationSlashable(pk, data)
}

func (s *SSVSigner) IsBeaconBlockSlashable(pk []byte, slot phase0.Slot) error {
	return s.keyManager.IsBeaconBlockSlashable(pk, slot)
}

func (s *SSVSigner) UpdateHighestAttestation(pk []byte, attestationData *phase0.AttestationData) error {
	return s.keyManager.UpdateHighestAttestation(pk, attestationData)
}

func (s *SSVSigner) UpdateHighestProposal(pk []byte, slot phase0.Slot) error {
	return s.keyManager.UpdateHighestProposal(pk, slot)
}

// AddShare is a dummy method to match KeyManager interface. This method panics and should never be called.
func (s *SSVSigner) AddShare(encryptedShare []byte) error {
	statuses, publicKeys, err := s.client.AddValidators(encryptedShare)
	if err != nil {
		return fmt.Errorf("add validator: %w", err)
	}

	if statuses[0] == ssvsignerclient.StatusImported || statuses[0] == ssvsignerclient.StatusDuplicated {
		if err := s.keyManager.BumpSlashingProtection(publicKeys[0]); err != nil {
			return fmt.Errorf("could not bump slashing protection: %w", err)
		}
	}

	return nil
}

func (s *SSVSigner) RemoveShare(pubKey []byte) error {
	statuses, err := s.client.RemoveValidators(pubKey)
	if err != nil {
		return fmt.Errorf("remove validator: %w", err)
	}

	if statuses[0] == ssvsignerclient.StatusDeleted {
		if err := s.keyManager.RemoveHighestAttestation(pubKey); err != nil {
			return fmt.Errorf("could not remove highest attestation: %w", err)
		}
		if err := s.keyManager.RemoveHighestProposal(pubKey); err != nil {
			return fmt.Errorf("could not remove highest proposal: %w", err)
		}
	}

	return nil
}

func (s *SSVSigner) SignBeaconObject(
	obj ssz.HashRoot,
	domain phase0.Domain,
	sharePubkey []byte,
	signatureDomain phase0.DomainType,
) (spectypes.Signature, [32]byte, error) {
	forkInfo, err := s.getForkInfo()
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

		if err := s.keyManager.IsAttestationSlashable(sharePubkey, data); err != nil {
			return nil, [32]byte{}, err
		}

		if err := s.keyManager.UpdateHighestAttestation(sharePubkey, data); err != nil {
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

			if err := s.keyManager.IsBeaconBlockSlashable(sharePubkey, v.Slot); err != nil {
				return nil, [32]byte{}, err
			}

			if err = s.keyManager.UpdateHighestProposal(sharePubkey, v.Slot); err != nil {
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

			if err := s.keyManager.IsBeaconBlockSlashable(sharePubkey, v.Slot); err != nil {
				return nil, [32]byte{}, err
			}

			if err = s.keyManager.UpdateHighestProposal(sharePubkey, v.Slot); err != nil {
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

	sig, err := s.client.Sign(sharePubkey, req)
	if err != nil {
		return spectypes.Signature{}, [32]byte{}, err
	}
	return sig, root, nil
}

func (s *SSVSigner) getForkInfo() (web3signer.ForkInfo, error) {
	denebForkHolesky := web3signer.ForkType{
		PreviousVersion: "0x04017000",
		CurrentVersion:  "0x05017000",
		Epoch:           29696,
	}

	genesis := s.consensusClient.Genesis()
	if genesis == nil {
		return web3signer.ForkInfo{}, fmt.Errorf("genesis is not ready")
	}
	return web3signer.ForkInfo{
		Fork:                  denebForkHolesky,
		GenesisValidatorsRoot: hex.EncodeToString(genesis.GenesisValidatorsRoot[:]),
	}, nil
}

func (s *SSVSigner) Sign(payload []byte) ([]byte, error) {
	return s.client.OperatorSign(payload)
}

func (s *SSVSigner) Public() keys.OperatorPublicKey {
	pubkeyString, err := s.client.GetOperatorIdentity()
	if err != nil {
		return nil // TODO: handle, consider changing the interface to return error
	}

	pubkey, err := keys.PublicKeyFromString(pubkeyString)
	if err != nil {
		return nil // TODO: handle, consider changing the interface to return error
	}

	return pubkey
}

func (s *SSVSigner) SignSSVMessage(ssvMsg *spectypes.SSVMessage) ([]byte, error) {
	encodedMsg, err := ssvMsg.Encode()
	if err != nil {
		return nil, err
	}

	return s.client.OperatorSign(encodedMsg)
}

func (s *SSVSigner) GetOperatorID() spectypes.OperatorID {
	return s.getOperatorId()
}
