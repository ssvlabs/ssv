package goclient

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/sourcegraph/conc/pool"
	"github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/operator/slotticker"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/ssvlabs/ssv/utils/hashmap"
)

var (
	// epochs is a bunch of random epochs.
	epochs = []phase0.Epoch{318584, 318585, 318586, 318587, 318588}

	defaultHardTimeout = time.Second * 2
	defaultSoftTimeout = time.Duration(float64(defaultHardTimeout) / 2.5)

	// roots is a bunch of random roots.
	roots = []string{
		"0x1662a3d288b0338436d74083b4ce68908a0ece0661aa236acd95c8a4c3f6e8fc",
		"0x631d529ec5a78dcdeed7c253549a937d12cecbf9090e4a6ba9bd62141d62fa46",
		"0x623ec12028d777d55a9e007c8bcd3fb6262878f2dfd00d1d810374b8f63df7b3",
		"0x623ec12028d777d55a9e007ccccc3fb6262878f2dfd00d1d810374b8f63df7b3",
		"0x623ec12028d777d55a9e007ddddd3fb6262878f2dfd00d1d810374b8f63df7b3",
	}

	beaconEndpointResponses = map[string][]byte{
		"/eth/v1/events": []byte(`{
			"slot": "4239945",
			"block": "0xa5e8aca6e7542aeb1cb4a779821880b2f8cc020b56fbe17f94ef949eee295713"
		}`),
		"/eth/v1/node/syncing": []byte(`{
			"data": {
				"head_slot": "4239945",
				"sync_distance": "1", 
				"is_syncing": false,
				"is_optimistic": false,
				"el_offline": false
			}
		}`),
		"/eth/v1/node/version": []byte(`{
			"data": {
				"version": "Lighthouse/v4.5.0-441fc16/x86_64-linux"
			}
		}`),
		"/eth/v1/beacon/genesis": []byte(`{
			"data": {
				"genesis_time": "1695902400",
				"genesis_validators_root": "0x9143aa7c615a7f7115e2b6aac319c03529df8242ae705fba9df39b79c59fa8b1",
				"genesis_fork_version": "0x00000000"
			}
		}`),
		"/eth/v1/config/spec": []byte(`{
			"data": {
				"CONFIG_NAME": "holesky",
				"GENESIS_FORK_VERSION": "0x00000000",
				"CAPELLA_FORK_VERSION": "0x04017000", 
				"MIN_GENESIS_TIME": "1695902100",
				"SECONDS_PER_SLOT": "12",
				"SLOTS_PER_EPOCH": "32",
				"EPOCHS_PER_SYNC_COMMITTEE_PERIOD": "256",
				"SYNC_COMMITTEE_SIZE": "512",
				"SYNC_COMMITTEE_SUBNET_COUNT": "4",
				"TARGET_AGGREGATORS_PER_COMMITTEE": "16",
				"TARGET_AGGREGATORS_PER_SYNC_SUBCOMMITTEE": "16",
				"INTERVALS_PER_SLOT": "3",
				"ALTAIR_FORK_EPOCH": "74240",
				"BELLATRIX_FORK_EPOCH": "144896",
				"CAPELLA_FORK_EPOCH": "194048",
				"DENEB_FORK_EPOCH": "269568",
				"ELECTRA_FORK_EPOCH": "18446744073709551615"
			}
		}`),
	}
)

func TestGoClient_GetAttestationData_Simple(t *testing.T) {
	ctx := context.Background()
	const withWeightedAttestationData = false

	t.Run("requests must be cached (per slot)", func(t *testing.T) {
		slot1 := phase0.Slot(12345678)
		slot2 := phase0.Slot(12345679)

		server, serverGotRequests := createBeaconServer(t, beaconServerResponseOptions{})

		client, err := createClient(ctx, server.URL, withWeightedAttestationData)
		require.NoError(t, err)

		// First request with slot1.
		gotResult1a, gotVersion, err := client.GetAttestationData(slot1)
		require.NoError(t, err)
		require.Equal(t, spec.DataVersionPhase0, gotVersion)
		require.Equal(t, slot1, gotResult1a.Slot)
		require.NotEmpty(t, gotResult1a.BeaconBlockRoot)
		require.NotEmpty(t, gotResult1a.Source.Epoch)
		require.NotEmpty(t, gotResult1a.Source.Root)
		require.NotEmpty(t, gotResult1a.Target.Epoch)
		require.NotEmpty(t, gotResult1a.Target.Root)

		// Second request with slot1, result should have been cached.
		gotResult1b, gotVersion, err := client.GetAttestationData(slot1)
		require.NoError(t, err)
		require.Equal(t, spec.DataVersionPhase0, gotVersion)
		require.Equal(t, slot1, gotResult1b.Slot)
		require.NotEmpty(t, gotResult1b.BeaconBlockRoot)
		require.NotEmpty(t, gotResult1b.Source.Epoch)
		require.NotEmpty(t, gotResult1b.Source.Root)
		require.NotEmpty(t, gotResult1b.Target.Epoch)
		require.NotEmpty(t, gotResult1b.Target.Root)
		// Cached result returned must contain the same data.
		require.Equal(t, gotResult1b.BeaconBlockRoot, gotResult1a.BeaconBlockRoot)
		require.Equal(t, gotResult1b.Source.Epoch, gotResult1a.Source.Epoch)
		require.Equal(t, gotResult1b.Source.Root, gotResult1a.Source.Root)
		require.Equal(t, gotResult1b.Target.Epoch, gotResult1a.Target.Epoch)
		require.Equal(t, gotResult1b.Target.Root, gotResult1a.Target.Root)

		// Third request with slot2.
		gotResult2a, gotVersion, err := client.GetAttestationData(slot2)
		require.NoError(t, err)
		require.Equal(t, spec.DataVersionPhase0, gotVersion)
		require.Equal(t, slot2, gotResult2a.Slot)
		require.NotEmpty(t, gotResult2a.BeaconBlockRoot)
		require.NotEmpty(t, gotResult2a.Source.Epoch)
		require.NotEmpty(t, gotResult2a.Source.Root)
		require.NotEmpty(t, gotResult2a.Target.Epoch)
		require.NotEmpty(t, gotResult2a.Target.Root)

		// Fourth request with slot2, result should have been cached.
		gotResult2b, gotVersion, err := client.GetAttestationData(slot2)
		require.NoError(t, err)
		require.Equal(t, spec.DataVersionPhase0, gotVersion)
		require.Equal(t, slot2, gotResult2b.Slot)
		require.NotEmpty(t, gotResult2b.BeaconBlockRoot)
		require.NotEmpty(t, gotResult2b.Source.Epoch)
		require.NotEmpty(t, gotResult2b.Source.Root)
		require.NotEmpty(t, gotResult2b.Target.Epoch)
		require.NotEmpty(t, gotResult2b.Target.Root)
		// Cached result returned must contain the same data.
		require.Equal(t, gotResult2b.BeaconBlockRoot, gotResult2a.BeaconBlockRoot)
		require.Equal(t, gotResult2b.Source.Epoch, gotResult2a.Source.Epoch)
		require.Equal(t, gotResult2b.Source.Root, gotResult2a.Source.Root)
		require.Equal(t, gotResult2b.Target.Epoch, gotResult2a.Target.Epoch)
		require.Equal(t, gotResult2b.Target.Root, gotResult2a.Target.Root)

		// Second request with slot1, result STILL should be cached.
		gotResult1c, gotVersion, err := client.GetAttestationData(slot1)
		require.NoError(t, err)
		require.Equal(t, spec.DataVersionPhase0, gotVersion)
		require.Equal(t, slot1, gotResult1c.Slot)
		require.NotEmpty(t, gotResult1c.BeaconBlockRoot)
		require.NotEmpty(t, gotResult1c.Source.Epoch)
		require.NotEmpty(t, gotResult1c.Source.Root)
		require.NotEmpty(t, gotResult1c.Target.Epoch)
		require.NotEmpty(t, gotResult1c.Target.Root)
		// Cached result returned must contain the same data.
		require.Equal(t, gotResult1c.BeaconBlockRoot, gotResult1a.BeaconBlockRoot)
		require.Equal(t, gotResult1c.Source.Epoch, gotResult1a.Source.Epoch)
		require.Equal(t, gotResult1c.Source.Root, gotResult1a.Source.Root)
		require.Equal(t, gotResult1c.Target.Epoch, gotResult1a.Target.Epoch)
		require.Equal(t, gotResult1c.Target.Root, gotResult1a.Target.Root)

		uniqueSlots := serverGotRequests.SlowLen()
		require.Equal(t, 2, uniqueSlots)
		reqCntSlot1, ok := serverGotRequests.Get(slot1)
		require.True(t, ok)
		require.Equal(t, 1, reqCntSlot1)
		reqCntSlot2, ok := serverGotRequests.Get(slot2)
		require.True(t, ok)
		require.Equal(t, 1, reqCntSlot2)
	})

	t.Run("returns error when server responds with error", func(t *testing.T) {
		beaconServer, _ := createBeaconServer(t, beaconServerResponseOptions{WithAttestationDataEndpointError: true})
		client, err := createClient(ctx, beaconServer.URL, withWeightedAttestationData)
		require.NoError(t, err)

		response, dataVersion, err := client.GetAttestationData(phase0.Slot(100))

		require.Nil(t, response)
		require.Equal(t, DataVersionNil, dataVersion)
		require.Error(t, err)
		require.Equal(t, err.Error(), "failed to get attestation data: GET failed with status 500")
	})

	t.Run("concurrency: race conditions and deadlocks", func(t *testing.T) {
		server, serverGotRequests := createBeaconServer(t, beaconServerResponseOptions{WithAttestationDataEndpointError: false})

		client, err := New(
			zap.NewNop(),
			Options{
				Context:        ctx,
				Network:        beacon.NewNetwork(types.MainNetwork),
				BeaconNodeAddr: server.URL,
				CommonTimeout:  1 * time.Second,
				LongTimeout:    1 * time.Second,
			},
			func() slotticker.SlotTicker {
				return slotticker.New(zap.NewNop(), slotticker.Config{
					SlotDuration: 12 * time.Second,
					GenesisTime:  time.Now(),
				})
			},
		)
		require.NoError(t, err)

		// slotsTotalCnt is how many slots we want to spread our GetAttestationData requests between.
		const slotsTotalCnt = 10
		// slotStartPos start at some non-0 slot (GetAttestationData requests will be made in the slot
		// range [slotStartPos, slotStartPos + slotsTotalCnt).
		const slotStartPos = 100000000

		gotResults := hashmap.New[phase0.Slot, *phase0.AttestationData]()

		p := pool.New()
		for i := 0; i < 1000; i++ {
			slot := phase0.Slot(slotStartPos + i%slotsTotalCnt)
			p.Go(func() {
				gotResult, gotVersion, err := client.GetAttestationData(slot)
				require.NoError(t, err)
				require.Equal(t, spec.DataVersionPhase0, gotVersion)
				require.Equal(t, slot, gotResult.Slot)

				prevResult, ok := gotResults.GetOrSet(slot, gotResult)
				if ok {
					// Compare the result we got against previously observed (should have same data).
					require.Equal(t, prevResult.BeaconBlockRoot, gotResult.BeaconBlockRoot)
					require.Equal(t, prevResult.Source.Epoch, gotResult.Source.Epoch)
					require.Equal(t, prevResult.Source.Root, gotResult.Source.Root)
					require.Equal(t, prevResult.Target.Epoch, gotResult.Target.Epoch)
					require.Equal(t, prevResult.Target.Root, gotResult.Target.Root)
				}
			})
		}

		p.Wait()

		for i := 0; i < slotsTotalCnt; i++ {
			slot := phase0.Slot(slotStartPos + i)
			// There is about ~1 % chance a particular slot didn't receive any requests, just
			// accounting for that here by setting reqCnt to 1 in this case.
			reqCnt, _ := serverGotRequests.GetOrSet(slot, 1)
			require.Equal(t, 1, reqCnt)
		}
		uniqueSlots := serverGotRequests.SlowLen()
		require.Equal(t, slotsTotalCnt, uniqueSlots)
	})
}

func TestGoClient_GetAttestationData_Weighted(t *testing.T) {
	ctx := context.Background()
	const withWeightedAttestationData = true

	t.Run("single beacon client: returns response provided by server", func(t *testing.T) {
		const testSlot = phase0.Slot(100)
		beaconServer, serverHandledRequests := createBeaconServer(t, beaconServerResponseOptions{WithAttestationDataEndpointError: false})
		client, err := createClient(ctx, beaconServer.URL, withWeightedAttestationData)
		require.NoError(t, err)

		response, dataVersion, err := client.GetAttestationData(testSlot)

		require.NoError(t, err)
		require.Equal(t, spec.DataVersionPhase0, dataVersion)
		require.NotNil(t, response)
		require.Contains(t, roots, "0x"+hex.EncodeToString(response.BeaconBlockRoot[:]))
		require.Contains(t, roots, "0x"+hex.EncodeToString(response.Source.Root[:]))
		require.Contains(t, roots, "0x"+hex.EncodeToString(response.Target.Root[:]))
		require.Contains(t, epochs, response.Target.Epoch)
		require.Contains(t, epochs, response.Source.Epoch)
		require.Equal(t, testSlot, response.Slot)
		slotRequests, contains := serverHandledRequests.Get(testSlot)
		require.True(t, contains)
		require.NotZero(t, slotRequests)
	})

	t.Run("single beacon client: returns response provided by server when soft timeout reached", func(t *testing.T) {
		const testSlot = phase0.Slot(100)
		beaconServer, serverHandledRequests := createBeaconServer(t, beaconServerResponseOptions{
			AttestationDataResponseDuration: time.Duration(float64(defaultSoftTimeout) * 1.1),
		})
		client, err := createClient(ctx, beaconServer.URL, withWeightedAttestationData)
		require.NoError(t, err)

		response, dataVersion, err := client.GetAttestationData(testSlot)

		require.NoError(t, err)
		require.Equal(t, spec.DataVersionPhase0, dataVersion)
		require.NotNil(t, response)
		require.Contains(t, roots, "0x"+hex.EncodeToString(response.BeaconBlockRoot[:]))
		require.Contains(t, roots, "0x"+hex.EncodeToString(response.Source.Root[:]))
		require.Contains(t, roots, "0x"+hex.EncodeToString(response.Target.Root[:]))
		require.Contains(t, epochs, response.Target.Epoch)
		require.Contains(t, epochs, response.Source.Epoch)
		require.Equal(t, testSlot, response.Slot)
		slotRequests, contains := serverHandledRequests.Get(testSlot)
		require.True(t, contains)
		require.NotZero(t, slotRequests)
	})

	t.Run("single beacon client: does not await soft timeout", func(t *testing.T) {
		beaconServer, _ := createBeaconServer(t, beaconServerResponseOptions{WithAttestationDataEndpointError: false})
		client, err := createClient(ctx, beaconServer.URL, withWeightedAttestationData)
		require.NoError(t, err)

		startTime := time.Now()
		_, _, err = client.GetAttestationData(phase0.Slot(100))

		require.NoError(t, err)
		require.Less(t, time.Since(startTime), defaultSoftTimeout)
	})

	t.Run("single beacon client: returns error when server responds with error", func(t *testing.T) {
		testSlot := phase0.Slot(100)
		beaconServer, _ := createBeaconServer(t, beaconServerResponseOptions{WithAttestationDataEndpointError: true})
		client, err := createClient(ctx, beaconServer.URL, withWeightedAttestationData)
		require.NoError(t, err)

		response, dataVersion, err := client.GetAttestationData(testSlot)

		require.Nil(t, response)
		require.Equal(t, DataVersionNil, dataVersion)
		require.Error(t, err)
		require.Equal(t, err.Error(), "no attestations received")
	})

	t.Run("single beacon client: should not return error when Slot via Block Root Header returns error", func(t *testing.T) {
		beaconServer, _ := createBeaconServer(t, beaconServerResponseOptions{
			WithHeaderEndpointError: true,
		})
		client, err := createClient(ctx, beaconServer.URL, withWeightedAttestationData)
		require.NoError(t, err)

		_, _, err = client.GetAttestationData(phase0.Slot(100))

		require.NoError(t, err)
	})

	t.Run("single beacon client: populates blockRootToSlot cache", func(t *testing.T) {
		expectedCachedSlot := phase0.Slot(500)
		beaconServer, _ := createBeaconServer(t, beaconServerResponseOptions{
			SlotReturnedFromHeaderEndpoint: expectedCachedSlot,
		})
		client, err := createClient(ctx, beaconServer.URL, withWeightedAttestationData)
		require.NoError(t, err)

		client.GetAttestationData(phase0.Slot(100))

		require.Equal(t, 1, client.blockRootToSlotCache.Len())
		for root, item := range client.blockRootToSlotCache.Items() {
			require.Contains(t, roots, "0x"+hex.EncodeToString(root[:]))
			require.Equal(t, expectedCachedSlot, item.Value())
		}
	})

	t.Run("multiple beacon clients: does not await for soft timeout when all servers respond", func(t *testing.T) {
		const numberOfBeaconServers = 3
		var beaconServersURLs []string
		for i := 0; i < numberOfBeaconServers; i++ {
			server, _ := createBeaconServer(t, beaconServerResponseOptions{})
			beaconServersURLs = append(beaconServersURLs, server.URL)
		}
		client, err := createClient(ctx, strings.Join(beaconServersURLs, ";"), withWeightedAttestationData)
		require.NoError(t, err)

		startTime := time.Now()
		client.GetAttestationData(phase0.Slot(100))

		require.Less(t, time.Since(startTime), defaultSoftTimeout)
	})

	t.Run("multiple beacon clients: awaits for soft timeout when one of the servers is a slow responder", func(t *testing.T) {
		const numberOfFastServers = 2
		var beaconServersURLs []string
		for i := 0; i < numberOfFastServers; i++ {
			server, _ := createBeaconServer(t, beaconServerResponseOptions{})
			beaconServersURLs = append(beaconServersURLs, server.URL)
		}
		slowServer, _ := createBeaconServer(t, beaconServerResponseOptions{AttestationDataResponseDuration: time.Minute})
		beaconServersURLs = append(beaconServersURLs, slowServer.URL)

		client, err := createClient(ctx, strings.Join(beaconServersURLs, ";"), withWeightedAttestationData)
		require.NoError(t, err)

		startTime := time.Now()
		_, _, err = client.GetAttestationData(phase0.Slot(100))

		require.NoError(t, err)
		timeElapsed := time.Since(startTime)
		require.GreaterOrEqual(t, timeElapsed, defaultSoftTimeout)
		require.LessOrEqual(t, timeElapsed, defaultSoftTimeout+(defaultSoftTimeout/10)) //time elapsed should not be greater than soft timeout + 10%
	})

	t.Run("multiple beacon clients: awaits for hard timeout when no responses after soft timeout reached", func(t *testing.T) {
		const numberOfSlowServers = 3
		var beaconServersURLs []string
		for i := 0; i < numberOfSlowServers; i++ {
			server, _ := createBeaconServer(t, beaconServerResponseOptions{AttestationDataResponseDuration: defaultHardTimeout * 2})
			beaconServersURLs = append(beaconServersURLs, server.URL)
		}
		client, err := createClient(ctx, strings.Join(beaconServersURLs, ";"), withWeightedAttestationData)
		require.NoError(t, err)

		startTime := time.Now()
		response, version, err := client.GetAttestationData(phase0.Slot(100))

		require.Error(t, err)
		require.Equal(t, err.Error(), "no attestations received")
		require.Nil(t, response)
		require.Equal(t, DataVersionNil, version)
		timeElapsed := time.Since(startTime)
		require.GreaterOrEqual(t, timeElapsed, defaultHardTimeout)
		require.LessOrEqual(t, timeElapsed, defaultHardTimeout+(defaultHardTimeout/10)) //time elapsed should not be greater than hard timeout + 10%
	})

	t.Run("multiple beacon clients: succeeds within soft timeout when BeaconBlockHeader(scoring) has timeout higher than hard timeout", func(t *testing.T) {
		const numberOfServers = 3
		var beaconServersURLs []string
		for i := 0; i < numberOfServers; i++ {
			server, _ := createBeaconServer(t, beaconServerResponseOptions{BeaconHeadersResponseDuration: defaultHardTimeout * 2})
			beaconServersURLs = append(beaconServersURLs, server.URL)
		}
		client, err := createClient(ctx, strings.Join(beaconServersURLs, ";"), withWeightedAttestationData)
		require.NoError(t, err)

		startTime := time.Now()
		response, version, err := client.GetAttestationData(phase0.Slot(100))

		require.NoError(t, err)
		require.NotNil(t, response)
		require.Equal(t, spec.DataVersionPhase0, version)
		timeElapsed := time.Since(startTime)
		require.LessOrEqual(t, timeElapsed, client.weightedAttestationDataSoftTimeout)
	})

	t.Run("multiple beacon clients: responses are correctly weighted", func(t *testing.T) {
		const testSlot, testEpoch = phase0.Slot(100), phase0.Epoch(10)
		const numberOfBeaconServers = 3
		var (
			beaconServersURLs []string
			sourceEpoch       = testEpoch
			bestSourceEpoch   = testEpoch + 1 // epoch number has a lot of weight, increasing its value  makes it 'best'
			targetEpoch       = testEpoch
		)
		for i := 0; i < numberOfBeaconServers; i++ {
			lastServer := i == numberOfBeaconServers-1
			if lastServer {
				sourceEpoch = bestSourceEpoch
			}
			attestationDataResponse := createAttestationDataResponse(
				testSlot,
				0,
				roots[1],
				roots[2],
				roots[3],
				sourceEpoch,
				targetEpoch,
			)
			server, _ := createBeaconServer(t, beaconServerResponseOptions{
				AttestationDataResponse: attestationDataResponse,
			})
			beaconServersURLs = append(beaconServersURLs, server.URL)
		}

		client, err := createClient(ctx, strings.Join(beaconServersURLs, ";"), withWeightedAttestationData)
		require.NoError(t, err)

		response, version, err := client.GetAttestationData(testSlot)

		require.NoError(t, err)
		require.Equal(t, spec.DataVersionPhase0, version)
		require.NotNil(t, response)
		require.Equal(t, roots[1], "0x"+hex.EncodeToString(response.BeaconBlockRoot[:]))
		require.Equal(t, roots[2], "0x"+hex.EncodeToString(response.Source.Root[:]))
		require.Equal(t, roots[3], "0x"+hex.EncodeToString(response.Target.Root[:]))
		require.Equal(t, testEpoch, response.Target.Epoch)
		require.Equal(t, bestSourceEpoch, response.Source.Epoch)
		require.Equal(t, testSlot, response.Slot)
	})
}

func createClient(
	ctx context.Context,
	beaconServerURL string,
	withWeightedAttestationData bool) (*GoClient, error) {
	client, err := New(zap.NewNop(),
		Options{
			Context:                     ctx,
			Network:                     beacon.NewNetwork(types.MainNetwork),
			BeaconNodeAddr:              beaconServerURL,
			CommonTimeout:               defaultHardTimeout,
			LongTimeout:                 time.Second,
			WithWeightedAttestationData: withWeightedAttestationData,
		},
		func() slotticker.SlotTicker {
			return slotticker.New(zap.NewNop(), slotticker.Config{
				SlotDuration: 12 * time.Second,
				GenesisTime:  time.Now(),
			})
		},
	)
	return client, err
}

type beaconServerResponseOptions struct {
	WithAttestationDataEndpointError,
	WithHeaderEndpointError bool
	// BeaconHeadersResponseDuration helps configure scenarios where the '/eth/v1/beacon/headers' Beacon endpoint responds with a delay specified by this variable.
	BeaconHeadersResponseDuration,
	// AttestationDataResponseDuration helps configure scenarios where the '/eth/v1/validator/attestation_data' Beacon endpoint responds with a delay specified by this variable.
	AttestationDataResponseDuration time.Duration
	AttestationDataResponse        []byte
	SlotReturnedFromHeaderEndpoint phase0.Slot
}

func createBeaconServer(t *testing.T, options beaconServerResponseOptions) (*httptest.Server, *hashmap.Map[phase0.Slot, int]) {
	// serverGotRequests keeps track of server requests made (in thread-safe manner).
	serverGotRequests := hashmap.New[phase0.Slot, int]()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		for endpoint, responseBody := range beaconEndpointResponses {
			if r.URL.Path == endpoint {
				if _, err := w.Write(responseBody); err != nil {
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				return
			}
		}

		//this endpoint is not called for simple attestation data
		if strings.HasPrefix(r.URL.Path, "/eth/v1/beacon/headers") {
			time.Sleep(options.BeaconHeadersResponseDuration)
			if options.WithHeaderEndpointError {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			resp := []byte(fmt.Sprintf(`{
				"execution_optimistic": false,
				"finalized": false,
				"data": {
					"header": {
						"message": {
							"slot": "%d",
							"proposer_index": "595427",
							"parent_root": "0xba8c80a13eecced00fe61d628d15d471694a2d253c0a9d9157055a6f19941fee",
							"state_root": "0x9689b331f33a227d54ad7c4c17e2b7c8e2e3fec9c925e6f212fe9e3941e4f6f9",
							"body_root": "0x6be1346b5e812847696c6f18d86754b930ebe4421a1d108b3ae14d02e19a7cef"
						},
						"signature": "0xb4edd7ffa8cba8e976dfcb5d375f4715fb2993fd27677776805733d454895e76f2d249b81a34a0ae6a37c1072d713bcd0fbc5617b13a51e36807bc17d8de1dd18d670a8bc8e8f9481e888822d08dba067e58844d8796653536cd450ad01acf90"
					},
					"root": "0x2922d4d36529c39ae7c463bc0a18f434d616954bdc0a38f7c24e0847a181de15",
					"canonical": true
				}
			}`, options.SlotReturnedFromHeaderEndpoint))
			if _, err := w.Write(resp); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			return
		}

		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/eth/v1/validator/attestation_data", r.URL.Path)

		slotStr := r.URL.Query().Get("slot")
		slotUint, err := strconv.ParseUint(slotStr, 10, 64)
		require.NoError(t, err)
		require.NotZero(t, slotUint)
		slot := phase0.Slot(slotUint)

		// Record this server request.
		for {
			prevCnt, _ := serverGotRequests.GetOrSet(slot, 0)
			success := serverGotRequests.CompareAndSwap(slot, prevCnt, prevCnt+1)
			if success {
				break
			}
		}

		committeeIndexStr := r.URL.Query().Get("committee_index")
		committeeIndex, err := strconv.ParseUint(committeeIndexStr, 10, 64)
		require.NoError(t, err)
		require.Zero(t, committeeIndex)

		//setup scenario when the endpoint responds with the pre-configured delay (tests related to timeouts)
		time.Sleep(options.AttestationDataResponseDuration)

		if options.WithAttestationDataEndpointError {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		var attestationDataResponse []byte
		if len(options.AttestationDataResponse) != 0 {
			attestationDataResponse = options.AttestationDataResponse
		} else {
			attestationDataResponse = createAttestationDataResponse(
				slot,
				phase0.CommitteeIndex(committeeIndex),
				roots[rand.Int()%len(roots)],
				roots[rand.Int()%len(roots)],
				roots[rand.Int()%len(roots)],
				epochs[rand.Int()%len(epochs)],
				epochs[rand.Int()%len(epochs)])
		}
		if _, err := w.Write(attestationDataResponse); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	}))

	return server, serverGotRequests
}

func createAttestationDataResponse(
	slot phase0.Slot,
	committeeIndex phase0.CommitteeIndex,
	blockRoot, sourceRoot, targetRoot string,
	sourceEpoch, targetEpoch phase0.Epoch,
) []byte {
	resp := []byte(fmt.Sprintf(`{
		"data": {
		  "slot": "%d",
		  "index": "%d",
		  "beacon_block_root": "%s",
		  "source": {
			"epoch": "%d",
			"root": "%s"
		  },
		  "target": {
			"epoch": "%d",
			"root": "%s"
		  }
		}
	  }`, slot, committeeIndex, blockRoot, sourceEpoch, sourceRoot, targetEpoch, targetRoot))

	return resp
}
