package ekm

import (
	genesisphase0 "github.com/AKorpusenko/genesis-go-eth2-client/spec/phase0"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	"github.com/herumi/bls-eth-go-binary/bls"
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

type GenesisKeyManagerAdapter struct {
	KeyManager KeyManager
}

func (k *GenesisKeyManagerAdapter) SignBeaconObject(obj ssz.HashRoot, domain genesisphase0.Domain, pk []byte, domainType genesisphase0.DomainType) (genesisspectypes.Signature, [32]byte, error) {
	signature, root, err := k.KeyManager.SignBeaconObject(obj, phase0.Domain(domain), pk, phase0.DomainType(domainType))
	if err != nil {
		return nil, [32]byte{}, err
	}
	return genesisspectypes.Signature(signature), root, nil
}

func (k *GenesisKeyManagerAdapter) IsAttestationSlashable(pk []byte, data *genesisphase0.AttestationData) error {
	attestationData := &phase0.AttestationData{
		Slot:            phase0.Slot(data.Slot),
		Index:           phase0.CommitteeIndex(data.Index),
		BeaconBlockRoot: phase0.Root(data.BeaconBlockRoot),
		Source:          &phase0.Checkpoint{Epoch: phase0.Epoch(data.Source.Epoch), Root: phase0.Root(data.Source.Root)},
		Target:          &phase0.Checkpoint{Epoch: phase0.Epoch(data.Target.Epoch), Root: phase0.Root(data.Target.Root)},
	}

	return k.KeyManager.IsAttestationSlashable(pk, attestationData)
}

func (k *GenesisKeyManagerAdapter) IsBeaconBlockSlashable(pk []byte, slot genesisphase0.Slot) error {
	return k.KeyManager.IsBeaconBlockSlashable(pk, phase0.Slot(slot))
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
