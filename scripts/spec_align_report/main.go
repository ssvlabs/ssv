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
	if err:= utils.Mkdir(specDataPath); err != nil {
		fmt.Println(err)
		return
	}

	processController()
	//utils.CleanSpecPath()


	fmt.Println("done")
}

// Controller

func InitControllerCompareStruct() *utils.Compare {
	if err:= utils.Mkdir(specDataPath+"/controller"); err != nil {
		panic(err)
	}
	c:= &utils.Compare{
		Replace: mapping.ControllerReplace(),
		SpecReplace: mapping.SpecControllerReplace(),
		SSVPath: specDataPath + "/controller/controller.go",
		SpecPath: specDataPath + "/controller/controller-spec.go",
	}
	if err:=utils.Copy("./protocol/v2/qbft/controller/controller.go", c.SSVPath); err != nil {
		panic(err)
	}
	if err:=utils.Copy("./scripts/spec_align_report/ssv-spec/qbft/controller.go",c.SpecPath); err != nil {
		panic(err)
	}
	return c
}
func processController()  {
	//controllerCompareStruct:= InitControllerCompareStruct()
	//if err:= controllerCompareStruct.ReplaceMap(); err != nil {
	//	panic(err)
	//}
	//
	//if err  := utils.GitDiff(controllerCompareStruct.SSVPath,controllerCompareStruct.SpecPath, specDataPath+ "/controller.diff") ; err != nil {
	//	fmt.Println("Controller is not aligned to spec: "+ specDataPath+ "/controller.diff")
	//}

	decidedCompareStruct:= InitDecidedCompareStruct()
	if err:= decidedCompareStruct.ReplaceMap(); err != nil {
		panic(err)
	}

	if err  := utils.GitDiff(decidedCompareStruct.SSVPath,decidedCompareStruct.SpecPath, specDataPath+ "/decided.diff") ; err != nil {
		fmt.Println("Decided is not aligned to spec: "+ specDataPath+ "/decided.diff")
	}

}

func InitDecidedCompareStruct() *utils.Compare {
	if err:= utils.Mkdir(specDataPath+"/controller"); err != nil {
		panic(err)
	}
	c:= &utils.Compare{
		Replace: mapping.DecidedReplace(),
		SpecReplace: mapping.SpecDecidedReplace(),
		SSVPath: specDataPath + "/controller/decided.go",
		SpecPath: specDataPath + "/controller/decided-spec.go",
	}
	if err:=utils.Copy("./protocol/v2/qbft/controller/decided.go", c.SSVPath); err != nil {
		panic(err)
	}
	if err:=utils.Copy("./scripts/spec_align_report/ssv-spec/qbft/decided.go",c.SpecPath); err != nil {
		panic(err)
	}
	return c
}