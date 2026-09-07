package runner

import (
	"context"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/deneb"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"

	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/ssvlabs/ssv/protocol/v2/ssv"
	protocoltesting "github.com/ssvlabs/ssv/protocol/v2/testing"
	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
	"github.com/ssvlabs/ssv/ssvsigner/ekm"
)

func envelopeDuty(slot phase0.Slot) *spectypes.ValidatorDuty {
	return &spectypes.ValidatorDuty{
		Type:           spectypes.BNRoleEnvelopeProposer,
		PubKey:         spectestingutils.TestingValidatorPubKey,
		Slot:           slot,
		ValidatorIndex: spectestingutils.TestingValidatorIndex,
	}
}

// envelopeTestBeacon embeds the spec testing beacon so DomainData (used by the signing-root computation)
// resolves, and records the published reveals.
type envelopeTestBeacon struct {
	beacon.BeaconNode
	submitted []*gloas.SignedExecutionPayloadEnvelopeContents
}

func newEnvelopeTestBeacon() *envelopeTestBeacon {
	return &envelopeTestBeacon{BeaconNode: protocoltesting.NewTestingBeaconNodeWrapped()}
}

func (b *envelopeTestBeacon) SubmitExecutionPayloadEnvelope(_ context.Context, contents *gloas.SignedExecutionPayloadEnvelopeContents) error {
	b.submitted = append(b.submitted, contents)
	return nil
}

func sampleEnvelope() *gloas.ExecutionPayloadEnvelope {
	return &gloas.ExecutionPayloadEnvelope{
		Payload:               &gloas.ExecutionPayload{BlockNumber: 42},
		ExecutionRequests:     &gloas.ExecutionRequests{},
		BuilderIndex:          gloas.BuilderIndexSelfBuild,
		BeaconBlockRoot:       phase0.Root{0xaa},
		ParentBeaconBlockRoot: phase0.Root{0xbb},
	}
}

// sampleProduced is the reveal data the builder operator's produce response carried for the envelope.
func sampleProduced(envelope *gloas.ExecutionPayloadEnvelope) *gloas.ProducedEnvelope {
	return &gloas.ProducedEnvelope{Envelope: envelope, KZGProofs: []deneb.KZGProof{{0x01}}, Blobs: []deneb.Blob{{0x02}}}
}

// proposalFor derives the §4 decision the envelope binds to, as the proposer runner would record it; the
// builder operator's record also carries the reveal data.
func proposalFor(t *testing.T, envelope *gloas.ExecutionPayloadEnvelope, producedLocally bool) ssv.ProposedBlock {
	t.Helper()
	requestsRoot, err := envelope.ExecutionRequests.HashTreeRoot()
	require.NoError(t, err)
	proposal := ssv.ProposedBlock{
		BlockRoot:             envelope.BeaconBlockRoot,
		ParentRoot:            envelope.ParentBeaconBlockRoot,
		ExecutionRequestsRoot: phase0.Root(requestsRoot),
		ProducedLocally:       producedLocally,
	}
	if producedLocally {
		proposal.ProducedEnvelope = sampleProduced(envelope)
	}
	return proposal
}

func newEnvelopeProposerRunnerForTest(t *testing.T, bn beacon.BeaconNode) (*EnvelopeProposerRunner, *spectestingutils.TestKeySet) {
	t.Helper()

	cfg := cloneTestNetworkConfig()
	keySet := spectestingutils.Testing4SharesSet()
	share := spectestingutils.TestingShare(keySet, spectestingutils.TestingValidatorIndex)
	network := protocoltesting.NewTestingNetwork(1, keySet.OperatorKeys[1])
	km := ekm.NewTestingKeyManagerAdapter(spectestingutils.NewTestingKeyManager())
	operatorSigner := spectestingutils.NewOperatorSigner(keySet, 1)

	runnerIface, err := NewEnvelopeProposerRunner(EnvelopeProposerRunnerOptions{
		BaseRunnerOptions: BaseRunnerOptions{
			NetworkConfig:  cfg,
			Share:          map[phase0.ValidatorIndex]*spectypes.Share{share.ValidatorIndex: share},
			Beacon:         bn,
			Network:        network,
			Signer:         km,
			OperatorSigner: operatorSigner,
		},
		ProposedBlocks: ssv.NewProposedBlocks(),
	})
	require.NoError(t, err)
	return runnerIface.(*EnvelopeProposerRunner), keySet
}

func broadcastMsgs(r *EnvelopeProposerRunner) []*spectypes.SignedSSVMessage {
	return r.network.(*protocoltesting.TestingNetwork).BroadcastedMsgs
}

// disseminationMsg wraps a blinded envelope as operator signer's dissemination for the slot (the operator
// signature is irrelevant to the runner, which receives already-validated messages).
func disseminationMsg(t *testing.T, slot phase0.Slot, blinded *gloas.BlindedExecutionPayloadEnvelope, signer spectypes.OperatorID) (*spectypes.SignedSSVMessage, *spectypes.EnvelopeDissemination) {
	t.Helper()
	dissemination := &spectypes.EnvelopeDissemination{Slot: slot, Envelope: blinded}
	data, err := dissemination.Encode()
	require.NoError(t, err)
	return &spectypes.SignedSSVMessage{
		OperatorIDs: []spectypes.OperatorID{signer},
		SSVMessage:  &spectypes.SSVMessage{MsgType: spectypes.SSVEnvelopeDisseminationMsgType, Data: data},
	}, dissemination
}

// envelopePartialSig builds operator opID's EnvelopePartialSig over the blinded envelope's root, signed
// under DOMAIN_BEACON_BUILDER with its share key. The testing beacon's domain is epoch-invariant, so it
// matches the domain the runner derives at the duty's epoch.
func envelopePartialSig(t *testing.T, keySet *spectestingutils.TestKeySet, blinded *gloas.BlindedExecutionPayloadEnvelope, slot phase0.Slot, opID spectypes.OperatorID) *spectypes.PartialSignatureMessages {
	t.Helper()
	signer := spectestingutils.NewTestingKeyManager()
	domain, err := spectestingutils.NewTestingBeaconNode().DomainData(1, spectypes.DomainBeaconBuilder)
	require.NoError(t, err)
	root, err := blinded.HashTreeRoot()
	require.NoError(t, err)
	sig, signingRoot, err := signer.SignBeaconObject(spectypes.SSZ32Bytes(root), domain, keySet.Shares[opID].GetPublicKey().Serialize(), spectypes.DomainBeaconBuilder)
	require.NoError(t, err)
	blsSig := phase0.BLSSignature{}
	copy(blsSig[:], sig)
	return &spectypes.PartialSignatureMessages{
		Type: spectypes.EnvelopePartialSig,
		Slot: slot,
		Messages: []*spectypes.PartialSignatureMessage{{
			PartialSignature: blsSig[:],
			SigningRoot:      signingRoot,
			Signer:           opID,
			ValidatorIndex:   spectestingutils.TestingValidatorIndex,
		}},
	}
}

func decodeDissemination(t *testing.T, msg *spectypes.SignedSSVMessage) *spectypes.EnvelopeDissemination {
	t.Helper()
	require.Equal(t, spectypes.SSVEnvelopeDisseminationMsgType, msg.SSVMessage.MsgType)
	dissemination := &spectypes.EnvelopeDissemination{}
	require.NoError(t, dissemination.Decode(msg.SSVMessage.Data))
	return dissemination
}

func decodePartialSig(t *testing.T, msg *spectypes.SignedSSVMessage) *spectypes.PartialSignatureMessages {
	t.Helper()
	require.Equal(t, spectypes.SSVPartialSignatureMsgType, msg.SSVMessage.MsgType)
	msgs := &spectypes.PartialSignatureMessages{}
	require.NoError(t, msgs.Decode(msg.SSVMessage.Data))
	return msgs
}

func TestNewEnvelopeProposerRunner_RequiresOneShare(t *testing.T) {
	_, err := NewEnvelopeProposerRunner(EnvelopeProposerRunnerOptions{})
	require.Error(t, err)
}

// The §4→§6 linkage store is read unconditionally, so it is required.
func TestNewEnvelopeProposerRunner_RequiresProposedBlocks(t *testing.T) {
	_, err := NewEnvelopeProposerRunner(EnvelopeProposerRunnerOptions{
		BaseRunnerOptions: BaseRunnerOptions{
			Share: map[phase0.ValidatorIndex]*spectypes.Share{3: {ValidatorIndex: 3}},
		},
	})
	require.ErrorContains(t, err, "proposed blocks")
}

// The builder operator — its own produce response is the decided block — disseminates the blinded form of
// the envelope that response carried and signs it in the same step, with no beacon-node call.
func TestEnvelopeProposerRunner_BuilderDisseminatesAndSigns(t *testing.T) {
	const slot = phase0.Slot(8)
	envelope := sampleEnvelope()
	r, _ := newEnvelopeProposerRunnerForTest(t, newEnvelopeTestBeacon())
	proposal := proposalFor(t, envelope, true)
	r.proposedBlocks.Record(slot, proposal)

	require.NoError(t, r.StartNewDuty(context.Background(), zap.NewNop(), envelopeDuty(slot), 3))

	require.Same(t, proposal.ProducedEnvelope, r.produced)
	require.NotNil(t, r.selectedEnvelope)
	require.True(t, r.builtSelectedEnvelope())

	broadcast := broadcastMsgs(r)
	require.Len(t, broadcast, 2, "one dissemination, then one partial signature")

	dissemination := decodeDissemination(t, broadcast[0])
	require.Equal(t, slot, dissemination.Slot)
	wantPayloadRoot, err := envelope.Payload.HashTreeRoot()
	require.NoError(t, err)
	require.Equal(t, phase0.Root(wantPayloadRoot), dissemination.Envelope.PayloadRoot)
	require.Equal(t, envelope.BeaconBlockRoot, dissemination.Envelope.BeaconBlockRoot)
	require.Equal(t, uint64(gloas.BuilderIndexSelfBuild), uint64(dissemination.Envelope.BuilderIndex))

	partial := decodePartialSig(t, broadcast[1])
	require.Equal(t, spectypes.EnvelopePartialSig, partial.Type)
	require.Equal(t, slot, partial.Slot)
	require.Len(t, partial.Messages, 1)
	selectedRoot, err := r.selectedEnvelope.HashTreeRoot()
	require.NoError(t, err)
	wantSigningRoot, err := spectypes.ComputeETHSigningRoot(spectypes.SSZ32Bytes(selectedRoot), mustDomain(t, r))
	require.NoError(t, err)
	require.Equal(t, wantSigningRoot, phase0.Root(partial.Messages[0].SigningRoot))
}

// A non-builder disseminates nothing: it did not produce the decided block, so it signs only once a peer's
// binding dissemination arrives.
func TestEnvelopeProposerRunner_NonBuilderWaitsForDissemination(t *testing.T) {
	const slot = phase0.Slot(8)
	r, _ := newEnvelopeProposerRunnerForTest(t, newEnvelopeTestBeacon())
	r.proposedBlocks.Record(slot, proposalFor(t, sampleEnvelope(), false))

	require.NoError(t, r.StartNewDuty(context.Background(), zap.NewNop(), envelopeDuty(slot), 3))

	require.True(t, r.HasRunningDuty())
	require.Nil(t, r.produced)
	require.Nil(t, r.selectedEnvelope)
	require.Empty(t, broadcastMsgs(r))
}

// Without a recorded §4 decision the duty stays running and does nothing yet.
func TestEnvelopeProposerRunner_NoDecisionYetWaits(t *testing.T) {
	r, _ := newEnvelopeProposerRunnerForTest(t, newEnvelopeTestBeacon())

	require.NoError(t, r.StartNewDuty(context.Background(), zap.NewNop(), envelopeDuty(8), 3))

	require.True(t, r.HasRunningDuty())
	require.Empty(t, broadcastMsgs(r))
}

// A builder operator whose produce response carried no reveal data (the beacon node ignored
// include_payload=true) cannot disseminate; the duty fails.
func TestEnvelopeProposerRunner_BuilderWithoutRevealDataFailsDuty(t *testing.T) {
	const slot = phase0.Slot(8)
	r, _ := newEnvelopeProposerRunnerForTest(t, newEnvelopeTestBeacon())
	proposal := proposalFor(t, sampleEnvelope(), true)
	proposal.ProducedEnvelope = nil
	r.proposedBlocks.Record(slot, proposal)

	err := r.StartNewDuty(context.Background(), zap.NewNop(), envelopeDuty(slot), 3)
	require.ErrorContains(t, err, "include_payload=true not honored")
	require.Empty(t, broadcastMsgs(r))
}

// Content-based selection: the first dissemination that binds to the §4 decision is signed; non-binding
// ones are skipped, and later ones are ignored once an envelope is selected.
func TestEnvelopeProposerRunner_ProcessEnvelopeDisseminationSelectsFirstBinding(t *testing.T) {
	const slot = phase0.Slot(8)
	ctx, logger := context.Background(), zap.NewNop()
	envelope := sampleEnvelope()
	r, _ := newEnvelopeProposerRunnerForTest(t, newEnvelopeTestBeacon())
	r.proposedBlocks.Record(slot, proposalFor(t, envelope, false))
	require.NoError(t, r.StartNewDuty(ctx, logger, envelopeDuty(slot), 3))

	binding, err := gloas.Blinded(envelope)
	require.NoError(t, err)

	// A well-formed dissemination for another block does not bind: skipped without selecting.
	nonBinding, err := gloas.Blinded(envelope)
	require.NoError(t, err)
	nonBinding.BeaconBlockRoot = phase0.Root{0xcc}
	msg, dissemination := disseminationMsg(t, slot, nonBinding, 2)
	require.NoError(t, r.ProcessEnvelopeDissemination(ctx, logger, msg, dissemination))
	require.Nil(t, r.selectedEnvelope)
	require.Empty(t, broadcastMsgs(r))

	// The binding one is selected and signed.
	msg, dissemination = disseminationMsg(t, slot, binding, 2)
	require.NoError(t, r.ProcessEnvelopeDissemination(ctx, logger, msg, dissemination))
	require.Equal(t, binding, r.selectedEnvelope)
	require.Len(t, broadcastMsgs(r), 1)
	require.Equal(t, spectypes.EnvelopePartialSig, decodePartialSig(t, broadcastMsgs(r)[0]).Type)

	// A later binding dissemination with a different PayloadRoot is ignored: selection is first-binding.
	later, err := gloas.Blinded(envelope)
	require.NoError(t, err)
	later.PayloadRoot = phase0.Root{0x10}
	msg, dissemination = disseminationMsg(t, slot, later, 3)
	require.NoError(t, r.ProcessEnvelopeDissemination(ctx, logger, msg, dissemination))
	require.Equal(t, binding, r.selectedEnvelope)
	require.Len(t, broadcastMsgs(r), 1)
}

// A dissemination that arrives before the duty started, or before this operator's block instance
// decided, is retried rather than dropped: the builder broadcasts it once.
func TestEnvelopeProposerRunner_ProcessEnvelopeDisseminationRetries(t *testing.T) {
	const slot = phase0.Slot(8)
	ctx, logger := context.Background(), zap.NewNop()
	envelope := sampleEnvelope()
	blinded, err := gloas.Blinded(envelope)
	require.NoError(t, err)
	r, _ := newEnvelopeProposerRunnerForTest(t, newEnvelopeTestBeacon())

	// Before the duty starts.
	msg, dissemination := disseminationMsg(t, slot, blinded, 2)
	err = r.ProcessEnvelopeDissemination(ctx, logger, msg, dissemination)
	require.True(t, IsRetryable(err))
	require.ErrorIs(t, err, ErrNoDutyAssigned)

	// Started, but §4 has not decided on this operator yet.
	require.NoError(t, r.StartNewDuty(ctx, logger, envelopeDuty(slot), 3))
	err = r.ProcessEnvelopeDissemination(ctx, logger, msg, dissemination)
	require.True(t, IsRetryable(err))
	require.ErrorIs(t, err, errEnvelopeProposalNotDecided)
	require.Nil(t, r.selectedEnvelope)

	// Once the decision lands, the retried dissemination is selected.
	r.proposedBlocks.Record(slot, proposalFor(t, envelope, false))
	require.NoError(t, r.ProcessEnvelopeDissemination(ctx, logger, msg, dissemination))
	require.Equal(t, blinded, r.selectedEnvelope)

	// A dissemination for a later duty is retried; one for a slot behind us is dropped.
	future, futureDissemination := disseminationMsg(t, slot+1, blinded, 2)
	err = r.ProcessEnvelopeDissemination(ctx, logger, future, futureDissemination)
	require.True(t, IsRetryable(err))
	require.ErrorIs(t, err, ErrFuturePartialSigMsg)
	past, pastDissemination := disseminationMsg(t, slot-1, blinded, 2)
	require.NoError(t, r.ProcessEnvelopeDissemination(ctx, logger, past, pastDissemination))
}

// A peer's partial signature that arrives before this operator selected an envelope has no expected
// root yet; it is retried rather than dropped.
func TestEnvelopeProposerRunner_PartialSignatureBeforeSelectionRetries(t *testing.T) {
	const slot = phase0.Slot(8)
	ctx, logger := context.Background(), zap.NewNop()
	envelope := sampleEnvelope()
	blinded, err := gloas.Blinded(envelope)
	require.NoError(t, err)
	r, keySet := newEnvelopeProposerRunnerForTest(t, newEnvelopeTestBeacon())
	r.proposedBlocks.Record(slot, proposalFor(t, envelope, false))
	require.NoError(t, r.StartNewDuty(ctx, logger, envelopeDuty(slot), 3))

	err = r.ProcessPreConsensus(ctx, logger, envelopePartialSig(t, keySet, blinded, slot, 2))
	require.True(t, IsRetryable(err))
	require.ErrorIs(t, err, errNoSelectedEnvelope)
}

// On quorum the builder operator reconstructs the signature and publishes the reveal: the full envelope
// carrying it, with the blobs and KZG proofs from its produce response.
func TestEnvelopeProposerRunner_QuorumBuilderPublishes(t *testing.T) {
	const slot = phase0.Slot(8)
	ctx, logger := context.Background(), zap.NewNop()
	envelope := sampleEnvelope()
	bn := newEnvelopeTestBeacon()
	r, keySet := newEnvelopeProposerRunnerForTest(t, bn)
	proposal := proposalFor(t, envelope, true)
	r.proposedBlocks.Record(slot, proposal)
	require.NoError(t, r.StartNewDuty(ctx, logger, envelopeDuty(slot), keySet.Threshold))

	for opID := spectypes.OperatorID(1); opID <= keySet.Threshold; opID++ {
		require.NoError(t, r.ProcessPreConsensus(ctx, logger, envelopePartialSig(t, keySet, r.selectedEnvelope, slot, opID)))
	}

	require.Len(t, bn.submitted, 1)
	published := bn.submitted[0]
	require.Equal(t, envelope, published.SignedExecutionPayloadEnvelope.Message)
	require.NotEqual(t, phase0.BLSSignature{}, published.SignedExecutionPayloadEnvelope.Signature) // the reconstructed signature
	require.Equal(t, proposal.ProducedEnvelope.KZGProofs, published.KZGProofs)
	require.Equal(t, proposal.ProducedEnvelope.Blobs, published.Blobs)
	require.True(t, r.State.Succeeded)
}

// A non-builder completes the duty on quorum without publishing: it holds no reveal data behind the root.
func TestEnvelopeProposerRunner_QuorumNonBuilderDoesNotPublish(t *testing.T) {
	const slot = phase0.Slot(8)
	ctx, logger := context.Background(), zap.NewNop()
	envelope := sampleEnvelope()
	blinded, err := gloas.Blinded(envelope)
	require.NoError(t, err)
	bn := newEnvelopeTestBeacon()
	r, keySet := newEnvelopeProposerRunnerForTest(t, bn)
	r.proposedBlocks.Record(slot, proposalFor(t, envelope, false))
	require.NoError(t, r.StartNewDuty(ctx, logger, envelopeDuty(slot), keySet.Threshold))

	msg, dissemination := disseminationMsg(t, slot, blinded, 2)
	require.NoError(t, r.ProcessEnvelopeDissemination(ctx, logger, msg, dissemination))
	for opID := spectypes.OperatorID(1); opID <= keySet.Threshold; opID++ {
		require.NoError(t, r.ProcessPreConsensus(ctx, logger, envelopePartialSig(t, keySet, blinded, slot, opID)))
	}

	require.Empty(t, bn.submitted)
	require.False(t, r.builtSelectedEnvelope())
	require.True(t, r.State.Succeeded)
}

// The signing target is the selected blinded envelope's root under DOMAIN_BEACON_BUILDER — equal to the
// full envelope's root, so the reconstructed signature is valid for the full envelope.
func TestEnvelopeProposerRunner_ExpectedPreConsensusRootsAndDomain(t *testing.T) {
	r := &EnvelopeProposerRunner{BaseRunner: &BaseRunner{}}
	_, _, err := r.expectedPreConsensusRootsAndDomain()
	require.ErrorIs(t, err, errNoSelectedEnvelope)

	envelope := sampleEnvelope()
	blinded, err := gloas.Blinded(envelope)
	require.NoError(t, err)
	r.selectedEnvelope = blinded

	roots, domain, err := r.expectedPreConsensusRootsAndDomain()
	require.NoError(t, err)
	require.Equal(t, phase0.DomainType(spectypes.DomainBeaconBuilder), domain)
	require.Len(t, roots, 1)
	got, err := roots[0].HashTreeRoot()
	require.NoError(t, err)
	want, err := envelope.HashTreeRoot()
	require.NoError(t, err)
	require.Equal(t, want, got)
}

// The envelope duty has neither a consensus nor a post-consensus phase.
func TestEnvelopeProposerRunner_NoConsensusPhases(t *testing.T) {
	r := &EnvelopeProposerRunner{BaseRunner: &BaseRunner{}}
	require.Error(t, r.ProcessConsensus(context.Background(), zap.NewNop(), &spectypes.SignedSSVMessage{}))
	require.Error(t, r.ProcessPostConsensus(context.Background(), zap.NewNop(), &spectypes.PartialSignatureMessages{}))
	_, _, err := r.expectedPostConsensusRootsAndDomain(context.Background())
	require.Error(t, err)
}

// mustDomain returns the beacon domain the runner signs the envelope under.
func mustDomain(t *testing.T, r *EnvelopeProposerRunner) phase0.Domain {
	t.Helper()
	domain, err := r.beacon.DomainData(context.Background(), 1, phase0.DomainType(spectypes.DomainBeaconBuilder))
	require.NoError(t, err)
	return domain
}
