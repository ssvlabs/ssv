package logs_catcher

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ssvlabs/ssv-spec/qbft"
	"github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/e2e/logs_catcher/docker"
	"github.com/ssvlabs/ssv/e2e/logs_catcher/logs"
	"github.com/ssvlabs/ssv/e2e/logs_catcher/parser"
	"github.com/ssvlabs/ssv/protocol/v2/message"
)

const (
	targetContainer = "ssv-node-1"

	verifySignatureErr           = "failed processing consensus message: could not process msg: invalid signed message: msg signature invalid: failed to verify signature"
	reconstructSignatureErr      = "could not reconstruct post consensus signature: could not reconstruct beacon sig: failed to verify reconstruct signature: could not reconstruct a valid signature"
	pastRoundErr                 = "failed processing consensus message: could not process msg: invalid signed message: past round"
	reconstructSignaturesSuccess = "reconstructed partial signatures"
	submittedAttSuccess          = "✅ successfully submitted attestation"
	gotDutiesSuccess             = "🗂 got duties"

	msgHeightField        = "\"msg_height\":%d"
	msgRoundField         = "\"msg_round\":%d"
	msgTypeField          = "\"msg_type\":\"%s\""
	consensusMsgTypeField = "\"consensus_msg_type\":%d"
	signersField          = "\"signers\":[%d]"
	errorField            = "\"error\":\"%s\""
	dutyIDField           = "\"duty_id\":\"%s\""
	roleField             = "\"role\":\"%s\""
	slotField             = "\"slot\":%d"
)

type logCondition struct {
	role             string
	slot             phase0.Slot
	round            int
	msgType          types.MsgType
	consensusMsgType qbft.MessageType
	signer           types.OperatorID
	error            string
}

type CorruptedShare struct {
	ValidatorIndex  uint64           `json:"validator_index"`
	ValidatorPubKey string           `json:"validator_pub_key"`
	OperatorID      types.OperatorID `json:"operator_id"`
}

func VerifyBLSSignature(pctx context.Context, logger *zap.Logger, cli DockerCLI, share *CorruptedShare) error {
	startctx, startc := context.WithTimeout(pctx, time.Second*12*35) // wait max 35 slots
	defer startc()

	validatorIndex := fmt.Sprintf("v%d", share.ValidatorIndex)
	conditionLog, err := StartCondition(startctx, logger, []string{gotDutiesSuccess, validatorIndex}, targetContainer, cli)
	if err != nil {
		return fmt.Errorf("failed to start condition: %w", err)
	}

	dutyID, dutySlot, err := ParseAndExtractDutyInfo(conditionLog, validatorIndex)
	if err != nil {
		return fmt.Errorf("failed to parse and extract duty info: %w", err)
	}
	logger.Debug("Duty ID: ", zap.String("duty_id", dutyID))

	committee := []*types.CommitteeMember{
		{OperatorID: 1},
		{OperatorID: 2},
		{OperatorID: 3},
		{OperatorID: 4},
	}
	leader := DetermineLeader(dutySlot, committee)
	logger.Debug("Leader: ", zap.Uint64("leader", leader))

	_, err = StartCondition(startctx, logger, []string{submittedAttSuccess, share.ValidatorPubKey}, targetContainer, cli)
	if err != nil {
		return fmt.Errorf("failed to start condition: %w", err)
	}

	ctx, c := context.WithCancel(pctx)
	defer c()

	return ProcessLogs(ctx, logger, cli, committee, leader, dutyID, dutySlot, share.OperatorID)
}

func ParseAndExtractDutyInfo(conditionLog string, corruptedValidatorIndex string) (string, phase0.Slot, error) {
	parsedData, err := parser.JSON(conditionLog)
	if err != nil {
		return "", 0, fmt.Errorf("failed to parse log string: %w", err)
	}

	dutyID, err := extractDutyID(parsedData, corruptedValidatorIndex)
	if err != nil {
		return "", 0, fmt.Errorf("failed to extract duty id: %w", err)
	}

	dutySlot, err := extractDutySlot(dutyID)
	if err != nil {
		return "", 0, fmt.Errorf("failed to extract duty slot: %w", err)
	}

	return dutyID, dutySlot, nil
}

func DetermineLeader(dutySlot phase0.Slot, committee []*types.CommitteeMember) types.OperatorID {
	share := &types.Operator{
		Committee: committee,
	}
	leader := qbft.RoundRobinProposer(&qbft.State{Height: qbft.Height(dutySlot), Share: share}, qbft.FirstRound)

	return leader
}

func ProcessLogs(ctx context.Context, logger *zap.Logger, cli DockerCLI, committee []*types.CommitteeMember, leader types.OperatorID, dutyID string, dutySlot phase0.Slot, corruptedOperator types.OperatorID) error {
	for _, operator := range committee {
		target := fmt.Sprintf("ssv-node-%d", operator.OperatorID)
		if operator.OperatorID == corruptedOperator {
			err := processCorruptedOperatorLogs(ctx, logger, cli, dutyID, dutySlot, corruptedOperator, target)
			if err != nil {
				return fmt.Errorf("failed to process corrupted operator logs: %w", err)
			}
		} else {
			err := processNonCorruptedOperatorLogs(ctx, logger, cli, leader, dutySlot, corruptedOperator, target)
			if err != nil {
				return fmt.Errorf("failed to process non corrupted operator logs: %w", err)
			}
		}
	}
	return nil
}

func processCorruptedOperatorLogs(ctx context.Context, logger *zap.Logger, cli DockerCLI, dutyID string, dutySlot phase0.Slot, corruptedOperator types.OperatorID, target string) error {
	successConditions := []string{
		reconstructSignaturesSuccess,
		fmt.Sprintf(dutyIDField, dutyID),
	}
	failConditions := []string{
		fmt.Sprintf(roleField, types.BNRoleAttester.String()),
		fmt.Sprintf(slotField, dutySlot),
		fmt.Sprintf(msgTypeField, message.MsgTypeToString(types.SSVPartialSignatureMsgType)),
		fmt.Sprintf(errorField, reconstructSignatureErr),
	}
	return matchDualConditionLog(ctx, logger, cli, corruptedOperator, successConditions, failConditions, target)
}

func processNonCorruptedOperatorLogs(ctx context.Context, logger *zap.Logger, cli DockerCLI, leader types.OperatorID, dutySlot phase0.Slot, corruptedOperator types.OperatorID, target string) error {
	var conditions []logCondition
	if leader == corruptedOperator {
		conditions = []logCondition{
			{
				role:             types.BNRoleAttester.String(),
				slot:             dutySlot,
				round:            1,
				msgType:          types.SSVConsensusMsgType,
				consensusMsgType: qbft.ProposalMsgType,
				signer:           corruptedOperator,
				error:            verifySignatureErr,
			},
			{
				role:             types.BNRoleAttester.String(),
				slot:             dutySlot,
				round:            1,
				msgType:          types.SSVConsensusMsgType,
				consensusMsgType: qbft.PrepareMsgType,
				signer:           corruptedOperator,
				error:            pastRoundErr,
			},
			{
				role:             types.BNRoleAttester.String(),
				slot:             dutySlot,
				round:            2,
				msgType:          types.SSVConsensusMsgType,
				consensusMsgType: qbft.RoundChangeMsgType,
				signer:           corruptedOperator,
				error:            verifySignatureErr,
			},
			{
				role:             types.BNRoleAttester.String(),
				slot:             dutySlot,
				round:            2,
				msgType:          types.SSVConsensusMsgType,
				consensusMsgType: qbft.PrepareMsgType,
				signer:           corruptedOperator,
				error:            verifySignatureErr,
			},
			// TODO: handle decided failed signature
		}
	} else {
		conditions = []logCondition{
			{
				role:             types.BNRoleAttester.String(),
				slot:             dutySlot,
				round:            1,
				msgType:          types.SSVConsensusMsgType,
				consensusMsgType: qbft.PrepareMsgType,
				signer:           corruptedOperator,
				error:            verifySignatureErr,
			},
			{
				role:             types.BNRoleAttester.String(),
				slot:             dutySlot,
				round:            1,
				msgType:          types.SSVConsensusMsgType,
				consensusMsgType: qbft.CommitMsgType,
				signer:           corruptedOperator,
				error:            verifySignatureErr,
			},
			// TODO: handle decided failed signature
		}
	}

	for _, condition := range conditions {
		if err := matchCondition(ctx, logger, cli, condition, target); err != nil {
			return fmt.Errorf("failed to match condition: %w", err)
		}
	}
	return nil
}

func matchCondition(ctx context.Context, logger *zap.Logger, cli DockerCLI, condition logCondition, target string) error {
	conditionStrings := []string{
		fmt.Sprintf(roleField, condition.role),
		fmt.Sprintf(msgHeightField, condition.slot),
		fmt.Sprintf(msgRoundField, condition.round),
		fmt.Sprintf(msgTypeField, message.MsgTypeToString(condition.msgType)),
		fmt.Sprintf(consensusMsgTypeField, condition.consensusMsgType),
		fmt.Sprintf(signersField, condition.signer),
		fmt.Sprintf(errorField, condition.error),
	}
	return matchSingleConditionLog(ctx, logger, cli, conditionStrings, target)
}

func matchSingleConditionLog(ctx context.Context, logger *zap.Logger, cli DockerCLI, first []string, target string) error {
	res, err := docker.DockerLogs(ctx, cli, target, "")
	if err != nil {
		return err
	}

	filteredLogs := res.Grep(first)
	logger.Info("matched", zap.Int("count", len(filteredLogs)), zap.String("target", target), zap.Strings("match_string", first))

	if len(filteredLogs) != 1 {
		return fmt.Errorf("found non matching messages on %v, want %v got %v", target, 1, len(filteredLogs))
	}

	return nil
}

func matchDualConditionLog(ctx context.Context, logger *zap.Logger, cli DockerCLI, corruptedOperator types.OperatorID, success []string, fail []string, target string) error {
	res, err := docker.DockerLogs(ctx, cli, target, "")
	if err != nil {
		return err
	}

	filteredLogs := res.Grep(success)
	if len(filteredLogs) > 1 {
		return fmt.Errorf("found too many matching messages on %v, got %v", target, len(filteredLogs))
	}

	if len(filteredLogs) == 1 {
		logger.Info("matched", zap.Int("count", len(filteredLogs)), zap.String("target", target), zap.Strings("match_string", success), zap.String("RAW", filteredLogs[0]))
		parsedData, err := parser.JSON(filteredLogs[0])
		if err != nil {
			return fmt.Errorf("error parsing log string: %v", err)
		}

		signers, err := extractSigners(parsedData)
		if err != nil {
			return fmt.Errorf("error extracting signers: %v", err)
		}

		for _, signer := range signers {
			if signer == corruptedOperator {
				return fmt.Errorf("found corrupted signer %v on successful signers %v", corruptedOperator, signers)
			}
		}
	} else {
		filteredLogs = res.Grep(fail)
		logger.Info("matched", zap.Int("count", len(filteredLogs)), zap.String("target", target), zap.Strings("match_string", fail))

		if len(filteredLogs) != 1 {
			return fmt.Errorf("found non matching messages on %v, want %v got %v", target, 1, len(filteredLogs))
		}
	}

	return nil
}

func extractDutyID(parsedData logs.ParsedLine, searchPart string) (string, error) {
	if duties, ok := parsedData["duties"].(string); ok {
		dutyList := strings.Split(duties, ", ")
		for _, duty := range dutyList {
			if strings.Contains(duty, searchPart) {
				return duty, nil
			}
		}
	}
	return "", fmt.Errorf("no duty id found for %v", searchPart)
}

func extractDutySlot(dutyID string) (phase0.Slot, error) {
	// Extracting the part after "s" and before the next "-"
	parts := strings.Split(dutyID, "-")
	for _, part := range parts {
		if strings.HasPrefix(part, "s") {
			slotStr := strings.TrimPrefix(part, "s")
			slotInt, err := strconv.Atoi(slotStr)
			if err != nil {
				return 0, fmt.Errorf("failed to parse duty slot to int: %w", err)
			}
			return phase0.Slot(slotInt), nil
		}
	}
	return 0, fmt.Errorf("no duty slot found for %v", dutyID)
}

func extractSigners(parsedData logs.ParsedLine) ([]types.OperatorID, error) {
	if signers, ok := parsedData["signers"].([]interface{}); ok {
		signerIDs := make([]types.OperatorID, len(signers))
		for i, signer := range signers {
			if signerNum, ok := signer.(float64); ok { // JSON numbers are parsed as float64
				signerIDs[i] = types.OperatorID(signerNum)
			} else {
				return nil, fmt.Errorf("failed to parse signer to int: %v", signer)
			}
		}
		return signerIDs, nil
	}
	return nil, fmt.Errorf("no signers found")
}
