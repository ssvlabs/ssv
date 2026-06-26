package runner

import (
	"context"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/electra"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/ssvlabs/ssv/protocol/v2/ssv"
	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
)

// envelopeConsensusDataSSZ builds a decided EnvelopeConsensusData carrying a self-build blinded envelope,
// returning the blinded value (for root comparison) and the encoded consensus data.
func envelopeConsensusDataSSZ(t *testing.T, slot phase0.Slot, blockRoot phase0.Root) (*gloas.BlindedExecutionPayloadEnvelope, []byte) {
	t.Helper()
	blinded := &gloas.BlindedExecutionPayloadEnvelope{
		PayloadRoot:           phase0.Root{0x09},
		ExecutionRequests:     &electra.ExecutionRequests{},
		BuilderIndex:          gloas.BuilderIndexSelfBuild,
		BeaconBlockRoot:       blockRoot,
		ParentBeaconBlockRoot: phase0.Root{0x08},
	}
	dataSSZ, err := blinded.Encode()
	require.NoError(t, err)
	cd := &gloas.EnvelopeConsensusData{
		Duty:    spectypes.ValidatorDuty{Type: spectypes.BNRoleEnvelopeBuilder, Slot: slot, ValidatorIndex: 3},
		DataSSZ: dataSSZ,
	}
	encoded, err := cd.Encode()
	require.NoError(t, err)
	return blinded, encoded
}

func TestNewEnvelopeBuilderRunner_RequiresOneShare(t *testing.T) {
	_, err := NewEnvelopeBuilderRunner(EnvelopeBuilderRunnerOptions{})
	require.Error(t, err)
}

// The post-consensus signing target is the decided blinded envelope's root under DOMAIN_BEACON_BUILDER —
// equal to the full envelope's root, so the partial signature is valid for the full envelope.
func TestEnvelopeBuilderRunner_ExpectedPostConsensusRootsAndDomain(t *testing.T) {
	blinded, encoded := envelopeConsensusDataSSZ(t, 5, phase0.Root{0xaa})
	r := &EnvelopeBuilderRunner{BaseRunner: &BaseRunner{State: &State{DecidedValue: encoded}}}

	roots, domain, err := r.expectedPostConsensusRootsAndDomain(context.Background())
	require.NoError(t, err)
	require.Equal(t, phase0.DomainType(spectypes.DomainBeaconBuilder), domain)
	require.Len(t, roots, 1)

	got, err := roots[0].HashTreeRoot()
	require.NoError(t, err)
	want, err := blinded.HashTreeRoot()
	require.NoError(t, err)
	require.Equal(t, want, got)
}

// The envelope duty has no pre-consensus phase; both entry points reject.
func TestEnvelopeBuilderRunner_NoPreConsensus(t *testing.T) {
	r := &EnvelopeBuilderRunner{BaseRunner: &BaseRunner{}}
	require.Error(t, r.ProcessPreConsensus(context.Background(), zap.NewNop(), &spectypes.PartialSignatureMessages{}))
	_, _, err := r.expectedPreConsensusRootsAndDomain()
	require.Error(t, err)
}

// executeDuty guards on the proposer having recorded the §4 block root for the slot before producing.
func TestEnvelopeBuilderRunner_ExecuteDutyRequiresDecidedRoot(t *testing.T) {
	r := &EnvelopeBuilderRunner{
		BaseRunner: &BaseRunner{
			RunnerRoleType: spectypes.RoleEnvelopeBuilder,
			Share: map[phase0.ValidatorIndex]*spectypes.Share{
				3: {ValidatorIndex: 3, ValidatorPubKey: spectypes.ValidatorPK{0x42}},
			},
		},
		measurements:       newMeasurementsStore(),
		proposedBlockRoots: ssv.NewProposedBlockRoots(),
	}
	duty := &spectypes.ValidatorDuty{Type: spectypes.BNRoleEnvelopeBuilder, Slot: 5, ValidatorIndex: 3}

	require.ErrorContains(t, r.executeDuty(context.Background(), zap.NewNop(), duty), "no decided block root")
}

type envelopeTestBeacon struct {
	beacon.BeaconNode
	envelope  *gloas.ExecutionPayloadEnvelope
	submitted []*gloas.SignedExecutionPayloadEnvelope
}

func (b *envelopeTestBeacon) GetExecutionPayloadEnvelope(_ context.Context, _ phase0.Slot, _ phase0.Root) (*gloas.ExecutionPayloadEnvelope, error) {
	return b.envelope, nil
}

func (b *envelopeTestBeacon) SubmitExecutionPayloadEnvelope(_ context.Context, signed *gloas.SignedExecutionPayloadEnvelope) error {
	b.submitted = append(b.submitted, signed)
	return nil
}

func sampleEnvelope() *gloas.ExecutionPayloadEnvelope {
	return &gloas.ExecutionPayloadEnvelope{
		Payload:               &gloas.ExecutionPayload{BlockNumber: 42},
		ExecutionRequests:     &electra.ExecutionRequests{},
		BuilderIndex:          gloas.BuilderIndexSelfBuild,
		BeaconBlockRoot:       phase0.Root{0xaa},
		ParentBeaconBlockRoot: phase0.Root{0xbb},
	}
}

// produceBlindedEnvelope fetches the envelope, caches the full one, and wraps its blinded form (PayloadRoot
// = HTR(payload), with the §4 root and builder index preserved) as the QBFT value.
func TestEnvelopeBuilderRunner_ProduceBlindedEnvelope(t *testing.T) {
	envelope := sampleEnvelope()
	r := &EnvelopeBuilderRunner{BaseRunner: &BaseRunner{}, beacon: &envelopeTestBeacon{envelope: envelope}}
	duty := &spectypes.ValidatorDuty{Type: spectypes.BNRoleEnvelopeBuilder, Slot: 5, ValidatorIndex: 3}

	cd, err := r.produceBlindedEnvelope(context.Background(), duty, phase0.Root{0xaa})
	require.NoError(t, err)
	require.Same(t, envelope, r.cachedEnvelope) // cached for the later content-matched publish

	blinded := &gloas.BlindedExecutionPayloadEnvelope{}
	require.NoError(t, blinded.Decode(cd.DataSSZ))
	wantRoot, err := envelope.Payload.HashTreeRoot()
	require.NoError(t, err)
	require.Equal(t, phase0.Root(wantRoot), blinded.PayloadRoot)
	require.Equal(t, envelope.BeaconBlockRoot, blinded.BeaconBlockRoot)
	require.Equal(t, gloas.BuilderIndexSelfBuild, blinded.BuilderIndex)
}

// builtDecidedEnvelope is the content match: only the operator whose cached envelope blinds to the decided
// value holds the full bytes and publishes.
func TestEnvelopeBuilderRunner_BuiltDecidedEnvelope(t *testing.T) {
	envelope := sampleEnvelope()
	blinded, err := envelope.Blinded()
	require.NoError(t, err)
	decided, err := blinded.Encode()
	require.NoError(t, err)

	r := &EnvelopeBuilderRunner{cachedEnvelope: envelope}
	require.True(t, r.builtDecidedEnvelope(decided))       // our cached envelope blinds to the decided value
	require.False(t, r.builtDecidedEnvelope([]byte{0x01})) // a different decided value

	r.cachedEnvelope = nil
	require.False(t, r.builtDecidedEnvelope(decided)) // nothing cached (e.g. after a round change)
}
