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
