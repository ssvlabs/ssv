package types_test

import (
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/electra"
	eth2gloas "github.com/attestantio/go-eth2-client/spec/gloas"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectests "github.com/ssvlabs/ssv-spec/types/spectest/tests"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"

	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/protocol/v2/types/ssvtestingutils"
)

// The pre-Gloas helpers are the spec logic the Gloas-only spec dropped, so these tests carry over the
// pre-Gloas cases of the spec's proposer and aggregator-committee consensus-data vectors, and check that
// Gloas values still reach the spec.

var (
	preGloasProposalVersions = []spec.DataVersion{
		spec.DataVersionCapella,
		spec.DataVersionDeneb,
		spec.DataVersionElectra,
		spec.DataVersionFulu,
	}
	preGloasVersions = []spec.DataVersion{
		spec.DataVersionPhase0,
		spec.DataVersionAltair,
		spec.DataVersionBellatrix,
		spec.DataVersionCapella,
		spec.DataVersionDeneb,
		spec.DataVersionElectra,
		spec.DataVersionFulu,
	}
)

// PreGloasBeaconVote must encode and merkleize exactly as the spec's BeaconVote did before it became the
// Gloas vote: nodes on earlier versions still decide on it. The bytes and root were recorded from that
// BeaconVote.
func TestPreGloasBeaconVoteEncoding(t *testing.T) {
	const (
		wantSSZ = "0101010101010101010101010101010101010101010101010101010101010101" +
			"0807060504030201" + "0202020202020202020202020202020202020202020202020202020202020202" +
			"1817161514131211" + "0303030303030303030303030303030303030303030303030303030303030303"
		wantRoot = "5e7cda20e678e98b2accc8e128433ab5157688e39a3f92d027dca855a31e50c8"
	)
	filledRoot := func(b byte) (r phase0.Root) {
		for i := range r {
			r[i] = b
		}
		return r
	}
	vote := &ssvtypes.PreGloasBeaconVote{
		BlockRoot: filledRoot(0x01),
		Source:    &phase0.Checkpoint{Epoch: 0x0102030405060708, Root: filledRoot(0x02)},
		Target:    &phase0.Checkpoint{Epoch: 0x1112131415161718, Root: filledRoot(0x03)},
	}

	data, err := vote.Encode()
	require.NoError(t, err)
	require.Equal(t, wantSSZ, hex.EncodeToString(data))
	root, err := vote.HashTreeRoot()
	require.NoError(t, err)
	require.Equal(t, wantRoot, hex.EncodeToString(root[:]))

	decoded := &ssvtypes.PreGloasBeaconVote{}
	require.NoError(t, decoded.Decode(data))
	require.Equal(t, vote, decoded)

	// The 120-byte Gloas vote doesn't decode as a pre-Gloas one.
	require.Error(t, decoded.Decode(append(data, make([]byte, 8)...)))
}

func TestPreGloasBeaconVoteValidate(t *testing.T) {
	checkpoint := func(epoch phase0.Epoch) *phase0.Checkpoint { return &phase0.Checkpoint{Epoch: epoch} }
	tests := []struct {
		name     string
		vote     *ssvtypes.PreGloasBeaconVote
		wantCode int
	}{
		{name: "valid", vote: &ssvtypes.PreGloasBeaconVote{Source: checkpoint(0), Target: checkpoint(1)}},
		{name: "nil vote", wantCode: spectypes.BeaconVoteNilCheckpointErrorCode},
		{name: "nil source", vote: &ssvtypes.PreGloasBeaconVote{Target: checkpoint(1)}, wantCode: spectypes.BeaconVoteNilCheckpointErrorCode},
		{name: "nil target", vote: &ssvtypes.PreGloasBeaconVote{Source: checkpoint(0)}, wantCode: spectypes.BeaconVoteNilCheckpointErrorCode},
		{name: "source at target", vote: &ssvtypes.PreGloasBeaconVote{Source: checkpoint(1), Target: checkpoint(1)}, wantCode: spectypes.AttestationSourceNotLessThanTargetErrorCode},
		{name: "source after target", vote: &ssvtypes.PreGloasBeaconVote{Source: checkpoint(2), Target: checkpoint(1)}, wantCode: spectypes.AttestationSourceNotLessThanTargetErrorCode},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			spectests.AssertErrorCode(t, tc.wantCode, tc.vote.Validate())
		})
	}
}

func proposerConsensusData(version spec.DataVersion, dataSSZ []byte) *spectypes.ProposerConsensusData {
	return &spectypes.ProposerConsensusData{
		Duty:    *ssvtestingutils.TestingProposerDutyV(version),
		Version: version,
		DataSSZ: dataSSZ,
	}
}

func TestPreGloasBlockData(t *testing.T) {
	for _, version := range preGloasProposalVersions {
		for _, blinded := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s blinded=%t", version, blinded), func(t *testing.T) {
				cd := proposerConsensusData(version, ssvtestingutils.TestingBeaconBlockBytesV(version))
				want := ssvtestingutils.TestingBeaconBlockV(version)
				if blinded {
					cd = ssvtestingutils.TestProposerBlindedBlockConsensusDataV(version)
					want = ssvtestingutils.TestingBlindedBeaconBlockV(version)
				}

				proposal, hashRoot, err := ssvtypes.PreGloasBlockData(cd)
				require.NoError(t, err)
				require.Equal(t, version, proposal.Version)
				require.Equal(t, blinded, proposal.Blinded)

				// The signing root is the block's root, which is the fixture's.
				wantRoot, err := want.Root()
				require.NoError(t, err)
				blockRoot, err := proposal.Root()
				require.NoError(t, err)
				require.Equal(t, wantRoot, blockRoot)
				signingRoot, err := hashRoot.HashTreeRoot()
				require.NoError(t, err)
				require.Equal(t, [32]byte(wantRoot), signingRoot)
			})
		}

		t.Run(fmt.Sprintf("%s undecodable", version), func(t *testing.T) {
			for _, dataSSZ := range [][]byte{nil, {}, {0x01, 0x02, 0x03}} {
				_, _, err := ssvtypes.PreGloasBlockData(proposerConsensusData(version, dataSSZ))
				spectests.AssertErrorCode(t, spectypes.UnmarshalSSZErrorCode, err)
				require.ErrorContains(t, err, "could not unmarshal ssz (blinded err:")
			}
		})
	}

	// Proposals before Capella, Gloas proposals and unknown versions are not pre-Gloas proposals.
	for _, version := range []spec.DataVersion{spec.DataVersionBellatrix, spec.DataVersionGloas, spec.DataVersion(100)} {
		cd := proposerConsensusData(spec.DataVersionDeneb, ssvtestingutils.TestingBeaconBlockBytesV(spec.DataVersionDeneb))
		cd.Version = version
		_, _, err := ssvtypes.PreGloasBlockData(cd)
		spectests.AssertErrorCode(t, spectypes.UnknownBlockVersionErrorCode, err)
	}
}

func TestValidateConsensusData(t *testing.T) {
	tests := []struct {
		name     string
		cd       *spectypes.ProposerConsensusData
		wantCode int
	}{
		{
			name: "proposer block",
			cd:   proposerConsensusData(spec.DataVersionCapella, ssvtestingutils.TestingBeaconBlockBytesV(spec.DataVersionCapella)),
		},
		{
			name: "proposer blinded block",
			cd:   ssvtestingutils.TestProposerBlindedBlockConsensusDataV(spec.DataVersionFulu),
		},
		{
			name:     "proposer undecodable block",
			cd:       proposerConsensusData(spec.DataVersionCapella, []byte{}),
			wantCode: spectypes.QBFTValueInvalidErrorCode,
		},
		{
			name: "unknown duty role",
			cd: &spectypes.ProposerConsensusData{
				Duty:    spectypes.ValidatorDuty{Type: spectypes.BeaconRole(100), PubKey: spectestingutils.TestingValidatorPubKey},
				Version: spec.DataVersionCapella,
				DataSSZ: ssvtestingutils.TestingBeaconBlockBytesV(spec.DataVersionCapella),
			},
			wantCode: spectypes.UnknownDutyRoleDataErrorCode,
		},
		// Duties without consensus data fail before their data is decoded, so these carry none.
		{
			name: "validator registration has no consensus data",
			cd: &spectypes.ProposerConsensusData{
				Duty:    ssvtestingutils.TestingValidatorRegistrationDuty,
				Version: spec.DataVersionCapella,
			},
			wantCode: ssvtypes.ValidatorRegistrationNoConsensusPhaseErrorCode,
		},
		{
			name: "voluntary exit has no consensus data",
			cd: &spectypes.ProposerConsensusData{
				Duty:    spectestingutils.TestingVoluntaryExitDuty,
				Version: spec.DataVersionCapella,
			},
			wantCode: spectypes.ValidatorExitNoConsensusPhaseErrorCode,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			spectests.AssertErrorCode(t, tc.wantCode, ssvtypes.ValidateConsensusData(tc.cd))
		})
	}
}

func aggregatorConsensusData(version spec.DataVersion) *spectypes.AggregatorCommitteeConsensusData {
	duty := ssvtestingutils.TestingAggregatorCommitteeDuty([]int{spectestingutils.TestingValidatorIndex}, nil, version)
	return ssvtestingutils.TestAggregatorCommitteeConsensusDataForDuty(duty, version, nil)
}

func contributionConsensusData() *spectypes.AggregatorCommitteeConsensusData {
	duty := spectestingutils.TestingAggregatorCommitteeDutyOnlySyncCommittee()
	return ssvtestingutils.TestAggregatorCommitteeConsensusDataForDuty(duty, spec.DataVersionPhase0, nil)
}

// maxCommitteeIndex is the largest of cd's aggregator committee indexes.
func maxCommitteeIndex(cd *spectypes.AggregatorCommitteeConsensusData) uint64 {
	maxIndex := cd.AggregatorsCommitteeIndexes[0]
	for _, idx := range cd.AggregatorsCommitteeIndexes {
		maxIndex = max(maxIndex, idx)
	}
	return maxIndex
}

func TestValidateAggregatorCommitteeConsensusData(t *testing.T) {
	tests := []struct {
		name     string
		cd       func() *spectypes.AggregatorCommitteeConsensusData
		wantCode int
	}{
		{
			name: "phase0 aggregator",
			cd: func() *spectypes.AggregatorCommitteeConsensusData {
				return aggregatorConsensusData(spec.DataVersionPhase0)
			},
		},
		{
			name: "electra aggregator",
			cd: func() *spectypes.AggregatorCommitteeConsensusData {
				return aggregatorConsensusData(spec.DataVersionElectra)
			},
		},
		{
			name: "sync committee contribution",
			cd:   contributionConsensusData,
		},
		{
			name: "no validators",
			cd: func() *spectypes.AggregatorCommitteeConsensusData {
				return &spectypes.AggregatorCommitteeConsensusData{Version: spec.DataVersionPhase0}
			},
			wantCode: spectypes.AggCommConsensusDataNoValidatorErrorCode,
		},
		{
			name: "more committee indexes than attestations",
			cd: func() *spectypes.AggregatorCommitteeConsensusData {
				cd := aggregatorConsensusData(spec.DataVersionPhase0)
				cd.AggregatorsCommitteeIndexes = append(cd.AggregatorsCommitteeIndexes, cd.AggregatorsCommitteeIndexes...)
				return cd
			},
			wantCode: spectypes.AggCommAggCommIdxCntMismatchErrorCode,
		},
		{
			name: "duplicate committee index",
			cd: func() *spectypes.AggregatorCommitteeConsensusData {
				cd := aggregatorConsensusData(spec.DataVersionPhase0)
				cd.AggregatorsCommitteeIndexes = append(cd.AggregatorsCommitteeIndexes, cd.AggregatorsCommitteeIndexes[0])
				cd.AggregatedAttestations = append(cd.AggregatedAttestations, cd.AggregatedAttestations[0])
				return cd
			},
			wantCode: spectypes.AggCommDuplicatedCommIdxErrorCode,
		},
		{
			name: "aggregator committee index not listed",
			cd: func() *spectypes.AggregatorCommitteeConsensusData {
				cd := aggregatorConsensusData(spec.DataVersionPhase0)
				cd.Aggregators[0].CommitteeIndex = maxCommitteeIndex(cd) + 1
				return cd
			},
			wantCode: spectypes.AggCommCommIdxMismatchErrorCode,
		},
		{
			name: "committee index no aggregator uses",
			cd: func() *spectypes.AggregatorCommitteeConsensusData {
				cd := aggregatorConsensusData(spec.DataVersionPhase0)
				unused := maxCommitteeIndex(cd) + 1
				cd.Aggregators[0].CommitteeIndex = unused
				cd.AggregatorsCommitteeIndexes = append(cd.AggregatorsCommitteeIndexes, unused)
				cd.AggregatedAttestations = append(cd.AggregatedAttestations, cd.AggregatedAttestations[0])
				return cd
			},
			wantCode: spectypes.AggCommUnusedCommIdxErrorCode,
		},
		{
			name: "undecodable phase0 attestation",
			cd: func() *spectypes.AggregatorCommitteeConsensusData {
				cd := aggregatorConsensusData(spec.DataVersionPhase0)
				cd.AggregatedAttestations[0] = []byte{0x01, 0x02, 0x03}
				return cd
			},
			wantCode: spectypes.AggCommAttestationDecodingErrorCode,
		},
		{
			name: "undecodable electra attestation",
			cd: func() *spectypes.AggregatorCommitteeConsensusData {
				cd := aggregatorConsensusData(spec.DataVersionElectra)
				cd.AggregatedAttestations[0] = []byte{0x01, 0x02, 0x03}
				return cd
			},
			wantCode: spectypes.AggCommAttestationDecodingErrorCode,
		},
		{
			name: "duplicate contribution subnet",
			cd: func() *spectypes.AggregatorCommitteeConsensusData {
				cd := contributionConsensusData()
				cd.SyncCommitteeContributions = append(cd.SyncCommitteeContributions, cd.SyncCommitteeContributions[0])
				return cd
			},
			wantCode: spectypes.AggCommSCCSubnetDuplicateErrorCode,
		},
		{
			name: "contributor subnet not listed",
			cd: func() *spectypes.AggregatorCommitteeConsensusData {
				cd := contributionConsensusData()
				cd.Contributors[0].CommitteeIndex = 100
				return cd
			},
			wantCode: spectypes.AggCommSubnetNotInSCSubnetsErrorCode,
		},
		{
			name: "contribution subnet no contributor uses",
			cd: func() *spectypes.AggregatorCommitteeConsensusData {
				cd := contributionConsensusData()
				unused := cd.SyncCommitteeContributions[0]
				unused.SubcommitteeIndex = 100
				cd.SyncCommitteeContributions = append(cd.SyncCommitteeContributions, unused)
				return cd
			},
			wantCode: spectypes.AggCommUnusedSubnetErrorCode,
		},
		{
			name: "gloas aggregator",
			cd: func() *spectypes.AggregatorCommitteeConsensusData {
				return spectestingutils.TestAggregatorConsensusData(spec.DataVersionGloas)
			},
		},
		{
			name: "gloas without validators",
			cd: func() *spectypes.AggregatorCommitteeConsensusData {
				return &spectypes.AggregatorCommitteeConsensusData{Version: spec.DataVersionGloas}
			},
			wantCode: spectypes.AggCommConsensusDataNoValidatorErrorCode,
		},
		{
			// Data of a later fork isn't validated as pre-Gloas data.
			name: "version after gloas",
			cd: func() *spectypes.AggregatorCommitteeConsensusData {
				cd := spectestingutils.TestAggregatorConsensusData(spec.DataVersionGloas)
				cd.Version = spec.DataVersion(100)
				return cd
			},
			wantCode: spectypes.UnknownVersionErrorCode,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			spectests.AssertErrorCode(t, tc.wantCode, ssvtypes.ValidateAggregatorCommitteeConsensusData(tc.cd()))
		})
	}
}

func TestGetAggregateAndProofs(t *testing.T) {
	for _, version := range append(preGloasVersions, spec.DataVersionGloas) {
		t.Run(version.String(), func(t *testing.T) {
			cd := aggregatorConsensusData(version)
			if version == spec.DataVersionGloas {
				cd = spectestingutils.TestAggregatorConsensusData(version)
			}

			proofs, err := ssvtypes.GetAggregateAndProofs(cd)
			require.NoError(t, err)
			require.Len(t, proofs, len(cd.Aggregators))
			for i, proof := range proofs {
				// The accessors fail unless the proof sits in the field of its version.
				require.Equal(t, version, proof.Version)
				aggregatorIndex, err := proof.AggregatorIndex()
				require.NoError(t, err)
				require.Equal(t, cd.Aggregators[i].ValidatorIndex, aggregatorIndex)
				selectionProof, err := proof.SelectionProof()
				require.NoError(t, err)
				require.Equal(t, cd.Aggregators[i].SelectionProof, selectionProof)

				// The hash root is the proof in the field of its version.
				hashRoot, err := ssvtypes.GetAggregateAndProofHashRoot(proof)
				require.NoError(t, err)
				wantRoot, err := proof.HashTreeRoot()
				require.NoError(t, err)
				gotRoot, err := hashRoot.HashTreeRoot()
				require.NoError(t, err)
				require.Equal(t, wantRoot, gotRoot)
			}
		})
	}

	errorTests := []struct {
		name     string
		cd       func() *spectypes.AggregatorCommitteeConsensusData
		wantCode int
	}{
		{
			name: "aggregator committee index not listed",
			cd: func() *spectypes.AggregatorCommitteeConsensusData {
				cd := aggregatorConsensusData(spec.DataVersionPhase0)
				cd.Aggregators[0].CommitteeIndex = maxCommitteeIndex(cd) + 1
				return cd
			},
			wantCode: spectypes.AggCommCommIdxMismatchErrorCode,
		},
		{
			name: "no attestation for the committee index",
			cd: func() *spectypes.AggregatorCommitteeConsensusData {
				cd := aggregatorConsensusData(spec.DataVersionPhase0)
				cd.AggregatedAttestations = nil
				return cd
			},
			wantCode: spectypes.AggCommCommIdxMismatchErrorCode,
		},
		{
			name: "undecodable phase0 attestation",
			cd: func() *spectypes.AggregatorCommitteeConsensusData {
				cd := aggregatorConsensusData(spec.DataVersionPhase0)
				cd.AggregatedAttestations[0] = []byte{0x01, 0x02, 0x03}
				return cd
			},
			wantCode: spectypes.UnmarshalSSZErrorCode,
		},
		{
			name: "undecodable electra attestation",
			cd: func() *spectypes.AggregatorCommitteeConsensusData {
				cd := aggregatorConsensusData(spec.DataVersionElectra)
				cd.AggregatedAttestations[0] = []byte{0x01, 0x02, 0x03}
				return cd
			},
			wantCode: spectypes.UnmarshalSSZErrorCode,
		},
		{
			name: "unknown version",
			cd: func() *spectypes.AggregatorCommitteeConsensusData {
				cd := aggregatorConsensusData(spec.DataVersionPhase0)
				cd.Version = spec.DataVersionUnknown
				return cd
			},
			wantCode: spectypes.UnknownBlockVersionErrorCode,
		},
	}
	for _, tc := range errorTests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ssvtypes.GetAggregateAndProofs(tc.cd())
			spectests.AssertErrorCode(t, tc.wantCode, err)
		})
	}
}

func TestGetAggregateAndProofHashRoot(t *testing.T) {
	phase0Proof := &phase0.AggregateAndProof{AggregatorIndex: 1}
	electraProof := &electra.AggregateAndProof{AggregatorIndex: 2}
	gloasProof := &eth2gloas.AggregateAndProof{AggregatorIndex: 3}
	tests := []struct {
		proof *spec.VersionedAggregateAndProof
		want  spectypes.HashRoot
	}{
		{&spec.VersionedAggregateAndProof{Version: spec.DataVersionPhase0, Phase0: phase0Proof}, phase0Proof},
		{&spec.VersionedAggregateAndProof{Version: spec.DataVersionAltair, Altair: phase0Proof}, phase0Proof},
		{&spec.VersionedAggregateAndProof{Version: spec.DataVersionBellatrix, Bellatrix: phase0Proof}, phase0Proof},
		{&spec.VersionedAggregateAndProof{Version: spec.DataVersionCapella, Capella: phase0Proof}, phase0Proof},
		{&spec.VersionedAggregateAndProof{Version: spec.DataVersionDeneb, Deneb: phase0Proof}, phase0Proof},
		{&spec.VersionedAggregateAndProof{Version: spec.DataVersionElectra, Electra: electraProof}, electraProof},
		{&spec.VersionedAggregateAndProof{Version: spec.DataVersionFulu, Fulu: electraProof}, electraProof},
		{&spec.VersionedAggregateAndProof{Version: spec.DataVersionGloas, Gloas: gloasProof}, gloasProof},
	}
	for _, tc := range tests {
		got, err := ssvtypes.GetAggregateAndProofHashRoot(tc.proof)
		require.NoError(t, err, tc.proof.Version)
		require.Same(t, tc.want, got, tc.proof.Version)
	}

	_, err := ssvtypes.GetAggregateAndProofHashRoot(&spec.VersionedAggregateAndProof{Version: spec.DataVersionUnknown})
	spectests.AssertErrorCode(t, spectypes.UnknownVersionErrorCode, err)
}

func TestVersionedAttestation(t *testing.T) {
	duty := &spectypes.ValidatorDuty{
		Type:                    spectypes.BNRoleAttester,
		ValidatorIndex:          7,
		CommitteeIndex:          3,
		CommitteeLength:         128,
		ValidatorCommitteeIndex: 11,
	}
	attestationData := &phase0.AttestationData{
		Slot:            12,
		BeaconBlockRoot: phase0.Root{0x01},
		Source:          &phase0.Checkpoint{Epoch: 0},
		Target:          &phase0.Checkpoint{Epoch: 1},
	}
	sig := phase0.BLSSignature{0xaa}

	for _, version := range append(preGloasVersions, spec.DataVersionGloas) {
		t.Run(version.String(), func(t *testing.T) {
			att, err := ssvtypes.ConstructVersionedAttestationWithoutSignature(attestationData, version, duty)
			require.NoError(t, err)

			// The accessors fail unless the attestation sits in the field of its version.
			require.Equal(t, version, att.Version)
			require.Equal(t, duty.ValidatorIndex, *att.ValidatorIndex)
			data, err := att.Data()
			require.NoError(t, err)
			require.Same(t, attestationData, data)
			aggregationBits, err := att.AggregationBits()
			require.NoError(t, err)
			require.Equal(t, duty.CommitteeLength, aggregationBits.Len())
			require.Equal(t, uint64(1), aggregationBits.Count())
			require.True(t, aggregationBits.BitAt(duty.ValidatorCommitteeIndex))
			if version >= spec.DataVersionElectra {
				committeeBits, err := att.CommitteeBits()
				require.NoError(t, err)
				require.Equal(t, uint64(1), committeeBits.Count())
				require.True(t, committeeBits.BitAt(uint64(duty.CommitteeIndex)))
			}

			signed, err := ssvtypes.VersionedAttestationWithSignature(att, sig)
			require.NoError(t, err)
			gotSig, err := signed.Signature()
			require.NoError(t, err)
			require.Equal(t, sig, gotSig)

			// A versioned attestation missing the attestation of its version can't be signed.
			_, err = ssvtypes.VersionedAttestationWithSignature(&spec.VersionedAttestation{Version: version}, sig)
			require.ErrorContains(t, err, "attestation")
		})
	}

	for _, version := range []spec.DataVersion{spec.DataVersionUnknown, spec.DataVersion(100)} {
		_, err := ssvtypes.ConstructVersionedAttestationWithoutSignature(attestationData, version, duty)
		require.ErrorContains(t, err, "unknown version", version)
		_, err = ssvtypes.VersionedAttestationWithSignature(&spec.VersionedAttestation{Version: version}, sig)
		require.ErrorContains(t, err, "unknown version", version)
	}
}
