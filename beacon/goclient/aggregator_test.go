package goclient

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/attestantio/go-eth2-client/api"
	"github.com/attestantio/go-eth2-client/mock"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/electra"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/jellydator/ttlcache/v3"
	"github.com/prysmaticlabs/go-bitfield"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/networkconfig"
)

// aggregatorClientMock is used both as a MultiClient (single-client path) and, when placed in
// GoClient.clients, as a Client for the parallel-submission path.
type aggregatorClientMock struct {
	mock.Service

	submitAttestationsFn func(context.Context, *api.SubmitAttestationsOpts) error
}

var _ Client = (*aggregatorClientMock)(nil)

func (*aggregatorClientMock) SubmitProposal(context.Context, *api.SubmitProposalOpts) error {
	return nil
}

func (m *aggregatorClientMock) SubmitAttestations(ctx context.Context, opts *api.SubmitAttestationsOpts) error {
	if m.submitAttestationsFn != nil {
		return m.submitAttestationsFn(ctx, opts)
	}
	return nil
}

func (*aggregatorClientMock) NodeClient(context.Context) (*api.Response[string], error) {
	return &api.Response[string]{Data: "mock"}, nil
}

func (*aggregatorClientMock) SubmitBlindedProposal(context.Context, *api.SubmitBlindedProposalOpts) error {
	return nil
}

func TestSubmitAggregateSelectionProof_UsesForkSpecificAggregateAndProof(t *testing.T) {
	t.Parallel()

	slotSig := make([]byte, 96)
	for i := range slotSig {
		slotSig[i] = byte(i + 1)
	}

	validatorIndex := phase0.ValidatorIndex(42)
	committeeIndex := phase0.CommitteeIndex(7)

	testCases := []struct {
		name             string
		version          spec.DataVersion
		epoch            phase0.Epoch
		attDataSlotEpoch *phase0.Epoch
		expectedIndex    phase0.CommitteeIndex
	}{
		{
			name:          "phase0 uses committee index",
			version:       spec.DataVersionPhase0,
			epoch:         0,
			expectedIndex: committeeIndex,
		},
		{
			name:          "altair uses committee index",
			version:       spec.DataVersionAltair,
			epoch:         1,
			expectedIndex: committeeIndex,
		},
		{
			name:          "bellatrix uses committee index",
			version:       spec.DataVersionBellatrix,
			epoch:         2,
			expectedIndex: committeeIndex,
		},
		{
			name:          "capella uses committee index",
			version:       spec.DataVersionCapella,
			epoch:         3,
			expectedIndex: committeeIndex,
		},
		{
			name:          "deneb uses committee index",
			version:       spec.DataVersionDeneb,
			epoch:         4,
			expectedIndex: committeeIndex,
		},
		{
			name:          "electra uses zero index",
			version:       spec.DataVersionElectra,
			epoch:         5,
			expectedIndex: 0,
		},
		{
			name:             "electra duty with pre electra attestation slot uses zero index",
			version:          spec.DataVersionElectra,
			epoch:            5,
			attDataSlotEpoch: epochPtr(4),
			expectedIndex:    0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := aggregatorTestBeaconConfig(time.Now().Add(-1000 * networkconfig.TestNetwork.SlotDuration))
			slot := cfg.FirstSlotAtEpoch(tc.epoch)
			attDataSlot := slot
			if tc.attDataSlotEpoch != nil {
				attDataSlot = cfg.FirstSlotAtEpoch(*tc.attDataSlotEpoch)
			}

			attData := &phase0.AttestationData{
				Slot:  attDataSlot,
				Index: phase0.CommitteeIndex(99),
				Source: &phase0.Checkpoint{
					Epoch: 1,
				},
				Target: &phase0.Checkpoint{
					Epoch: 2,
				},
			}

			expectedAttData := *attData
			expectedAttData.Index = tc.expectedIndex

			expectedRoot, err := expectedAttData.HashTreeRoot()
			require.NoError(t, err)

			versionedAttestation := aggregatorVersionedAttestation(tc.version, &expectedAttData)
			service := &aggregatorClientMock{}
			service.AttestationDataFunc = func(_ context.Context, opts *api.AttestationDataOpts) (*api.Response[*phase0.AttestationData], error) {
				require.Equal(t, slot, opts.Slot)
				require.Zero(t, opts.CommitteeIndex)

				return &api.Response[*phase0.AttestationData]{
					Data: attData,
				}, nil
			}
			service.AggregateAttestationFunc = func(_ context.Context, opts *api.AggregateAttestationOpts) (*api.Response[*spec.VersionedAttestation], error) {
				require.Equal(t, slot, opts.Slot)
				require.Equal(t, committeeIndex, opts.CommitteeIndex)
				require.EqualValues(t, expectedRoot, opts.AttestationDataRoot)

				return &api.Response[*spec.VersionedAttestation]{
					Data: versionedAttestation,
				}, nil
			}

			client := newAggregatorTestClient(&cfg, service)

			gotProof, gotVersion, err := client.SubmitAggregateSelectionProof(
				t.Context(),
				slot,
				committeeIndex,
				128,
				validatorIndex,
				slotSig,
			)
			require.NoError(t, err)
			require.Equal(t, tc.version, gotVersion)

			requireAggregateAndProof(t, tc.version, gotProof, validatorIndex, slotSig, tc.expectedIndex)
		})
	}
}

func TestSubmitAggregateSelectionProof_PrefersAttestedDataRoot(t *testing.T) {
	t.Parallel()

	slotSig := make([]byte, 96)
	validatorIndex := phase0.ValidatorIndex(42)
	committeeIndex := phase0.CommitteeIndex(7)

	testCases := []struct {
		name    string
		version spec.DataVersion
		epoch   phase0.Epoch
	}{
		{name: "electra committee comes from committee bits", version: spec.DataVersionElectra, epoch: 5},
		{name: "fulu committee comes from committee bits", version: spec.DataVersionFulu, epoch: 6},
		{name: "pre-electra committee comes from data index", version: spec.DataVersionPhase0, epoch: 0},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := aggregatorTestBeaconConfig(time.Now().Add(-1000 * networkconfig.TestNetwork.SlotDuration))
			slot := cfg.FirstSlotAtEpoch(tc.epoch)

			// The data the cluster decided and attested with.
			attestedData := &phase0.AttestationData{
				Slot:            slot,
				BeaconBlockRoot: phase0.Root{1, 2, 3},
				Source:          &phase0.Checkpoint{Epoch: 1},
				Target:          &phase0.Checkpoint{Epoch: 2},
			}
			if tc.version < spec.DataVersionElectra {
				attestedData.Index = committeeIndex
			}
			expectedRoot, err := attestedData.HashTreeRoot()
			require.NoError(t, err)

			service := &aggregatorClientMock{}
			service.AttestationDataFunc = func(context.Context, *api.AttestationDataOpts) (*api.Response[*phase0.AttestationData], error) {
				t.Error("attestation data must not be re-derived when the attested root is known")
				return nil, nil
			}
			service.AggregateAttestationFunc = func(_ context.Context, opts *api.AggregateAttestationOpts) (*api.Response[*spec.VersionedAttestation], error) {
				require.Equal(t, slot, opts.Slot)
				require.Equal(t, committeeIndex, opts.CommitteeIndex)
				require.EqualValues(t, expectedRoot, opts.AttestationDataRoot)

				return &api.Response[*spec.VersionedAttestation]{
					Data: aggregatorVersionedAttestation(tc.version, attestedData),
				}, nil
			}

			client := newAggregatorTestClient(&cfg, service)

			require.NoError(t, client.SubmitAttestations(t.Context(), []*spec.VersionedAttestation{
				attestedVersionedAttestation(tc.version, attestedData, committeeIndex),
			}))

			_, gotVersion, err := client.SubmitAggregateSelectionProof(
				t.Context(), slot, committeeIndex, 128, validatorIndex, slotSig,
			)
			require.NoError(t, err)
			require.Equal(t, tc.version, gotVersion)
		})
	}
}

func TestGetAggregateAttestation_PrefersAttestedDataRoot(t *testing.T) {
	t.Parallel()

	// Electra is representative: the boole-specific adaptation fixes both SubmitAggregateSelectionProof
	// (AggregatorRunner) and GetAggregateAttestation (AggregatorCommitteeRunner) via the shared
	// fetchVersionedAggregate injection point. This test exercises that second path.
	cfg := aggregatorTestBeaconConfig(time.Now().Add(-1000 * networkconfig.TestNetwork.SlotDuration))
	committeeIndex := phase0.CommitteeIndex(7)
	epoch := phase0.Epoch(5)
	slot := cfg.FirstSlotAtEpoch(epoch)

	attestedData := &phase0.AttestationData{
		Slot:            slot,
		BeaconBlockRoot: phase0.Root{4, 5, 6},
		Source:          &phase0.Checkpoint{Epoch: 1},
		Target:          &phase0.Checkpoint{Epoch: 2},
	}
	expectedRoot, err := attestedData.HashTreeRoot()
	require.NoError(t, err)

	service := &aggregatorClientMock{}
	service.AttestationDataFunc = func(context.Context, *api.AttestationDataOpts) (*api.Response[*phase0.AttestationData], error) {
		t.Error("attestation data must not be re-derived when the attested root is known")
		return nil, nil
	}
	service.AggregateAttestationFunc = func(_ context.Context, opts *api.AggregateAttestationOpts) (*api.Response[*spec.VersionedAttestation], error) {
		require.Equal(t, slot, opts.Slot)
		require.Equal(t, committeeIndex, opts.CommitteeIndex)
		require.EqualValues(t, expectedRoot, opts.AttestationDataRoot)

		return &api.Response[*spec.VersionedAttestation]{
			Data: aggregatorVersionedAttestation(spec.DataVersionElectra, attestedData),
		}, nil
	}

	client := newAggregatorTestClient(&cfg, service)

	require.NoError(t, client.SubmitAttestations(t.Context(), []*spec.VersionedAttestation{
		attestedVersionedAttestation(spec.DataVersionElectra, attestedData, committeeIndex),
	}))

	_, gotVersion, err := client.GetAggregateAttestation(t.Context(), slot, committeeIndex)
	require.NoError(t, err)
	require.Equal(t, spec.DataVersionElectra, gotVersion)
}

func TestSubmitAggregateSelectionProof_FallbackSurfacesAttestationDataError(t *testing.T) {
	t.Parallel()

	cfg := aggregatorTestBeaconConfig(time.Now().Add(-1000 * networkconfig.TestNetwork.SlotDuration))
	slot := cfg.FirstSlotAtEpoch(0)

	var attestationCalls atomic.Int32
	service := &aggregatorClientMock{}
	service.AttestationDataFunc = func(context.Context, *api.AttestationDataOpts) (*api.Response[*phase0.AttestationData], error) {
		attestationCalls.Add(1)
		return nil, errors.New("attestation data unavailable")
	}

	client := newAggregatorTestClient(&cfg, service)

	// No prior SubmitAttestations, so the attested root is unknown and the flow falls back to
	// re-deriving it via GetAttestationData — whose error must surface to the caller.
	_, _, err := client.SubmitAggregateSelectionProof(t.Context(), slot, 7, 128, 42, make([]byte, 96))
	require.ErrorContains(t, err, "fetch attestation data")
	require.EqualValues(t, 1, attestationCalls.Load())
}

func TestRememberAttestedDataRoots(t *testing.T) {
	t.Parallel()

	newClient := func() *GoClient {
		return &GoClient{
			log:                   zap.NewNop(),
			attestedDataRootCache: ttlcache.New[attestedDataRootKey, phase0.Root](),
		}
	}

	preElectraData := func(slot phase0.Slot, committee phase0.CommitteeIndex, block byte) *phase0.AttestationData {
		return &phase0.AttestationData{
			Slot:            slot,
			Index:           committee, // pre-Electra the committee is part of the data
			BeaconBlockRoot: phase0.Root{block},
			Source:          &phase0.Checkpoint{Epoch: 1},
			Target:          &phase0.Checkpoint{Epoch: 2},
		}
	}

	t.Run("keys remembered roots by committee", func(t *testing.T) {
		t.Parallel()
		gc := newClient()
		slot := phase0.Slot(100)

		// Pre-Electra the committee is encoded in the data, so the two committees produce
		// distinct roots — exactly what the (slot, committee) key must keep apart.
		dataC1 := preElectraData(slot, 1, 0x01)
		dataC2 := preElectraData(slot, 2, 0x02)
		rootC1 := mustHashTreeRoot(t, dataC1)
		rootC2 := mustHashTreeRoot(t, dataC2)
		require.NotEqual(t, rootC1, rootC2)

		gc.rememberAttestedDataRoots(t.Context(), []*spec.VersionedAttestation{
			aggregatorVersionedAttestation(spec.DataVersionPhase0, dataC1),
			aggregatorVersionedAttestation(spec.DataVersionPhase0, dataC2),
		})

		got1, ok1 := gc.attestedDataRoot(slot, 1)
		require.True(t, ok1)
		require.Equal(t, rootC1, got1)

		got2, ok2 := gc.attestedDataRoot(slot, 2)
		require.True(t, ok2)
		require.Equal(t, rootC2, got2)

		// A committee we never attested for, and a different slot, both miss.
		_, ok3 := gc.attestedDataRoot(slot, 3)
		require.False(t, ok3)
		_, okOtherSlot := gc.attestedDataRoot(slot+1, 1)
		require.False(t, okOtherSlot)
	})

	t.Run("remembers electra roots via committee bits", func(t *testing.T) {
		t.Parallel()
		gc := newClient()
		slot := phase0.Slot(200)
		data := &phase0.AttestationData{Slot: slot, Source: &phase0.Checkpoint{Epoch: 1}, Target: &phase0.Checkpoint{Epoch: 2}}
		root := mustHashTreeRoot(t, data)

		gc.rememberAttestedDataRoots(t.Context(), []*spec.VersionedAttestation{
			attestedVersionedAttestation(spec.DataVersionElectra, data, 4),
		})

		got, ok := gc.attestedDataRoot(slot, 4)
		require.True(t, ok)
		require.Equal(t, root, got)
	})

	t.Run("last submission wins for the same key", func(t *testing.T) {
		t.Parallel()
		gc := newClient()
		slot := phase0.Slot(300)
		committee := phase0.CommitteeIndex(5)

		first := preElectraData(slot, committee, 0x01)
		second := preElectraData(slot, committee, 0x02)
		require.NotEqual(t, mustHashTreeRoot(t, first), mustHashTreeRoot(t, second))

		gc.rememberAttestedDataRoots(t.Context(), []*spec.VersionedAttestation{aggregatorVersionedAttestation(spec.DataVersionPhase0, first)})
		gc.rememberAttestedDataRoots(t.Context(), []*spec.VersionedAttestation{aggregatorVersionedAttestation(spec.DataVersionPhase0, second)})

		got, ok := gc.attestedDataRoot(slot, committee)
		require.True(t, ok)
		require.Equal(t, mustHashTreeRoot(t, second), got)
	})

	t.Run("skips malformed attestations and keeps valid ones", func(t *testing.T) {
		t.Parallel()
		gc := newClient()
		slot := phase0.Slot(400)
		valid := preElectraData(slot, 6, 0x01)

		twoBits := bitfield.NewBitvector64()
		twoBits.SetBitAt(1, true)
		twoBits.SetBitAt(2, true)

		gc.rememberAttestedDataRoots(t.Context(), []*spec.VersionedAttestation{
			// Data() fails (nil inner attestation).
			{Version: spec.DataVersionElectra},
			// Committee extraction fails (two committee bits).
			{Version: spec.DataVersionElectra, Electra: &electra.Attestation{Data: valid, CommitteeBits: twoBits}},
			// Valid — must still be remembered despite the malformed entries above.
			aggregatorVersionedAttestation(spec.DataVersionPhase0, valid),
		})

		got, ok := gc.attestedDataRoot(slot, 6)
		require.True(t, ok)
		require.Equal(t, mustHashTreeRoot(t, valid), got)
	})
}

func TestSubmitAttestations_ParallelSubmissionRemembersRoots(t *testing.T) {
	t.Parallel()

	slot := phase0.Slot(500)
	committee := phase0.CommitteeIndex(8)
	data := &phase0.AttestationData{
		Slot:   slot,
		Index:  committee,
		Source: &phase0.Checkpoint{Epoch: 1},
		Target: &phase0.Checkpoint{Epoch: 2},
	}
	root := mustHashTreeRoot(t, data)

	var failingCalls, succeedingCalls atomic.Int32
	failing := &aggregatorClientMock{submitAttestationsFn: func(context.Context, *api.SubmitAttestationsOpts) error {
		failingCalls.Add(1)
		return errors.New("first client failed")
	}}
	succeeding := &aggregatorClientMock{submitAttestationsFn: func(context.Context, *api.SubmitAttestationsOpts) error {
		succeedingCalls.Add(1)
		return nil
	}}

	gc := &GoClient{
		log:                     zap.NewNop(),
		clients:                 []Client{failing, succeeding},
		withParallelSubmissions: true,
		attestedDataRootCache:   ttlcache.New[attestedDataRootKey, phase0.Root](),
	}

	require.NoError(t, gc.SubmitAttestations(t.Context(), []*spec.VersionedAttestation{
		aggregatorVersionedAttestation(spec.DataVersionPhase0, data),
	}))
	require.EqualValues(t, 1, failingCalls.Load())
	require.EqualValues(t, 1, succeedingCalls.Load())

	// One client failed but the other accepted the attestation, so the root is still remembered.
	got, ok := gc.attestedDataRoot(slot, committee)
	require.True(t, ok)
	require.Equal(t, root, got)
}

func TestSubmitAttestations_ParallelSubmissionNoClientsErrsWithoutRemembering(t *testing.T) {
	t.Parallel()

	slot := phase0.Slot(500)
	committee := phase0.CommitteeIndex(8)
	data := &phase0.AttestationData{
		Slot:   slot,
		Index:  committee,
		Source: &phase0.Checkpoint{Epoch: 1},
		Target: &phase0.Checkpoint{Epoch: 2},
	}

	// No clients to submit to. SubmitAttestations must surface an error and must NOT remember the
	// root: nothing was submitted, so the aggregator flow would otherwise request an aggregate for
	// a root no beacon node holds — the exact 404 this cache exists to avoid.
	gc := &GoClient{
		log:                     zap.NewNop(),
		clients:                 nil,
		withParallelSubmissions: true,
		attestedDataRootCache:   ttlcache.New[attestedDataRootKey, phase0.Root](),
	}

	err := gc.SubmitAttestations(t.Context(), []*spec.VersionedAttestation{
		aggregatorVersionedAttestation(spec.DataVersionPhase0, data),
	})
	require.Error(t, err)

	_, ok := gc.attestedDataRoot(slot, committee)
	require.False(t, ok, "root must not be remembered when nothing was submitted")
}

func mustHashTreeRoot(t *testing.T, data *phase0.AttestationData) phase0.Root {
	t.Helper()
	root, err := data.HashTreeRoot()
	require.NoError(t, err)
	return root
}

func TestSubmitAggregateSelectionProof_RespectsContextCancellationWhileWaiting(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := aggregatorTestBeaconConfig(time.Now())

		var attestationCalls atomic.Int32
		var aggregateCalls atomic.Int32
		service := &aggregatorClientMock{}
		service.AttestationDataFunc = func(context.Context, *api.AttestationDataOpts) (*api.Response[*phase0.AttestationData], error) {
			attestationCalls.Add(1)
			return nil, nil
		}
		service.AggregateAttestationFunc = func(context.Context, *api.AggregateAttestationOpts) (*api.Response[*spec.VersionedAttestation], error) {
			aggregateCalls.Add(1)
			return nil, nil
		}

		client := newAggregatorTestClient(&cfg, service)

		ctx, cancel := context.WithCancel(context.Background())
		errCh := make(chan error, 1)

		go func() {
			_, _, err := client.SubmitAggregateSelectionProof(ctx, 0, 1, 128, 42, make([]byte, 96))
			errCh <- err
		}()

		time.Sleep(cfg.IntervalDuration())
		cancel()

		err := <-errCh
		require.ErrorIs(t, err, context.Canceled)
		require.ErrorContains(t, err, "wait for 2/3 of slot")
		require.Zero(t, attestationCalls.Load())
		require.Zero(t, aggregateCalls.Load())
	})
}

func aggregatorTestBeaconConfig(genesisTime time.Time) networkconfig.Beacon {
	cfg := *networkconfig.TestNetwork.Beacon
	cfg.GenesisTime = genesisTime
	return cfg
}

func epochPtr(epoch phase0.Epoch) *phase0.Epoch {
	return &epoch
}

func newAggregatorTestClient(cfg *networkconfig.Beacon, service MultiClient) *GoClient {
	client := &GoClient{
		log:                   zap.NewNop(),
		beaconConfig:          cfg,
		multiClient:           service,
		attestationDataCache:  ttlcache.New[phase0.Slot, *phase0.AttestationData](),
		attestedDataRootCache: ttlcache.New[attestedDataRootKey, phase0.Root](),
		headCache:             ttlcache.New[phase0.Slot, phase0.Root](),
	}
	client.fetchAttestationDataFunc = client.fetchAttestationData
	return client
}

func aggregatorVersionedAttestation(version spec.DataVersion, data *phase0.AttestationData) *spec.VersionedAttestation {
	switch version {
	case spec.DataVersionPhase0:
		return &spec.VersionedAttestation{Version: version, Phase0: &phase0.Attestation{Data: data}}
	case spec.DataVersionAltair:
		return &spec.VersionedAttestation{Version: version, Altair: &phase0.Attestation{Data: data}}
	case spec.DataVersionBellatrix:
		return &spec.VersionedAttestation{Version: version, Bellatrix: &phase0.Attestation{Data: data}}
	case spec.DataVersionCapella:
		return &spec.VersionedAttestation{Version: version, Capella: &phase0.Attestation{Data: data}}
	case spec.DataVersionDeneb:
		return &spec.VersionedAttestation{Version: version, Deneb: &phase0.Attestation{Data: data}}
	case spec.DataVersionElectra:
		return &spec.VersionedAttestation{Version: version, Electra: &electra.Attestation{Data: data}}
	case spec.DataVersionFulu:
		return &spec.VersionedAttestation{Version: version, Fulu: &electra.Attestation{Data: data}}
	default:
		panic("unsupported version")
	}
}

// attestedVersionedAttestation builds an attestation the way the committee runner submits
// it: pre-Electra the committee is the data's Index, Electra+ it's the set committee bit.
func attestedVersionedAttestation(version spec.DataVersion, data *phase0.AttestationData, committee phase0.CommitteeIndex) *spec.VersionedAttestation {
	att := aggregatorVersionedAttestation(version, data)
	if version < spec.DataVersionElectra {
		return att
	}

	committeeBits := bitfield.NewBitvector64()
	committeeBits.SetBitAt(uint64(committee), true)
	switch version {
	case spec.DataVersionElectra:
		att.Electra.CommitteeBits = committeeBits
	case spec.DataVersionFulu:
		att.Fulu.CommitteeBits = committeeBits
	default:
		panic("unsupported electra+ version")
	}
	return att
}

func requireAggregateAndProof(
	t *testing.T,
	version spec.DataVersion,
	gotProof any,
	validatorIndex phase0.ValidatorIndex,
	slotSig []byte,
	expectedIndex phase0.CommitteeIndex,
) {
	t.Helper()

	switch version {
	case spec.DataVersionElectra, spec.DataVersionFulu:
		proof, ok := gotProof.(*electra.AggregateAndProof)
		require.Truef(t, ok, "expected *electra.AggregateAndProof, got %T", gotProof)
		require.Equal(t, validatorIndex, proof.AggregatorIndex)
		require.Equal(t, slotSig, proof.SelectionProof[:])
		require.Equal(t, expectedIndex, proof.Aggregate.Data.Index)
	default:
		proof, ok := gotProof.(*phase0.AggregateAndProof)
		require.Truef(t, ok, "expected *phase0.AggregateAndProof, got %T", gotProof)
		require.Equal(t, validatorIndex, proof.AggregatorIndex)
		require.Equal(t, slotSig, proof.SelectionProof[:])
		require.Equal(t, expectedIndex, proof.Aggregate.Data.Index)
	}
}
