package logs_catcher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go.uber.org/zap"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/bloxapp/ssv/e2e/logs_catcher/docker"
	"github.com/bloxapp/ssv/e2e/logs_catcher/logs"
)

const beaconProxyContainer = "beacon_proxy"

var ssvNodesContainers = []string{"ssv-node-1", "ssv-node-2", "ssv-node-3", "ssv-node-4"}

type Matcher struct {
	logger *zap.Logger
	cli    DockerCLI
	mode   SubMode
}

func NewLogMatcher(logger *zap.Logger, cli DockerCLI, mode SubMode) *Matcher {
	return &Matcher{
		mode:   mode,
		cli:    cli,
		logger: logger,
	}
}

// Match is the entry point to start log catching based on the mode
func (m *Matcher) Match(pctx context.Context) error {
	ctx, cancel := context.WithTimeout(pctx, time.Minute*6*4) // wait max 4 epochs
	defer cancel()

	if _, err := m.waitForStartCondition(ctx, []string{waitFor}, beaconProxyContainer); err != nil {
		return err
	}

	return m.processMode(ctx)
}

func (m *Matcher) processMode(ctx context.Context) error {
	switch m.mode {
	case RsaVerification, Slashable, NonSlashable:
		return m.testDuty(ctx)
	default:
		m.logger.Error("Unknown mode", zap.String("mode", m.mode))
		return fmt.Errorf("unknown mode: %s", m.mode)
	}
}

// testDuty performs a generic validation of attestation logs by comparing entries across beacon proxy and SSV node containers.
// It's designed to handle both non-slashable and slashable attestation log validations.
func (m *Matcher) testDuty(ctx context.Context) error {
	var beaconCriteria, nodeCriteria []string
	var discrepancyCheck func(beaconCount, nodeCount []any) bool

	switch m.mode {
	case NonSlashable:
		beaconCriteria = []string{origMessage, nonSlashableMessage}
		nodeCriteria = []string{nonSlashableMatchMessage}
		discrepancyCheck = func(beaconCount, nodeCount []any) bool {
			return len(nodeCount) != 2
		}
		// For non-slashable attestations, we expect the node count to be exactly 2.
		// Extract logs from the beaconProxyContainer.
		beaconLogs, err := m.processDockerLogs(ctx, beaconProxyContainer, beaconCriteria)
		if err != nil {
			return err
		}

		beaconLogsByPublicKey, err := m.organizeLogsByPubKey(beaconLogs, beaconProxyContainer)
		if err != nil {
			return err
		}

		// Verify corresponding logs in each SSV node container match the validator public key.
		for _, nodeContainer := range ssvNodesContainers {
			nodeLogs, err := m.processDockerLogs(ctx, nodeContainer, nodeCriteria)
			if err != nil {
				return err
			}
			nodeLogsByPublicKey, err := m.organizeLogsByPubKey(nodeLogs, nodeContainer)
			if err != nil {
				return err
			}

			// Compare the counts for each public key between beacon proxy and node container.
			for validatorPubKey, validatorBeaconLogs := range beaconLogsByPublicKey {
				validatorNodeLogs, exists := nodeLogsByPublicKey[validatorPubKey]
				if !exists || discrepancyCheck(validatorBeaconLogs, validatorNodeLogs) {
					m.logger.Info("Discrepancy found", zap.String("PublicKey", validatorPubKey), zap.Int("BeaconCount", len(validatorBeaconLogs)), zap.Int("NodeCount", len(validatorNodeLogs)))
					return fmt.Errorf("discrepancy for pubkey %s in %s: expected %d, got %d", validatorPubKey, nodeContainer, len(validatorBeaconLogs), len(validatorNodeLogs))
				}
			}
		}
	case Slashable:
		beaconCriteria = []string{origMessage, slashableMessage}
		nodeCriteria = []string{slashableMatchMessage}
		// For slashable attestations, the node count must match the beacon count exactly.
		discrepancyCheck = func(beaconCount, nodeCount []any) bool {
			return len(beaconCount) != len(nodeCount)
		}

		// Extract logs from the beaconProxyContainer.
		beaconLogs, err := m.processDockerLogs(ctx, beaconProxyContainer, beaconCriteria)
		if err != nil {
			return err
		}

		beaconLogsByPublicKey, err := m.organizeLogsByPubKey(beaconLogs, beaconProxyContainer)
		if err != nil {
			return err
		}

		// Verify corresponding logs in each SSV node container match the validator public key.
		for _, nodeContainer := range ssvNodesContainers {
			nodeLogs, err := m.processDockerLogs(ctx, nodeContainer, nodeCriteria)
			if err != nil {
				return err
			}
			nodeLogsByPublicKey, err := m.organizeLogsByPubKey(nodeLogs, nodeContainer)
			if err != nil {
				return err
			}

			// Compare the counts for each public key between beacon proxy and node container.
			for validatorPubKey, validatorBeaconLogs := range beaconLogsByPublicKey {
				validatorNodeLogs, exists := nodeLogsByPublicKey[validatorPubKey]
				if !exists || discrepancyCheck(validatorBeaconLogs, validatorNodeLogs) {
					m.logger.Info("Discrepancy found", zap.String("PublicKey", validatorPubKey), zap.Int("BeaconCount", len(validatorBeaconLogs)), zap.Int("NodeCount", len(validatorNodeLogs)))
					return fmt.Errorf("discrepancy for pubkey %s in %s: expected %d, got %d", validatorPubKey, nodeContainer, len(validatorBeaconLogs), len(validatorNodeLogs))
				}
			}
		}

	case RsaVerification:
		beaconCriteria = nil
		nodeCriteria = []string{rsaVerificationErrorMessage}

		for _, nodeContainer := range ssvNodesContainers {
			nodeLogs, err := m.processDockerLogs(ctx, nodeContainer, nodeCriteria)
			if err != nil {
				return err
			}

			nodeOpids := make(map[string]map[int]bool)
			opidRegex := regexp.MustCompile(`opid: (\d+)`)

			for _, log := range nodeLogs {
				errorMsg, ok := log["error"].(string)
				if !ok {
					return fmt.Errorf("log entry without error message")
				}

				// Extract opid from the error message
				matches := opidRegex.FindStringSubmatch(errorMsg)
				if len(matches) < 2 {
					return fmt.Errorf("could not extract opid from error", errorMsg)
				}

				opid, err := strconv.Atoi(matches[1])
				if err != nil || opid < 1 || opid > 4 {
					return fmt.Errorf("Invalid or out-of-range opid extracted: %s\n", matches[1])
				}

				// Initialize the set for this node if it doesn't exist
				if _, exists := nodeOpids[nodeContainer]; !exists {
					nodeOpids[nodeContainer] = make(map[int]bool)
				}

				// Record this opid for the node, checking for duplicates
				if _, exists := nodeOpids[nodeContainer][opid]; exists {
					return fmt.Errorf("Duplicate opid %d found for node %s\n", opid, nodeContainer)
				}
				nodeOpids[nodeContainer][opid] = true
			}

			// Verify each node has exactly 4 logs with distinct opids 1, 2, 3, 4
			for node, opids := range nodeOpids {
				if len(opids) != 4 {
					return fmt.Errorf("Discrepancy found for node %s: expected 4 unique opids, got %d\n", node, len(opids))
				}
			}
		}
	default:
		return fmt.Errorf("unknown attestation type: %s", m.mode)

	}
	return nil
}

func (m *Matcher) waitForStartCondition(pctx context.Context, condition []string, targetContainer string) (string, error) {
	ctx, cancel := context.WithCancel(pctx)
	defer cancel()

	conditionLog := ""

	m.logger.Debug("Waiting for start condition at target", zap.String("target", targetContainer), zap.Strings("condition", condition))
	ch := make(chan string, 1024)
	go func() {
		for log := range ch {
			if logs.GrepLine(log, condition) {
				conditionLog = log
				m.logger.Info("Start condition arrived", zap.Strings("log_message", condition))
				cancel()
			}
		}
	}()
	// TODO: either apply logs collection on each container or fan in the containers to one log stream
	err := docker.StreamDockerLogs(ctx, m.cli, targetContainer, ch)
	if err != nil && !errors.Is(err, context.Canceled) {
		m.logger.Error("Log streaming stopped with err ", zap.Error(err))
		return conditionLog, err
	}
	return conditionLog, nil
}

// Retrieves and processes Docker logs based on matching strings.
func (m *Matcher) processDockerLogs(ctx context.Context, containerName string, matchStrings []string) ([]map[string]any, error) {
	m.logger.Info("process logs", zap.String("target", containerName), zap.Strings("match_string", matchStrings))
	if matchStrings == nil {
		return nil, nil
	}

	res, err := docker.DockerLogs(ctx, m.cli, containerName, "")
	if err != nil {
		return nil, err
	}

	var processedLogs []map[string]any
	res.Grep(matchStrings).ParseAll(func(log string) (map[string]any, error) {
		var result map[string]any
		err := json.Unmarshal([]byte(log), &result)
		if err != nil {
			m.logger.Error("failed to Unmarshal log: ", zap.Error(err))
			return nil, err
		}
		// Process the pubkey as before.
		if pubkey, ok := result["pubkey"].(string); ok {
			if strings.HasPrefix(pubkey, "0x") {
				pubkey = strings.TrimPrefix(pubkey, "0x")
			}
			result["pubkey"] = pubkey
		}

		processedLogs = append(processedLogs, result)
		return result, nil
	})

	return processedLogs, nil
}

func (m *Matcher) organizeLogsByPubKey(logs []map[string]any, containerName string) (map[string][]any, error) {
	publicKeyLogs := make(map[string][]any)
	for _, logMap := range logs {
		if pubkey, ok := logMap["pubkey"].(string); ok {
			publicKeyLogs[pubkey] = append(publicKeyLogs[pubkey], logMap)
		}
	}

	m.logger.Info("organize logs by public key", zap.Int("count", len(publicKeyLogs)), zap.String("target", containerName))
	return publicKeyLogs, nil
}
