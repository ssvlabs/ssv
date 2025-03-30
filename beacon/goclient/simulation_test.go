package goclient

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aquasecurity/table"
	"github.com/attestantio/go-eth2-client/api"
	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/operator/datastore"
	"github.com/ssvlabs/ssv/operator/slotticker"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/ssvlabs/ssv/registry/storage"
	"go.uber.org/zap"
	"golang.org/x/exp/maps"
)

func TestSimulation(t *testing.T) {
	var (
		// Number of weighted clients to create
		weightedClients = 4

		// Beacon nodes to use for the benchmark.
		beaconNodes = map[string]string{
			"geth-lh-1":    "http://mainnet-geth-lh-1-a-beacon.ethereum.svc:5052",
			"geth-teku-2":  "http://mainnet-geth-teku-2-b-beacon.ethereum.svc:5052",
			"nethermind-3": "http://mainnet-nethermind-lh-3-c-beacon.ethereum.svc:5052",
			"nethermind-4": "http://mainnet-nethermind-teku-4-d-beacon.ethereum.svc:5052",
		}
	)

	logger, err := zap.NewDevelopment()
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	// Initialize network config for mainnet
	networkConfig := networkconfig.Mainnet

	// Create slot ticker based on network config
	newSlotTicker := func() slotticker.SlotTicker {
		return slotticker.New(zap.NewNop(), slotticker.Config{
			SlotDuration: networkConfig.SlotDurationSec(),
			GenesisTime:  networkConfig.GetGenesisTime(),
		})
	}

	// Create a simple operator data store
	operatorDataStore := datastore.New(&storage.OperatorData{
		ID:           1,
		PublicKey:    []byte("test-public-key"),
		OwnerAddress: [20]byte{},
	})

	// Connect to all BNs.
	clients := make(map[string]*GoClient)
	for name, addr := range beaconNodes {
		client, err := New(logger.Named("Client."+name), beacon.Options{
			Context:        context.Background(),
			Network:        beacon.NewNetwork(networkConfig.Beacon.GetBeaconNetwork()),
			BeaconNodeAddr: addr,
			CommonTimeout:  5 * time.Second,
			LongTimeout:    60 * time.Second,
		}, operatorDataStore, newSlotTicker)
		if err != nil {
			fmt.Printf("Failed to initialize goclient: %v\n", err)
			return
		}
		clients[name] = client
	}

	// Add multiple weighted clients using all BNs with different node priorities
	nodeAddrs := maps.Values(beaconNodes)
	retryingClients := weightedClients / 2
	for i := 1; i <= weightedClients; i++ {
		retrying := i < retryingClients
		withRetriesLabel := ""
		if retrying {
			withRetriesLabel = "-retries"
		}
		weightedClientName := fmt.Sprintf("weighted-%d%s", i, withRetriesLabel)

		// Create a weighted client with the current address order
		weightedNodeAddrs := strings.Join(nodeAddrs, ";")

		retries := 0
		if retrying {
			retries = i
		}
		weightedClient, err := New(
			logger.Named("Client."+weightedClientName),
			beacon.Options{
				Context:                     context.Background(),
				Network:                     beacon.NewNetwork(networkConfig.Beacon.GetBeaconNetwork()),
				BeaconNodeAddr:              weightedNodeAddrs,
				CommonTimeout:               5 * time.Second,
				LongTimeout:                 60 * time.Second,
				WithWeightedAttestationData: true,
				MaxHeaderFetchRetries:       retries,
			},
			operatorDataStore,
			newSlotTicker,
		)
		if err != nil {
			fmt.Printf("Failed to initialize weighted client %s: %v\n", weightedClientName, err)
			return
		}
		if err := weightedClient.Healthy(context.Background()); err != nil {
			fmt.Printf("Weighted client %s is not healthy: %v\n", weightedClientName, err)
			return
		}
		clients[weightedClientName] = weightedClient

		// Rotate addresses for the next client by shifting to the right.
		nodeAddrs = append(nodeAddrs[1:], nodeAddrs[0])

		fmt.Printf("weighted client %s setup with addresses: %s\n", weightedClientName, weightedNodeAddrs)
	}

	// Every slot, request all BNs for AttestationData in parallel.
	type clientTrace struct {
		attestation          bool
		attestationBlockRoot phase0.Root
		attestationDuration  time.Duration

		blockEvent         bool
		blockEventDuration time.Duration
	}
	traces := make(map[string]map[phase0.Slot]*clientTrace)
	// Add mutex to protect access to traces
	tracesMutex := &sync.RWMutex{}

	for name, client := range clients {
		traces[name] = make(map[phase0.Slot]*clientTrace)
		go func(name string, client *GoClient) {
			slotTicker := newSlotTicker()
			headEvents := make(chan *eth2apiv1.HeadEvent, 32)
			blockCond := sync.NewCond(&sync.Mutex{})
			headSlot := phase0.Slot(0)

			// Subscribe to head events
			err := client.SubscribeToHeadEvents(context.Background(), "benchmark", headEvents)
			if err != nil {
				fmt.Printf("Failed to subscribe to head events for %s: %v\n", name, err)
				return
			}

			// Handle head events
			go func() {
				for event := range headEvents {
					if event == nil {
						continue
					}
					blockCond.L.Lock()
					headSlot = event.Slot
					blockCond.Broadcast()
					blockCond.L.Unlock()
				}
			}()

			for {
				<-slotTicker.Next()
				slot := slotTicker.Slot()

				// Create a local trace object
				localTrace := &clientTrace{}

				// Use a simple timeout approach
				start := time.Now()
				timeoutCh := time.After(4 * time.Second)
				waitFinished := make(chan struct{})

				// Start waiting for block event in a goroutine
				go func() {
					blockCond.L.Lock()
					for headSlot < slot {
						blockCond.Wait()
					}
					blockCond.L.Unlock()
					close(waitFinished)
				}()

				// Wait for either the condition to be satisfied or timeout
				select {
				case <-waitFinished:
					// Block event received
					localTrace.blockEvent = true
				case <-timeoutCh:
					// Timeout occurred
					fmt.Printf("Timeout waiting for block event on %s for slot %d\n", name, slot)
				}

				elapsed := time.Since(start)
				fmt.Printf("Time taken to wait for block event on %s for slot %d: %v\n", name, slot, elapsed)

				localTrace.blockEventDuration = elapsed

				attestationData, _, err := client.GetAttestationData(slot)
				if err != nil {
					fmt.Printf("Failed to get attestation data from %s at slot %d: %v\n", name, slot, err)
					// Save the partial trace with just block event data
					tracesMutex.Lock()
					traces[name][slot] = localTrace
					tracesMutex.Unlock()
					continue
				}

				localTrace.attestation = true
				localTrace.attestationBlockRoot = attestationData.BeaconBlockRoot
				localTrace.attestationDuration = time.Since(start)

				tracesMutex.Lock()
				traces[name][slot] = localTrace
				tracesMutex.Unlock()
			}
		}(name, client)
	}

	// Track performance.
	type clientPerformance struct {
		attestations        int
		correctAttestations int
		attestationDuration time.Duration

		blockEvents        int
		blockEventDuration time.Duration
	}
	clientPerformances := make(map[string]*clientPerformance)
	for clientName := range clients {
		clientPerformances[clientName] = &clientPerformance{}
	}

	followDistance := phase0.Slot(2)
	startCheckingAtSlot := networkConfig.Beacon.EstimatedCurrentSlot() + 1
	slotTicker := newSlotTicker()
	checkedSlots := phase0.Slot(0)
	for {
		<-slotTicker.Next()
		slot := slotTicker.Slot() - followDistance

		// Skip first slots without data.
		if slot < startCheckingAtSlot {
			continue
		}
		checkedSlots++

		found := false
		var root phase0.Root
		for clientName, client := range clients {
			resp, err := client.multiClient.BeaconBlockHeader(context.Background(), &api.BeaconBlockHeaderOpts{
				Block: fmt.Sprintf("%d", slot),
			})
			if err != nil {
				fmt.Printf("Failed to get beacon block header from %s at slot %d: %v\n", clientName, slot, err)
				continue
			}
			root = resp.Data.Root
			found = true
			break
		}
		if !found {
			fmt.Printf("Block is missing at slot %d\n", slot)
			continue
		}

		// Log client traces for this slot as JSON.
		tracesMutex.RLock()
		slotTraces := make(map[string]*clientTrace)
		for clientName, clientTraces := range traces {
			slotTraces[clientName] = clientTraces[slot]
		}
		tracesMutex.RUnlock()
		logger.Debug("Slot traces", zap.Any("slot", slot), zap.Any("traces", slotTraces))

		// Print per-slot performance table
		fmt.Println()
		fmt.Printf("Slot %d (iteration %d)\n", slot, checkedSlots)
		slotTbl := table.New(os.Stdout)
		slotTbl.AddRow("Client", "Attestation", "Correct", "Block Event", "Att. Time", "Event Time")

		slotClientNames := maps.Keys(clients)
		sort.Strings(slotClientNames)

		for _, clientName := range slotClientNames {
			tracesMutex.RLock()
			trace, ok := traces[clientName][slot]
			tracesMutex.RUnlock()

			if !ok {
				slotTbl.AddRow(clientName, "✗", "✗", "✗", "-", "-")
				continue
			}

			isCorrect := trace.attestation && trace.attestationBlockRoot == root
			slotTbl.AddRow(
				clientName,
				boolMark(trace.attestation),
				boolMark(isCorrect),
				boolMark(trace.blockEvent),
				shortDuration(trace.attestationDuration),
				shortDuration(trace.blockEventDuration),
			)
		}
		slotTbl.Render()
		fmt.Println()

		// Update cumulative stats
		for clientName := range clients {
			tracesMutex.RLock()
			trace, ok := traces[clientName][slot]
			tracesMutex.RUnlock()

			if !ok {
				continue
			}
			if trace.attestation {
				clientPerformances[clientName].attestations++
				if trace.attestationBlockRoot == root {
					clientPerformances[clientName].correctAttestations++
				}
				clientPerformances[clientName].attestationDuration += trace.attestationDuration
			}
			if trace.blockEvent {
				clientPerformances[clientName].blockEvents++
				clientPerformances[clientName].blockEventDuration += trace.blockEventDuration
			}
		}

		// Print cumulative stats table
		tbl := table.New(os.Stdout)
		tbl.AddRow("Client", "Attestations", "Correctness", "Block Events")

		// Sort by correct votes
		clientNames := maps.Keys(clientPerformances)
		sort.Slice(clientNames, func(i, j int) bool {
			return clientPerformances[clientNames[i]].correctAttestations > clientPerformances[clientNames[j]].correctAttestations
		})

		for _, clientName := range clientNames {
			performance := clientPerformances[clientName]
			attestationDuration := time.Duration(0)
			if performance.attestations > 0 {
				attestationDuration = performance.attestationDuration / time.Duration(performance.attestations)
			}
			correctness := 0.0
			if performance.attestations > 0 {
				correctness = float64(performance.correctAttestations) * 100 / float64(performance.attestations)
			}
			blockEventDuration := time.Duration(0)
			if performance.blockEvents > 0 {
				blockEventDuration = performance.blockEventDuration / time.Duration(performance.blockEvents)
			}
			tbl.AddRow(clientName,
				fmt.Sprintf("%d/%d (%s)", performance.attestations, checkedSlots, shortDuration(attestationDuration)),
				fmt.Sprintf("%d (%.2f%%)", performance.correctAttestations, correctness),
				fmt.Sprintf("%d/%d (%s)", performance.blockEvents, checkedSlots, shortDuration(blockEventDuration)))
		}
		fmt.Println()
		fmt.Printf("Cumulative Status\n")
		tbl.Render()

		// Free memory.
		tracesMutex.Lock()
		for clientName := range clients {
			delete(traces[clientName], slot)
		}
		tracesMutex.Unlock()
	}

}

func shortDuration(d time.Duration) string {
	return fmt.Sprintf("%.4fs", d.Seconds())
}

func boolMark(b bool) string {
	if b {
		return "✅"
	}
	return "❌"
}
