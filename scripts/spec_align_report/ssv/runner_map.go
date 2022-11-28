package ssv

import "github.com/bloxapp/ssv/scripts/spec_align_report/utils"

func RunnerSet() []utils.KeyValue {
	var runnerSet = utils.NewMap()
	runnerSet.Set("package runner", "package ssv")
	runnerSet.Set("\"fmt\"\n", "")
	runnerSet.Set("specqbft \"github.com/bloxapp/ssv-spec/qbft\"", "\"github.com/bloxapp/ssv-spec/qbft\"")
	runnerSet.Set("specssv \"github.com/bloxapp/ssv-spec/ssv\"\n", "")
	runnerSet.Set("spectypes \"github.com/bloxapp/ssv-spec/types\"", "\"github.com/bloxapp/ssv-spec/types\"")
	runnerSet.Set("\"github.com/bloxapp/ssv/protocol/v2/qbft/roundtimer\"", "")
	runnerSet.Set("\"github.com/bloxapp/ssv/protocol/v2/qbft/controller\"\n", "")

	//TODO in spec its in type file - should we move it out too? why do we need Identifiers function
	runnerSet.Set("// DutyRunners is a map of duty runners mapped by msg id hex.\ntype DutyRunners map[spectypes.BeaconRole]Runner\n\n// DutyRunnerForMsgID returns a Runner from the provided msg ID, or nil if not found\nfunc (dr DutyRunners) DutyRunnerForMsgID(msgID spectypes.MessageID) Runner {\n\trole := msgID.GetRoleType()\n\treturn dr[role]\n}\n\n// Identifiers gathers identifiers of all shares.\nfunc (dr DutyRunners) Identifiers() []spectypes.MessageID {\n\tvar identifiers []spectypes.MessageID\n\tfor role, r := range dr {\n\t\tshare := r.GetBaseRunner().Share\n\t\tif share == nil { // TODO: handle missing share?\n\t\t\tcontinue\n\t\t}\n\t\ti := spectypes.NewMsgID(r.GetBaseRunner().Share.ValidatorPubKey, role)\n\t\tidentifiers = append(identifiers, i)\n\t}\n\treturn identifiers\n}", "")

	runnerSet.Set("specssv.", "")
	runnerSet.Set("specqbft.", "qbft.")
	runnerSet.Set("spectypes.", "types.")
	runnerSet.Set("controller.Controller", "qbft.Controller")
	return runnerSet.Range()
}
func SpecRunnerSet() []utils.KeyValue {
	var specRunnerSet = utils.NewMap()
	return specRunnerSet.Range()
}

func RunnerStateSet() []utils.KeyValue {
	var mapSet = utils.NewMap()
	mapSet.Set("package runner", "package ssv")
	mapSet.Set("specssv \"github.com/bloxapp/ssv-spec/ssv\"\n", "")
	mapSet.Set("spectypes \"github.com/bloxapp/ssv-spec/types\"", "\"github.com/bloxapp/ssv-spec/types\"")
	mapSet.Set("\"github.com/bloxapp/ssv/protocol/v2/qbft/instance\"", "")
	mapSet.Set("specssv.", "")
	mapSet.Set("instance.Instance", "qbft.Instance")
	mapSet.Set("spectypes.", "types.")

	return mapSet.Range()
}
func SpecRunnerStateSet() []utils.KeyValue {
	var mapSet = utils.NewMap()

	mapSet.Set("\"github.com/bloxapp/ssv-spec/qbft\"\n", "")
	return mapSet.Range()
}

func AggregatorSet() []utils.KeyValue {
	var mapSet = utils.NewMap()
	mapSet.Set("package runner", "package ssv")
	mapSet.Set("specqbft \"github.com/bloxapp/ssv-spec/qbft\"", "\"github.com/bloxapp/ssv-spec/qbft\"")
	mapSet.Set("specssv \"github.com/bloxapp/ssv-spec/ssv\"\n", "")
	mapSet.Set("spectypes \"github.com/bloxapp/ssv-spec/types\"", "\"github.com/bloxapp/ssv-spec/types\"")
	mapSet.Set("\"github.com/bloxapp/ssv/protocol/v2/qbft/controller\"\n", "")
	mapSet.Set("specssv.", "")
	mapSet.Set("specqbft.", "qbft.")
	mapSet.Set("spectypes.", "types.")
	mapSet.Set("controller.Controller", "qbft.Controller")
	// ignored typo in spec
	mapSet.Set("BaseRunner.HasRunningDuty()", "BaseRunner.HashRunningDuty()")
	return mapSet.Range()
}
func SpecAggregatorSet() []utils.KeyValue {
	var mapSet = utils.NewMap()
	return mapSet.Range()
}

func AttesterSet() []utils.KeyValue {
	var mapSet = utils.NewMap()

	mapSet.Set("package runner", "package ssv")
	mapSet.Set("specqbft \"github.com/bloxapp/ssv-spec/qbft\"", "\"github.com/bloxapp/ssv-spec/qbft\"")
	mapSet.Set("specssv \"github.com/bloxapp/ssv-spec/ssv\"\n", "")
	mapSet.Set("spectypes \"github.com/bloxapp/ssv-spec/types\"", "\"github.com/bloxapp/ssv-spec/types\"")
	mapSet.Set("\"github.com/bloxapp/ssv/protocol/v2/qbft/controller\"\n", "")
	mapSet.Set("specssv.", "")
	mapSet.Set("specqbft.", "qbft.")
	mapSet.Set("spectypes.", "types.")
	mapSet.Set("controller.Controller", "qbft.Controller")
	// ignored typo in spec
	mapSet.Set("BaseRunner.HasRunningDuty()", "BaseRunner.HashRunningDuty()")
	return mapSet.Range()
}
func SpecAttesterSet() []utils.KeyValue {
	var mapSet = utils.NewMap()
	return mapSet.Range()
}

func ProposerSet() []utils.KeyValue {
	var mapSet = utils.NewMap()

	mapSet.Set("package runner", "package ssv")
	mapSet.Set("specqbft \"github.com/bloxapp/ssv-spec/qbft\"", "\"github.com/bloxapp/ssv-spec/qbft\"")
	mapSet.Set("specssv \"github.com/bloxapp/ssv-spec/ssv\"\n", "")
	mapSet.Set("spectypes \"github.com/bloxapp/ssv-spec/types\"", "\"github.com/bloxapp/ssv-spec/types\"")
	mapSet.Set("\"github.com/bloxapp/ssv/protocol/v2/qbft/controller\"\n", "")
	mapSet.Set("specssv.", "")
	mapSet.Set("specqbft.", "qbft.")
	mapSet.Set("spectypes.", "types.")
	mapSet.Set("controller.Controller", "qbft.Controller")
	// ignored typo in spec
	mapSet.Set("BaseRunner.HasRunningDuty()", "BaseRunner.HashRunningDuty()")
	return mapSet.Range()
}
func SpecProposerSet() []utils.KeyValue {
	var mapSet = utils.NewMap()
	return mapSet.Range()
}

func SyncCommitteeSet() []utils.KeyValue {
	var mapSet = utils.NewMap()

	mapSet.Set("package runner", "package ssv")
	mapSet.Set("specqbft \"github.com/bloxapp/ssv-spec/qbft\"", "\"github.com/bloxapp/ssv-spec/qbft\"")
	mapSet.Set("specssv \"github.com/bloxapp/ssv-spec/ssv\"\n", "")
	mapSet.Set("spectypes \"github.com/bloxapp/ssv-spec/types\"", "\"github.com/bloxapp/ssv-spec/types\"")
	mapSet.Set("\"github.com/bloxapp/ssv/protocol/v2/qbft/controller\"\n", "")
	mapSet.Set("specssv.", "")
	mapSet.Set("specqbft.", "qbft.")
	mapSet.Set("spectypes.", "types.")
	mapSet.Set("controller.Controller", "qbft.Controller")
	// ignored typo in spec
	mapSet.Set("BaseRunner.HasRunningDuty()", "BaseRunner.HashRunningDuty()")
	return mapSet.Range()
}
func SpecSyncCommitteeSet() []utils.KeyValue {
	var mapSet = utils.NewMap()
	return mapSet.Range()
}

func SyncCommitteeAggregatorSet() []utils.KeyValue {
	var mapSet = utils.NewMap()

	mapSet.Set("package runner", "package ssv")
	mapSet.Set("specqbft \"github.com/bloxapp/ssv-spec/qbft\"", "\"github.com/bloxapp/ssv-spec/qbft\"")
	mapSet.Set("specssv \"github.com/bloxapp/ssv-spec/ssv\"\n", "")
	mapSet.Set("spectypes \"github.com/bloxapp/ssv-spec/types\"", "\"github.com/bloxapp/ssv-spec/types\"")
	mapSet.Set("\"github.com/bloxapp/ssv/protocol/v2/qbft/controller\"\n", "")
	mapSet.Set("specssv.", "")
	mapSet.Set("specqbft.", "qbft.")
	mapSet.Set("spectypes.", "types.")
	mapSet.Set("controller.Controller", "qbft.Controller")
	// ignored typo in spec
	mapSet.Set("BaseRunner.HasRunningDuty()", "BaseRunner.HashRunningDuty()")
	return mapSet.Range()
}
func SpecSyncCommitteeAggregatorSet() []utils.KeyValue {
	var mapSet = utils.NewMap()
	return mapSet.Range()
}
