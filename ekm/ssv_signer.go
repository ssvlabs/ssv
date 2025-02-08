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

	"github.com/ssvlabs/ssv/operator/keys"
)

// TODO: move to another package?

type SSVSignerKeyManagerAdapter struct {
	client *ssvsignerclient.SSVSignerClient
}

func NewSSVSignerKeyManagerAdapter(client *ssvsignerclient.SSVSignerClient) *SSVSignerKeyManagerAdapter {
	return &SSVSignerKeyManagerAdapter{client: client}
}

func (s *SSVSignerKeyManagerAdapter) SignBeaconObject(obj ssz.HashRoot, domain phase0.Domain, sharePubkey []byte, domainType phase0.DomainType) (spectypes.Signature, [32]byte, error) {
	switch domainType {
	case spectypes.DomainAttester:
		data, ok := obj.(*phase0.AttestationData)
		if !ok {
			return nil, [32]byte{}, errors.New("could not cast obj to AttestationData")
		}

		root, err := spectypes.ComputeETHSigningRoot(data, domain)
		if err != nil {
			return nil, [32]byte{}, err
		}

		req := web3signer.SignRequest{
			Type: "ATTESTATION",
			ForkInfo: web3signer.ForkInfo{
				Fork: web3signer.ForkType{ // TODO
					PreviousVersion: hex.EncodeToString([]byte{2, 0, 0, 0}),
					CurrentVersion:  hex.EncodeToString([]byte{3, 0, 0, 0}),
					Epoch:           194048,
				},
				GenesisValidatorsRoot: "0x9143aa7c615a7f7115e2b6aac319c03529df8242ae705fba9df39b79c59fa8b1", // TODO
			},
			SigningRoot: hex.EncodeToString(root[:]),
			Attestation: data,
		}

		sig, err := s.client.Sign(sharePubkey, req)
		if err != nil {
			return spectypes.Signature{}, [32]byte{}, err
		}

		return []byte(sig), root, nil // TODO: need sig hex decoding?

		// TODO: support other domains
	default:
		return nil, [32]byte{}, errors.New("domain unknown")
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

func (s *SSVSignerKeyManagerAdapter) AddEncryptedShare(encryptedShare []byte, validatorPubKey spectypes.ValidatorPK) error {
	// TODO: consider using spectypes.ValidatorPK
	if err := s.client.AddValidator(encryptedShare, validatorPubKey[:]); err != nil {
		// TODO: if it fails on share decryption, which only the ssv-signer can know: return malformedError
		// TODO: if it fails for any other reason: retry X times or crash
		return err
	}
	return nil
}

func (s *SSVSignerKeyManagerAdapter) RemoveShare(pubKey string) error {
	decoded, _ := hex.DecodeString(pubKey) // TODO: caller passes hex encoded string, need to fix this workaround
	return s.client.RemoveValidator(decoded)
}

type SSVSignerOperatorSignerAdapter struct {
	client *ssvsignerclient.SSVSignerClient
}

func NewSSVSignerOperatorSignerAdapter(client *ssvsignerclient.SSVSignerClient) *SSVSignerOperatorSignerAdapter {
	return &SSVSignerOperatorSignerAdapter{client: client}
}

func (s *SSVSignerOperatorSignerAdapter) Sign(payload []byte) ([]byte, error) {
	return s.client.OperatorSign(payload)
}

func (s *SSVSignerOperatorSignerAdapter) Public() keys.OperatorPublicKey {
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
