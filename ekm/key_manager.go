package ekm

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/protocol/v2/ssv/signing"
)

type KeyManager interface {
	signing.BeaconSigner
	SlashingProtector
	// AddShare decrypts and saves an encrypted share private key
	AddShare(encryptedSharePrivKey []byte, sharePubKey phase0.BLSPubKey) error
	// RemoveShare removes a share key
	RemoveShare(pubKey phase0.BLSPubKey) error
}

type ShareDecryptionError error

type BlockRootWithSlot struct {
	BlockRoot phase0.Root
	Slot      phase0.Slot // Slot is just a payload, so BlockRootWithSlot's methods don't use it
}

func (b BlockRootWithSlot) HashTreeRoot() ([32]byte, error) {
	return ssz.HashWithDefaultHasher(spectypes.SSZBytes(b.BlockRoot[:]))
}

func (b BlockRootWithSlot) GetTree() (*ssz.Node, error) {
	return ssz.ProofTree(spectypes.SSZBytes(b.BlockRoot[:]))
}

func (b BlockRootWithSlot) HashTreeRootWith(hh ssz.HashWalker) error {
	indx := hh.Index()
	hh.PutBytes(b.BlockRoot[:])
	hh.Merkleize(indx)
	return nil
}
