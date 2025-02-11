package ekm

import (
	"encoding/hex"
	"errors"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	"github.com/herumi/bls-eth-go-binary/bls"
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
	switch signatureDomain {
	case spectypes.DomainAttester:
		s.logger.Debug("Signing Attester")

		data, ok := obj.(*phase0.AttestationData)
		if !ok {
			return nil, [32]byte{}, errors.New("could not cast obj to AttestationData")
		}

		root, err := spectypes.ComputeETHSigningRoot(data, domain)
		if err != nil {
			return nil, [32]byte{}, err
		}

		denebForkHolesky := web3signer.ForkType{
			PreviousVersion: "0x04017000",
			CurrentVersion:  "0x05017000",
			Epoch:           29696,
		}

		req := web3signer.SignRequest{
			Type: web3signer.Attestation,
			ForkInfo: web3signer.ForkInfo{
				Fork:                  denebForkHolesky,
				GenesisValidatorsRoot: hex.EncodeToString(s.consensusClient.Genesis().GenesisValidatorsRoot[:]),
			},
			SigningRoot: hex.EncodeToString(root[:]),
			Attestation: data,
		}

		sig, err := s.client.Sign(sharePubkey, req)
		s.logger.Debug("Signing Attester result", zap.Any("signature", sig), zap.Error(err))
		if err != nil {
			return spectypes.Signature{}, [32]byte{}, err
		}

		return []byte(sig), root, nil // TODO: need sig hex decoding?

	default:
		// TODO: support other domains
		return nil, [32]byte{}, errors.New("domain not supported at the moment")
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

type SSVSignerOperatorSignerAdapter struct {
	logger *zap.Logger
	client *ssvsignerclient.SSVSignerClient
}

func NewSSVSignerOperatorSignerAdapter(
	logger *zap.Logger,
	client *ssvsignerclient.SSVSignerClient,
) *SSVSignerOperatorSignerAdapter {
	return &SSVSignerOperatorSignerAdapter{
		logger: logger.Named("SSVSignerOperatorSignerAdapter"),
		client: client,
	}
}

func (s *SSVSignerOperatorSignerAdapter) Sign(payload []byte) ([]byte, error) {
	s.logger.Debug("Signing payload")
	return s.client.OperatorSign(payload)
}

func (s *SSVSignerOperatorSignerAdapter) Public() keys.OperatorPublicKey {
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
