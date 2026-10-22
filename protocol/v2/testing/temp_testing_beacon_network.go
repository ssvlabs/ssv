package testing

import (
	"context"
	"fmt"

	"github.com/attestantio/go-eth2-client/api"
	apiv1capella "github.com/attestantio/go-eth2-client/api/v1/capella"
	apiv1deneb "github.com/attestantio/go-eth2-client/api/v1/deneb"
	apiv1electra "github.com/attestantio/go-eth2-client/api/v1/electra"
	apiv1fulu "github.com/attestantio/go-eth2-client/api/v1/fulu"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/capella"
	"github.com/attestantio/go-eth2-client/spec/deneb"
	"github.com/attestantio/go-eth2-client/spec/electra"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"

	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/ssvlabs/ssv/protocol/v2/types/ssvtestingutils"
)

type BeaconNodeWrapped struct {
	beacon.BeaconNode
	Bn *spectestingutils.TestingBeaconNode
	// HeadRoots is what HeadRootAtSlot answers: the block root a head event named for the slot. Unset
	// slots have no view.
	HeadRoots map[phase0.Slot]phase0.Root
}

func (bn *BeaconNodeWrapped) SetSyncCommitteeAggregatorRootHexes(roots map[string]bool) {
	bn.Bn.SetSyncCommitteeAggregatorRootHexes(roots)
}

func (bn *BeaconNodeWrapped) GetBroadcastedRoots() []phase0.Root {
	return bn.Bn.BroadcastedRoots
}

func (bn *BeaconNodeWrapped) GetBeaconNode() *spectestingutils.TestingBeaconNode {
	return bn.Bn
}

// GetAttestationData serves pre-Gloas attestation data, which the Gloas-only spec's testing beacon node no longer does.
func (bn *BeaconNodeWrapped) GetAttestationData(ctx context.Context, slot phase0.Slot) (*phase0.AttestationData, spec.DataVersion, error) {
	version := ssvtestingutils.VersionBySlot(slot)
	if version >= spec.DataVersionGloas {
		return bn.Bn.GetAttestationData(slot)
	}
	data := *ssvtestingutils.TestingAttestationData(version)
	data.Slot = slot
	return &data, version, nil
}

func (bn *BeaconNodeWrapped) HeadRootAtSlot(slot phase0.Slot) (phase0.Root, bool) {
	root, ok := bn.HeadRoots[slot]
	return root, ok
}
func (bn *BeaconNodeWrapped) DomainData(ctx context.Context, epoch phase0.Epoch, domain phase0.DomainType) (phase0.Domain, error) {
	return bn.Bn.DomainData(epoch, domain)
}
func (bn *BeaconNodeWrapped) IsSyncCommitteeAggregator(proof []byte) bool {
	return bn.Bn.IsSyncCommitteeAggregator(proof)
}

// GetSyncCommitteeContribution answers pre-Gloas slots with the version the spec's testing beacon node gave before it
// became Gloas-only.
func (bn *BeaconNodeWrapped) GetSyncCommitteeContribution(ctx context.Context, slot phase0.Slot, selectionProofs []phase0.BLSSignature, subnetIDs []uint64) (ssz.Marshaler, spec.DataVersion, error) {
	contributions, version, err := bn.Bn.GetSyncCommitteeContribution(slot, selectionProofs, subnetIDs)
	if err == nil && ssvtestingutils.VersionBySlot(slot) < spec.DataVersionGloas {
		version = spec.DataVersionBellatrix
	}
	return contributions, version, err
}

// SubmitAggregateSelectionProof serves a pre-Gloas aggregate, which the Gloas-only spec's testing beacon node no
// longer does.
func (bn *BeaconNodeWrapped) SubmitAggregateSelectionProof(ctx context.Context, slot phase0.Slot, committeeIndex phase0.CommitteeIndex, committeeLength uint64, index phase0.ValidatorIndex, slotSig []byte) (ssz.Marshaler, spec.DataVersion, error) {
	version := ssvtestingutils.VersionBySlot(slot)
	if version >= spec.DataVersionGloas {
		return bn.Bn.SubmitAggregateSelectionProof(slot, committeeIndex, committeeLength, index, slotSig)
	}
	return ssvtestingutils.TestingAggregateAndProofV(version, spectestingutils.TestingValidatorIndex), version, nil
}

func (bn *BeaconNodeWrapped) IsAggregator(ctx context.Context, slot phase0.Slot, committeeIndex phase0.CommitteeIndex, committeeLength uint64, slotSig []byte) bool {
	return bn.Bn.IsAggregator(slot, committeeIndex, committeeLength, slotSig)
}
func (bn *BeaconNodeWrapped) GetAggregateAttestation(ctx context.Context, slot phase0.Slot, committeeIndex phase0.CommitteeIndex) (ssz.Marshaler, spec.DataVersion, error) {
	version := ssvtestingutils.VersionBySlot(slot)
	switch {
	case version >= spec.DataVersionGloas:
		att, err := bn.Bn.GetAggregateAttestation(slot, committeeIndex)
		if err != nil {
			return nil, 0, err
		}
		return att, version, nil
	case version >= spec.DataVersionElectra:
		return ssvtestingutils.TestingElectraAggregateAndProofV(spectestingutils.TestingValidatorIndex, version).Aggregate, version, nil
	default:
		return ssvtestingutils.TestingPhase0AggregateAndProof(spectestingutils.TestingValidatorIndex).Aggregate, version, nil
	}
}
func (bn *BeaconNodeWrapped) GetBeaconNetwork() spectypes.BeaconNetwork {
	return bn.Bn.GetBeaconNetwork()
}

// GetBeaconBlock serves a pre-Gloas block, which the Gloas-only spec's testing beacon node no longer does.
func (bn *BeaconNodeWrapped) GetBeaconBlock(ctx context.Context, slot phase0.Slot, graffiti, randao []byte) (*api.VersionedProposal, ssz.Marshaler, error) {
	version := ssvtestingutils.VersionBySlot(slot)
	vBlk := ssvtestingutils.TestingBeaconBlockV(version)
	switch version {
	case spec.DataVersionCapella:
		return vBlk, vBlk.Capella, nil
	case spec.DataVersionDeneb:
		return vBlk, vBlk.Deneb, nil
	case spec.DataVersionElectra:
		return vBlk, vBlk.Electra, nil
	case spec.DataVersionFulu:
		return vBlk, vBlk.Fulu, nil
	default:
		return nil, nil, fmt.Errorf("unsupported version %s", version)
	}
}

// SubmitValidatorRegistrations records each registration's root as broadcast, as the spec's testing beacon
// node did before validator registration was retired.
func (bn *BeaconNodeWrapped) SubmitValidatorRegistrations(ctx context.Context, registrations []*api.VersionedSignedValidatorRegistration) error {
	for _, registration := range registrations {
		r, err := registration.V1.HashTreeRoot()
		if err != nil {
			return err
		}
		bn.Bn.BroadcastedRoots = append(bn.Bn.BroadcastedRoots, r)
	}
	return nil
}
func (bn *BeaconNodeWrapped) SubmitVoluntaryExit(ctx context.Context, voluntaryExit *phase0.SignedVoluntaryExit) error {
	return bn.Bn.SubmitVoluntaryExit(voluntaryExit)
}

// SubmitAttestations records each attestation's root as broadcast, pre-Gloas ones as the spec's testing beacon node
// did before it became Gloas-only.
func (bn *BeaconNodeWrapped) SubmitAttestations(ctx context.Context, attestations []*spec.VersionedAttestation) error {
	for _, att := range attestations {
		if att.Version >= spec.DataVersionGloas {
			if err := bn.Bn.SubmitAttestations([]*spec.VersionedAttestation{att}); err != nil {
				return err
			}
			continue
		}
		r, err := preGloasAttestationRoot(att)
		if err != nil {
			return err
		}
		bn.Bn.BroadcastedRoots = append(bn.Bn.BroadcastedRoots, r)
	}
	return nil
}

func (bn *BeaconNodeWrapped) SubmitSyncMessages(ctx context.Context, msgs []*altair.SyncCommitteeMessage) error {
	return bn.Bn.SubmitSyncMessages(msgs)
}
func (bn *BeaconNodeWrapped) SubmitSignedContributionAndProof(ctx context.Context, contribution *altair.SignedContributionAndProof) error {
	return bn.Bn.SubmitSignedContributionAndProof(contribution)
}

// SubmitSignedAggregateSelectionProof records the aggregate's root as broadcast, a pre-Gloas one as the spec's
// testing beacon node did before it became Gloas-only.
func (bn *BeaconNodeWrapped) SubmitSignedAggregateSelectionProof(ctx context.Context, msg *spec.VersionedSignedAggregateAndProof) error {
	if msg.Version >= spec.DataVersionGloas {
		return bn.Bn.SubmitSignedAggregateAndProof(msg)
	}
	r, err := preGloasSignedAggregateRoot(msg)
	if err != nil {
		return err
	}
	bn.Bn.BroadcastedRoots = append(bn.Bn.BroadcastedRoots, r)
	return nil
}

// SubmitBeaconBlock records a pre-Gloas signed block's root as broadcast, as the spec's testing beacon node
// did before it became Gloas-only.
func (bn *BeaconNodeWrapped) SubmitBeaconBlock(ctx context.Context, block *api.VersionedProposal, sig phase0.BLSSignature) error {
	r, err := preGloasSignedBlockRoot(block, sig)
	if err != nil {
		return err
	}
	bn.Bn.BroadcastedRoots = append(bn.Bn.BroadcastedRoots, r)
	return nil
}

func preGloasSignedBlockRoot(block *api.VersionedProposal, sig phase0.BLSSignature) (phase0.Root, error) {
	var signed interface{ HashTreeRoot() ([32]byte, error) }
	switch {
	case block.Blinded && block.Version == spec.DataVersionCapella && block.CapellaBlinded != nil:
		signed = &apiv1capella.SignedBlindedBeaconBlock{Message: block.CapellaBlinded, Signature: sig}
	case block.Blinded && block.Version == spec.DataVersionDeneb && block.DenebBlinded != nil:
		signed = &apiv1deneb.SignedBlindedBeaconBlock{Message: block.DenebBlinded, Signature: sig}
	case block.Blinded && block.Version == spec.DataVersionElectra && block.ElectraBlinded != nil:
		signed = &apiv1electra.SignedBlindedBeaconBlock{Message: block.ElectraBlinded, Signature: sig}
	case block.Blinded && block.Version == spec.DataVersionFulu && block.FuluBlinded != nil:
		signed = &apiv1electra.SignedBlindedBeaconBlock{Message: block.FuluBlinded, Signature: sig}
	case !block.Blinded && block.Version == spec.DataVersionCapella && block.Capella != nil:
		signed = &capella.SignedBeaconBlock{Message: block.Capella, Signature: sig}
	case !block.Blinded && block.Version == spec.DataVersionDeneb && block.Deneb != nil && block.Deneb.Block != nil:
		signed = &apiv1deneb.SignedBlockContents{
			SignedBlock: &deneb.SignedBeaconBlock{Message: block.Deneb.Block, Signature: sig},
			KZGProofs:   block.Deneb.KZGProofs,
			Blobs:       block.Deneb.Blobs,
		}
	case !block.Blinded && block.Version == spec.DataVersionElectra && block.Electra != nil && block.Electra.Block != nil:
		signed = &apiv1electra.SignedBlockContents{
			SignedBlock: &electra.SignedBeaconBlock{Message: block.Electra.Block, Signature: sig},
			KZGProofs:   block.Electra.KZGProofs,
			Blobs:       block.Electra.Blobs,
		}
	case !block.Blinded && block.Version == spec.DataVersionFulu && block.Fulu != nil && block.Fulu.Block != nil:
		signed = &apiv1fulu.SignedBlockContents{
			SignedBlock: &electra.SignedBeaconBlock{Message: block.Fulu.Block, Signature: sig},
			KZGProofs:   block.Fulu.KZGProofs,
			Blobs:       block.Fulu.Blobs,
		}
	default:
		return phase0.Root{}, fmt.Errorf("unsupported or missing %s block (blinded: %t)", block.Version, block.Blinded)
	}

	return signed.HashTreeRoot()
}

// preGloasAttestationRoot is the root of the attestation as broadcast: from Electra on, a single attestation.
func preGloasAttestationRoot(att *spec.VersionedAttestation) (phase0.Root, error) {
	switch att.Version {
	case spec.DataVersionPhase0:
		return att.Phase0.HashTreeRoot()
	case spec.DataVersionAltair:
		return att.Altair.HashTreeRoot()
	case spec.DataVersionBellatrix:
		return att.Bellatrix.HashTreeRoot()
	case spec.DataVersionCapella:
		return att.Capella.HashTreeRoot()
	case spec.DataVersionDeneb:
		return att.Deneb.HashTreeRoot()
	case spec.DataVersionElectra, spec.DataVersionFulu:
		electraAtt := att.Electra
		if att.Version == spec.DataVersionFulu {
			electraAtt = att.Fulu
		}
		single, err := electraAtt.ToSingleAttestation(att.ValidatorIndex)
		if err != nil {
			return phase0.Root{}, err
		}
		return single.HashTreeRoot()
	default:
		return phase0.Root{}, fmt.Errorf("unsupported attestation version %s", att.Version)
	}
}

func preGloasSignedAggregateRoot(msg *spec.VersionedSignedAggregateAndProof) (phase0.Root, error) {
	switch msg.Version {
	case spec.DataVersionPhase0:
		return msg.Phase0.HashTreeRoot()
	case spec.DataVersionAltair:
		return msg.Altair.HashTreeRoot()
	case spec.DataVersionBellatrix:
		return msg.Bellatrix.HashTreeRoot()
	case spec.DataVersionCapella:
		return msg.Capella.HashTreeRoot()
	case spec.DataVersionDeneb:
		return msg.Deneb.HashTreeRoot()
	case spec.DataVersionElectra:
		return msg.Electra.HashTreeRoot()
	case spec.DataVersionFulu:
		return msg.Fulu.HashTreeRoot()
	default:
		return phase0.Root{}, fmt.Errorf("unsupported aggregate version %s", msg.Version)
	}
}

func NewTestingBeaconNodeWrapped() beacon.BeaconNode {
	bnw := &BeaconNodeWrapped{}
	bnw.Bn = spectestingutils.NewTestingBeaconNode()

	return bnw
}
