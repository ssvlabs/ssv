package signing

import (
	"context"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/gloas"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/suite"

	"github.com/ssvlabs/ssv/ssvsigner/e2e"
	"github.com/ssvlabs/ssv/ssvsigner/e2e/common"
	"github.com/ssvlabs/ssv/ssvsigner/e2e/testenv"
	"github.com/ssvlabs/ssv/ssvsigner/ekm"
	"github.com/ssvlabs/ssv/ssvsigner/internal/beacontest"
	"github.com/ssvlabs/ssv/ssvsigner/web3signer"
)

// GloasSigningTestSuite covers remote signing at Gloas (ePBS) slots: the Gloas request types (block,
// aggregate, payload attestation, proposer preferences, builder request auth) and the older ones the
// node still sends there (attestation, randao reveal, aggregation slot and the sync committee
// requests), plus a pre-Gloas aggregate on the legacy request type. Web3Signer rebuilds each object
// from the request and rejects a mismatching signing_root, so a remote signature equal to the local
// one shows that RemoteKeyManager, SSV-Signer and Web3Signer agree on the object, its root and its
// domain.
type GloasSigningTestSuite struct {
	e2e.E2ETestSuite
}

// TestGloasSigningConsistency signs each object with LocalKeyManager and with RemoteKeyManager (via
// SSV-Signer and Web3Signer) and requires the same signing root and signature.
func (s *GloasSigningTestSuite) TestGloasSigningConsistency() {
	// Move the clock past the Gloas fork the environment schedules.
	testCurrentEpoch := testenv.GloasForkEpoch + 2
	s.GetEnv().SetTestCurrentEpoch(testCurrentEpoch)

	ctx, cancel := context.WithTimeout(s.GetContext(), 60*time.Second)
	defer cancel()

	validatorKeyPair := s.AddValidator(ctx)
	pubKey := validatorKeyPair.BLSPubKey

	beaconConfig := s.GetEnv().GetBeaconConfig()
	// Past the slashing-protection bump AddValidator applies at the current slot.
	slot := beaconConfig.FirstSlotAtEpoch(testCurrentEpoch+1) + 5
	proposalSlot := slot + 1

	domainAt := func(domainType phase0.DomainType, slot phase0.Slot) phase0.Domain {
		domain, err := s.CalculateDomain(domainType, beaconConfig.EstimatedEpochAtSlot(slot))
		s.Require().NoError(err)
		return domain
	}
	requestAuthDomain, err := s.CalculateGenesisDomain(spectypes.DomainBuilderRequestAuth)
	s.Require().NoError(err)

	attestationData := common.NewTestAttestationData(testCurrentEpoch, testCurrentEpoch+1, slot)
	// A Fulu slot, for the pre-Gloas aggregate that stays on the legacy request type.
	preGloasSlot := beaconConfig.FirstSlotAtEpoch(testenv.GloasForkEpoch-1) + 5
	preGloasAttestationData := common.NewTestAttestationData(testenv.GloasForkEpoch-2, testenv.GloasForkEpoch-1, preGloasSlot)
	// From Gloas on, a committee attestation's index carries the payload status (SIP #94 §2).
	payloadStatusAttestation := *attestationData
	payloadStatusAttestation.Index = 1
	syncCommitteeBlockRoot := phase0.Root{0x04}

	tests := []struct {
		name       string
		obj        spectypes.HashRoot
		domainType phase0.DomainType
		domain     phase0.Domain
		slot       phase0.Slot
	}{
		{
			name:       "beacon block",
			obj:        beacontest.GloasBlock(slot, 0),
			domainType: spectypes.DomainProposer,
			domain:     domainAt(spectypes.DomainProposer, slot),
			slot:       slot,
		},
		{
			name:       "aggregate and proof",
			obj:        common.NewTestGloasAggregateAndProof(attestationData, 0),
			domainType: spectypes.DomainAggregateAndProof,
			domain:     domainAt(spectypes.DomainAggregateAndProof, slot),
			slot:       slot,
		},
		{
			name:       "pre-Gloas aggregate and proof",
			obj:        common.NewTestElectraAggregateAndProof(preGloasAttestationData, 0),
			domainType: spectypes.DomainAggregateAndProof,
			domain:     domainAt(spectypes.DomainAggregateAndProof, preGloasSlot),
			slot:       preGloasSlot,
		},
		{
			name: "payload attestation",
			obj: &gloas.PayloadAttestationData{
				BeaconBlockRoot:   phase0.Root{0x01},
				Slot:              slot,
				PayloadPresent:    true,
				BlobDataAvailable: true,
			},
			domainType: spectypes.DomainPTCAttester,
			domain:     domainAt(spectypes.DomainPTCAttester, slot),
			slot:       slot,
		},
		{
			name: "proposer preferences",
			obj: &gloas.ProposerPreferences{
				DependentRoot:  phase0.Root{0x02},
				ProposalSlot:   proposalSlot,
				ValidatorIndex: 0,
				FeeRecipient:   bellatrix.ExecutionAddress{0x03},
				TargetGasLimit: 60_000_000,
			},
			domainType: spectypes.DomainProposerPreferences,
			domain:     domainAt(spectypes.DomainProposerPreferences, proposalSlot),
			slot:       proposalSlot,
		},
		{
			name: "builder request auth",
			obj: &gloas.BuilderRequestAuth{
				Data: []byte("builder.example.com"),
				Slot: proposalSlot,
			},
			domainType: spectypes.DomainBuilderRequestAuth,
			domain:     requestAuthDomain,
			slot:       proposalSlot,
		},
		{
			name:       "attestation with a payload-status index",
			obj:        &payloadStatusAttestation,
			domainType: spectypes.DomainAttester,
			domain:     domainAt(spectypes.DomainAttester, slot),
			slot:       slot,
		},
		{
			name:       "sync committee message",
			obj:        spectypes.SSZBytes(syncCommitteeBlockRoot[:]),
			domainType: spectypes.DomainSyncCommittee,
			domain:     domainAt(spectypes.DomainSyncCommittee, slot),
			slot:       slot,
		},
		{
			name:       "randao reveal",
			obj:        spectypes.SSZUint64(beaconConfig.EstimatedEpochAtSlot(slot)),
			domainType: spectypes.DomainRandao,
			domain:     domainAt(spectypes.DomainRandao, slot),
			slot:       slot,
		},
		{
			name:       "aggregation slot",
			obj:        spectypes.SSZUint64(slot),
			domainType: spectypes.DomainSelectionProof,
			domain:     domainAt(spectypes.DomainSelectionProof, slot),
			slot:       slot,
		},
		{
			name:       "sync committee selection proof",
			obj:        &altair.SyncAggregatorSelectionData{Slot: slot, SubcommitteeIndex: 1},
			domainType: spectypes.DomainSyncCommitteeSelectionProof,
			domain:     domainAt(spectypes.DomainSyncCommitteeSelectionProof, slot),
			slot:       slot,
		},
		{
			name:       "sync committee contribution and proof",
			obj:        common.NewTestContributionAndProof(slot, 1, 0),
			domainType: spectypes.DomainContributionAndProof,
			domain:     domainAt(spectypes.DomainContributionAndProof, slot),
			slot:       slot,
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			localSig, localRoot, err := s.GetEnv().GetLocalKeyManager().SignBeaconObject(
				ctx, tt.obj, tt.domain, pubKey, tt.slot, tt.domainType)
			s.Require().NoError(err, "Local key manager failed to sign")

			remoteSig, remoteRoot, err := s.GetEnv().GetRemoteKeyManager().SignBeaconObject(
				ctx, tt.obj, tt.domain, pubKey, tt.slot, tt.domainType)
			s.Require().NoError(err, "Remote key manager failed to sign")

			s.Require().Equal(localRoot, remoteRoot, "local vs remote signing root should match")
			s.Require().Equal(localSig, remoteSig, "local vs remote signature should match")
		})
	}

	s.Run("web3signer recomputes the root", func() {
		// The consistency checks above rest on this: Web3Signer rebuilds the object from the request's
		// data and refuses a signing_root that doesn't match it, rather than signing the root it's given.
		data := &gloas.PayloadAttestationData{BeaconBlockRoot: phase0.Root{0x01}, Slot: slot, PayloadPresent: true}
		domain := domainAt(spectypes.DomainPTCAttester, slot)
		root, err := spectypes.ComputeETHSigningRoot(data, domain)
		s.Require().NoError(err)

		requestFor := func(data *gloas.PayloadAttestationData) web3signer.SignRequest {
			return web3signer.SignRequest{
				ForkInfo:    s.GetEnv().GetRemoteKeyManager().GetForkInfo(beaconConfig.EstimatedEpochAtSlot(slot)),
				SigningRoot: root,
				Type:        web3signer.TypePayloadAttestationMessage,
				PayloadAttestationMessage: &web3signer.VersionedRequest[*gloas.PayloadAttestationData]{
					Version: web3signer.DataVersion(spec.DataVersionGloas),
					Data:    data,
				},
			}
		}

		_, err = s.GetEnv().GetWeb3SignerClient().Sign(ctx, pubKey, requestFor(data))
		s.Require().NoError(err, "Web3Signer should sign a request whose signing_root matches the data")

		tampered := *data
		tampered.PayloadPresent = false
		_, err = s.GetEnv().GetWeb3SignerClient().Sign(ctx, pubKey, requestFor(&tampered))
		var httpErr web3signer.HTTPResponseError
		s.Require().ErrorAs(err, &httpErr, "Web3Signer should refuse a signing_root that doesn't match the data")
		s.Require().Equal(400, httpErr.Status)
	})

	s.Run("execution payload envelope", func() {
		// The remote signer can't sign the blinded envelope, and refuses before reaching SSV-Signer.
		_, _, err := s.GetEnv().GetRemoteKeyManager().SignBeaconObject(
			ctx, spectypes.SSZ32Bytes{0x01}, domainAt(spectypes.DomainBeaconBuilder, slot), pubKey, slot, spectypes.DomainBeaconBuilder)
		s.Require().ErrorIs(err, ekm.ErrEnvelopeSigningUnsupported)
	})

	s.T().Logf("✅ Local and remote signers agree on every Gloas request")
}

// TestGloasSigning runs the Gloas signing test suite.
func TestGloasSigning(t *testing.T) {
	suite.Run(t, new(GloasSigningTestSuite))
}
