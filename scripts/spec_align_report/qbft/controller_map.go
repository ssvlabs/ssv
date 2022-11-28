package qbft

import "github.com/bloxapp/ssv/scripts/spec_align_report/utils"

// Controller mapping

func ControllerSet() []utils.KeyValue {
	var controllerMap = utils.NewMap()

	// list of changes package names & imports between spec and implementation
	controllerMap.Set("package controller", "package qbft")
	controllerMap.Set("specqbft \"github.com/bloxapp/ssv-spec/qbft\"", "")
	controllerMap.Set("\"github.com/bloxapp/ssv/protocol/v2/qbft/instance\"", "")
	controllerMap.Set("\"github.com/bloxapp/ssv/protocol/v2/types\"", "")
	controllerMap.Set("spectypes \"github.com/bloxapp/ssv-spec/types\"", "\"github.com/bloxapp/ssv-spec/types\"")
	controllerMap.Set("*instance.Instance", "*Instance")
	controllerMap.Set("specqbft.", "")
	controllerMap.Set("spectypes.", "types.")
	controllerMap.Set("types.IConfig", "IConfig")
	controllerMap.Set("instance.NewInstance", "NewInstance")

	// list of approved changes in code between spec and implementation
	controllerMap.Set("// TODO-spec-align changed due to instance and controller are not in same package as in spec, do we still need it for test?", "")
	controllerMap.Set("i.SetConfig(config)", "i.config = config")

	return controllerMap.Range()
}

func SpecControllerSet() []utils.KeyValue {
	var specControllerSet = utils.NewMap()
	return specControllerSet.Range()
}

// Decided mapping

func DecidedSet() []utils.KeyValue {
	var decidedMap = utils.NewMap()

	// list of changes package names & imports between spec and implementation
	decidedMap.Set("package controller", "package qbft")
	decidedMap.Set("\"fmt\"\n", "")
	decidedMap.Set("specqbft \"github.com/bloxapp/ssv-spec/qbft\"\n", "")
	decidedMap.Set("\"github.com/bloxapp/ssv/protocol/v2/qbft/instance\"\n", "")
	decidedMap.Set("\"github.com/bloxapp/ssv/protocol/v2/types\"\n", "")
	decidedMap.Set("spectypes \"github.com/bloxapp/ssv-spec/types\"", "\"github.com/bloxapp/ssv-spec/types\"")
	decidedMap.Set("specqbft.", "")
	decidedMap.Set("spectypes.", "types.")
	decidedMap.Set("types.IConfig", "IConfig")
	decidedMap.Set("instance.NewInstance", "NewInstance")
	decidedMap.Set("instance.BaseCommitValidation", "baseCommitValidation")

	//TODO remove after comment add to spec
	decidedMap.Set("// isDecidedMsg", "//")

	// list of approved changes in code between spec and implementation
	// This handles storage of HighestInstance to storage - only implementation level
	decidedMap.Set("if futureInstance := c.StoredInstances.FindInstance(msg.Message.Height); futureInstance != nil {\n\t\t\tif err = c.GetConfig().GetStorage().SaveHighestInstance(futureInstance.State); err != nil {\n\t\t\t\tfmt.Printf(\"failed to save instance: %s\\n\", err.Error())\n\t\t\t}\n\t\t}", "")

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
	futureMessageMap.Set("\"github.com/bloxapp/ssv/protocol/v2/types\"", "")
	futureMessageMap.Set("spectypes \"github.com/bloxapp/ssv-spec/types\"", "\"github.com/bloxapp/ssv-spec/types\"")
	futureMessageMap.Set("specqbft.", "")
	futureMessageMap.Set("spectypes.", "types.")
	futureMessageMap.Set("types.IConfig", "IConfig")

	// list of approved changes in code between spec and implementation

	return futureMessageMap.Range()
}
func SpecFutureMessageSet() []utils.KeyValue {
	var specFutureMessageSet = utils.NewMap()
	return specFutureMessageSet.Range()
}
