package ekm

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	"github.com/herumi/bls-eth-go-binary/bls"
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

type GenesisKeyManagerAdapter struct {
	KeyManager KeyManager
}

func (k *GenesisKeyManagerAdapter) SignBeaconObject(obj ssz.HashRoot, domain phase0.Domain, pk []byte, domainType phase0.DomainType) (genesisspectypes.Signature, [32]byte, error) {
	signature, root, err := k.KeyManager.SignBeaconObject(obj, domain, pk, domainType)
	if err != nil {
		return nil, [32]byte{}, err
	}
	return genesisspectypes.Signature(signature), root, nil
}

func (k *GenesisKeyManagerAdapter) IsAttestationSlashable(pk []byte, data *phase0.AttestationData) error {
	return k.KeyManager.IsAttestationSlashable(pk, data)
}

func (k *GenesisKeyManagerAdapter) IsBeaconBlockSlashable(pk []byte, slot phase0.Slot) error {
	return k.KeyManager.IsBeaconBlockSlashable(pk, slot)
}

func (k *GenesisKeyManagerAdapter) SignRoot(data genesisspectypes.Root, genSigType genesisspectypes.SignatureType, pk []byte) (genesisspectypes.Signature, error) {
	var sigType spectypes.SignatureType
	copy(sigType[:], genSigType[:])

	signature, err := k.KeyManager.SignRoot(data, sigType, pk)
	if err != nil {
		return nil, err
	}

	return genesisspectypes.Signature(signature), nil
}

func (k *GenesisKeyManagerAdapter) AddShare(shareKey *bls.SecretKey) error {
	return k.KeyManager.AddShare(shareKey)
}

func (k *GenesisKeyManagerAdapter) RemoveShare(pubKey string) error {
	return k.KeyManager.RemoveShare(pubKey)
}
