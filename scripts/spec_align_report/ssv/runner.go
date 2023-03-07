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
	_ = runnerCompareStruct.Run()

	runnerStateCompareStruct := initRunnerStateCompareStruct()
	if err := runnerStateCompareStruct.ReplaceMap(); err != nil {
		panic(err)
	}
	_ = runnerStateCompareStruct.Run()

	aggregatorCompareStruct := initAggregatorCompareStruct()
	if err := aggregatorCompareStruct.ReplaceMap(); err != nil {
		panic(err)
	}
	_ = aggregatorCompareStruct.Run()

	attesterCompareStruct := initAttesterCompareStruct()
	if err := attesterCompareStruct.ReplaceMap(); err != nil {
		panic(err)
	}
	_ = attesterCompareStruct.Run()

	proposerCompareStruct := initProposerCompareStruct()
	if err := proposerCompareStruct.ReplaceMap(); err != nil {
		panic(err)
	}
	_ = proposerCompareStruct.Run()

	syncCommitteeCompareStruct := initSyncCommitteeCompareStruct()
	if err := syncCommitteeCompareStruct.ReplaceMap(); err != nil {
		panic(err)
	}
	_ = syncCommitteeCompareStruct.Run()

	syncCommitteeAggregatorCompareStruct := initSyncCommitteeAggregatorCompareStruct()
	if err := syncCommitteeAggregatorCompareStruct.ReplaceMap(); err != nil {
		panic(err)
	}
	_ = syncCommitteeAggregatorCompareStruct.Run()

	runnerValidationsCompareStruct := initRunnerValidationsCompareStruct()
	if err := runnerValidationsCompareStruct.ReplaceMap(); err != nil {
		panic(err)
	}
	_ = runnerValidationsCompareStruct.Run()

	runnerSignaturesCompareStruct := initRunnerSignaturesCompareStruct()
	if err := runnerSignaturesCompareStruct.ReplaceMap(); err != nil {
		panic(err)
	}
	_ = runnerSignaturesCompareStruct.Run()

}

func initRunnerCompareStruct() *utils.Compare {
	c := &utils.Compare{
		Name:        "runner",
		Replace:     RunnerSet(),
		SpecReplace: SpecRunnerSet(),
		SSVPath:     utils.DataPath + "/runner/runner.go",
		SpecPath:    utils.DataPath + "/runner/runner_spec.go",
	}
	if err := utils.Copy("./protocol/ssv/runner/runner.go", c.SSVPath); err != nil {
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
	if err := utils.Copy("./protocol/ssv/runner/runner_state.go", c.SSVPath); err != nil {
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
	if err := utils.Copy("./protocol/ssv/runner/aggregator.go", c.SSVPath); err != nil {
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
	if err := utils.Copy("./protocol/ssv/runner/attester.go", c.SSVPath); err != nil {
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
	if err := utils.Copy("./protocol/ssv/runner/proposer.go", c.SSVPath); err != nil {
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
	if err := utils.Copy("./protocol/ssv/runner/sync_committee.go", c.SSVPath); err != nil {
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
	if err := utils.Copy("./protocol/ssv/runner/sync_committee_aggregator.go", c.SSVPath); err != nil {
		panic(err)
	}
	if err := utils.Copy("./scripts/spec_align_report/ssv-spec/ssv/sync_committee_aggregator.go", c.SpecPath); err != nil {
		panic(err)
	}
	return c
}

func initRunnerValidationsCompareStruct() *utils.Compare {
	c := &utils.Compare{
		Name:        "runner_validations",
		Replace:     RunnerValidationsSet(),
		SpecReplace: SpecRunnerValidationsSet(),
		SSVPath:     utils.DataPath + "/runner/runner_validations.go",
		SpecPath:    utils.DataPath + "/runner/runner_validations_spec.go",
	}
	if err := utils.Copy("./protocol/ssv/runner/runner_validations.go", c.SSVPath); err != nil {
		panic(err)
	}
	if err := utils.Copy("./scripts/spec_align_report/ssv-spec/ssv/runner_validations.go", c.SpecPath); err != nil {
		panic(err)
	}
	return c
}

func initRunnerSignaturesCompareStruct() *utils.Compare {
	c := &utils.Compare{
		Name:        "runner_signatures",
		Replace:     RunnerSignaturesSet(),
		SpecReplace: SpecSRunnerSignaturesSet(),
		SSVPath:     utils.DataPath + "/runner/runner_signatures.go",
		SpecPath:    utils.DataPath + "/runner/runner_signatures_spec.go",
	}
	if err := utils.Copy("./protocol/ssv/runner/runner_signatures.go", c.SSVPath); err != nil {
		panic(err)
	}
	if err := utils.Copy("./scripts/spec_align_report/ssv-spec/ssv/runner_signatures.go", c.SpecPath); err != nil {
		panic(err)
	}
	return c
}
