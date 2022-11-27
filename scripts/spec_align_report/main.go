package main

import (
	"fmt"
	"github.com/bloxapp/ssv/scripts/spec_align_report/mapping"
	"github.com/bloxapp/ssv/scripts/spec_align_report/utils"
)

const specDataPath = "./scripts/spec_align_report/data"

func main() {
	if err := utils.CloneSpec("v0.2.7"); err != nil {
		fmt.Println(err)
		return
	}
	if err := utils.Mkdir(specDataPath, true); err != nil {
		fmt.Println(err)
		return
	}

	processController()

	processInstance()
	//utils.CleanSpecPath()

	fmt.Println("done")
}

// ########### QBFT #####################################

// Controller
func processController() {
	if err := utils.Mkdir(specDataPath+"/controller", true); err != nil {
		panic(err)
	}

	controllerCompareStruct := initControllerCompareStruct()
	if err := controllerCompareStruct.ReplaceMap(); err != nil {
		panic(err)
	}

	if err := utils.GitDiff(controllerCompareStruct.SSVPath, controllerCompareStruct.SpecPath, specDataPath+"/controller.diff"); err != nil {
		fmt.Println(utils.Error("Controller is not aligned to spec: " + specDataPath + "/controller.diff"))
	} else {
		fmt.Println(utils.Success("Controller is aligned to spec"))
	}

	decidedCompareStruct := initDecidedCompareStruct()
	if err := decidedCompareStruct.ReplaceMap(); err != nil {
		panic(err)
	}

	if err := utils.GitDiff(decidedCompareStruct.SSVPath, decidedCompareStruct.SpecPath, specDataPath+"/decided.diff"); err != nil {
		fmt.Println(utils.Error("Decided is not aligned to spec: " + specDataPath + "/decided.diff"))
	} else {
		fmt.Println(utils.Success("Decided is aligned to spec"))
	}

	futureMsgCompareStruct := initFutureMsgCompareStruct()
	if err := futureMsgCompareStruct.ReplaceMap(); err != nil {
		panic(err)
	}

	if err := utils.GitDiff(futureMsgCompareStruct.SSVPath, futureMsgCompareStruct.SpecPath, specDataPath+"/decided.diff"); err != nil {
		fmt.Println(utils.Error("FutureMsg is not aligned to spec: " + specDataPath + "/decided.diff"))
	} else {
		fmt.Println(utils.Success("FutureMsg is aligned to spec"))
	}

}
func initControllerCompareStruct() *utils.Compare {
	c := &utils.Compare{
		Replace:     mapping.ControllerSet(),
		SpecReplace: mapping.SpecControllerSet(),
		SSVPath:     specDataPath + "/controller/controller.go",
		SpecPath:    specDataPath + "/controller/controller_spec.go",
	}
	if err := utils.Copy("./protocol/v2/qbft/controller/controller.go", c.SSVPath); err != nil {
		panic(err)
	}
	if err := utils.Copy("./scripts/spec_align_report/ssv-spec/qbft/controller.go", c.SpecPath); err != nil {
		panic(err)
	}
	return c
}
func initDecidedCompareStruct() *utils.Compare {
	c := &utils.Compare{
		Replace:     mapping.DecidedSet(),
		SpecReplace: mapping.SpecDecidedSet(),
		SSVPath:     specDataPath + "/controller/decided.go",
		SpecPath:    specDataPath + "/controller/decided_spec.go",
	}
	if err := utils.Copy("./protocol/v2/qbft/controller/decided.go", c.SSVPath); err != nil {
		panic(err)
	}
	if err := utils.Copy("./scripts/spec_align_report/ssv-spec/qbft/decided.go", c.SpecPath); err != nil {
		panic(err)
	}
	return c
}
func initFutureMsgCompareStruct() *utils.Compare {
	c := &utils.Compare{
		Replace:     mapping.FutureMessageSet(),
		SpecReplace: mapping.SpecFutureMessageSet(),
		SSVPath:     specDataPath + "/controller/future_msg.go",
		SpecPath:    specDataPath + "/controller/future_msg_spec.go",
	}
	if err := utils.Copy("./protocol/v2/qbft/controller/future_msg.go", c.SSVPath); err != nil {
		panic(err)
	}
	if err := utils.Copy("./scripts/spec_align_report/ssv-spec/qbft/future_msg.go", c.SpecPath); err != nil {
		panic(err)
	}
	return c
}

// Instance
func processInstance() {
	if err := utils.Mkdir(specDataPath+"/instance", true); err != nil {
		panic(err)
	}
	instanceCompareStruct := initInstanceCompareStruct()
	if err := instanceCompareStruct.ReplaceMap(); err != nil {
		panic(err)
	}

	if err := utils.GitDiff(instanceCompareStruct.SSVPath, instanceCompareStruct.SpecPath, specDataPath+"/instance.diff"); err != nil {
		fmt.Println(utils.Error("Instance is not aligned to spec: " + specDataPath + "/instance.diff"))
	} else {
		fmt.Println(utils.Success("Instance is aligned to spec"))
	}

	proposalCompareStruct := initProposalCompareStruct()
	if err := proposalCompareStruct.ReplaceMap(); err != nil {
		panic(err)
	}

	if err := utils.GitDiff(proposalCompareStruct.SSVPath, proposalCompareStruct.SpecPath, specDataPath+"/proposal.diff"); err != nil {
		fmt.Println(utils.Error("Proposal is not aligned to spec: " + specDataPath + "/proposal.diff"))
	} else {
		fmt.Println(utils.Success("Proposal is aligned to spec"))
	}

	prepareCompareStruct := initPrepareCompareStruct()
	if err := prepareCompareStruct.ReplaceMap(); err != nil {
		panic(err)
	}

	if err := utils.GitDiff(prepareCompareStruct.SSVPath, prepareCompareStruct.SpecPath, specDataPath+"/prepare.diff"); err != nil {
		fmt.Println(utils.Error("Prepare is not aligned to spec: " + specDataPath + "/prepare.diff"))
	} else {
		fmt.Println(utils.Success("Prepare is aligned to spec"))
	}

	commitCompareStruct := initCommitCompareStruct()
	if err := commitCompareStruct.ReplaceMap(); err != nil {
		panic(err)
	}

	if err := utils.GitDiff(commitCompareStruct.SSVPath, commitCompareStruct.SpecPath, specDataPath+"/commit.diff"); err != nil {
		fmt.Println(utils.Error("Commit is not aligned to spec: " + specDataPath + "/commit.diff"))
	} else {
		fmt.Println(utils.Success("Commit is aligned to spec"))
	}

	roundChangeCompareStruct := initRoundChangeCompareStruct()
	if err := roundChangeCompareStruct.ReplaceMap(); err != nil {
		panic(err)
	}

	if err := utils.GitDiff(roundChangeCompareStruct.SSVPath, roundChangeCompareStruct.SpecPath, specDataPath+"/round_change.diff"); err != nil {
		fmt.Println(utils.Error("RoundChange is not aligned to spec: " + specDataPath + "/round_change.diff"))
	} else {
		fmt.Println(utils.Success("RoundChange is aligned to spec"))
	}

}
func initInstanceCompareStruct() *utils.Compare {
	c := &utils.Compare{
		Replace:     mapping.InstanceSet(),
		SpecReplace: mapping.SpecInstanceSet(),
		SSVPath:     specDataPath + "/instance/instance.go",
		SpecPath:    specDataPath + "/instance/instance_spec.go",
	}
	if err := utils.Copy("./protocol/v2/qbft/instance/instance.go", c.SSVPath); err != nil {
		panic(err)
	}
	if err := utils.Copy("./scripts/spec_align_report/ssv-spec/qbft/instance.go", c.SpecPath); err != nil {
		panic(err)
	}
	return c
}
func initProposalCompareStruct() *utils.Compare {
	c := &utils.Compare{
		Replace:     mapping.ProposalSet(),
		SpecReplace: mapping.SpecProposalSet(),
		SSVPath:     specDataPath + "/instance/proposal.go",
		SpecPath:    specDataPath + "/instance/proposal_spec.go",
	}
	if err := utils.Copy("./protocol/v2/qbft/instance/proposal.go", c.SSVPath); err != nil {
		panic(err)
	}
	if err := utils.Copy("./scripts/spec_align_report/ssv-spec/qbft/proposal.go", c.SpecPath); err != nil {
		panic(err)
	}
	return c
}
func initPrepareCompareStruct() *utils.Compare {
	c := &utils.Compare{
		Replace:     mapping.PrepareSet(),
		SpecReplace: mapping.SpecPrepareSet(),
		SSVPath:     specDataPath + "/instance/prepare.go",
		SpecPath:    specDataPath + "/instance/prepare_spec.go",
	}
	if err := utils.Copy("./protocol/v2/qbft/instance/prepare.go", c.SSVPath); err != nil {
		panic(err)
	}
	if err := utils.Copy("./scripts/spec_align_report/ssv-spec/qbft/prepare.go", c.SpecPath); err != nil {
		panic(err)
	}
	return c
}
func initCommitCompareStruct() *utils.Compare {
	c := &utils.Compare{
		Replace:     mapping.CommitSet(),
		SpecReplace: mapping.SpecCommitSet(),
		SSVPath:     specDataPath + "/instance/commit.go",
		SpecPath:    specDataPath + "/instance/commit_spec.go",
	}
	if err := utils.Copy("./protocol/v2/qbft/instance/commit.go", c.SSVPath); err != nil {
		panic(err)
	}
	if err := utils.Copy("./scripts/spec_align_report/ssv-spec/qbft/commit.go", c.SpecPath); err != nil {
		panic(err)
	}
	return c
}
func initRoundChangeCompareStruct() *utils.Compare {
	c := &utils.Compare{
		Replace:     mapping.RoundChangeSet(),
		SpecReplace: mapping.SpecRoundChangeSet(),
		SSVPath:     specDataPath + "/instance/round_change.go",
		SpecPath:    specDataPath + "/instance/round_change_spec.go",
	}
	if err := utils.Copy("./protocol/v2/qbft/instance/round_change.go", c.SSVPath); err != nil {
		panic(err)
	}
	if err := utils.Copy("./scripts/spec_align_report/ssv-spec/qbft/round_change.go", c.SpecPath); err != nil {
		panic(err)
	}
	return c
}

// ########### SSV #####################################
