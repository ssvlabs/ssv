package mapping

// controllerGeneral describes the list of changes between spec and implementation
// It includes package names & imports
var controllerGeneral = map[string]string{
	"package controller": "package qbft",
	"qbftspec \"github.com/bloxapp/ssv-spec/qbft\"" : "",
	"\"github.com/bloxapp/ssv/protocol/v2/qbft/instance\"" : "",
	// TODO change types2 to types
	"types2 \"github.com/bloxapp/ssv/protocol/v2/types\"" : "",
	"*instance.Instance": "*Instance",
	"qbftspec.": "",
	// TODO change types2 to types
	"types2.IConfig": "IConfig",
	//"instance.NewInstance": "NewInstance",
}

// controllerChanges describes the list of changes in code between spec and implementation
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

// specControllerGeneral describes the list of changes between spec and implementation
// It includes package names & imports
var specControllerGeneral = map[string]string{

}

func SpecControllerReplace()  map[string]string {
	return specControllerGeneral
}
