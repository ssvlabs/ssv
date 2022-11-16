package mapping

// Instance mapping

// instanceGeneral describes the list of changes between spec and implementation
// It includes package names & imports
var instanceGeneral = map[string]string{
	"package instance": "package qbft",
	"spectypes \"github.com/bloxapp/ssv-spec/types\"" : "\"github.com/bloxapp/ssv-spec/types\"",
	"specqbft \"github.com/bloxapp/ssv-spec/qbft\"\n" : "",
	"\"github.com/bloxapp/ssv/protocol/v2/types\"\n" : "",
	"specqbft.": "",
	"spectypes.": "types.",
	"types.IConfig": "IConfig",
	//TODO need to fix nil check on spec
	"state := i.State\n\tif state == nil {\n\t\treturn false, nil\n\t}\n\treturn state.Decided, state.DecidedValue" : "return i.State.Decided, i.State.DecidedValue",
	//TODO remove log
	"fmt.Println(fmt.Sprintf(\"operator %d is the leader!\", i.State.Share.OperatorID))": "",
	//TODO remove after instance container to storage spec PR
	"// SetConfig returns the instance config\nfunc (i *Instance) SetConfig(config types.IConfig) {\n\ti.config = config\n}": "",

}

// instanceChanges describes the list of approved changes in code between spec and implementation
var instanceChanges = map[string]string{
}

func InstanceReplace()  map[string]string {
	for k, v := range instanceChanges {
		instanceGeneral[k] = v
	}
	return instanceGeneral
}
func SpecInstanceReplace()  map[string]string {
	return map[string]string{
		// We import from spec - so for the diff we remove it from spec
		"type ProposedValueCheckF func(data []byte) error": "",
		"type ProposerF func(state *State, round Round) types.OperatorID": "",
	}
}
