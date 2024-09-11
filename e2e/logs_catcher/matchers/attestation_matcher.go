package matchers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ssvlabs/ssv/e2e/logs_catcher"
	"github.com/ssvlabs/ssv/e2e/logs_catcher/docker"
	"github.com/ssvlabs/ssv/e2e/logs_catcher/logs"
	"go.uber.org/zap"
	"strings"
	"time"
)

type DutyMatcher struct {
	pastAlanFork bool
	logger       *zap.Logger
	ctx          context.Context
	cli          logs_catcher.DockerCLI
}

func NewDutyMatcher(logger *zap.Logger, cli logs_catcher.DockerCLI, ctx context.Context, pastAlanFork bool) *DutyMatcher {
	return &DutyMatcher{
		cli:          cli,
		ctx:          ctx,
		logger:       logger,
		pastAlanFork: pastAlanFork,
	}
}

func (d *DutyMatcher) Match() error {
	// Wait for the start condition (e.g., end of epoch log)
	if _, err := d.startCondition([]string{logs_catcher.EndOfEpochLog}, logs_catcher.BeaconProxyContainer); err != nil {
		return err
	}

	//fromLogs, err := d.getEndEpochFromLogs([]string{logs_catcher.EndOfEpochLog})
	//if err != nil {
	//	return err
	//}

	// find slashable attestation not signing for each slashable validator
	if err := d.validateAttestationLogs(logs_catcher.Slashable); err != nil {
		return err
	}
	// find non-slashable validators successfully submitting (all first round + 1 for second round)
	if err := d.validateAttestationLogs(logs_catcher.NonSlashable); err != nil {
		return err
	}

	// TODO: match proposals

	return nil
}

func (d *DutyMatcher) startCondition(condition []string, targetContainer string) (string, error) {
	// Create a context with a timeout of 4 epochs
	ctx, cancel := context.WithTimeout(d.ctx, 4*time.Minute)
	defer cancel()

	conditionLog := ""

	d.logger.Debug("Waiting for start condition at target", zap.String("target", targetContainer), zap.Strings("condition", condition))
	ch := make(chan string, 1024)
	go func() {
		for log := range ch {
			if logs.GrepLine(log, condition) {
				conditionLog = log
				d.logger.Info("Start condition arrived", zap.Strings("log_message", condition))
				cancel()
			}
		}
	}()
	// TODO: either apply logs collection on each container or fan in the containers to one log stream
	err := docker.StreamDockerLogs(ctx, d.cli, targetContainer, ch)
	if err != nil && !errors.Is(err, context.Canceled) {
		d.logger.Error("Log streaming stopped with err ", zap.Error(err))
		return conditionLog, err
	}
	return conditionLog, nil
}

// testDuty performs a generic validation of attestation logs by comparing entries across beacon proxy and SSV node containers.
// It's designed to handle both non-slashable and slashable attestation log validations.
func (d *DutyMatcher) validateAttestationLogs(attestationType string) error {
	var beaconCriteria, nodeCriteria []string
	var discrepancyCheck func(beaconCount, nodeCount int) bool

	switch attestationType {
	case logs_catcher.Slashable:
		beaconCriteria = []string{logs_catcher.SetUpValidatorLog, logs_catcher.SlashableMessage}
		nodeCriteria = []string{logs_catcher.SlashableAttestationLog}
		// For slashable attestations, the node count must match the beacon count exactly.
		discrepancyCheck = func(beaconCount, nodeCount int) bool {
			return nodeCount != beaconCount
		}
	case logs_catcher.NonSlashable:
		beaconCriteria = []string{logs_catcher.SetUpValidatorLog, logs_catcher.NonSlashableMessage}
		nodeCriteria = []string{logs_catcher.SuccessfullySubmittedAttestationLog, fmt.Sprintf("\"duty_id\":\"ATTESTER-e%s", 123)}
		discrepancyCheck = func(beaconCount, nodeCount int) bool {
			return nodeCount != beaconCount
		}
	default:
		return fmt.Errorf("unknown attestation type: %s", attestationType)
	}

	// Extract and count logs from the beaconProxyContainer based on validator public key.
	beaconLogsBy, err := d.dockerLogsByVersion(logs_catcher.BeaconProxyContainer, beaconCriteria, true)
	if err != nil {
		return err
	}

	var searchBy string
	if d.pastAlanFork {
		searchBy = "validator_index"
	} else {
		searchBy = "public_key"
	}

	// Verify corresponding logs in each SSV node container match the validator public key.
	for _, nodeContainer := range logs_catcher.SSVNodesContainers {
		nodeLogsBy, err := d.dockerLogsByVersion(nodeContainer, nodeCriteria, false)
		if err != nil {
			return err
		}

		// Compare the counts for each public key between beacon proxy and node container.
		for validatorKey, validatorBeaconLogs := range beaconLogsBy {
			validatorNodeLogs, exists := nodeLogsBy[validatorKey]
			beaconCount := len(validatorBeaconLogs) // Get the count of beacon logs
			nodeCount := len(validatorNodeLogs)     // Get the count of node logs
			if !exists || discrepancyCheck(beaconCount, nodeCount) {
				d.logger.Info("Discrepancy found",
					zap.Strings("beaconCriteria", beaconCriteria),
					zap.Strings("nodeCriteria", nodeCriteria),
					zap.String(searchBy, validatorKey),
					zap.Int("BeaconCount", beaconCount),
					zap.Int("NodeCount", nodeCount))
				return fmt.Errorf("discrepancy for %s %s in %s: expected %d, got %d", searchBy, validatorKey, nodeContainer, beaconCount, nodeCount)
			}
		}
	}

	return nil
}

func (d *DutyMatcher) dockerLogsByVersion(containerName string, matchStrings []string, beaconNode bool) (map[string][]any, error) {
	if d.pastAlanFork {
		return d.dockerLogsByValidatorIndex(containerName, matchStrings, beaconNode)
	}
	return d.dockerLogsByPubKey(containerName, matchStrings)
}

func (d *DutyMatcher) dockerLogsByField(containerName string, matchStrings []string, processFunc func(map[string]any) string) (map[string][]any, error) {
	containerLogs, err := docker.DockerLogs(d.ctx, d.cli, containerName, "")
	if err != nil {
		return nil, err
	}
	grepped := containerLogs.Grep(matchStrings).ParseAll(func(log string) (map[string]any, error) {
		var result logs.ParsedLine

		err := json.Unmarshal([]byte(log), &result)
		if err != nil {
			return nil, err
		}

		result["key"] = processFunc(result)
		return result, nil
	})

	fieldLogs := make(map[string][]any)
	for _, logMap := range grepped {
		if fieldValue, ok := logMap["key"].(string); ok {
			fieldLogs[fieldValue] = append(fieldLogs[fieldValue], logMap)
		}
	}

	d.logger.Info("matched",
		zap.String("target", containerName),
		zap.Int("count_grepped", len(grepped)),
		zap.Int("count_fieldLogs", len(fieldLogs)),
		zap.Strings("match_string", matchStrings),
	)

	return fieldLogs, nil
}

func (d *DutyMatcher) getEndEpochFromLogs(matchStrings []string) (map[string][]any, error) {
	containerLogs, err := docker.DockerLogs(d.ctx, d.cli, logs_catcher.BeaconProxyContainer, "")
	if err != nil {
		return nil, err
	}
	grepped := containerLogs.Grep(matchStrings)
	for index, value := range grepped {
		fmt.Printf("Index %d: %s\n", index, value)
	}
	return nil, nil
}

func (d *DutyMatcher) dockerLogsByValidatorIndex(containerName string, matchStrings []string, beaconNode bool) (map[string][]any, error) {
	if beaconNode {
		processFunc := func(result map[string]any) string {
			if validatorIndex, ok := result["validator_index"].(float64); ok {
				validatorIndexStr := fmt.Sprintf("%.0f", validatorIndex)
				return validatorIndexStr
			}
			return ""
		}
		return d.dockerLogsByField(containerName, matchStrings, processFunc)
	}
	processFunc := func(result map[string]any) string {
		if duties, ok := result["duties"].(string); ok {
			if strings.Contains(duties, "-v") {
				parts := strings.Split(duties, "-v")
				if len(parts) > 1 {
					return strings.TrimPrefix(parts[1], "v")
				}
			}
		}
		return ""
	}
	return d.dockerLogsByField(containerName, matchStrings, processFunc)
}

func (d *DutyMatcher) dockerLogsByPubKey(containerName string, matchStrings []string) (map[string][]any, error) {
	processFunc := func(result map[string]any) string {
		if pubkey, ok := result["pubkey"].(string); ok {
			if strings.HasPrefix(pubkey, "0x") {
				return strings.TrimPrefix(pubkey, "0x")
			}
			return pubkey
		}
		return ""
	}
	return d.dockerLogsByField(containerName, matchStrings, processFunc)
}
