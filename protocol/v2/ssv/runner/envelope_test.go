package runner

import (
	"context"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/electra"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

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

// executeDuty requires the proposer to have recorded the §4 block root for the slot, then stubs the
// (heavy-payload-dependent) production.
func TestEnvelopeBuilderRunner_ExecuteDuty(t *testing.T) {
	store := ssv.NewProposedBlockRoots()
	r := &EnvelopeBuilderRunner{
		BaseRunner: &BaseRunner{
			RunnerRoleType: spectypes.RoleEnvelopeBuilder,
			Share: map[phase0.ValidatorIndex]*spectypes.Share{
				3: {ValidatorIndex: 3, ValidatorPubKey: spectypes.ValidatorPK{0x42}},
			},
		},
		measurements:       newMeasurementsStore(),
		proposedBlockRoots: store,
	}
	duty := &spectypes.ValidatorDuty{Type: spectypes.BNRoleEnvelopeBuilder, Slot: 5, ValidatorIndex: 3}

	// No recorded §4 root → guarded.
	require.ErrorContains(t, r.executeDuty(context.Background(), zap.NewNop(), duty), "no decided block root")

	// With the root present, production is reached and stubs out pending the full Gloas payload.
	store.Set(5, phase0.Root{0xaa})
	require.ErrorContains(t, r.executeDuty(context.Background(), zap.NewNop(), duty), "not yet implemented")
}

func TestEnvelopeBuilderRunner_SubmitEnvelopeStub(t *testing.T) {
	r := &EnvelopeBuilderRunner{BaseRunner: &BaseRunner{}}
	err := r.submitEnvelope(context.Background(), zap.NewNop(), &gloas.EnvelopeConsensusData{}, phase0.BLSSignature{})
	require.ErrorContains(t, err, "not yet implemented")
}
