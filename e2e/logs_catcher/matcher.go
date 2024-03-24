package logs_catcher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
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
	mode   SubMode
	logger *zap.Logger
	cli    *client.Client
}

func NewLogMatcher(logger *zap.Logger, cli *client.Client, mode SubMode) *Matcher {
	return &Matcher{mode: mode, cli: cli, logger: logger}
}

// Match starts log catching based on the Matcher mode
func (m *Matcher) Match(pctx context.Context) error {
	ctx, cancel := context.WithTimeout(pctx, 24*time.Minute) // 24 minutes for 4 epochs
	defer cancel()

	if _, err := m.waitForStartCondition(ctx, []string{waitFor}, beaconProxyContainer); err != nil {
		return err
	}
	return m.testDuty(ctx)
}

// TestRestartNode waits until first successfully attestation then restart the nodes and look for miss attestation
func (m *Matcher) TestRestartNode(pctx context.Context) error {
	ctx, c := context.WithCancel(pctx)
	defer c()

	_, err := m.waitForStartCondition(ctx, []string{reconstructSignaturesSuccess}, ssvNodesContainers[0])
	if err != nil {
		return err
	}
	_, cancel := context.WithCancel(pctx)
	defer cancel()

	//err = m.restartContainer(beaconProxyContainer)
	//if err != nil {
	//	return err
	//}
	//println("<<<<<<before timeout>>>>>")
	//time.Sleep(time.Duration(2) * time.Second)
	//println("<<<<<<after timeout>>>>>")
	for _, ssvNode := range ssvNodesContainers {
		err := m.restartContainer(ssvNode)
		if err != nil {
			return err
		}
	}

	return nil
}

func (m *Matcher) restartContainer(containerName string) error {
	// Context with a timeout to avoid hanging restarts
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Restart the container
	err := m.cli.ContainerRestart(ctx, containerName, container.StopOptions{})
	if err != nil {
		return fmt.Errorf("error restarting container %s: %w", containerName, err)
	}

	fmt.Printf("Successfully restarted container %s\n", containerName)
	return nil
}

func (m *Matcher) testDuty(ctx context.Context) error {
	switch m.mode {
	case NonSlashable, Slashable:
		return m.validateAttestationLogs(ctx)
	case RsaVerification:
		return m.validateRSAVerificationLogs(ctx)
	default:
		return fmt.Errorf("unknown mode: %s", m.mode)
	}
}

func (m *Matcher) validateAttestationLogs(ctx context.Context) error {
	beaconCriteria, nodeCriteria, discrepancyCheck := m.getModeCriteria()

	// Extract and organize logs from the beacon proxy container
	beaconLogsByPublicKey, err := m.logsByPublicKey(ctx, beaconProxyContainer, beaconCriteria)
	if err != nil {
		return err
	}

	// Verify and compare logs in SSV node containers
	for _, nodeContainer := range ssvNodesContainers {
		nodeLogsByPublicKey, err := m.logsByPublicKey(ctx, nodeContainer, nodeCriteria)
		if err != nil {
			return err
		}

		for validatorPubKey, beaconLogs := range beaconLogsByPublicKey {
			nodeLogs, exists := nodeLogsByPublicKey[validatorPubKey]
			if !exists || discrepancyCheck(beaconLogs, nodeLogs) {
				m.logger.Info("Discrepancy found", zap.String("PublicKey", validatorPubKey), zap.Int("BeaconCount", len(beaconLogs)), zap.Int("NodeCount", len(nodeLogs)))
				return fmt.Errorf("discrepancy for pubkey %s in %s: expected %d, got %d", validatorPubKey, nodeContainer, len(beaconLogs), len(nodeLogs))
			}
		}
	}
	return nil
}

func (m *Matcher) validateRSAVerificationLogs(ctx context.Context) error {
	nodeCriteria := []string{rsaVerificationErrorMessage}
	opidRegex := regexp.MustCompile(`opid: (\d+)`)

	for _, nodeContainer := range ssvNodesContainers {
		nodeLogs, err := m.processDockerLogs(ctx, nodeContainer, nodeCriteria)
		if err != nil {
			return err
		}
		if err := m.validateOpids(nodeLogs, opidRegex, nodeContainer); err != nil {
			return err
		}
	}
	return nil
}

func (m *Matcher) getModeCriteria() (beaconCriteria, nodeCriteria []string, discrepancyCheck func(beaconCount, nodeCount []any) bool) {
	switch m.mode {
	case NonSlashable:
		beaconCriteria = []string{origMessage, nonSlashableMessage}
		nodeCriteria = []string{nonSlashableMatchMessage}
		discrepancyCheck = func(beaconCount, nodeCount []any) bool { return len(nodeCount) != 2 }
	case Slashable:
		beaconCriteria = []string{origMessage, slashableMessage}
		nodeCriteria = []string{slashableMatchMessage}
		discrepancyCheck = func(beaconCount, nodeCount []any) bool { return len(beaconCount) != len(nodeCount) }
	}
	return
}

func (m *Matcher) waitForStartCondition(ctx context.Context, condition []string, targetContainer string) (string, error) {
	ch := make(chan string, 1024)
	found := make(chan bool)

	m.logger.Debug("Waiting for start condition", zap.String("target", targetContainer), zap.Strings("condition", condition))

	var conditionLog string
	go func() {
		for log := range ch {
			if logs.GrepLine(log, condition) {
				conditionLog = log
				m.logger.Info("Start condition met", zap.String("log_message", log))
				found <- true
				return
			}
		}
	}()

	go func() {
		if err := docker.StreamDockerLogs(ctx, m.cli, targetContainer, ch); err != nil && !errors.Is(err, context.Canceled) {
			m.logger.Error("Log streaming error", zap.Error(err))
		}
	}()

	select {
	case <-ctx.Done():
		return conditionLog, ctx.Err()
	case <-found:
		return conditionLog, nil
	}
}

func (m *Matcher) processDockerLogs(ctx context.Context, containerName string, matchStrings []string) ([]map[string]any, error) {
	if matchStrings == nil {
		return nil, nil // No matching strings provided
	}

	m.logger.Info("Processing Docker logs", zap.String("container", containerName), zap.Strings("criteria", matchStrings))
	res, err := docker.DockerLogs(ctx, m.cli, containerName, "")
	if err != nil {
		return nil, err
	}

	var processedLogs []map[string]any
	for _, log := range res.Grep(matchStrings) {
		var logEntry map[string]any
		if err := json.Unmarshal([]byte(log), &logEntry); err != nil {
			m.logger.Error("Failed to unmarshal log", zap.Error(err))
			continue // Skip this log entry on error
		}
		processPubKey(logEntry) // Process the public key if present
		processedLogs = append(processedLogs, logEntry)
	}

	return processedLogs, nil
}

func (m *Matcher) logsByPublicKey(ctx context.Context, containerName string, matchStrings []string) (map[string][]any, error) {
	logss, err := m.processDockerLogs(ctx, containerName, matchStrings)
	if err != nil {
		return nil, err
	}

	publicKeyLogs := make(map[string][]any)
	for _, log := range logss {
		if pubkey, ok := log["pubkey"].(string); ok {
			publicKeyLogs[pubkey] = append(publicKeyLogs[pubkey], log)
		}
	}
	m.logger.Info("Logs organized by public key", zap.Int("count", len(publicKeyLogs)), zap.String("container", containerName))
	return publicKeyLogs, nil
}

func processPubKey(logEntry map[string]any) {
	if pubkey, ok := logEntry["pubkey"].(string); ok && strings.HasPrefix(pubkey, "0x") {
		logEntry["pubkey"] = strings.TrimPrefix(pubkey, "0x")
	}
}

func (m *Matcher) validateOpids(nodeLogs []map[string]any, opidRegex *regexp.Regexp, nodeContainer string) error {
	nodeOpids := make(map[int]bool)
	for _, log := range nodeLogs {
		if errorMsg, ok := log["error"].(string); ok {
			matches := opidRegex.FindStringSubmatch(errorMsg)
			if len(matches) < 2 {
				continue // No opid found, skip this log
			}
			opid, err := strconv.Atoi(matches[1])
			if err != nil || !validOpid(opid) {
				continue // Invalid opid, skip this log
			}
			if nodeOpids[opid] {
				return fmt.Errorf("duplicate opid %d in node %s", opid, nodeContainer)
			}
			nodeOpids[opid] = true
		}
	}
	if len(nodeOpids) != 4 {
		return fmt.Errorf("expected 4 unique opids in node %s, got %d", nodeContainer, len(nodeOpids))
	}
	return nil
}

func validOpid(opid int) bool {
	return opid >= 1 && opid <= 4
}
