package qbft

import "github.com/bloxapp/ssv/scripts/spec_align_report/utils"

// Instance mapping

func InstanceSet() []utils.KeyValue {
	var instanceMap = utils.NewMap()

	// list of changes package names & imports between spec and implementation
	instanceMap.Set("package instance", "package qbft")
	instanceMap.Set("spectypes \"github.com/bloxapp/ssv-spec/types\"\n", "")
	instanceMap.Set("specqbft \"github.com/bloxapp/ssv-spec/qbft\"\n", "")
	instanceMap.Set("logging \"github.com/ipfs/go-log\"\n", "")
	instanceMap.Set("\"github.com/bloxapp/ssv/protocol/v2/qbft\"\n", "")
	instanceMap.Set("\"go.uber.org/zap\"\n\n", "")
	instanceMap.Set("specqbft.", "")
	instanceMap.Set("spectypes.", "types.")
	instanceMap.Set("qbft.IConfig", "IConfig")

	// list of approved changes in code between spec and implementation
	instanceMap.Set("var logger = logging.Logger(\"ssv/protocol/qbft/instance\").Desugar()", "")
	instanceMap.Set("logger *zap.Logger", "")
	instanceMap.Set("logger: logger.With(zap.String(\"identifier\", types.MessageIDFromBytes(identifier).String()),\n\t\t\tzap.Uint64(\"height\", uint64(height))),", "")
	instanceMap.Set("i.logger.Debug(\"starting QBFT instance\")", "")
	instanceMap.Set("if err != nil {\n\t\t\t\ti.logger.Warn(\"failed to create proposal\", zap.Error(err))\n\t\t\t\t// TODO align spec to add else to avoid broadcast errored proposal\n\t\t\t} else {\n\t\t\t\t// nolint\n\t\t\t\tif err := i.Broadcast(proposal); err != nil {\n\t\t\t\t\ti.logger.Warn(\"failed to broadcast proposal\", zap.Error(err))\n\t\t\t\t}\n\t\t\t}",
		"if err != nil {\n\t\t\t\tfmt.Printf(\"%s\\n\", err.Error())\n\t\t\t}\n\t\t\t// nolint\n\t\t\tif err := i.Broadcast(proposal); err != nil {\n\t\t\t\tfmt.Printf(\"%s\\n\", err.Error())\n\t\t\t}")
	instanceMap.Set("// SetConfig returns the instance config\nfunc (i *Instance) SetConfig(config IConfig) {\n\ti.config = config\n}", "")

	return instanceMap.Range()
}

func SpecInstanceSet() []utils.KeyValue {
	var specInstanceMap = utils.NewMap()
	specInstanceMap.Set("\"fmt\"\n", "")
	specInstanceMap.Set("\"github.com/bloxapp/ssv-spec/types\"\n", "")

	// We import from spec - so for the diff we remove it from spec
	specInstanceMap.Set("type ProposedValueCheckF func(data []byte) error", "")
	specInstanceMap.Set("type ProposerF func(state *State, round Round) types.OperatorID", "")

	return specInstanceMap.Range()

}

func ProposalSet() []utils.KeyValue {
	var proposalMap = utils.NewMap()

	// list of changes package names & imports between spec and implementation
	proposalMap.Set("package instance", "package qbft")
	proposalMap.Set("spectypes \"github.com/bloxapp/ssv-spec/types\"", "\"github.com/bloxapp/ssv-spec/types\"")
	proposalMap.Set("\"go.uber.org/zap\"\n", "")
	proposalMap.Set("\"github.com/bloxapp/ssv/protocol/v2/qbft\"\n", "")

	proposalMap.Set("qbft.IConfig", "IConfig")
	proposalMap.Set("specqbft \"github.com/bloxapp/ssv-spec/qbft\"\n", "")

	proposalMap.Set("specqbft.", "")
	proposalMap.Set("spectypes.", "types.")

	// list of approved changes in code between spec and implementation
	proposalMap.Set("i.logger.Debug(\"got proposal, broadcasting prepare message\",\n\t\tzap.Uint64(\"round\", uint64(i.State.Round)),\n\t\tzap.Any(\"signers\", prepare.Signers))\n", "")

	return proposalMap.Range()
}
func SpecProposalSet() []utils.KeyValue {
	var specProposalMap = utils.NewMap()
	return specProposalMap.Range()
}

func PrepareSet() []utils.KeyValue {
	var prepareMap = utils.NewMap()
	// list of changes package names & imports between spec and implementation

	prepareMap.Set("package instance", "package qbft")
	prepareMap.Set("spectypes \"github.com/bloxapp/ssv-spec/types\"", "\"github.com/bloxapp/ssv-spec/types\"")
	prepareMap.Set("qbft.IConfig", "IConfig")
	prepareMap.Set("specqbft \"github.com/bloxapp/ssv-spec/qbft\"\n", "")
	prepareMap.Set("\"go.uber.org/zap\"\n", "")
	prepareMap.Set("\"github.com/bloxapp/ssv/protocol/v2/qbft\"\n", "")

	prepareMap.Set("specqbft.", "")
	prepareMap.Set("spectypes.", "types.")
	prepareMap.Set("i.logger.Debug(\"got prepare quorum, broadcasting commit message\", // TODO: what node?\n\t\tzap.Uint64(\"round\", uint64(i.State.Round)),\n\t\tzap.Any(\"signers\", commitMsg.Signers))\n", "")
	return prepareMap.Range()
}
func SpecPrepareSet() []utils.KeyValue {
	var specPrepareMap = utils.NewMap()
	return specPrepareMap.Range()
}

func CommitSet() []utils.KeyValue {
	var commitMap = utils.NewMap()
	// list of changes package names & imports between spec and implementation

	commitMap.Set("package instance", "package qbft")
	commitMap.Set("spectypes \"github.com/bloxapp/ssv-spec/types\"", "\"github.com/bloxapp/ssv-spec/types\"")
	commitMap.Set("qbft.IConfig", "IConfig")
	commitMap.Set("specqbft \"github.com/bloxapp/ssv-spec/qbft\"\n", "")
	commitMap.Set("\"go.uber.org/zap\"\n", "")
	commitMap.Set("\"github.com/bloxapp/ssv/protocol/v2/qbft\"\n", "")

	commitMap.Set("specqbft.", "")
	commitMap.Set("spectypes.", "types.")
	commitMap.Set("BaseCommitValidation", "baseCommitValidation")
	commitMap.Set("i.logger.Debug(\"got commit quorum\", zap.Any(\"signers\", agg.Signers))", "")
	return commitMap.Range()
}
func SpecCommitSet() []utils.KeyValue {
	var specCommitMap = utils.NewMap()
	return specCommitMap.Range()
}

func RoundChangeSet() []utils.KeyValue {
	var roundChangeMap = utils.NewMap()
	// list of changes package names & imports between spec and implementation

	roundChangeMap.Set("package instance", "package qbft")
	roundChangeMap.Set("spectypes \"github.com/bloxapp/ssv-spec/types\"", "\"github.com/bloxapp/ssv-spec/types\"")
	roundChangeMap.Set("qbft.IConfig", "IConfig")
	roundChangeMap.Set("specqbft \"github.com/bloxapp/ssv-spec/qbft\"\n", "")
	roundChangeMap.Set("\"go.uber.org/zap\"\n", "")
	roundChangeMap.Set("\"github.com/bloxapp/ssv/protocol/v2/qbft\"\n", "")

	roundChangeMap.Set("specqbft.", "")
	roundChangeMap.Set("spectypes.", "types.")
	roundChangeMap.Set("i.logger.Debug(\"got justified change round, broadcasting proposal message\",\n\t\t\tzap.Uint64(\"round\", uint64(i.State.Round)))", "")
	return roundChangeMap.Range()
}
func SpecRoundChangeSet() []utils.KeyValue {
	var specRoundChangeMap = utils.NewMap()
	specRoundChangeMap.Set("could not get proposal justification for leading ronud", "could not get proposal justification for leading round")

	return specRoundChangeMap.Range()
}
