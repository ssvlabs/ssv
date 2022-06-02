package tests
//
//import (
//	"testing"
//
//	"go.uber.org/zap/zapcore"
//
//	"github.com/bloxapp/ssv/automation/qbft/runner"
//	"github.com/bloxapp/ssv/automation/qbft/scenarios"
//	"github.com/bloxapp/ssv/utils/logex"
//)
//
//func Test_Automation_QBFTScenarios(t *testing.T) {
//	logger := logex.Build("simulation", zapcore.DebugLevel, nil)
//	scenariosToRun := []string{
//		scenarios.OnForkV1Scenario,
//	}
//
//	for _, s := range scenariosToRun {
//		scenario := scenarios.NewScenario(s, logger)
//		runner.Start(logger, scenario, scenarios.QBFTScenarioBootstrapper())
//	}
//}
