package genesisgoclient

import (
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	"github.com/ssvlabs/ssv/beacon/goclient"
	genesisbeacon "github.com/ssvlabs/ssv/protocol/genesis/blockchain/beacon"
)

type adapter struct {
	*goclient.GoClient
}

func NewAdapter(bn *goclient.GoClient) genesisbeacon.BeaconNode {
	return &adapter{
		GoClient: bn,
	}
}

func (a *adapter) GetAttestationData(slot phase0.Slot, committeeIndex phase0.CommitteeIndex) (ssz.Marshaler, spec.DataVersion, error) {
	return a.GoClient.GetAttestationData(slot, committeeIndex)
}

func (a *adapter) GetBeaconNetwork() genesisspectypes.BeaconNetwork {
	return genesisspectypes.BeaconNetwork(a.GoClient.GetBeaconNetwork())
}

func (a *adapter) GetBlindedBeaconBlock(slot phase0.Slot, graffiti []byte, sig []byte) (ssz.Marshaler, spec.DataVersion, error) {
	return a.GoClient.GetBeaconBlock(slot, graffiti, sig)
}

func (a *adapter) SubmitAttestation(attestation *phase0.Attestation) error {
	return a.GoClient.SubmitAttestations([]*phase0.Attestation{attestation})
}

func (a *adapter) SubmitSyncMessage(message *altair.SyncCommitteeMessage) error {
	return a.GoClient.SubmitSyncMessages([]*altair.SyncCommitteeMessage{message})
}
