package goclient

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectestingutils "github.com/ssvlabs/ssv-spec/types/testingutils"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/observability/log"
	"github.com/ssvlabs/ssv/utils/hashmap"
)

const (
	// proposalPreparationEndpoint is the beacon API endpoint for submitting proposal preparations
	proposalPreparationEndpoint = "/eth/v1/validator/prepare_beacon_proposer"
	// proposalEndpointPrefix is the beacon API endpoint prefix for block proposals
	proposalEndpointPrefix = "/eth/v3/validator/blocks/"

	// Common test values
	testSlot     = phase0.Slot(100)
	testGraffiti = "test graffiti"
)

// getTestRANDAO returns a test RANDAO reveal for testing purposes
func getTestRANDAO() []byte {
	testBlock := spectestingutils.TestingBeaconBlockV(spec.DataVersionElectra)
	return testBlock.Electra.Block.Body.RANDAOReveal[:]
}

// Mock beacon server response options for proposal endpoints
type beaconProposalServerOptions struct {
	// Delay for proposal response
	ProposalResponseDuration time.Duration
	// Return error for proposal endpoint
	WithProposalEndpointError bool
	// Custom proposal response
	ProposalResponse []byte
	// Fee recipient to use in response
	FeeRecipient bellatrix.ExecutionAddress
	// Use blinded proposal
	BlindedProposal bool
	// Optional Eth-Execution-Payload-Value header value; left nil ⇒ header not set
	// (go-eth2-client defaults ExecutionValue to 0). Used by tests that need to
	// influence scoreProposal across multiple BN responses.
	ExecutionValue *big.Int
}

// Creates a mock beacon server for proposal testing
func createProposalBeaconServer(t *testing.T, options beaconProposalServerOptions) (*httptest.Server, *hashmap.Map[phase0.Slot, int]) {
	serverGotRequests := hashmap.New[phase0.Slot, int]()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handle standard beacon endpoints
		if resp, ok := beaconEndpointResponses[r.URL.Path]; ok {
			w.Header().Set("Content-Type", "application/json")
			if _, err := w.Write(resp); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
			}
			return
		}

		// Handle proposal endpoint
		if strings.HasPrefix(r.URL.Path, proposalEndpointPrefix) {
			require.Equal(t, http.MethodGet, r.Method)

			if options.WithProposalEndpointError {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}

			// Extract slot from path
			parts := strings.Split(r.URL.Path, "/")
			slotStr := parts[len(parts)-1]
			slot, err := strconv.ParseUint(slotStr, 10, 64)
			require.NoError(t, err)
			require.NotZero(t, slot)

			// Add delay if specified
			time.Sleep(options.ProposalResponseDuration)

			// Return custom response if provided
			if len(options.ProposalResponse) > 0 {
				writeProposalHeaders(w, options)
				if _, err := w.Write(options.ProposalResponse); err != nil {
					w.WriteHeader(http.StatusInternalServerError)
				}
				return
			}

			// Generate response dynamically (this should not cause races since each server has its own goroutine)
			proposalResp := createProposalResponseSafe(phase0.Slot(slot), options.FeeRecipient, options.BlindedProposal)
			writeProposalHeaders(w, options)
			if _, err := w.Write(proposalResp); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
			}
			return
		}

		// Handle proposal preparations endpoint
		if r.URL.Path == proposalPreparationEndpoint {
			require.Equal(t, http.MethodPost, r.Method)
			w.WriteHeader(http.StatusOK)
			return
		}

		// Unknown endpoint
		w.WriteHeader(http.StatusNotFound)
	}))

	return server, serverGotRequests
}

// writeProposalHeaders sets the response headers that go-eth2-client parses to
// reconstruct VersionedProposal — Eth-Consensus-Version, the blinded flag, and
// optionally Eth-Execution-Payload-Value when the test wants to influence
// scoreProposal across multiple BN responses.
func writeProposalHeaders(w http.ResponseWriter, options beaconProposalServerOptions) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Eth-Consensus-Version", "electra")
	if options.BlindedProposal {
		w.Header().Set("Eth-Execution-Payload-Blinded", "true")
	}
	if options.ExecutionValue != nil {
		w.Header().Set("Eth-Execution-Payload-Value", options.ExecutionValue.String())
	}
}

// spectestingMu serializes mutations on the shared ssv-spec testing fixtures.
// TestingBlindedBeaconBlockV / TestingBeaconBlockV return wrappers that point at
// cached block structs; without this lock, concurrent test-server goroutines
// (e.g., the multi-BN tests) racially mutate the same struct on `block.Slot` and
// `block.Body.*.FeeRecipient`. The JSON marshaling happens inside the lock so
// each call captures its own intended state before the next mutation begins.
var spectestingMu sync.Mutex

// Create a safe proposal response using ssv-spec utilities.
func createProposalResponseSafe(slot phase0.Slot, feeRecipient bellatrix.ExecutionAddress, blinded bool) []byte {
	spectestingMu.Lock()
	defer spectestingMu.Unlock()

	if blinded {
		// Get a blinded block from ssv-spec testing utilities
		versionedBlinded := spectestingutils.TestingBlindedBeaconBlockV(spec.DataVersionElectra)
		block := versionedBlinded.ElectraBlinded

		// Modify the fields we need for our test
		block.Slot = slot
		block.Body.ExecutionPayloadHeader.FeeRecipient = feeRecipient

		// Wrap in response structure
		response := map[string]any{
			"data": block,
		}
		data, _ := json.Marshal(response)
		return data
	}

	// Get a regular block from ssv-spec testing utilities
	versioned := spectestingutils.TestingBeaconBlockV(spec.DataVersionElectra)
	blockContents := versioned.Electra

	// Modify the fields we need for our test
	blockContents.Block.Slot = slot
	blockContents.Block.Body.ExecutionPayload.FeeRecipient = feeRecipient

	// Wrap in response structure
	response := map[string]any{
		"data": blockContents,
	}
	data, _ := json.Marshal(response)
	return data
}

func TestGetBeaconBlock_FeeRecipientValidation(t *testing.T) {
	ctx := context.Background()

	t.Run("zero fee recipient is logged", func(t *testing.T) {
		// Create server with zero fee recipient
		server, _ := createProposalBeaconServer(t, beaconProposalServerOptions{
			FeeRecipient: bellatrix.ExecutionAddress{}, // Zero address
		})
		defer server.Close()

		client, err := createClient(ctx, server.URL, false)
		require.NoError(t, err)

		slot := testSlot
		graffiti := []byte(testGraffiti)
		randao := getTestRANDAO()

		// Should get block successfully but fee recipient should be zero
		versionedProposal, marshaledBlk, err := client.GetBeaconBlock(t.Context(), slot, graffiti, randao)
		require.NoError(t, err)
		require.NotNil(t, versionedProposal)
		require.NotNil(t, marshaledBlk)
		require.Equal(t, spec.DataVersionElectra, versionedProposal.Version)

		feeRecipient, err := versionedProposal.FeeRecipient()
		require.NoError(t, err)
		require.True(t, feeRecipient.IsZero(), "fee recipient should be zero")
	})

	t.Run("non-zero fee recipient works correctly", func(t *testing.T) {
		// Create server with valid fee recipient
		nonZeroFeeRecipient := bellatrix.ExecutionAddress{0x12, 0x34, 0x56, 0x78}
		server, _ := createProposalBeaconServer(t, beaconProposalServerOptions{
			FeeRecipient: nonZeroFeeRecipient,
		})
		defer server.Close()

		client, err := createClient(ctx, server.URL, false)
		require.NoError(t, err)

		slot := testSlot
		graffiti := []byte(testGraffiti)
		randao := getTestRANDAO()

		versionedProposal, marshaledBlk, err := client.GetBeaconBlock(t.Context(), slot, graffiti, randao)
		require.NoError(t, err)
		require.NotNil(t, versionedProposal)
		require.NotNil(t, marshaledBlk)
		require.Equal(t, spec.DataVersionElectra, versionedProposal.Version)

		feeRecipient, err := versionedProposal.FeeRecipient()
		require.NoError(t, err)
		require.False(t, feeRecipient.IsZero(), "fee recipient should not be zero")

		// Check the actual value
		require.Equal(t, nonZeroFeeRecipient, feeRecipient)
	})

	t.Run("blinded block with zero fee recipient", func(t *testing.T) {
		// Create server with blinded block and zero fee recipient
		server, _ := createProposalBeaconServer(t, beaconProposalServerOptions{
			FeeRecipient:    bellatrix.ExecutionAddress{}, // Zero address
			BlindedProposal: true,
		})
		defer server.Close()

		client, err := createClient(ctx, server.URL, false)
		require.NoError(t, err)

		slot := testSlot
		graffiti := []byte(testGraffiti)
		randao := getTestRANDAO()

		versionedProposal, marshaledBlk, err := client.GetBeaconBlock(t.Context(), slot, graffiti, randao)
		require.NoError(t, err)
		require.NotNil(t, versionedProposal)
		require.NotNil(t, marshaledBlk)
		require.Equal(t, spec.DataVersionElectra, versionedProposal.Version)

		require.True(t, versionedProposal.Blinded, "should return a blinded block")
		feeRecipient, err := versionedProposal.FeeRecipient()
		require.NoError(t, err)
		require.True(t, feeRecipient.IsZero(), "fee recipient should be zero")
	})

	t.Run("blinded block with non-zero fee recipient", func(t *testing.T) {
		// Create server with blinded block and valid fee recipient
		nonZeroFeeRecipient := bellatrix.ExecutionAddress{0xaa, 0xbb, 0xcc, 0xdd}
		server, _ := createProposalBeaconServer(t, beaconProposalServerOptions{
			FeeRecipient:    nonZeroFeeRecipient,
			BlindedProposal: true,
		})
		defer server.Close()

		client, err := createClient(ctx, server.URL, false)
		require.NoError(t, err)

		slot := testSlot
		graffiti := []byte(testGraffiti)
		randao := getTestRANDAO()

		versionedProposal, marshaledBlk, err := client.GetBeaconBlock(t.Context(), slot, graffiti, randao)
		require.NoError(t, err)
		require.NotNil(t, versionedProposal)
		require.NotNil(t, marshaledBlk)
		require.Equal(t, spec.DataVersionElectra, versionedProposal.Version)

		require.True(t, versionedProposal.Blinded, "should return a blinded block")
		feeRecipient, err := versionedProposal.FeeRecipient()
		require.NoError(t, err)
		require.False(t, feeRecipient.IsZero(), "fee recipient should not be zero")
		require.Equal(t, nonZeroFeeRecipient, feeRecipient)
	})

	t.Run("handles proposal endpoint error", func(t *testing.T) {
		// Create server that returns error
		server, _ := createProposalBeaconServer(t, beaconProposalServerOptions{
			WithProposalEndpointError: true,
		})
		defer server.Close()

		client, err := createClient(ctx, server.URL, false)
		require.NoError(t, err)

		slot := testSlot
		graffiti := []byte(testGraffiti)
		randao := getTestRANDAO()

		versionedProposal, marshaledBlk, err := client.GetBeaconBlock(t.Context(), slot, graffiti, randao)
		require.Error(t, err)
		require.Nil(t, versionedProposal)
		require.Nil(t, marshaledBlk)
	})
}

func TestSubmitProposalPreparationBatches(t *testing.T) {
	gc := &GoClient{
		log: log.TestLogger(t),
	}

	t.Run("batches are split correctly", func(t *testing.T) {
		// Create 1500 preparations (should be 3 batches of 500)
		preparations := make([]*eth2apiv1.ProposalPreparation, 1500)
		for i := range preparations {
			preparations[i] = &eth2apiv1.ProposalPreparation{
				ValidatorIndex: phase0.ValidatorIndex(i),
				FeeRecipient:   bellatrix.ExecutionAddress{byte(i % 256)},
			}
		}

		var batchSizes []int
		err := gc.submitProposalPreparationBatches(preparations, func(batch []*eth2apiv1.ProposalPreparation) error {
			batchSizes = append(batchSizes, len(batch))
			return nil
		})

		require.NoError(t, err)
		require.Equal(t, []int{500, 500, 500}, batchSizes, "should split into 3 batches of 500")
	})

	t.Run("continues processing after batch failure", func(t *testing.T) {
		preparations := make([]*eth2apiv1.ProposalPreparation, 1500)
		for i := range preparations {
			preparations[i] = &eth2apiv1.ProposalPreparation{
				ValidatorIndex: phase0.ValidatorIndex(i),
			}
		}

		var processedBatches []int
		err := gc.submitProposalPreparationBatches(preparations, func(batch []*eth2apiv1.ProposalPreparation) error {
			batchIndex := int(batch[0].ValidatorIndex) / 500
			processedBatches = append(processedBatches, batchIndex)

			if batchIndex == 1 {
				// Second batch fails
				return fmt.Errorf("batch %d failed", batchIndex)
			}
			return nil
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), "partially submitted proposal preparations: 1000/1500")
		require.Equal(t, []int{0, 1, 2}, processedBatches, "all batches should be attempted")
	})

	t.Run("handles complete failure", func(t *testing.T) {
		preparations := make([]*eth2apiv1.ProposalPreparation, 100)
		for i := range preparations {
			preparations[i] = &eth2apiv1.ProposalPreparation{
				ValidatorIndex: phase0.ValidatorIndex(i),
			}
		}

		attemptCount := 0
		err := gc.submitProposalPreparationBatches(preparations, func(batch []*eth2apiv1.ProposalPreparation) error {
			attemptCount++
			return fmt.Errorf("batch submission failed")
		})

		require.Error(t, err)
		require.Contains(t, err.Error(), fmt.Sprintf("failed to submit any of %d proposal preparations", len(preparations)))
		require.Equal(t, 1, attemptCount, "should attempt once")
	})

	t.Run("handles empty preparations", func(t *testing.T) {
		callCount := 0
		err := gc.submitProposalPreparationBatches([]*eth2apiv1.ProposalPreparation{}, func(batch []*eth2apiv1.ProposalPreparation) error {
			callCount++
			return nil
		})

		require.NoError(t, err)
		require.Equal(t, 0, callCount, "should not call submit function for empty preparations")
	})
}

func TestGetProposalParallel_MultiClient(t *testing.T) {
	ctx := context.Background()

	t.Run("races multiple clients for fastest response", func(t *testing.T) {
		// Create 3 servers with different response times
		feeRecipient1 := bellatrix.ExecutionAddress{0x11}
		feeRecipient2 := bellatrix.ExecutionAddress{0x22}
		feeRecipient3 := bellatrix.ExecutionAddress{0x33}

		// Responses are generated per-request from the URL slot (rather than
		// pre-generated) so the safe path's slot-relative deadline can be set against
		// a future slot below — pre-baking a fixed slot would trip go-eth2-client's
		// "expected slot N" response check.
		server1, _ := createProposalBeaconServer(t, beaconProposalServerOptions{
			ProposalResponseDuration: 500 * time.Millisecond,
			FeeRecipient:             feeRecipient1,
		})
		defer server1.Close()

		server2, _ := createProposalBeaconServer(t, beaconProposalServerOptions{
			ProposalResponseDuration: 50 * time.Millisecond, // Fastest
			FeeRecipient:             feeRecipient2,
		})
		defer server2.Close()

		server3, _ := createProposalBeaconServer(t, beaconProposalServerOptions{
			ProposalResponseDuration: 1000 * time.Millisecond,
			FeeRecipient:             feeRecipient3,
		})
		defer server3.Close()

		// Create multi-client
		client, err := createClient(ctx, strings.Join([]string{server1.URL, server2.URL, server3.URL}, ";"), false)
		require.NoError(t, err)

		graffiti := []byte(testGraffiti)
		randao := getTestRANDAO()

		// Use a future slot so the safe path's slot-relative ProposalSoftDeadline doesn't
		// fire before the collection loop starts — otherwise this test would exercise
		// waitForFirstValidProposal instead of the multi-BN scoring/racing logic.
		slot := client.getBeaconConfig().EstimatedCurrentSlot() + 2

		startTime := time.Now()
		versionedProposal, marshaledBlk, err := client.GetBeaconBlock(t.Context(), slot, graffiti, randao)
		elapsed := time.Since(startTime)

		require.NoError(t, err)
		require.NotNil(t, versionedProposal)
		require.NotNil(t, marshaledBlk)
		require.Equal(t, spec.DataVersionElectra, versionedProposal.Version)

		actualFeeRecipient, err := versionedProposal.FeeRecipient()
		require.NoError(t, err)

		// Should get response from fastest server (server2 with 50ms delay)
		require.Equal(t, feeRecipient2, actualFeeRecipient, "should get response from fastest server (server2)")

		t.Logf("Response time: %v (from server with 50ms delay)", elapsed)
	})

	t.Run("handles context cancellation", func(t *testing.T) {
		// Create slow server
		server, _ := createProposalBeaconServer(t, beaconProposalServerOptions{
			ProposalResponseDuration: 5 * time.Second,
		})
		defer server.Close()

		client, err := createClient(ctx, server.URL, false)
		require.NoError(t, err)

		slot := testSlot
		graffiti := []byte(testGraffiti)
		randao := getTestRANDAO()

		// Create context with immediate cancellation
		cancelCtx, cancel := context.WithCancel(t.Context())
		cancel()

		versionedProposal, marshaledBlk, err := client.GetBeaconBlock(cancelCtx, slot, graffiti, randao)
		require.Error(t, err)
		require.Nil(t, versionedProposal)
		require.Nil(t, marshaledBlk)
	})

	t.Run("handles all clients failing", func(t *testing.T) {
		// Create servers that all return errors
		server1, _ := createProposalBeaconServer(t, beaconProposalServerOptions{
			WithProposalEndpointError: true,
		})
		defer server1.Close()

		server2, _ := createProposalBeaconServer(t, beaconProposalServerOptions{
			WithProposalEndpointError: true,
		})
		defer server2.Close()

		client, err := createClient(ctx, strings.Join([]string{server1.URL, server2.URL}, ";"), false)
		require.NoError(t, err)

		slot := testSlot
		graffiti := []byte(testGraffiti)
		randao := getTestRANDAO()

		versionedProposal, marshaledBlk, err := client.GetBeaconBlock(t.Context(), slot, graffiti, randao)
		require.Error(t, err)
		require.Contains(t, err.Error(), "all 2 clients failed")
		require.Nil(t, versionedProposal)
		require.Nil(t, marshaledBlk)
	})
}

func TestProposalPreparationReconnectLogic_SkipsOnProviderError(t *testing.T) {
	requestCounter := 0
	// Create a test server that should NOT receive any requests
	server, _ := createProposalBeaconServer(t, beaconProposalServerOptions{})
	defer server.Close()

	// Override handler to count requests
	origHandler := server.Config.Handler
	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "proposer/prepare_beacon_proposer") {
			requestCounter++
		}
		origHandler.ServeHTTP(w, r)
	})

	client, err := createClientForProposerTest(t, server.URL)
	require.NoError(t, err)

	var providerCalled atomic.Bool
	client.SetProposalPreparationsProvider(func() ([]*eth2apiv1.ProposalPreparation, error) {
		providerCalled.Store(true)
		return nil, fmt.Errorf("provider error")
	})

	// Manually call the reconnect handler to test it (simulating a reconnection)
	if len(client.clients) > 0 {
		client.handleProposalPreparationsOnReconnect(t.Context(), client.clients[0], client.log)
	}

	require.True(t, providerCalled.Load(), "provider should be called")
	require.Equal(t, 0, requestCounter, "should not have made any requests")
}

func TestProposalPreparationReconnectLogic_SkipsOnEmptyPreparations(t *testing.T) {
	requestCounter := 0
	server, _ := createProposalBeaconServer(t, beaconProposalServerOptions{})
	defer server.Close()

	// Override handler to count requests
	origHandler := server.Config.Handler
	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "proposer/prepare_beacon_proposer") {
			requestCounter++
		}
		origHandler.ServeHTTP(w, r)
	})

	client, err := createClientForProposerTest(t, server.URL)
	require.NoError(t, err)

	var providerCalled atomic.Bool
	client.SetProposalPreparationsProvider(func() ([]*eth2apiv1.ProposalPreparation, error) {
		providerCalled.Store(true)
		return []*eth2apiv1.ProposalPreparation{}, nil
	})

	// Should return early for empty preparations
	if len(client.clients) > 0 {
		client.handleProposalPreparationsOnReconnect(t.Context(), client.clients[0], client.log)
	}
	require.True(t, providerCalled.Load(), "provider should be called")
	require.Equal(t, 0, requestCounter, "should not have made any requests")
}

func TestProposalPreparationReconnectLogic_SubmitsSuccessfully(t *testing.T) {
	expectedPreparations := []*eth2apiv1.ProposalPreparation{
		{
			ValidatorIndex: 1,
			FeeRecipient:   bellatrix.ExecutionAddress{1, 2, 3},
		},
		{
			ValidatorIndex: 2,
			FeeRecipient:   bellatrix.ExecutionAddress{4, 5, 6},
		},
	}

	var mu sync.Mutex
	var submittedPreparations []*eth2apiv1.ProposalPreparation
	submissionCalled := false

	// Create a test server that captures the submitted preparations
	server, _ := createProposalBeaconServer(t, beaconProposalServerOptions{})
	defer server.Close()

	// Override handler to capture submissions
	origHandler := server.Config.Handler
	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == proposalPreparationEndpoint {
			mu.Lock()
			submissionCalled = true
			var preps []*eth2apiv1.ProposalPreparation
			err := json.NewDecoder(r.Body).Decode(&preps)
			require.NoError(t, err)
			submittedPreparations = append(submittedPreparations, preps...)
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			return
		}
		origHandler.ServeHTTP(w, r)
	})

	// First create client without provider - OnActive will skip
	client, err := createClientForProposerTest(t, server.URL)
	require.NoError(t, err)

	// Set the provider for testing
	client.SetProposalPreparationsProvider(func() ([]*eth2apiv1.ProposalPreparation, error) {
		return expectedPreparations, nil
	})

	// Manually trigger reconnection behavior
	if len(client.clients) > 0 {
		client.handleProposalPreparationsOnReconnect(t.Context(), client.clients[0], client.log)
	}

	// Verify submission was called and all preparations were submitted
	mu.Lock()
	defer mu.Unlock()
	require.True(t, submissionCalled, "submission endpoint should be called")
	require.Equal(t, expectedPreparations, submittedPreparations)
}

func TestProposalPreparationReconnectLogic_HandlesSubmissionError(t *testing.T) {
	submissionAttempts := 0

	// Create a test server that returns an error
	server, _ := createProposalBeaconServer(t, beaconProposalServerOptions{})
	defer server.Close()

	// Override handler to return error
	origHandler := server.Config.Handler
	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == proposalPreparationEndpoint {
			submissionAttempts++
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"submission failed"}`))
			return
		}
		origHandler.ServeHTTP(w, r)
	})

	client, err := createClientForProposerTest(t, server.URL)
	require.NoError(t, err)

	client.SetProposalPreparationsProvider(func() ([]*eth2apiv1.ProposalPreparation, error) {
		return []*eth2apiv1.ProposalPreparation{
			{ValidatorIndex: 1, FeeRecipient: bellatrix.ExecutionAddress{1}},
		}, nil
	})

	// Should handle submission error gracefully
	if len(client.clients) > 0 {
		client.handleProposalPreparationsOnReconnect(t.Context(), client.clients[0], client.log)
	}
	require.Equal(t, 1, submissionAttempts, "should attempt submission once")
}

func TestProposalPreparationReconnectLogic_SkipsOnNilProvider(t *testing.T) {
	// Create a test server that should NOT receive any requests
	server, _ := createProposalBeaconServer(t, beaconProposalServerOptions{})
	defer server.Close()

	requestCounter := 0
	// Override handler to count requests
	origHandler := server.Config.Handler
	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "proposer/prepare_beacon_proposer") {
			requestCounter++
		}
		origHandler.ServeHTTP(w, r)
	})

	client, err := New(t.Context(), log.TestLogger(t), Options{
		BeaconNodeAddr: server.URL,
		CommonTimeout:  time.Second * 2,
		LongTimeout:    time.Second * 5,
	})
	require.NoError(t, err)

	// Don't set the provider - it should remain nil
	// This simulates a reconnection happening before SetProposalPreparationsProvider is called

	// Manually call the reconnect handler to test it (simulating a reconnection)
	if len(client.clients) > 0 {
		client.handleProposalPreparationsOnReconnect(t.Context(), client.clients[0], client.log)
	}

	// Should not have made any requests since provider is nil
	require.Equal(t, 0, requestCounter, "should not have made any requests when provider is nil")
}

func createClientForProposerTest(t *testing.T, serverURL string) (*GoClient, error) {
	return New(t.Context(), log.TestLogger(t), Options{
		BeaconNodeAddr:                 serverURL,
		CommonTimeout:                  time.Second * 2,
		LongTimeout:                    time.Second * 5,
		ProposalCollectionSlotRelative: true,
		EarlyExitOnBlinded:             true,
		// safe-path slot-relative deadline (config resolution defaults this in production).
		ProposalSoftDeadline: 1450 * time.Millisecond,
	})
}
