package logs_catcher

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"errors"

	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/e2e/logs_catcher/docker"
	"github.com/ssvlabs/ssv/e2e/logs_catcher/logs"
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

// Todo: match messages based on fields. ex: take all X messages from target one,
// 	extract pubkey field and get all matching messages with this pubkey field on target two.

// The revised matchMessages function with reduced auxiliary functions.
func matchMessages(ctx context.Context, logger *zap.Logger, cli DockerCLI, firstMatches, secondMatches []string) error {
	// Process logs for the beaconProxyContainer.
	initialCounts, err := dockerLogsByPubKey(ctx, logger, cli, beaconProxyContainer, firstMatches)
	if err != nil {
		return err
	}

	fmt.Printf("<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<her1>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>")
	// Print initial public key counts.
	for pubkey, count := range initialCounts {
		fmt.Printf("Public key: %s, Count: %d\n", pubkey, count)
	}
	fmt.Printf("<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<her2>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>")

	// Iterate over SSV node containers and compare public key counts.
	for _, ssvNodeContainer := range ssvNodesContainers {
		currentCounts, err := dockerLogsByPubKey(ctx, logger, cli, ssvNodeContainer, secondMatches)
		if err != nil {
			return err
		}
		fmt.Printf("<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<her3>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>")
		for pubkey1, count1 := range currentCounts {
			fmt.Printf("container: %s, Public key: %s, Count: %d\n", ssvNodeContainer, pubkey1, count1)
		}
		fmt.Printf("<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<<her4>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>")

		// Compare counts for each public key.
		for pubkey, initialCount := range initialCounts {
			currentCount, exists := currentCounts[pubkey]
			if !exists || currentCount != initialCount {
				// Print initial public key counts.
				logger.Info("fuckkkkkk>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>")
				logger.Info(pubkey)
				fmt.Println(currentCount)
				fmt.Println(secondMatches)
				logger.Debug("Key exists", zap.Bool("exists", exists))
				logger.Info("fuckkkkkk>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>>")
				return fmt.Errorf("found non matching messages for pubkey %s in %s: expected %d, got %d", pubkey, ssvNodeContainer, initialCount, currentCount)
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
	if err := matchMessages(ctx, logger, cli, []string{origMessage, slashableMessage}, []string{slashableMatchMessage}); err != nil {
		return err
	}

	// find non-slashable validators successfully submitting (all first round + 1 for second round)
	if err := matchMessages(ctx, logger, cli, []string{origMessage, nonSlashableMessage}, []string{nonSlashableMatchMessage}); err != nil {
		return err
	}

	//TODO: match proposals
	return nil
}
