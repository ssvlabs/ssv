package qbft

import "github.com/bloxapp/ssv/scripts/spec_align_report/utils"

// Controller mapping

func ControllerSet() []utils.KeyValue {
	var controllerMap = utils.NewMap()

	// list of changes package names & imports between spec and implementation
	controllerMap.Set("package controller", "package qbft")
	controllerMap.Set("specqbft \"github.com/bloxapp/ssv-spec/qbft\"\n", "")
	controllerMap.Set("\"go.uber.org/zap\"\n\n", "")
	controllerMap.Set("\"github.com/bloxapp/ssv/protocol/v2/qbft\"\n", "")
	controllerMap.Set("\"github.com/bloxapp/ssv/protocol/v2/qbft/instance\"\n", "")
	controllerMap.Set("spectypes \"github.com/bloxapp/ssv-spec/types\"", "\"github.com/bloxapp/ssv-spec/types\"")
	controllerMap.Set("logging \"github.com/ipfs/go-log\"\n", "")
	controllerMap.Set("*instance.Instance", "*Instance")
	controllerMap.Set("specqbft.", "")
	controllerMap.Set("spectypes.", "types.")
	controllerMap.Set("qbft.IConfig", "IConfig")
	controllerMap.Set("instance.NewInstance", "NewInstance")
	controllerMap.Set("var logger = logging.Logger(\"ssv/protocol/qbft/controller\").Desugar()", "")

	// list of approved changes in code between spec and implementation

	controllerMap.Set("qbft.IConfig", "IConfig")
	controllerMap.Set("logger              *zap.Logger", "")
	controllerMap.Set("logger:              logger.With(zap.String(\"identifier\", types.MessageIDFromBytes(identifier).String())),", "")
	controllerMap.Set("// TODO-spec-align changed due to instance and controller are not in same package as in spec, do we still need it for test?", "")
	controllerMap.Set("i.SetConfig(config)", "i.config = config")
	controllerMap.Set("c.logger.Debug(\"failed to broadcast decided message\", zap.Error(err))", "fmt.Printf(\"%s\\n\", err.Error())")

	return controllerMap.Range()
}

func SpecControllerSet() []utils.KeyValue {
	var specControllerSet = utils.NewMap()

	specControllerSet.Set("\"fmt\"\n", "")

	return specControllerSet.Range()
}

// Decided mapping

func DecidedSet() []utils.KeyValue {
	var decidedMap = utils.NewMap()

	// list of changes package names & imports between spec and implementation
	decidedMap.Set("package controller", "package qbft")
	decidedMap.Set("specqbft \"github.com/bloxapp/ssv-spec/qbft\"\n", "")
	decidedMap.Set("\"go.uber.org/zap\"\n", "")
	decidedMap.Set("\"github.com/bloxapp/ssv/protocol/v2/qbft/instance\"\n", "")
	decidedMap.Set("\"github.com/bloxapp/ssv/protocol/v2/qbft\"\n", "")
	decidedMap.Set("spectypes \"github.com/bloxapp/ssv-spec/types\"", "\"github.com/bloxapp/ssv-spec/types\"")
	decidedMap.Set("specqbft.", "")
	decidedMap.Set("spectypes.", "types.")
	decidedMap.Set("instance.NewInstance", "NewInstance")
	decidedMap.Set("instance.BaseCommitValidation", "baseCommitValidation")
	decidedMap.Set("qbft.IConfig", "IConfig")

	// list of approved changes in code between spec and implementation
	// This handles storage of HighestInstance to storage - only implementation level
	decidedMap.Set("if futureInstance := c.StoredInstances.FindInstance(msg.Message.Height); futureInstance != nil {\n\t\t\tif err = c.SaveHighestInstance(futureInstance, msg); err != nil {\n\t\t\t\tc.logger.Debug(\"failed to save instance\",\n\t\t\t\t\tzap.Uint64(\"height\", uint64(msg.Message.Height)),\n\t\t\t\t\tzap.Error(err))\n\t\t\t} else {\n\t\t\t\tc.logger.Debug(\"saved instance upon decided\", zap.Uint64(\"height\", uint64(msg.Message.Height)))\n\t\t\t}\n\t\t}", "")

	return decidedMap.Range()
}
func SpecDecidedSet() []utils.KeyValue {
	var specDecidedSet = utils.NewMap()
	return specDecidedSet.Range()
}

// FutureMessage mapping

func FutureMessageSet() []utils.KeyValue {
	var futureMessageMap = utils.NewMap()

	// list of changes package names & imports between spec and implementation
	futureMessageMap.Set("package controller", "package qbft")
	futureMessageMap.Set("specqbft \"github.com/bloxapp/ssv-spec/qbft\"", "")
	futureMessageMap.Set("\"github.com/bloxapp/ssv/protocol/v2/qbft\"\n", "")
	futureMessageMap.Set("\"go.uber.org/zap\"\n", "")
	futureMessageMap.Set("spectypes \"github.com/bloxapp/ssv-spec/types\"", "\"github.com/bloxapp/ssv-spec/types\"")
	futureMessageMap.Set("specqbft.", "")
	futureMessageMap.Set("spectypes.", "types.")
	futureMessageMap.Set("qbft.IConfig", "IConfig")

	// list of approved changes in code between spec and implementation
	futureMessageMap.Set("c.logger.Debug(\"triggered f+1 sync\",\n\t\t\tzap.Uint64(\"ctrl_height\", uint64(c.Height)),\n\t\t\tzap.Uint64(\"msg_height\", uint64(msg.Message.Height)))", "")

	return futureMessageMap.Range()
}
func SpecFutureMessageSet() []utils.KeyValue {
	var specFutureMessageSet = utils.NewMap()
	return specFutureMessageSet.Range()
}
