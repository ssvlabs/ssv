package ekm

import (
	"encoding/binary"
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
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/ssvlabs/eth2-key-manager/core"
	ssvsignerclient "github.com/ssvlabs/ssv-signer/client"
	"github.com/ssvlabs/ssv-signer/web3signer"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/beacon/goclient"
	"github.com/ssvlabs/ssv/operator/keys"
)

// TODO: move to another package?

type SSVSignerKeyManagerAdapter struct {
	logger          *zap.Logger
	client          *ssvsignerclient.SSVSignerClient
	consensusClient *goclient.GoClient
}

func (s *SSVSignerKeyManagerAdapter) ListAccounts() ([]core.ValidatorAccount, error) {
	//TODO implement me
	panic("implement me")
}

func (s *SSVSignerKeyManagerAdapter) RetrieveHighestAttestation(pubKey []byte) (*phase0.AttestationData, bool, error) {
	//TODO implement me
	panic("implement me")
}

func (s *SSVSignerKeyManagerAdapter) RetrieveHighestProposal(pubKey []byte) (phase0.Slot, bool, error) {
	//TODO implement me
	panic("implement me")
}

func (s *SSVSignerKeyManagerAdapter) BumpSlashingProtection(pubKey []byte) error {
	return nil
}

func NewSSVSignerKeyManagerAdapter(
	logger *zap.Logger,
	client *ssvsignerclient.SSVSignerClient,
	consensusClient *goclient.GoClient,
) *SSVSignerKeyManagerAdapter {
	return &SSVSignerKeyManagerAdapter{
		logger:          logger.Named("SSVSignerKeyManagerAdapter"),
		client:          client,
		consensusClient: consensusClient,
	}
}

func (s *SSVSignerKeyManagerAdapter) SignBeaconObject(
	obj ssz.HashRoot,
	domain phase0.Domain,
	sharePubkey []byte,
	signatureDomain phase0.DomainType,
) (spectypes.Signature, [32]byte, error) {
	req := web3signer.SignRequest{
		ForkInfo: s.getForkInfo(),
	}

	switch signatureDomain {
	case spectypes.DomainAttester:
		data, ok := obj.(*phase0.AttestationData)
		if !ok {
			return nil, [32]byte{}, errors.New("could not cast obj to AttestationData")
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
		data, ok := obj.(spectypes.SSZBytes)
		if !ok {
			return nil, [32]byte{}, errors.New("could not cast obj to SSZBytes")
		}

		// TODO: fix this workaround
		slot := binary.LittleEndian.Uint64(data[0:8])
		beaconBlockRoot := data[8:]
		obj = beaconBlockRoot

		if len(beaconBlockRoot) != 32 {
			return nil, [32]byte{}, fmt.Errorf("unexpected beacon block root length: %d", len(beaconBlockRoot))
		}

		req.Type = web3signer.SyncCommitteeMessage
		req.SyncCommitteeMessage = &web3signer.SyncCommitteeMessageData{
			BeaconBlockRoot: phase0.Root(beaconBlockRoot),
			Slot:            phase0.Slot(slot),
		}

		// workaround TODO: remove
		beaconBlockRootHash, err := spectypes.ComputeETHSigningRoot(beaconBlockRoot, domain)
		if err != nil {
			return nil, [32]byte{}, err
		}
		req.SigningRoot = hex.EncodeToString(beaconBlockRootHash[:])

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

func (s *SSVSignerKeyManagerAdapter) getForkInfo() web3signer.ForkInfo {
	denebForkHolesky := web3signer.ForkType{
		PreviousVersion: "0x04017000",
		CurrentVersion:  "0x05017000",
		Epoch:           29696,
	}

	return web3signer.ForkInfo{
		Fork:                  denebForkHolesky,
		GenesisValidatorsRoot: hex.EncodeToString(s.consensusClient.Genesis().GenesisValidatorsRoot[:]),
	}
}

func (s *SSVSignerKeyManagerAdapter) IsAttestationSlashable(pk spectypes.ShareValidatorPK, data *phase0.AttestationData) error {
	// TODO: Consider if this needs to be implemented.
	//  IsAttestationSlashable is called to avoid signing a slashable attestation, however,
	//  ssv-signer's Sign must perform the slashability check.
	return nil
}

func (s *SSVSignerKeyManagerAdapter) IsBeaconBlockSlashable(pk []byte, slot phase0.Slot) error {
	// TODO: Consider if this needs to be implemented.
	//  IsBeaconBlockSlashable is called to avoid signing a slashable attestation, however,
	//  ssv-signer's Sign must perform the slashability check.
	return nil
}

// AddShare is a dummy method to match KeyManager interface. This method panics and should never be called.
// TODO: get rid of this workaround
func (s *SSVSignerKeyManagerAdapter) AddShare(shareKey *bls.SecretKey) error {
	panic("should not be called")
}

func (s *SSVSignerKeyManagerAdapter) AddEncryptedShare(encryptedShare []byte) error {
	return s.client.AddValidator(encryptedShare)
}

func (s *SSVSignerKeyManagerAdapter) RemoveShare(pubKey string) error {
	decoded, _ := hex.DecodeString(pubKey) // TODO: caller passes hex encoded string, need to fix this workaround
	return s.client.RemoveValidator(decoded)
}

type SSVSignerKeysOperatorSignerAdapter struct {
	logger *zap.Logger
	client *ssvsignerclient.SSVSignerClient
}

func NewSSVSignerKeysOperatorSignerAdapter(
	logger *zap.Logger,
	client *ssvsignerclient.SSVSignerClient,
) *SSVSignerKeysOperatorSignerAdapter {
	return &SSVSignerKeysOperatorSignerAdapter{
		logger: logger.Named("SSVSignerKeysOperatorSignerAdapter"),
		client: client,
	}
}

func (s *SSVSignerKeysOperatorSignerAdapter) Sign(payload []byte) ([]byte, error) {
	s.logger.Debug("Signing payload")
	return s.client.OperatorSign(payload)
}

func (s *SSVSignerKeysOperatorSignerAdapter) Public() keys.OperatorPublicKey {
	s.logger.Debug("Getting public key")
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

type SSVSignerTypesOperatorSignerAdapter struct {
	logger        *zap.Logger
	client        *ssvsignerclient.SSVSignerClient
	getOperatorId func() spectypes.OperatorID
}

func NewSSVSignerTypesOperatorSignerAdapter(
	logger *zap.Logger,
	client *ssvsignerclient.SSVSignerClient,
	getOperatorId func() spectypes.OperatorID,
) *SSVSignerTypesOperatorSignerAdapter {
	return &SSVSignerTypesOperatorSignerAdapter{
		logger:        logger.Named("SSVSignerTypesOperatorSignerAdapter"),
		client:        client,
		getOperatorId: getOperatorId,
	}
}

func (s *SSVSignerTypesOperatorSignerAdapter) SignSSVMessage(ssvMsg *spectypes.SSVMessage) ([]byte, error) {
	encodedMsg, err := ssvMsg.Encode()
	if err != nil {
		return nil, err
	}

	return s.client.OperatorSign(encodedMsg)
}

func (s *SSVSignerTypesOperatorSignerAdapter) GetOperatorID() spectypes.OperatorID {
	return s.getOperatorId()
}
