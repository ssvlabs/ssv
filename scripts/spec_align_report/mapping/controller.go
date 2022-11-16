package mapping

// Controller mapping

// controllerGeneral describes the list of changes between spec and implementation
// It includes package names & imports
var controllerGeneral = map[string]string{
	"package controller": "package qbft",
	"specqbft \"github.com/bloxapp/ssv-spec/qbft\"" : "",
	"\"github.com/bloxapp/ssv/protocol/v2/qbft/instance\"" : "",
	"\"github.com/bloxapp/ssv/protocol/v2/types\"" : "",
	"spectypes \"github.com/bloxapp/ssv-spec/types\"" : "\"github.com/bloxapp/ssv-spec/types\"",
	"*instance.Instance": "*Instance",
	"specqbft.": "",
	"spectypes.": "types.",
	"types.IConfig": "IConfig",
	"instance.NewInstance": "NewInstance",
}

// controllerChanges describes the list of approved changes in code between spec and implementation
var controllerChanges = map[string]string{
	//TODO should be removed after instance container moved to storage in spec
	"//  TODO-spec-align rethink if we need it":"",
	"i.SetConfig(config)":"i.config = config",

}

func ControllerReplace()  map[string]string {
	for k, v := range controllerChanges {
		controllerGeneral[k] = v
	}
	return controllerGeneral
}
func SpecControllerReplace()  map[string]string {
	return map[string]string{}
}


// Decided mapping
var decidedGeneral = map[string]string{
	"package controller": "package qbft",
	"specqbft \"github.com/bloxapp/ssv-spec/qbft\"" : "",
	"\"github.com/bloxapp/ssv/protocol/v2/qbft/instance\"" : "",
	"\"github.com/bloxapp/ssv/protocol/v2/types\"" : "",
	"spectypes \"github.com/bloxapp/ssv-spec/types\"" : "\"github.com/bloxapp/ssv-spec/types\"",
	"specqbft.": "",
	"spectypes.": "types.",
	"types.IConfig": "IConfig",
	"instance.NewInstance": "NewInstance",
	"instance.BaseCommitValidation": "baseCommitValidation",
	//TODO remove after comment add to spec
	"// isDecidedMsg": "//",
}
var decidedChanges = map[string]string{}

func DecidedReplace()  map[string]string {
	for k, v := range decidedChanges {
		decidedGeneral[k] = v
	}
	return decidedGeneral
}
func SpecDecidedReplace()  map[string]string {
	return map[string]string{}
}


// FutureMessage mapping
var futureMessageGeneral = map[string]string{
	"package controller": "package qbft",
	"specqbft \"github.com/bloxapp/ssv-spec/qbft\"" : "",
	"\"github.com/bloxapp/ssv/protocol/v2/types\"" : "",
	"spectypes \"github.com/bloxapp/ssv-spec/types\"" : "\"github.com/bloxapp/ssv-spec/types\"",
	"specqbft.": "",
	"spectypes.": "types.",
	"types.IConfig": "IConfig",

}
var futureMessageChanges = map[string]string{}

func FutureMessageReplace()  map[string]string {
	for k, v := range futureMessageChanges {
		futureMessageGeneral[k] = v
	}
	return futureMessageGeneral
}
func SpecFutureMessageReplace()  map[string]string {
	return map[string]string{}
}