package qbft

import "github.com/bloxapp/ssv/scripts/spec_align_report/utils"

// Instance mapping

func InstanceSet() []utils.KeyValue {
	var instanceMap = utils.NewMap()

	// list of changes package names & imports between spec and implementation
	instanceMap.Set("package instance", "package qbft")
	instanceMap.Set("spectypes \"github.com/bloxapp/ssv-spec/types\"", "\"github.com/bloxapp/ssv-spec/types\"")
	instanceMap.Set("specqbft \"github.com/bloxapp/ssv-spec/qbft\"\n", "")
	instanceMap.Set("\"github.com/bloxapp/ssv/protocol/v2/types\"\n", "")
	instanceMap.Set("specqbft.", "")
	instanceMap.Set("spectypes.", "types.")
	instanceMap.Set("types.IConfig", "IConfig")

	// list of approved changes in code between spec and implementation
	instanceMap.Set("// TODO align spec to add else to avoid broadcast errored proposal\n\t\t\t} else {\n\t\t\t\t// nolint\n\t\t\t\tif err := i.Broadcast(proposal); err != nil {\n\t\t\t\t\tfmt.Printf(\"%s\\n\", err.Error())\n\t\t\t\t}\n\t\t\t}",
		"}\n\t\t\t// nolint\n\t\t\tif err := i.Broadcast(proposal); err != nil {\n\t\t\t\tfmt.Printf(\"%s\\n\", err.Error())\n\t\t\t}")
	instanceMap.Set("// SetConfig returns the instance config\nfunc (i *Instance) SetConfig(config IConfig) {\n\ti.config = config\n}", "")
	instanceMap.Set("// TODO will be removed upon https://github.com/bloxapp/SIPs/blob/main/sips/constant_qbft_timeout.md\n\t\tgo syncHighestRoundChange(i.config.GetNetwork(), types.MessageIDFromBytes(i.State.ID), i.State.Height)",
		"if err := i.config.GetNetwork().SyncHighestRoundChange(types.MessageIDFromBytes(i.State.ID), i.State.Height); err != nil {\n\t\t\tfmt.Printf(\"%s\\n\", err.Error())\n\t\t}")
	instanceMap.Set("func syncHighestRoundChange(syncer Syncer, mid types.MessageID, h Height) {\n\tif err := syncer.SyncHighestRoundChange(mid, h); err != nil {\n\t\tfmt.Printf(\"%s\\n\", err.Error())\n\t}\n}", "")

	return instanceMap.Range()
}

func SpecInstanceSet() []utils.KeyValue {
	var specInstanceMap = utils.NewMap()
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
	proposalMap.Set("types.IConfig", "IConfig")
	proposalMap.Set("specqbft \"github.com/bloxapp/ssv-spec/qbft\"\n", "")
	proposalMap.Set("\"github.com/bloxapp/ssv/protocol/v2/types\"\n", "")

	proposalMap.Set("specqbft.", "")
	proposalMap.Set("spectypes.", "types.")

	// list of approved changes in code between spec and implementation

	return proposalMap.Range()
}
func SpecProposalSet() []utils.KeyValue {
	var specProposalMap = utils.NewMap()
	// redundant else
	specProposalMap.Set("if round == FirstRound {\n\t\treturn nil\n\t} else {", "if round == FirstRound {\n\t\treturn nil\n\t}")
	specProposalMap.Set("if !previouslyPrepared {\n\t\t\treturn nil\n\t\t} else {", "if !previouslyPrepared {\n\t\t\treturn nil\n\t\t}")
	specProposalMap.Set("\t\t\t\t\treturn errors.New(\"signed prepare not valid\")\n\t\t\t\t}\n\t\t\t}\n\t\t\treturn nil\n\t\t}\n\t}\n}",
		"\t\t\treturn errors.New(\"signed prepare not valid\")\n\t\t}\n\t}\n\treturn nil\n}")

	return specProposalMap.Range()
}

func PrepareSet() []utils.KeyValue {
	var prepareMap = utils.NewMap()
	// list of changes package names & imports between spec and implementation

	prepareMap.Set("package instance", "package qbft")
	prepareMap.Set("spectypes \"github.com/bloxapp/ssv-spec/types\"", "\"github.com/bloxapp/ssv-spec/types\"")
	prepareMap.Set("types.IConfig", "IConfig")
	prepareMap.Set("specqbft \"github.com/bloxapp/ssv-spec/qbft\"\n", "")
	prepareMap.Set("\"github.com/bloxapp/ssv/protocol/v2/types\"\n", "")

	prepareMap.Set("specqbft.", "")
	prepareMap.Set("spectypes.", "types.")
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
	commitMap.Set("types.IConfig", "IConfig")
	commitMap.Set("specqbft \"github.com/bloxapp/ssv-spec/qbft\"\n", "")
	commitMap.Set("\"github.com/bloxapp/ssv/protocol/v2/types\"\n", "")

	commitMap.Set("specqbft.", "")
	commitMap.Set("spectypes.", "types.")
	commitMap.Set("BaseCommitValidation", "baseCommitValidation")
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
	roundChangeMap.Set("types.IConfig", "IConfig")
	roundChangeMap.Set("specqbft \"github.com/bloxapp/ssv-spec/qbft\"\n", "")
	roundChangeMap.Set("\"github.com/bloxapp/ssv/protocol/v2/types\"\n", "")

	roundChangeMap.Set("specqbft.", "")
	roundChangeMap.Set("spectypes.", "types.")
	return roundChangeMap.Range()
}
func SpecRoundChangeSet() []utils.KeyValue {
	var specRoundChangeMap = utils.NewMap()
	specRoundChangeMap.Set("could not get proposal justification for leading ronud", "could not get proposal justification for leading round")

	return specRoundChangeMap.Range()
}
