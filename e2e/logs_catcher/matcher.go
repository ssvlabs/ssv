package logs_catcher

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/e2e/logs_catcher/docker"
	"github.com/bloxapp/ssv/e2e/logs_catcher/logs"
)

// Test conditions:

const waitTarget = "beacon_proxy"
const beaconProxyContainer = "beacon_proxy"

var ssvNodesContainers = []string{"ssv-node-1", "ssv-node-2", "ssv-node-3", "ssv-node-4"}

const waitFor = "End epoch finished"

// For each in target #1
const origMessage = "set up validator"
const slashableMessage = "\"attester_slashable\":true"
const nonSlashableMessage = "\"attester_slashable\":false"

// Take field
const idField = "pubkey"

// and find in target #2
const slashableMatchMessage = "slashable attestation"
const nonSlashableMatchMessage = "successfully submitted attestation"

func StartCondition(pctx context.Context, logger *zap.Logger, condition []string, targetContainer string, cli DockerCLI) (string, error) {
	ctx, cancel := context.WithCancel(pctx)
	defer cancel()

	conditionLog := ""

	logger.Debug("Waiting for start condition at target", zap.String("target", targetContainer), zap.Strings("condition", condition))
	ch := make(chan string, 1024)
	go func() {
		for log := range ch {
			if logs.GrepLine(log, condition) {
				conditionLog = log
				logger.Info("Start condition arrived", zap.Strings("log_message", condition))
				cancel()
			}
		}
	}()
	// TODO: either apply logs collection on each container or fan in the containers to one log stream
	err := docker.StreamDockerLogs(ctx, cli, targetContainer, ch)
	if err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("Log streaming stopped with err ", zap.Error(err))
		return conditionLog, err
	}
	return conditionLog, nil
}

// testDuty performs a generic validation of attestation logs by comparing entries across beacon proxy and SSV node containers.
// It's designed to handle both non-slashable and slashable attestation log validations.
func testDuty(ctx context.Context, logger *zap.Logger, dockerCLI DockerCLI, attestationType string) error {
	var beaconCriteria, nodeCriteria []string
	var discrepancyCheck func(beaconCount, nodeCount int) bool

	switch attestationType {
	case "nonSlashable":
		beaconCriteria = []string{origMessage, nonSlashableMessage}
		nodeCriteria = []string{nonSlashableMatchMessage}
		// For non-slashable attestations, we expect the node count to be exactly 2.
		discrepancyCheck = func(beaconCount, nodeCount int) bool {
			return nodeCount != 2
		}
	case "slashable":
		beaconCriteria = []string{origMessage, slashableMessage}
		nodeCriteria = []string{slashableMatchMessage}
		// For slashable attestations, the node count must match the beacon count exactly.
		discrepancyCheck = func(beaconCount, nodeCount int) bool {
			return beaconCount != nodeCount
		}
	default:
		return fmt.Errorf("unknown attestation type: %s", attestationType)
	}

	// Extract and count logs from the beaconProxyContainer based on the specified criteria.
	beaconCounts, err := dockerLogsByPubKey(ctx, logger, dockerCLI, beaconProxyContainer, beaconCriteria)
	if err != nil {
		return err
	}

	// Verify corresponding logs in each SSV node container match the criteria.
	for _, nodeContainer := range ssvNodesContainers {
		nodeCounts, err := dockerLogsByPubKey(ctx, logger, dockerCLI, nodeContainer, nodeCriteria)
		if err != nil {
			return err
		}

		// Compare the counts for each public key between beacon proxy and node container.
		for pubkey, beaconCount := range beaconCounts {
			nodeCount, exists := nodeCounts[pubkey]
			if !exists || discrepancyCheck(beaconCount, nodeCount) {
				logger.Info("Discrepancy found", zap.String("PublicKey", pubkey), zap.Int("BeaconCount", beaconCount), zap.Int("NodeCount", nodeCount))
				return fmt.Errorf("discrepancy for pubkey %s in %s: expected %d, got %d", pubkey, nodeContainer, beaconCount, nodeCount)
			}
		}
	}

	return nil
}

// Combines the docker log retrieval, grepping, and counting of public keys into one function.
func dockerLogsByPubKey(ctx context.Context, logger *zap.Logger, cli DockerCLI, containerName string, matchStrings []string) (map[string]int, error) {
	res, err := docker.DockerLogs(ctx, cli, containerName, "")
	if err != nil {
		return nil, err
	}

	grepped := res.Grep(matchStrings)
	logger.Info("matched", zap.Int("count", len(grepped)), zap.String("target", containerName), zap.Strings("match_string", matchStrings))
	publicKeyCounts := make(map[string]int)

	for _, logStr := range grepped {
		trimmedLogStr := strings.TrimLeftFunc(logStr, func(r rune) bool {
			return !strings.ContainsRune("{[", r)
		})

		var logEntry map[string]interface{}
		if err := json.Unmarshal([]byte(trimmedLogStr), &logEntry); err != nil {
			continue // Consider logging this error.
		}

		if pubkey, ok := logEntry["pubkey"].(string); ok {
			// Check if pubkey starts with "0x" and remove it if present
			if strings.HasPrefix(pubkey, "0x") {
				pubkey = strings.TrimPrefix(pubkey, "0x")
			}
			publicKeyCounts[pubkey]++
		}
	}

	return publicKeyCounts, nil
}

func Match(pctx context.Context, logger *zap.Logger, cli DockerCLI) error {
	startctx, startc := context.WithTimeout(pctx, time.Minute*6*4) // wait max 4 epochs
	_, err := StartCondition(startctx, logger, []string{waitFor}, waitTarget, cli)
	if err != nil {
		startc() // Cancel the startctx context
		return err
	}
	startc()

	ctx, c := context.WithCancel(pctx)
	defer c()

	// find slashable attestation not signing for each slashable validator
	if err := testDuty(ctx, logger, cli, "slashable"); err != nil {
		return err
	}
	// find non-slashable validators successfully submitting (all first round + 1 for second round)
	if err := testDuty(ctx, logger, cli, "nonSlashable"); err != nil {
		return err
	}

	//TODO: match proposals
	return nil
}
