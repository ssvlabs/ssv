package ssv

import "github.com/bloxapp/ssv/scripts/spec_align_report/utils"

func ProcessRunner() {
	if err := utils.Mkdir(utils.DataPath+"/runner", true); err != nil {
		panic(err)
	}

	runnerCompareStruct := initRunnerCompareStruct()
	if err := runnerCompareStruct.ReplaceMap(); err != nil {
		panic(err)
	}
	runnerCompareStruct.Run()

	runnerStateCompareStruct := initRunnerStateCompareStruct()
	if err := runnerStateCompareStruct.ReplaceMap(); err != nil {
		panic(err)
	}
	runnerStateCompareStruct.Run()

	aggregatorCompareStruct := initAggregatorCompareStruct()
	if err := aggregatorCompareStruct.ReplaceMap(); err != nil {
		panic(err)
	}
	aggregatorCompareStruct.Run()

	attesterCompareStruct := initAttesterCompareStruct()
	if err := attesterCompareStruct.ReplaceMap(); err != nil {
		panic(err)
	}
	attesterCompareStruct.Run()

	proposerCompareStruct := initProposerCompareStruct()
	if err := proposerCompareStruct.ReplaceMap(); err != nil {
		panic(err)
	}
	proposerCompareStruct.Run()

	syncCommitteeCompareStruct := initSyncCommitteeCompareStruct()
	if err := syncCommitteeCompareStruct.ReplaceMap(); err != nil {
		panic(err)
	}
	syncCommitteeCompareStruct.Run()

	syncCommitteeAggregator := initSyncCommitteeAggregatorCompareStruct()
	if err := syncCommitteeAggregator.ReplaceMap(); err != nil {
		panic(err)
	}
	syncCommitteeAggregator.Run()

}

func initRunnerCompareStruct() *utils.Compare {
	c := &utils.Compare{
		Name:        "runner",
		Replace:     RunnerSet(),
		SpecReplace: SpecRunnerSet(),
		SSVPath:     utils.DataPath + "/runner/runner.go",
		SpecPath:    utils.DataPath + "/runner/runner_spec.go",
	}
	if err := utils.Copy("./protocol/v2/ssv/runner/runner.go", c.SSVPath); err != nil {
		panic(err)
	}
	if err := utils.Copy("./scripts/spec_align_report/ssv-spec/ssv/runner.go", c.SpecPath); err != nil {
		panic(err)
	}
	return c
}

func initRunnerStateCompareStruct() *utils.Compare {
	c := &utils.Compare{
		Name:        "runner_state",
		Replace:     RunnerStateSet(),
		SpecReplace: SpecRunnerStateSet(),
		SSVPath:     utils.DataPath + "/runner/runner_state.go",
		SpecPath:    utils.DataPath + "/runner/runner_state_spec.go",
	}
	if err := utils.Copy("./protocol/v2/ssv/runner/runner_state.go", c.SSVPath); err != nil {
		panic(err)
	}
	if err := utils.Copy("./scripts/spec_align_report/ssv-spec/ssv/runner_state.go", c.SpecPath); err != nil {
		panic(err)
	}
	return c
}

func initAggregatorCompareStruct() *utils.Compare {
	c := &utils.Compare{
		Name:        "aggregator",
		Replace:     AggregatorSet(),
		SpecReplace: SpecAggregatorSet(),
		SSVPath:     utils.DataPath + "/runner/aggregator.go",
		SpecPath:    utils.DataPath + "/runner/aggregator_spec.go",
	}
	if err := utils.Copy("./protocol/v2/ssv/runner/aggregator.go", c.SSVPath); err != nil {
		panic(err)
	}
	if err := utils.Copy("./scripts/spec_align_report/ssv-spec/ssv/aggregator.go", c.SpecPath); err != nil {
		panic(err)
	}
	return c
}

func initAttesterCompareStruct() *utils.Compare {
	c := &utils.Compare{
		Name:        "attester",
		Replace:     AttesterSet(),
		SpecReplace: SpecAttesterSet(),
		SSVPath:     utils.DataPath + "/runner/attester.go",
		SpecPath:    utils.DataPath + "/runner/attester_spec.go",
	}
	if err := utils.Copy("./protocol/v2/ssv/runner/attester.go", c.SSVPath); err != nil {
		panic(err)
	}
	if err := utils.Copy("./scripts/spec_align_report/ssv-spec/ssv/attester.go", c.SpecPath); err != nil {
		panic(err)
	}
	return c
}

func initProposerCompareStruct() *utils.Compare {
	c := &utils.Compare{
		Name:        "proposer",
		Replace:     ProposerSet(),
		SpecReplace: SpecProposerSet(),
		SSVPath:     utils.DataPath + "/runner/proposer.go",
		SpecPath:    utils.DataPath + "/runner/proposer_spec.go",
	}
	if err := utils.Copy("./protocol/v2/ssv/runner/proposer.go", c.SSVPath); err != nil {
		panic(err)
	}
	if err := utils.Copy("./scripts/spec_align_report/ssv-spec/ssv/proposer.go", c.SpecPath); err != nil {
		panic(err)
	}
	return c
}

func initSyncCommitteeCompareStruct() *utils.Compare {
	c := &utils.Compare{
		Name:        "sync_committee",
		Replace:     SyncCommitteeSet(),
		SpecReplace: SpecSyncCommitteeSet(),
		SSVPath:     utils.DataPath + "/runner/sync_committee.go",
		SpecPath:    utils.DataPath + "/runner/sync_committee_spec.go",
	}
	if err := utils.Copy("./protocol/v2/ssv/runner/sync_committee.go", c.SSVPath); err != nil {
		panic(err)
	}
	if err := utils.Copy("./scripts/spec_align_report/ssv-spec/ssv/sync_committee.go", c.SpecPath); err != nil {
		panic(err)
	}
	return c
}

func initSyncCommitteeAggregatorCompareStruct() *utils.Compare {
	c := &utils.Compare{
		Name:        "sync_committee_aggregator",
		Replace:     SyncCommitteeAggregatorSet(),
		SpecReplace: SpecSyncCommitteeAggregatorSet(),
		SSVPath:     utils.DataPath + "/runner/sync_committee_aggregator.go",
		SpecPath:    utils.DataPath + "/runner/sync_committee_aggregator_spec.go",
	}
	if err := utils.Copy("./protocol/v2/ssv/runner/sync_committee_aggregator.go", c.SSVPath); err != nil {
		panic(err)
	}
	if err := utils.Copy("./scripts/spec_align_report/ssv-spec/ssv/sync_committee_aggregator.go", c.SpecPath); err != nil {
		panic(err)
	}
	return c
}
