package ssv

import "github.com/bloxapp/ssv/scripts/spec_align_report/utils"

func RunnerSet() []utils.KeyValue {
	var runnerSet = utils.NewMap()
	runnerSet.Set("package runner", "package ssv")
	runnerSet.Set("\"fmt\"\n", "")
	runnerSet.Set("specqbft \"github.com/bloxapp/ssv-spec/qbft\"", "\"github.com/bloxapp/ssv-spec/qbft\"")
	runnerSet.Set("specssv \"github.com/bloxapp/ssv-spec/ssv\"\n", "")
	runnerSet.Set("spectypes \"github.com/bloxapp/ssv-spec/types\"", "\"github.com/bloxapp/ssv-spec/types\"")
	runnerSet.Set("\"github.com/bloxapp/ssv/protocol/v2/qbft/controller\"\n", "")

	runnerSet.Set("specssv.", "")
	runnerSet.Set("specqbft.", "qbft.")
	runnerSet.Set("spectypes.", "types.")
	runnerSet.Set("controller.Controller", "qbft.Controller")
	runnerSet.Set(" else {\n\t\tif inst := b.QBFTController.StoredInstances.FindInstance(decidedMsg.Message.Height); inst != nil {\n\t\t\tif err = b.QBFTController.SaveHighestInstance(inst, decidedMsg); err != nil {\n\t\t\t\tfmt.Printf(\"failed to save instance: %s\\n\", err.Error())\n\t\t\t}\n\t\t}\n\t}", "")
	// TODO change in spec to didDecideCorrectly to didDecideRunningInstanceCorrectly  decided := decidedMsg != nil && b.State.RunningInstance != nil
	runnerSet.Set("decidedRunningInstance := decided && b.State.RunningInstance != nil && decidedMsg.Message.Height == b.State.RunningInstance.GetHeight()", "decidedRunningInstance := decided && decidedMsg.Message.Height == b.State.RunningInstance.GetHeight()")
	runnerSet.Set("// registers a timeout handler\n\tb.registerTimeoutHandler(newInstance, runner.GetBaseRunner().QBFTController.Height)", "")
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

	return mapSet.Range()
}
func SpecSyncCommitteeAggregatorSet() []utils.KeyValue {
	var mapSet = utils.NewMap()
	return mapSet.Range()
}

func RunnerValidationsSet() []utils.KeyValue {
	var mapSet = utils.NewMap()

	mapSet.Set("package runner", "package ssv")
	mapSet.Set("specssv \"github.com/bloxapp/ssv-spec/ssv\"\n", "")
	mapSet.Set("specssv.", "")

	return mapSet.Range()
}
func SpecRunnerValidationsSet() []utils.KeyValue {
	var mapSet = utils.NewMap()
	return mapSet.Range()
}

func RunnerSignaturesSet() []utils.KeyValue {
	var mapSet = utils.NewMap()

	mapSet.Set("package runner", "package ssv")
	mapSet.Set("specssv \"github.com/bloxapp/ssv-spec/ssv\"\n", "")
	mapSet.Set("specssv.", "")

	return mapSet.Range()
}
func SpecSRunnerSignaturesSet() []utils.KeyValue {
	var mapSet = utils.NewMap()
	return mapSet.Range()
}
