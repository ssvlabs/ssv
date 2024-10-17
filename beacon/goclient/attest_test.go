package goclient

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/sourcegraph/conc/pool"
	"github.com/ssvlabs/ssv-spec/types"
	operatordatastore "github.com/ssvlabs/ssv/operator/datastore"
	"github.com/ssvlabs/ssv/operator/slotticker"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
	"github.com/ssvlabs/ssv/utils/hashmap"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestGoClient_GetAttestationData(t *testing.T) {
	ctx := context.Background()
	// roots is a bunch of random roots.
	roots := []string{
		"0x1662a3d288b0338436d74083b4ce68908a0ece0661aa236acd95c8a4c3f6e8fc",
		"0x631d529ec5a78dcdeed7c253549a937d12cecbf9090e4a6ba9bd62141d62fa46",
		"0x623ec12028d777d55a9e007c8bcd3fb6262878f2dfd00d1d810374b8f63df7b3",
		"0x623ec12028d777d55a9e007ccccc3fb6262878f2dfd00d1d810374b8f63df7b3",
		"0x623ec12028d777d55a9e007ddddd3fb6262878f2dfd00d1d810374b8f63df7b3",
	}
	// epochs is a bunch of random epochs.
	epochs := []int64{318584, 318585, 318586, 318587, 318588}

	newMockServer := func() (server *httptest.Server, serverGotRequests *hashmap.Map[phase0.Slot, int]) {
		// serverGotRequests keeps track of server requests made (in thread-safe manner).
		serverGotRequests = hashmap.New[phase0.Slot, int]()

		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Logf("mock server handling request: %s", r.URL.Path)

			expInitRequests := map[string][]byte{
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
			}
			for reqPath, respData := range expInitRequests {
				if reqPath == r.URL.Path {
					w.Header().Set("Content-Type", "application/json")
					if _, err := w.Write(respData); err != nil {
						w.WriteHeader(http.StatusInternalServerError)
						return
					}
					return
				}
			}

			require.Equal(t, http.MethodGet, r.Method)
			require.Equal(t, "/eth/v1/validator/attestation_data", r.URL.Path)

			slotStr := r.URL.Query().Get("slot")
			slotUint, err := strconv.ParseUint(slotStr, 10, 64)
			require.NoError(t, err)
			require.NotZero(t, slotUint)
			slot := phase0.Slot(slotUint)

			committeeIndexStr := r.URL.Query().Get("committee_index")
			committeeIndex, err := strconv.ParseUint(committeeIndexStr, 10, 64)
			require.NoError(t, err)
			require.Zero(t, committeeIndex)

			// Record this server request.
			for {
				prevCnt, _ := serverGotRequests.GetOrSet(slot, 0)
				success := serverGotRequests.CompareAndSwap(slot, prevCnt, prevCnt+1)
				if success {
					break
				}
			}

			blockRoot := roots[rand.Int()%len(roots)]
			sourceRoot := roots[rand.Int()%len(roots)]
			targetRoot := roots[rand.Int()%len(roots)]
			sourceEpoch := epochs[rand.Int()%len(epochs)]
			targetEpoch := epochs[rand.Int()%len(epochs)]

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

			w.Header().Set("Content-Type", "application/json")
			if _, err := w.Write(resp); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
		}))

		return server, serverGotRequests
	}

	t.Run("requests must be cached (per slot)", func(t *testing.T) {
		slot1 := phase0.Slot(12345678)
		slot2 := phase0.Slot(12345679)
		committeeIndex1 := phase0.CommitteeIndex(1)
		committeeIndex2 := phase0.CommitteeIndex(2)

		server, serverGotRequests := newMockServer()

		client, err := New(
			zap.NewNop(),
			beacon.Options{
				Context:        ctx,
				Network:        beacon.NewNetwork(types.MainNetwork),
				BeaconNodeAddr: server.URL,
				CommonTimeout:  1 * time.Second,
				LongTimeout:    1 * time.Second,
			},
			operatordatastore.New(&registrystorage.OperatorData{ID: 1}),
			func() slotticker.SlotTicker {
				return slotticker.New(zap.NewNop(), slotticker.Config{
					SlotDuration: 12 * time.Second,
					GenesisTime:  time.Now(),
				})
			},
		)
		require.NoError(t, err)

		// First request with slot1.
		gotResult1a, gotVersion, err := client.GetAttestationData(slot1, committeeIndex1)
		require.NoError(t, err)
		require.Equal(t, spec.DataVersionPhase0, gotVersion)
		require.Equal(t, slot1, gotResult1a.Slot)
		require.Equal(t, committeeIndex1, gotResult1a.Index)
		require.NotEmpty(t, gotResult1a.BeaconBlockRoot)
		require.NotEmpty(t, gotResult1a.Source.Epoch)
		require.NotEmpty(t, gotResult1a.Source.Root)
		require.NotEmpty(t, gotResult1a.Target.Epoch)
		require.NotEmpty(t, gotResult1a.Target.Root)

		// Second request with slot1, result should have been cached.
		gotResult1b, gotVersion, err := client.GetAttestationData(slot1, committeeIndex1)
		require.NoError(t, err)
		require.Equal(t, spec.DataVersionPhase0, gotVersion)
		require.Equal(t, slot1, gotResult1b.Slot)
		require.Equal(t, committeeIndex1, gotResult1b.Index)
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
		gotResult2a, gotVersion, err := client.GetAttestationData(slot2, committeeIndex2)
		require.NoError(t, err)
		require.Equal(t, spec.DataVersionPhase0, gotVersion)
		require.Equal(t, slot2, gotResult2a.Slot)
		require.Equal(t, committeeIndex2, gotResult2a.Index)
		require.NotEmpty(t, gotResult2a.BeaconBlockRoot)
		require.NotEmpty(t, gotResult2a.Source.Epoch)
		require.NotEmpty(t, gotResult2a.Source.Root)
		require.NotEmpty(t, gotResult2a.Target.Epoch)
		require.NotEmpty(t, gotResult2a.Target.Root)

		// Fourth request with slot2, result should have been cached.
		gotResult2b, gotVersion, err := client.GetAttestationData(slot2, committeeIndex2)
		require.NoError(t, err)
		require.Equal(t, spec.DataVersionPhase0, gotVersion)
		require.Equal(t, slot2, gotResult2b.Slot)
		require.Equal(t, committeeIndex2, gotResult2b.Index)
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
		gotResult1c, gotVersion, err := client.GetAttestationData(slot1, committeeIndex1)
		require.NoError(t, err)
		require.Equal(t, spec.DataVersionPhase0, gotVersion)
		require.Equal(t, slot1, gotResult1c.Slot)
		require.Equal(t, committeeIndex1, gotResult1c.Index)
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

		reqCntSlot1, ok := serverGotRequests.Get(slot1)
		require.True(t, ok)
		require.Equal(t, 1, reqCntSlot1)
		reqCntSlot2, ok := serverGotRequests.Get(slot2)
		require.True(t, ok)
		require.Equal(t, 1, reqCntSlot2)
	})

	t.Run("concurrency: race conditions and deadlocks", func(t *testing.T) {
		server, serverGotRequests := newMockServer()

		client, err := New(
			zap.NewNop(),
			beacon.Options{
				Context:        ctx,
				Network:        beacon.NewNetwork(types.MainNetwork),
				BeaconNodeAddr: server.URL,
				CommonTimeout:  1 * time.Second,
				LongTimeout:    1 * time.Second,
			},
			operatordatastore.New(&registrystorage.OperatorData{ID: 1}),
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
			committeeIndex := phase0.CommitteeIndex(i % 64)
			p.Go(func() {
				gotResult, gotVersion, err := client.GetAttestationData(slot, committeeIndex)
				require.NoError(t, err)
				require.Equal(t, spec.DataVersionPhase0, gotVersion)
				require.Equal(t, slot, gotResult.Slot)
				require.Equal(t, committeeIndex, gotResult.Index)

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
	})
}
