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

	root, err := spectypes.ComputeETHSigningRoot(obj, domain)
	if err != nil {
		return nil, [32]byte{}, err
	}

	req := web3signer.SignRequest{
		SigningRoot: hex.EncodeToString(root[:]),
		ForkInfo:    s.getForkInfo(),
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

			req.BeaconBlock = &struct {
				Version     string                    `json:"version"`
				BlockHeader *phase0.BeaconBlockHeader `json:"block_header"`
			}{
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

			req.BeaconBlock = &struct {
				Version     string                    `json:"version"`
				BlockHeader *phase0.BeaconBlockHeader `json:"block_header"`
			}{
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
		req.AggregationSlot = &struct {
			Slot phase0.Slot `json:"slot"`
		}{Slot: phase0.Slot(data)}

	case spectypes.DomainRandao:
		data, ok := obj.(spectypes.SSZUint64)
		if !ok {
			return nil, [32]byte{}, errors.New("could not cast obj to SSZUint64")
		}

		req.Type = web3signer.RandaoReveal
		req.RandaoReveal = &struct {
			Epoch phase0.Epoch `json:"epoch"`
		}{Epoch: phase0.Epoch(data)}

	case spectypes.DomainSyncCommittee:
		data, ok := obj.(spectypes.SSZBytes)
		if !ok {
			return nil, [32]byte{}, errors.New("could not cast obj to SSZBytes")
		}

		// TODO: fix signing root
		slot := binary.LittleEndian.Uint64(data[0:8])
		data = data[8:]

		if len(data) != 32 {
			return nil, [32]byte{}, fmt.Errorf("unexpected root length: %d", len(data))
		}

		req.Type = web3signer.SyncCommitteeMessage
		req.SyncCommitteeMessage = &struct {
			BeaconBlockRoot phase0.Root `json:"beacon_block_root"`
			Slot            phase0.Slot `json:"slot"`
		}{
			BeaconBlockRoot: phase0.Root(data),
			Slot:            phase0.Slot(slot),
		}

	case spectypes.DomainSyncCommitteeSelectionProof:
		data, ok := obj.(*altair.SyncAggregatorSelectionData)
		if !ok {
			return nil, [32]byte{}, errors.New("could not cast obj to SyncAggregatorSelectionData")
		}

		req.Type = web3signer.SyncCommitteeSelectionProof
		req.SyncAggregatorSelectionData = &struct {
			Slot              phase0.Slot           `json:"slot"`
			SubcommitteeIndex phase0.CommitteeIndex `json:"subcommittee_index"`
		}{
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

func (s *SSVSignerKeyManagerAdapter) AddEncryptedShare(
	encryptedShare []byte,
	validatorPubKey spectypes.ValidatorPK,
) error {
	s.logger.Debug("Adding Share")

	// TODO: consider using spectypes.ValidatorPK
	if err := s.client.AddValidator(encryptedShare, validatorPubKey[:]); err != nil {
		s.logger.Debug("Adding Share err", zap.Error(err))
		// TODO: if it fails on share decryption, which only the ssv-signer can know: return malformedError
		// TODO: if it fails for any other reason: retry X times or crash
		return err
	}
	s.logger.Debug("Adding Share ok")

	return nil
}

func (s *SSVSignerKeyManagerAdapter) RemoveShare(pubKey string) error {
	s.logger.Debug("Removing Share")
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
