package mapping

// Controller mapping

func ControllerSet()[]KeyValue {
	var controllerMap = NewMap()
	
	// list of changes package names & imports between spec and implementation
	controllerMap.Set("package controller", "package qbft")
	controllerMap.Set("specqbft \"github.com/bloxapp/ssv-spec/qbft\"", "")
	controllerMap.Set("\"github.com/bloxapp/ssv/protocol/v2/qbft/instance\"", "")
	controllerMap.Set("\"github.com/bloxapp/ssv/protocol/v2/types\"","")
	controllerMap.Set("spectypes \"github.com/bloxapp/ssv-spec/types\"",  "\"github.com/bloxapp/ssv-spec/types\"")
	controllerMap.Set("*instance.Instance", "*Instance")
	controllerMap.Set("specqbft.", "")
	controllerMap.Set("spectypes.", "types.")
	controllerMap.Set("types.IConfig", "IConfig")
	controllerMap.Set("instance.NewInstance", "NewInstance")
	

	// list of approved changes in code between spec and implementation
	//TODO should be removed after instance container moved to storage in spec
	controllerMap.Set("//  TODO-spec-align rethink if we need it", "")
	controllerMap.Set("i.SetConfig(config)", "i.config = config")


	return controllerMap.Range()
}

func SpecControllerSet()[]KeyValue {
	var specControllerSet = NewMap()
	return specControllerSet.Range()
}

// Decided mapping

func DecidedSet()[]KeyValue {
	var decidedMap = NewMap()

	// list of changes package names & imports between spec and implementation
	decidedMap.Set("package controller", "package qbft")
	decidedMap.Set("specqbft \"github.com/bloxapp/ssv-spec/qbft\"" , "")
	decidedMap.Set("\"github.com/bloxapp/ssv/protocol/v2/qbft/instance\"" , "")
	decidedMap.Set("\"github.com/bloxapp/ssv/protocol/v2/types\"" , "")
	decidedMap.Set("spectypes \"github.com/bloxapp/ssv-spec/types\"" , "\"github.com/bloxapp/ssv-spec/types\"")
	decidedMap.Set("specqbft.", "")
	decidedMap.Set("spectypes.", "types.")
	decidedMap.Set("types.IConfig", "IConfig")
	decidedMap.Set("instance.NewInstance", "NewInstance")
	decidedMap.Set("instance.BaseCommitValidation", "baseCommitValidation")
	
	//TODO remove after comment add to spec
	decidedMap.Set("// isDecidedMsg", "//")

	// list of approved changes in code between spec and implementation
	
	return decidedMap.Range()
}
func SpecDecidedSet()[]KeyValue {
	var specDecidedSet = NewMap()
	return specDecidedSet.Range()
}

// FutureMessage mapping

func FutureMessageSet()[]KeyValue {
	var futureMessageMap = NewMap()

	// list of changes package names & imports between spec and implementation
	futureMessageMap.Set("package controller", "package qbft")
	futureMessageMap.Set("specqbft \"github.com/bloxapp/ssv-spec/qbft\"" , "")
	futureMessageMap.Set("\"github.com/bloxapp/ssv/protocol/v2/types\"" , "")
	futureMessageMap.Set("spectypes \"github.com/bloxapp/ssv-spec/types\"" , "\"github.com/bloxapp/ssv-spec/types\"")
	futureMessageMap.Set("specqbft.", "")
	futureMessageMap.Set("spectypes.", "types.")
	futureMessageMap.Set("types.IConfig", "IConfig")

	// list of approved changes in code between spec and implementation

	return futureMessageMap.Range()
}
func SpecFutureMessageSet()[]KeyValue {
	var specFutureMessageSet = NewMap()
	return specFutureMessageSet.Range()
}