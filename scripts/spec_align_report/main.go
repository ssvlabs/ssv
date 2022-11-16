package main

import (
	"fmt"
	"github.com/bloxapp/ssv/scripts/spec_align_report/mapping"
	"github.com/bloxapp/ssv/scripts/spec_align_report/utils"
)
const specDataPath = "./scripts/spec_align_report/data"

func main() {
	if err:= utils.CloneSpec("v0.2.7"); err != nil {
		fmt.Println(err)
		return
	}
	if err:= utils.Mkdir(specDataPath, true); err != nil {
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
func processController()  {
	if err:= utils.Mkdir(specDataPath+"/controller", true); err != nil {
		panic(err)
	}

	controllerCompareStruct:= initControllerCompareStruct()
	if err:= controllerCompareStruct.ReplaceMap(); err != nil {
		panic(err)
	}

	if err  := utils.GitDiff(controllerCompareStruct.SSVPath,controllerCompareStruct.SpecPath, specDataPath+ "/controller.diff") ; err != nil {
		fmt.Println(utils.Error("Controller is not aligned to spec: "+ specDataPath+ "/controller.diff"))
	} else {
		fmt.Println(utils.Success("Controller is aligned to spec"))
	}


	decidedCompareStruct:= initDecidedCompareStruct()
	if err:= decidedCompareStruct.ReplaceMap(); err != nil {
		panic(err)
	}

	if err  := utils.GitDiff(decidedCompareStruct.SSVPath,decidedCompareStruct.SpecPath, specDataPath+ "/decided.diff") ; err != nil {
		fmt.Println(utils.Error("Decided is not aligned to spec: "+ specDataPath+ "/decided.diff"))
	} else {
		fmt.Println(utils.Success("Decided is aligned to spec"))
	}

	futureMsgCompareStruct:= initFutureMsgCompareStruct()
	if err:= futureMsgCompareStruct.ReplaceMap(); err != nil {
		panic(err)
	}

	if err  := utils.GitDiff(futureMsgCompareStruct.SSVPath,futureMsgCompareStruct.SpecPath, specDataPath+ "/decided.diff") ; err != nil {
		fmt.Println(utils.Error("FutureMsg is not aligned to spec: "+ specDataPath+ "/decided.diff"))
	} else {
		fmt.Println(utils.Success("FutureMsg is aligned to spec"))
	}

}
func initControllerCompareStruct() *utils.Compare {
	c:= &utils.Compare{
		Replace: mapping.ControllerReplace(),
		SpecReplace: mapping.SpecControllerReplace(),
		SSVPath: specDataPath + "/controller/controller.go",
		SpecPath: specDataPath + "/controller/controller_spec.go",
	}
	if err:=utils.Copy("./protocol/v2/qbft/controller/controller.go", c.SSVPath); err != nil {
		panic(err)
	}
	if err:=utils.Copy("./scripts/spec_align_report/ssv-spec/qbft/controller.go",c.SpecPath); err != nil {
		panic(err)
	}
	return c
}
func initDecidedCompareStruct() *utils.Compare {
	c:= &utils.Compare{
		Replace: mapping.DecidedReplace(),
		SpecReplace: mapping.SpecDecidedReplace(),
		SSVPath: specDataPath + "/controller/decided.go",
		SpecPath: specDataPath + "/controller/decided_spec.go",
	}
	if err:=utils.Copy("./protocol/v2/qbft/controller/decided.go", c.SSVPath); err != nil {
		panic(err)
	}
	if err:=utils.Copy("./scripts/spec_align_report/ssv-spec/qbft/decided.go",c.SpecPath); err != nil {
		panic(err)
	}
	return c
}
func initFutureMsgCompareStruct() *utils.Compare {
	c:= &utils.Compare{
		Replace: mapping.FutureMessageReplace(),
		SpecReplace: mapping.SpecFutureMessageReplace(),
		SSVPath: specDataPath + "/controller/future_msg.go",
		SpecPath: specDataPath + "/controller/future_msg_spec.go",
	}
	if err:=utils.Copy("./protocol/v2/qbft/controller/future_msg.go", c.SSVPath); err != nil {
		panic(err)
	}
	if err:=utils.Copy("./scripts/spec_align_report/ssv-spec/qbft/future_msg.go",c.SpecPath); err != nil {
		panic(err)
	}
	return c
}

// Instance
func processInstance(){

	instanceCompareStruct:= initInstanceCompareStruct()
	if err:= instanceCompareStruct.ReplaceMap(); err != nil {
		panic(err)
	}

	if err  := utils.GitDiff(instanceCompareStruct.SSVPath,instanceCompareStruct.SpecPath, specDataPath+ "/instance.diff") ; err != nil {
		fmt.Println(utils.Error("Instance is not aligned to spec: "+ specDataPath+ "/instance.diff"))
	} else {
		fmt.Println(utils.Success("Instance is aligned to spec"))
	}

}
func initInstanceCompareStruct() *utils.Compare {
	if err:= utils.Mkdir(specDataPath+"/instance", true); err != nil {
		panic(err)
	}
	c:= &utils.Compare{
		Replace: mapping.InstanceReplace(),
		SpecReplace: mapping.SpecInstanceReplace(),
		SSVPath: specDataPath + "/instance/instance.go",
		SpecPath: specDataPath + "/instance/instance_spec.go",
	}
	if err:=utils.Copy("./protocol/v2/qbft/instance/instance.go", c.SSVPath); err != nil {
		panic(err)
	}
	if err:=utils.Copy("./scripts/spec_align_report/ssv-spec/qbft/instance.go",c.SpecPath); err != nil {
		panic(err)
	}
	return c
}








// ########### SSV #####################################