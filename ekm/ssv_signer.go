package ekm

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	"github.com/herumi/bls-eth-go-binary/bls"
	ssvsignerclient "github.com/ssvlabs/ssv-signer/client"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/operator/keys"
)

// TODO: move to another package?

type SSVSignerKeyManagerAdapter struct {
	client *ssvsignerclient.Client
}

func NewSSVSignerKeyManagerAdapter(client *ssvsignerclient.Client) *SSVSignerKeyManagerAdapter {
	return &SSVSignerKeyManagerAdapter{client: client}
}

func (s *SSVSignerKeyManagerAdapter) SignBeaconObject(obj ssz.HashRoot, domain phase0.Domain, sharePubkey []byte, domainType phase0.DomainType) (spectypes.Signature, [32]byte, error) {
	var root [32]byte                                       // TODO: extract logic of building payload from obj+domain+domainType from ekm
	sig, err := s.client.Sign(string(sharePubkey), root[:]) // TODO: check sharePubkey conversion correctness
	if err != nil {
		return spectypes.Signature{}, [32]byte{}, err
	}

	return []byte(sig), root, nil // TODO: need sig hex decoding?
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

func (s *SSVSignerKeyManagerAdapter) AddShare(shareKey *bls.SecretKey) error {
	// TODO: need to change caller to pass encryptedShare instead of shareKey
	// if err := s.client.AddValidator(encryptedShare, validatorPubKey); err != nil {
	//     // TODO: if it fails on share decryption, which only the ssv-signer can know: return malformedError
	//.    // TODO: if it fails for any other reason: retry X times or crash
	//     return err
	// }
	// return nil

	//TODO implement me
	panic("implement me")
}

func (s *SSVSignerKeyManagerAdapter) RemoveShare(pubKey string) error {
	return s.client.RemoveValidator(pubKey)
}

type SSVSignerOperatorSignerAdapter struct {
	client *ssvsignerclient.Client
}

func NewSSVSignerOperatorSignerAdapter(client *ssvsignerclient.Client) *SSVSignerOperatorSignerAdapter {
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
