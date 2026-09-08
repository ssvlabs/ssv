package goclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/jellydator/ttlcache/v3"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
)

// GoClient must satisfy the PTC beacon-node surface.
var _ beacon.PTCCalls = (*GoClient)(nil)

func TestRequestPTCDuties(t *testing.T) {
	duty := &gloas.PTCDuty{PubKey: phase0.BLSPubKey{0x11, 0x22}, ValidatorIndex: 7, Slot: 9}
	dutyJSON, err := json.Marshal(duty)
	require.NoError(t, err)
	dependentRoot := phase0.Root{0x01, 0x02}

	var gotMethod, gotPath string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotBody, _ = io.ReadAll(r.Body)
		_, _ = fmt.Fprintf(w, `{"dependent_root":"%#x","execution_optimistic":false,"data":[%s]}`, dependentRoot, dutyJSON)
	}))
	defer srv.Close()

	duties, err := requestPTCDuties(context.Background(), srv.Client(), srv.URL, 3, []phase0.ValidatorIndex{7, 8})
	require.NoError(t, err)
	require.Equal(t, http.MethodPost, gotMethod)
	require.Equal(t, "/eth/v1/validator/duties/ptc/3", gotPath)
	require.JSONEq(t, `["7","8"]`, string(gotBody))
	require.Equal(t, &gloas.PTCDuties{DependentRoot: dependentRoot, Duties: []*gloas.PTCDuty{duty}}, duties)
}

func TestRequestPayloadAttestationData(t *testing.T) {
	data := &gloas.PayloadAttestationData{BeaconBlockRoot: phase0.Root{0xaa}, Slot: 9, PayloadPresent: true}
	dataJSON, err := json.Marshal(data)
	require.NoError(t, err)

	var gotMethod, gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotQuery = r.Method, r.URL.Path, r.URL.RawQuery
		_, _ = fmt.Fprintf(w, `{"version":"gloas","data":%s}`, dataJSON)
	}))
	defer srv.Close()

	got, err := requestPayloadAttestationData(context.Background(), srv.Client(), srv.URL, 9)
	require.NoError(t, err)
	require.Equal(t, http.MethodGet, gotMethod)
	require.Equal(t, "/eth/v1/validator/payload_attestation_data", gotPath)
	require.Equal(t, "slot=9", gotQuery)
	require.Equal(t, data, got)
}

// A 204 No Content is the beacon-APIs "no block seen" signal: requestPayloadAttestationData surfaces
// it as (nil, nil), not an error, so the PTC member abstains.
func TestRequestPayloadAttestationData_NoContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	got, err := requestPayloadAttestationData(context.Background(), srv.Client(), srv.URL, 9)
	require.NoError(t, err)
	require.Nil(t, got)
}

func TestSubmitPayloadAttestationMessages(t *testing.T) {
	msgs := []*gloas.PayloadAttestationMessage{{
		ValidatorIndex: 7,
		Data:           &gloas.PayloadAttestationData{BeaconBlockRoot: phase0.Root{0xaa}, Slot: 9, PayloadPresent: true},
		Signature:      phase0.BLSSignature{0xbb},
	}}

	var gotMethod, gotPath, gotVersion string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotVersion = r.Header.Get("Eth-Consensus-Version")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	require.NoError(t, submitPayloadAttestationMessages(context.Background(), srv.Client(), srv.URL, msgs))
	require.Equal(t, http.MethodPost, gotMethod)
	require.Equal(t, "/eth/v1/beacon/pool/payload_attestations", gotPath)
	require.Equal(t, consensusVersionGloas, gotVersion)
	want, err := json.Marshal(msgs)
	require.NoError(t, err)
	require.JSONEq(t, string(want), string(gotBody))
}

func TestPTCDo_ErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"code":503,"message":"beacon node is syncing"}`, http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	_, err := requestPayloadAttestationData(context.Background(), srv.Client(), srv.URL, 5)
	require.Error(t, err)
	require.Contains(t, err.Error(), "503")
}

// newPayloadAttestationTestClient wires a GoClient whose payload-attestation fetch is the given fake, with
// the real client's coalescing and caching around it. The returned stop ends the cache's expiry loop.
func newPayloadAttestationTestClient(fetch func(context.Context, phase0.Slot) (*gloas.PayloadAttestationData, error)) (*GoClient, func()) {
	cache := ttlcache.New(ttlcache.WithTTL[phase0.Slot, *gloas.PayloadAttestationData](time.Minute))
	go cache.Start()
	return &GoClient{
		log:                             zap.NewNop(),
		payloadAttestationDataCache:     cache,
		fetchPayloadAttestationDataFunc: fetch,
	}, cache.Stop
}

// Every PTC member of a slot fetches the slot-level payload-attestation data at the same cutoff; the calls
// in flight together are served by one request, and all of them get its result (issue #3031).
func TestPayloadAttestationData_CoalescesConcurrentCalls(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := &gloas.PayloadAttestationData{BeaconBlockRoot: phase0.Root{0xaa}, Slot: 9, PayloadPresent: true}
		var fetches atomic.Int32
		release := make(chan struct{})
		gc, stop := newPayloadAttestationTestClient(func(context.Context, phase0.Slot) (*gloas.PayloadAttestationData, error) {
			fetches.Add(1)
			<-release
			return data, nil
		})
		defer stop()

		type result struct {
			data *gloas.PayloadAttestationData
			err  error
		}
		const members = 8
		results := make(chan result, members)
		for range members {
			go func() {
				got, err := gc.PayloadAttestationData(context.Background(), 9)
				results <- result{got, err}
			}()
		}
		synctest.Wait() // every member is now either the in-flight leader or waiting on it
		close(release)

		for range members {
			r := <-results
			require.NoError(t, r.err)
			require.Same(t, data, r.data)
		}
		require.Equal(t, int32(1), fetches.Load())
	})
}

// A slot's data is fetched once and served from the cache to later callers; another slot is a new fetch.
func TestPayloadAttestationData_CachesPerSlot(t *testing.T) {
	fetches := 0
	gc, stop := newPayloadAttestationTestClient(func(_ context.Context, slot phase0.Slot) (*gloas.PayloadAttestationData, error) {
		fetches++
		return &gloas.PayloadAttestationData{BeaconBlockRoot: phase0.Root{byte(slot)}, Slot: slot, PayloadPresent: true}, nil
	})
	defer stop()

	first, err := gc.PayloadAttestationData(context.Background(), 9)
	require.NoError(t, err)
	second, err := gc.PayloadAttestationData(context.Background(), 9)
	require.NoError(t, err)
	require.Same(t, first, second)
	require.Equal(t, 1, fetches)

	_, err = gc.PayloadAttestationData(context.Background(), 10)
	require.NoError(t, err)
	require.Equal(t, 2, fetches)
}

// Neither the abstain signal (nil: a block may still arrive) nor a failure is cached: the next caller asks
// the beacon node again.
func TestPayloadAttestationData_DoesNotCacheAbstainOrError(t *testing.T) {
	data := &gloas.PayloadAttestationData{BeaconBlockRoot: phase0.Root{0xaa}, Slot: 9, PayloadPresent: true}
	answers := []func() (*gloas.PayloadAttestationData, error){
		func() (*gloas.PayloadAttestationData, error) { return nil, nil },
		func() (*gloas.PayloadAttestationData, error) { return nil, errors.New("beacon node unavailable") },
		func() (*gloas.PayloadAttestationData, error) { return data, nil },
	}
	fetches := 0
	gc, stop := newPayloadAttestationTestClient(func(context.Context, phase0.Slot) (*gloas.PayloadAttestationData, error) {
		answer := answers[fetches]
		fetches++
		return answer()
	})
	defer stop()

	got, err := gc.PayloadAttestationData(context.Background(), 9)
	require.NoError(t, err)
	require.Nil(t, got, "abstain passes through")

	_, err = gc.PayloadAttestationData(context.Background(), 9)
	require.Error(t, err)

	got, err = gc.PayloadAttestationData(context.Background(), 9)
	require.NoError(t, err)
	require.Same(t, data, got)
	require.Equal(t, 3, fetches)
}
