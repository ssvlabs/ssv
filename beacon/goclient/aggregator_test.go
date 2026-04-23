package goclient

import (
	"context"
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
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/networkconfig"
)

type aggregatorClientMock struct {
	mock.Service
}

func (*aggregatorClientMock) SubmitProposal(context.Context, *api.SubmitProposalOpts) error {
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
		log:                  zap.NewNop(),
		beaconConfig:         cfg,
		multiClient:          service,
		attestationDataCache: ttlcache.New[phase0.Slot, *phase0.AttestationData](),
		headCache:            ttlcache.New[phase0.Slot, phase0.Root](),
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
