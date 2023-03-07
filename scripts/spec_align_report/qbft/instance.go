package qbft

import (
	"github.com/bloxapp/ssv/scripts/spec_align_report/utils"
)

func ProcessInstance() {

	if err := utils.Mkdir(utils.DataPath+"/instance", true); err != nil {
		panic(err)
	}
	instanceCompareStruct := initInstanceCompareStruct()
	if err := instanceCompareStruct.ReplaceMap(); err != nil {
		panic(err)
	}
	_ = instanceCompareStruct.Run()

	proposalCompareStruct := initProposalCompareStruct()
	if err := proposalCompareStruct.ReplaceMap(); err != nil {
		panic(err)
	}
	_ = proposalCompareStruct.Run()

	prepareCompareStruct := initPrepareCompareStruct()
	if err := prepareCompareStruct.ReplaceMap(); err != nil {
		panic(err)
	}
	_ = prepareCompareStruct.Run()

	commitCompareStruct := initCommitCompareStruct()
	if err := commitCompareStruct.ReplaceMap(); err != nil {
		panic(err)
	}
	_ = commitCompareStruct.Run()

	roundChangeCompareStruct := initRoundChangeCompareStruct()
	if err := roundChangeCompareStruct.ReplaceMap(); err != nil {
		panic(err)
	}
	_ = roundChangeCompareStruct.Run()

}
func initInstanceCompareStruct() *utils.Compare {
	c := &utils.Compare{
		Name:        "instance",
		Replace:     InstanceSet(),
		SpecReplace: SpecInstanceSet(),
		SSVPath:     utils.DataPath + "/instance/instance.go",
		SpecPath:    utils.DataPath + "/instance/instance_spec.go",
	}
	if err := utils.Copy("./protocol/qbft/instance/instance.go", c.SSVPath); err != nil {
		panic(err)
	}
	if err := utils.Copy("./scripts/spec_align_report/ssv-spec/qbft/instance.go", c.SpecPath); err != nil {
		panic(err)
	}
	return c
}
func initProposalCompareStruct() *utils.Compare {
	c := &utils.Compare{
		Name:        "proposal",
		Replace:     ProposalSet(),
		SpecReplace: SpecProposalSet(),
		SSVPath:     utils.DataPath + "/instance/proposal.go",
		SpecPath:    utils.DataPath + "/instance/proposal_spec.go",
	}
	if err := utils.Copy("./protocol/qbft/instance/proposal.go", c.SSVPath); err != nil {
		panic(err)
	}
	if err := utils.Copy("./scripts/spec_align_report/ssv-spec/qbft/proposal.go", c.SpecPath); err != nil {
		panic(err)
	}
	return c
}
func initPrepareCompareStruct() *utils.Compare {
	c := &utils.Compare{
		Name:        "prepare",
		Replace:     PrepareSet(),
		SpecReplace: SpecPrepareSet(),
		SSVPath:     utils.DataPath + "/instance/prepare.go",
		SpecPath:    utils.DataPath + "/instance/prepare_spec.go",
	}
	if err := utils.Copy("./protocol/qbft/instance/prepare.go", c.SSVPath); err != nil {
		panic(err)
	}
	if err := utils.Copy("./scripts/spec_align_report/ssv-spec/qbft/prepare.go", c.SpecPath); err != nil {
		panic(err)
	}
	return c
}
func initCommitCompareStruct() *utils.Compare {
	c := &utils.Compare{
		Name:        "commit",
		Replace:     CommitSet(),
		SpecReplace: SpecCommitSet(),
		SSVPath:     utils.DataPath + "/instance/commit.go",
		SpecPath:    utils.DataPath + "/instance/commit_spec.go",
	}
	if err := utils.Copy("./protocol/qbft/instance/commit.go", c.SSVPath); err != nil {
		panic(err)
	}
	if err := utils.Copy("./scripts/spec_align_report/ssv-spec/qbft/commit.go", c.SpecPath); err != nil {
		panic(err)
	}
	return c
}
func initRoundChangeCompareStruct() *utils.Compare {
	c := &utils.Compare{
		Name:        "round_change",
		Replace:     RoundChangeSet(),
		SpecReplace: SpecRoundChangeSet(),
		SSVPath:     utils.DataPath + "/instance/round_change.go",
		SpecPath:    utils.DataPath + "/instance/round_change_spec.go",
	}
	if err := utils.Copy("./protocol/qbft/instance/round_change.go", c.SSVPath); err != nil {
		panic(err)
	}
	if err := utils.Copy("./scripts/spec_align_report/ssv-spec/qbft/round_change.go", c.SpecPath); err != nil {
		panic(err)
	}
	return c
}
