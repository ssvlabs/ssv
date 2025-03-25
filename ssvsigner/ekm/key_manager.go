package ekm

import (
	"context"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

type ShareDecryptionError error

type KeyManager interface {
	BeaconSigner
	SlashingProtector
	// AddShare decrypts and saves an encrypted share private key
	AddShare(ctx context.Context, encryptedSharePrivKey []byte, sharePubKey phase0.BLSPubKey) error
	// RemoveShare removes a share key
	RemoveShare(ctx context.Context, pubKey phase0.BLSPubKey) error
}

type BeaconSigner interface {
	// SignBeaconObject returns signature and root.
	SignBeaconObject(
		ctx context.Context,
		obj ssz.HashRoot,
		domain phase0.Domain,
		pk phase0.BLSPubKey,
		slot phase0.Slot,
		domainType phase0.DomainType,
	) (spectypes.Signature, phase0.Root, error)
	// IsAttestationSlashable returns error if attestation is slashable
	IsAttestationSlashable(pk phase0.BLSPubKey, data *phase0.AttestationData) error
	// IsBeaconBlockSlashable returns error if the given block is slashable
	IsBeaconBlockSlashable(pk phase0.BLSPubKey, slot phase0.Slot) error
}
