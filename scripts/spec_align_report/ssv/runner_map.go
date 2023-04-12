package ssv

import (
	"github.com/bloxapp/ssv/scripts/spec_align_report/utils"
)

func RunnerSet() []utils.KeyValue {
	var runnerSet = utils.NewMap()
	runnerSet.Set("package runner", "package ssv")
	runnerSet.Set("specqbft \"github.com/bloxapp/ssv-spec/qbft\"", "\"github.com/bloxapp/ssv-spec/qbft\"")
	runnerSet.Set("specssv \"github.com/bloxapp/ssv-spec/ssv\"\n", "")
	runnerSet.Set("spectypes \"github.com/bloxapp/ssv-spec/types\"", "\"github.com/bloxapp/ssv-spec/types\"")
	//runnerSet.Set("logging \"github.com/ipfs/go-log\"\n", "")
	//runnerSet.Set("\"go.uber.org/zap\"\n\n", "")
	runnerSet.Set("\"github.com/bloxapp/ssv/protocol/v2/qbft/controller\"\n", "")

	//runnerSet.Set("var logger = logging.Logger(\"ssv/protocol/ssv/runner\").Desugar()", "")
	runnerSet.Set("specssv.", "")
	runnerSet.Set("specqbft.", "qbft.")
	runnerSet.Set("spectypes.", "types.")
	runnerSet.Set("controller.Controller", "qbft.Controller")
	//runnerSet.Set("logger         *zap.Logger\n", "")
	runnerSet.Set("// implementation vars\n\tTimeoutF TimeoutF `json:\"-\"`\n", "")

	runnerSet.Set("} else {\n\t\tif inst := b.QBFTController.StoredInstances.FindInstance(decidedMsg.Message.Height); inst != nil {\n\t\t\tlogger := logger.With(\n\t\t\t\tzap.Uint64(\"msg_height\", uint64(msg.Message.Height)),\n\t\t\t\tzap.Uint64(\"ctrl_height\", uint64(b.QBFTController.Height)),\n\t\t\t\tzap.Any(\"signers\", msg.Signers),\n\t\t\t)\n\t\t\tif err = b.QBFTController.SaveInstance(inst, decidedMsg); err != nil {\n\t\t\t\tlogger.Debug(\"❗ failed to save instance\", zap.Error(err))\n\t\t\t} else {\n\t\t\t\tlogger.Debug(\"💾 saved instance\")\n\t\t\t}\n\t\t}\n\t}", "")
	// TODO change in spec to didDecideCorrectly to didDecideRunningInstanceCorrectly  decided := decidedMsg != nil && b.State.RunningInstance != nil
	runnerSet.Set("decidedRunningInstance := decided && b.State.RunningInstance != nil && decidedMsg.Message.Height == b.State.RunningInstance.GetHeight()", "decidedRunningInstance := decided && decidedMsg.Message.Height == b.State.RunningInstance.GetHeight()")
	runnerSet.Set("b.registerTimeoutHandler(logger, newInstance, runner.GetBaseRunner().QBFTController.Height)\n", "")
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
	//mapSet.Set("\"encoding/hex\"", "")
	mapSet.Set("specqbft \"github.com/bloxapp/ssv-spec/qbft\"", "\"github.com/bloxapp/ssv-spec/qbft\"")
	mapSet.Set("specssv \"github.com/bloxapp/ssv-spec/ssv\"\n", "")
	mapSet.Set("spectypes \"github.com/bloxapp/ssv-spec/types\"", "\"github.com/bloxapp/ssv-spec/types\"")
	//mapSet.Set("\"go.uber.org/zap\"\n\n", "")
	mapSet.Set("\"github.com/bloxapp/ssv/protocol/v2/qbft/controller\"\n", "")
	//mapSet.Set("var logger = logging.Logger(\"ssv/protocol/ssv/runner\").Desugar()", "")
	mapSet.Set("specssv.", "")
	mapSet.Set("specqbft.", "qbft.")
	mapSet.Set("spectypes.", "types.")
	mapSet.Set("controller.Controller", "qbft.Controller")
	//mapSet.Set("logger   *zap.Logger\n", "")
	//mapSet.Set("logger:         logger.With(zap.String(\"who\", \"BaseRunner\")),\n", "")
	//mapSet.Set("logger := logger.With(zap.String(\"validator\", hex.EncodeToString(share.ValidatorPubKey)))\n", "")
	//mapSet.Set("logger:   logger.With(zap.String(\"who\", \"AggregatorRunner\")),\n", "")

	mapSet.Set("logger.Debug(\"✅ successful submitted aggregate\")", "")

	return mapSet.Range()
}
func SpecAggregatorSet() []utils.KeyValue {
	var mapSet = utils.NewMap()
	// Handled in gc client
	mapSet.Set("// TODO waitToSlotTwoThirds\n", "")
	return mapSet.Range()
}

func AttesterSet() []utils.KeyValue {
	var mapSet = utils.NewMap()

	mapSet.Set("package runner", "package ssv")
	mapSet.Set("\"encoding/hex\"", "")
	mapSet.Set("specqbft \"github.com/bloxapp/ssv-spec/qbft\"", "\"github.com/bloxapp/ssv-spec/qbft\"")
	mapSet.Set("specssv \"github.com/bloxapp/ssv-spec/ssv\"\n", "")
	mapSet.Set("spectypes \"github.com/bloxapp/ssv-spec/types\"", "\"github.com/bloxapp/ssv-spec/types\"")
	//mapSet.Set("\"go.uber.org/zap\"\n\n", "")
	mapSet.Set("\"github.com/bloxapp/ssv/protocol/v2/qbft/controller\"\n", "")
	//mapSet.Set("logger   *zap.Logger\n", "")
	//mapSet.Set("logger := logger.With(zap.String(\"validator\", hex.EncodeToString(share.ValidatorPubKey)))\n", "")
	//mapSet.Set("logger:         logger.With(zap.String(\"who\", \"BaseRunner\")),\n", "")
	//mapSet.Set("logger: logger.With(zap.String(\"who\", \"AttesterRunner\")),", "")
	mapSet.Set("specssv.", "")
	mapSet.Set("specqbft.", "qbft.")
	mapSet.Set("spectypes.", "types.")
	mapSet.Set("controller.Controller", "qbft.Controller")

	mapSet.Set("logger.Debug(\"🧩 reconstructed partial signatures\", zap.Any(\"signers\", getPostConsensusSigners(r.GetState(), root)), fields.Slot(duty.Slot))", "")

	mapSet.Set("Submit it to the BN.", "broadcast")
	mapSet.Set("logger.Error(\"❌ failed to submit attestation to Beacon node\", fields.Slot(duty.Slot), zap.Error(err))", "")
	mapSet.Set("logger.Debug(\"✅ successfully submitted attestation\",\n\t\t\tfields.Slot(duty.Slot),\n\t\t\tzap.String(\"block_root\", hex.EncodeToString(signedAtt.Data.BeaconBlockRoot[:])),\n\t\t\tfields.ConsensusTime(time.Since(r.started)),\n\t\t\tfields.Round(r.GetState().RunningInstance.State.Round))", "")

	return mapSet.Range()
}
func SpecAttesterSet() []utils.KeyValue {
	var mapSet = utils.NewMap()
	return mapSet.Range()
}

func ProposerSet() []utils.KeyValue {
	var mapSet = utils.NewMap()

	mapSet.Set("package runner", "package ssv")
	//mapSet.Set("\"encoding/hex\"", "")
	mapSet.Set("specqbft \"github.com/bloxapp/ssv-spec/qbft\"", "\"github.com/bloxapp/ssv-spec/qbft\"")
	mapSet.Set("specssv \"github.com/bloxapp/ssv-spec/ssv\"\n", "")
	mapSet.Set("spectypes \"github.com/bloxapp/ssv-spec/types\"", "\"github.com/bloxapp/ssv-spec/types\"")
	//mapSet.Set("\"go.uber.org/zap\"\n\n", "")
	mapSet.Set("\"github.com/bloxapp/ssv/protocol/v2/qbft/controller\"\n", "")
	//mapSet.Set("logger   *zap.Logger\n", "")
	//mapSet.Set("logger := logger.With(zap.String(\"validator\", hex.EncodeToString(share.ValidatorPubKey)))\n", "")
	//mapSet.Set("logger:         logger.With(zap.String(\"who\", \"BaseRunner\")),\n", "")
	//mapSet.Set("logger:   logger.With(zap.String(\"who\", \"ProposerRunner\")),", "")
	mapSet.Set("specssv.", "")
	mapSet.Set("specqbft.", "qbft.")
	mapSet.Set("spectypes.", "types.")
	mapSet.Set("controller.Controller", "qbft.Controller")

	mapSet.Set("logger.Info(\"✅ successfully proposed block!\")\n", "")

	return mapSet.Range()
}
func SpecProposerSet() []utils.KeyValue {
	var mapSet = utils.NewMap()
	return mapSet.Range()
}

func SyncCommitteeSet() []utils.KeyValue {
	var mapSet = utils.NewMap()

	mapSet.Set("package runner", "package ssv")
	//mapSet.Set("\"encoding/hex\"", "")
	mapSet.Set("specqbft \"github.com/bloxapp/ssv-spec/qbft\"", "\"github.com/bloxapp/ssv-spec/qbft\"")
	mapSet.Set("specssv \"github.com/bloxapp/ssv-spec/ssv\"\n", "")
	mapSet.Set("spectypes \"github.com/bloxapp/ssv-spec/types\"", "\"github.com/bloxapp/ssv-spec/types\"")
	//mapSet.Set("\"go.uber.org/zap\"\n\n", "")
	mapSet.Set("\"github.com/bloxapp/ssv/protocol/v2/qbft/controller\"\n", "")
	//mapSet.Set("logger   *zap.Logger\n", "")
	//mapSet.Set("logger := logger.With(zap.String(\"validator\", hex.EncodeToString(share.ValidatorPubKey)))\n", "")
	//mapSet.Set("logger:         logger.With(zap.String(\"who\", \"BaseRunner\")),\n", "")
	//mapSet.Set("logger:   logger.With(zap.String(\"who\", \"SyncCommitteeRunner\")),", "")
	mapSet.Set("specssv.", "")
	mapSet.Set("specqbft.", "qbft.")
	mapSet.Set("spectypes.", "types.")
	mapSet.Set("controller.Controller", "qbft.Controller")

	mapSet.Set("logger.Debug(\"✅ successfully submitted sync committee!\", fields.Slot(msg.Slot), fields.Height(r.BaseRunner.QBFTController.Height))\n", "")

	return mapSet.Range()
}
func SpecSyncCommitteeSet() []utils.KeyValue {
	var mapSet = utils.NewMap()

	// Handled in gc client
	mapSet.Set("// TODO - waitOneThirdOrValidBlock\n", "")
	return mapSet.Range()
}

func SyncCommitteeAggregatorSet() []utils.KeyValue {
	var mapSet = utils.NewMap()

	mapSet.Set("package runner", "package ssv")
	//mapSet.Set("\"encoding/hex\"", "")
	mapSet.Set("specqbft \"github.com/bloxapp/ssv-spec/qbft\"", "\"github.com/bloxapp/ssv-spec/qbft\"")
	mapSet.Set("specssv \"github.com/bloxapp/ssv-spec/ssv\"\n", "")
	mapSet.Set("spectypes \"github.com/bloxapp/ssv-spec/types\"", "\"github.com/bloxapp/ssv-spec/types\"")
	//mapSet.Set("\"go.uber.org/zap\"\n\n", "")
	mapSet.Set("\"github.com/bloxapp/ssv/protocol/v2/qbft/controller\"\n", "")
	//mapSet.Set("logger   *zap.Logger\n", "")
	//mapSet.Set("logger := logger.With(zap.String(\"validator\", hex.EncodeToString(share.ValidatorPubKey)))\n", "")
	//mapSet.Set("logger:         logger.With(zap.String(\"who\", \"BaseRunner\")),\n", "")
	//mapSet.Set("logger:   logger.With(zap.String(\"who\", \"SyncCommitteeAggregatorRunner\")),", "")
	mapSet.Set("specssv.", "")
	mapSet.Set("specqbft.", "qbft.")
	mapSet.Set("spectypes.", "types.")
	mapSet.Set("controller.Controller", "qbft.Controller")

	mapSet.Set("logger.Debug(\"✅ submitted successfully sync committee aggregator!\")\n", "")

	return mapSet.Range()
}
func SpecSyncCommitteeAggregatorSet() []utils.KeyValue {
	var mapSet = utils.NewMap()
	return mapSet.Range()
}

func RunnerValidationsSet() []utils.KeyValue {
	var mapSet = utils.NewMap()

	mapSet.Set("package runner", "package ssv")
	//mapSet.Set("specssv \"github.com/bloxapp/ssv-spec/ssv\"\n", "")
	//mapSet.Set("specssv.", "")

	return mapSet.Range()
}
func SpecRunnerValidationsSet() []utils.KeyValue {
	var mapSet = utils.NewMap()
	return mapSet.Range()
}

func RunnerSignaturesSet() []utils.KeyValue {
	var mapSet = utils.NewMap()

	mapSet.Set("package runner", "package ssv")
	//mapSet.Set("specssv \"github.com/bloxapp/ssv-spec/ssv\"\n", "")
	//mapSet.Set("specssv.", "")

	return mapSet.Range()
}
func SpecSRunnerSignaturesSet() []utils.KeyValue {
	var mapSet = utils.NewMap()
	return mapSet.Range()
}
