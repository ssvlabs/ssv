package goclient

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
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

	submitAttestationsFn          func(context.Context, *api.SubmitAttestationsOpts) error
	submitAggregateAttestationsFn func(context.Context, *api.SubmitAggregateAttestationsOpts) error
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

func (m *aggregatorClientMock) SubmitAggregateAttestations(ctx context.Context, opts *api.SubmitAggregateAttestationsOpts) error {
	if m.submitAggregateAttestationsFn != nil {
		return m.submitAggregateAttestationsFn(ctx, opts)
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

		time.Sleep(cfg.IntervalDuration(0))
		cancel()

		err := <-errCh
		require.ErrorIs(t, err, context.Canceled)
		require.ErrorContains(t, err, "wait for aggregation deadline")
		require.Zero(t, attestationCalls.Load())
		require.Zero(t, aggregateCalls.Load())
	})
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

func TestAttestationCommitteeIndex(t *testing.T) {
	t.Parallel()

	data := &phase0.AttestationData{
		Slot:   100,
		Index:  3,
		Source: &phase0.Checkpoint{Epoch: 1},
		Target: &phase0.Checkpoint{Epoch: 2},
	}

	t.Run("pre-electra reads the data index", func(t *testing.T) {
		t.Parallel()
		att := aggregatorVersionedAttestation(spec.DataVersionPhase0, data)
		got, err := attestationCommitteeIndex(att, data)
		require.NoError(t, err)
		require.Equal(t, phase0.CommitteeIndex(3), got)
	})

	t.Run("electra reads the single committee bit", func(t *testing.T) {
		t.Parallel()
		att := attestedVersionedAttestation(spec.DataVersionElectra, data, 7)
		got, err := attestationCommitteeIndex(att, data)
		require.NoError(t, err)
		require.Equal(t, phase0.CommitteeIndex(7), got)
	})

	t.Run("fulu reads the single committee bit", func(t *testing.T) {
		t.Parallel()
		att := attestedVersionedAttestation(spec.DataVersionFulu, data, 9)
		got, err := attestationCommitteeIndex(att, data)
		require.NoError(t, err)
		require.Equal(t, phase0.CommitteeIndex(9), got)
	})

	t.Run("electra with no committee bit set errors", func(t *testing.T) {
		t.Parallel()
		att := &spec.VersionedAttestation{
			Version: spec.DataVersionElectra,
			Electra: &electra.Attestation{Data: data, CommitteeBits: bitfield.NewBitvector64()},
		}
		_, err := attestationCommitteeIndex(att, data)
		require.ErrorContains(t, err, "exactly one committee bit")
	})

	t.Run("electra with multiple committee bits errors", func(t *testing.T) {
		t.Parallel()
		bits := bitfield.NewBitvector64()
		bits.SetBitAt(1, true)
		bits.SetBitAt(2, true)
		att := &spec.VersionedAttestation{
			Version: spec.DataVersionElectra,
			Electra: &electra.Attestation{Data: data, CommitteeBits: bits},
		}
		_, err := attestationCommitteeIndex(att, data)
		require.ErrorContains(t, err, "exactly one committee bit")
	})

	t.Run("electra with no inner attestation errors", func(t *testing.T) {
		t.Parallel()
		att := &spec.VersionedAttestation{Version: spec.DataVersionElectra}
		_, err := attestationCommitteeIndex(att, data)
		require.Error(t, err)
	})
}

func TestIsAggregator(t *testing.T) {
	t.Parallel()

	slotSigA := bytes.Repeat([]byte{0xAA}, 96)
	slotSigB := bytes.Repeat([]byte{0xBB}, 96)

	// Mirrors the documented selection-proof modulo check so we can assert the production
	// code wires committeeLength/TargetAggregatorsPerCommittee/slotSig into it correctly,
	// rather than e.g. accidentally using TargetAggregatorsPerSyncSubcommittee.
	computeExpected := func(committeeLength, target uint64, slotSig []byte) bool {
		modulo := committeeLength / target
		if modulo == 0 {
			modulo = 1
		}
		h := sha256.Sum256(slotSig)
		x := binary.LittleEndian.Uint64(h[:8])
		return x%modulo == 0
	}

	testCases := []struct {
		name            string
		committeeLength uint64
		target          uint64
		slotSig         []byte
	}{
		{name: "small committee forces modulo to one", committeeLength: 2, target: 16, slotSig: slotSigA},
		{name: "zero committee length forces modulo to one", committeeLength: 0, target: 16, slotSig: slotSigA},
		{name: "large committee uses computed modulo (sig A)", committeeLength: 128, target: 16, slotSig: slotSigA},
		{name: "large committee uses computed modulo (sig B)", committeeLength: 128, target: 16, slotSig: slotSigB},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := aggregatorTestBeaconConfig(time.Now())
			cfg.TargetAggregatorsPerCommittee = tc.target
			// Sentinel: distinct from TargetAggregatorsPerCommittee so a bug reading the wrong
			// field would flip the expected/actual comparison below.
			cfg.TargetAggregatorsPerSyncSubcommittee = tc.target + 12345

			client := &GoClient{beaconConfig: &cfg}

			got := client.IsAggregator(t.Context(), 0, 0, tc.committeeLength, tc.slotSig)
			want := computeExpected(tc.committeeLength, tc.target, tc.slotSig)
			require.Equal(t, want, got)
		})
	}

	t.Run("modulo forced to one always selects aggregator regardless of signature", func(t *testing.T) {
		t.Parallel()

		cfg := aggregatorTestBeaconConfig(time.Now())
		cfg.TargetAggregatorsPerCommittee = 16

		client := &GoClient{beaconConfig: &cfg}

		require.True(t, client.IsAggregator(t.Context(), 0, 0, 2, slotSigA))
		require.True(t, client.IsAggregator(t.Context(), 0, 0, 0, slotSigB))
	})

	t.Run("deterministic for identical inputs", func(t *testing.T) {
		t.Parallel()

		cfg := aggregatorTestBeaconConfig(time.Now())
		cfg.TargetAggregatorsPerCommittee = 16

		client := &GoClient{beaconConfig: &cfg}

		first := client.IsAggregator(t.Context(), 5, 3, 128, slotSigA)
		second := client.IsAggregator(t.Context(), 5, 3, 128, slotSigA)
		require.Equal(t, first, second)
	})
}

func TestGetAggregateAttestation(t *testing.T) {
	t.Parallel()

	committeeIndex := phase0.CommitteeIndex(7)

	testCases := []struct {
		name    string
		version spec.DataVersion
	}{
		{name: "phase0", version: spec.DataVersionPhase0},
		{name: "altair", version: spec.DataVersionAltair},
		{name: "bellatrix", version: spec.DataVersionBellatrix},
		{name: "capella", version: spec.DataVersionCapella},
		{name: "deneb", version: spec.DataVersionDeneb},
		{name: "electra", version: spec.DataVersionElectra},
		{name: "fulu", version: spec.DataVersionFulu},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := aggregatorTestBeaconConfig(time.Now().Add(-1000 * networkconfig.TestNetwork.SlotDuration))
			slot := cfg.FirstSlotAtEpoch(0)

			attestedData := &phase0.AttestationData{
				Slot:            slot,
				Index:           committeeIndex,
				BeaconBlockRoot: phase0.Root{4, 5, 6},
				Source:          &phase0.Checkpoint{Epoch: 1},
				Target:          &phase0.Checkpoint{Epoch: 2},
			}
			if tc.version >= spec.DataVersionElectra {
				attestedData.Index = 0
			}
			expectedRoot, err := attestedData.HashTreeRoot()
			require.NoError(t, err)

			service := &aggregatorClientMock{}
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

			got, gotVersion, err := client.GetAggregateAttestation(t.Context(), slot, committeeIndex)
			require.NoError(t, err)
			require.Equal(t, tc.version, gotVersion)
			require.NotNil(t, got)
		})
	}

	t.Run("surfaces fetch error", func(t *testing.T) {
		t.Parallel()

		cfg := aggregatorTestBeaconConfig(time.Now().Add(-1000 * networkconfig.TestNetwork.SlotDuration))
		slot := cfg.FirstSlotAtEpoch(0)

		service := &aggregatorClientMock{}
		service.AttestationDataFunc = func(context.Context, *api.AttestationDataOpts) (*api.Response[*phase0.AttestationData], error) {
			return nil, errors.New("attestation data unavailable")
		}

		client := newAggregatorTestClient(&cfg, service)

		// No prior SubmitAttestations call, so the attested root is unknown and the fallback
		// re-derivation path's error must surface.
		_, _, err := client.GetAggregateAttestation(t.Context(), slot, 7)
		require.ErrorContains(t, err, "fetch attestation data")
	})

	t.Run("surfaces nil inner attestation error from unmarshal", func(t *testing.T) {
		t.Parallel()

		cfg := aggregatorTestBeaconConfig(time.Now().Add(-1000 * networkconfig.TestNetwork.SlotDuration))
		slot := cfg.FirstSlotAtEpoch(0)
		committeeIndex := phase0.CommitteeIndex(3)

		data := &phase0.AttestationData{
			Slot:   slot,
			Index:  committeeIndex,
			Source: &phase0.Checkpoint{Epoch: 1},
			Target: &phase0.Checkpoint{Epoch: 2},
		}
		root, err := data.HashTreeRoot()
		require.NoError(t, err)

		service := &aggregatorClientMock{}
		service.AggregateAttestationFunc = func(context.Context, *api.AggregateAttestationOpts) (*api.Response[*spec.VersionedAttestation], error) {
			// Version is set but the inner attestation payload is nil.
			return &api.Response[*spec.VersionedAttestation]{
				Data: &spec.VersionedAttestation{Version: spec.DataVersionPhase0},
			}, nil
		}

		client := newAggregatorTestClient(&cfg, service)
		client.rememberAttestedDataRoots([]*spec.VersionedAttestation{
			aggregatorVersionedAttestation(spec.DataVersionPhase0, data),
		})
		require.NotZero(t, root)

		_, _, err = client.GetAggregateAttestation(t.Context(), slot, committeeIndex)
		require.ErrorContains(t, err, "data is nil")
	})
}

func TestSubmitSignedAggregateSelectionProof(t *testing.T) {
	t.Parallel()

	t.Run("broadcasts the signed aggregate and proof", func(t *testing.T) {
		t.Parallel()

		msg := &spec.VersionedSignedAggregateAndProof{Version: spec.DataVersionPhase0}

		var gotOpts *api.SubmitAggregateAttestationsOpts
		service := &aggregatorClientMock{
			submitAggregateAttestationsFn: func(_ context.Context, opts *api.SubmitAggregateAttestationsOpts) error {
				gotOpts = opts
				return nil
			},
		}

		client := &GoClient{log: zap.NewNop(), multiClient: service}

		require.NoError(t, client.SubmitSignedAggregateSelectionProof(t.Context(), msg))
		require.NotNil(t, gotOpts)
		require.Len(t, gotOpts.SignedAggregateAndProofs, 1)
		require.Same(t, msg, gotOpts.SignedAggregateAndProofs[0])
	})

	t.Run("wraps submission error", func(t *testing.T) {
		t.Parallel()

		msg := &spec.VersionedSignedAggregateAndProof{Version: spec.DataVersionPhase0}

		service := &aggregatorClientMock{
			submitAggregateAttestationsFn: func(context.Context, *api.SubmitAggregateAttestationsOpts) error {
				return errors.New("node rejected the aggregate")
			},
		}

		client := &GoClient{log: zap.NewNop(), multiClient: service}

		err := client.SubmitSignedAggregateSelectionProof(t.Context(), msg)
		require.ErrorContains(t, err, "submit aggregate attestations")
		require.ErrorContains(t, err, "node rejected the aggregate")
	})
}

func TestVersionedAggregateToSSZ(t *testing.T) {
	t.Parallel()

	versions := []spec.DataVersion{
		spec.DataVersionPhase0,
		spec.DataVersionAltair,
		spec.DataVersionBellatrix,
		spec.DataVersionCapella,
		spec.DataVersionDeneb,
		spec.DataVersionElectra,
		spec.DataVersionFulu,
	}

	for _, version := range versions {
		t.Run(version.String()+" nil inner attestation errors", func(t *testing.T) {
			t.Parallel()

			_, _, err := versionedAggregateToSSZ(&spec.VersionedAttestation{Version: version})
			require.ErrorContains(t, err, "data is nil")
		})
	}

	t.Run("unknown version errors", func(t *testing.T) {
		t.Parallel()

		_, _, err := versionedAggregateToSSZ(&spec.VersionedAttestation{Version: spec.DataVersion(99)})
		require.ErrorContains(t, err, "unknown data version")
	})
}

func TestVersionedToAggregateAndProofNilAndUnknownVersion(t *testing.T) {
	t.Parallel()

	versions := []spec.DataVersion{
		spec.DataVersionPhase0,
		spec.DataVersionAltair,
		spec.DataVersionBellatrix,
		spec.DataVersionCapella,
		spec.DataVersionDeneb,
		spec.DataVersionElectra,
		spec.DataVersionFulu,
	}

	for _, version := range versions {
		t.Run(version.String()+" nil inner attestation errors", func(t *testing.T) {
			t.Parallel()

			_, _, err := versionedToAggregateAndProof(&spec.VersionedAttestation{Version: version}, 1, phase0.BLSSignature{})
			require.ErrorContains(t, err, "data is nil")
		})
	}

	t.Run("unknown version errors", func(t *testing.T) {
		t.Parallel()

		_, _, err := versionedToAggregateAndProof(&spec.VersionedAttestation{Version: spec.DataVersion(99)}, 1, phase0.BLSSignature{})
		require.ErrorContains(t, err, "unknown data version")
	})
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

		gc.rememberAttestedDataRoots([]*spec.VersionedAttestation{
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

		gc.rememberAttestedDataRoots([]*spec.VersionedAttestation{
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

		gc.rememberAttestedDataRoots([]*spec.VersionedAttestation{aggregatorVersionedAttestation(spec.DataVersionPhase0, first)})
		gc.rememberAttestedDataRoots([]*spec.VersionedAttestation{aggregatorVersionedAttestation(spec.DataVersionPhase0, second)})

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

		gc.rememberAttestedDataRoots([]*spec.VersionedAttestation{
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

// Gloas keeps the BN-supplied payload-status index in the aggregation root (SIP #94 §2); Electra+ zeroes it.
func TestComputeAttestationDataRoot_GloasKeepsBNIndex(t *testing.T) {
	t.Parallel()

	const gloasEpoch = 6
	cfg := *networkconfig.TestNetworkWithGloas(gloasEpoch).Beacon
	slot := cfg.FirstSlotAtEpoch(gloasEpoch)

	attData := &phase0.AttestationData{
		Slot:   slot,
		Index:  1, // FULL — the BN payload-status index, which must be preserved
		Source: &phase0.Checkpoint{Epoch: 1},
		Target: &phase0.Checkpoint{Epoch: 2},
	}
	expectedRoot, err := attData.HashTreeRoot()
	require.NoError(t, err)

	client := newAggregatorTestClient(&cfg, &aggregatorClientMock{})
	// On Gloas, GetAttestationData uses a hand-rolled fetch (not go-eth2-client, whose post-Electra
	// validation would reject the payload-status Index=1) — inject the BN's data via the fetch hook so this
	// test exercises computeAttestationDataRoot's index-keeping independent of the transport.
	client.fetchAttestationDataFunc = func(_ context.Context, gotSlot phase0.Slot) (*phase0.AttestationData, error) {
		require.Equal(t, slot, gotSlot)
		return attData, nil
	}
	root, err := client.computeAttestationDataRoot(t.Context(), slot, 7)
	require.NoError(t, err)
	require.Equal(t, expectedRoot, root)
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
