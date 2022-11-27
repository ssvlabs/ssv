package main

import (
	"fmt"
	"github.com/bloxapp/ssv/scripts/spec_align_report/qbft"
	"github.com/bloxapp/ssv/scripts/spec_align_report/utils"
)

func main() {
	if err := utils.CloneSpec("v0.2.7"); err != nil {
		fmt.Println(err)
		return
	}
	if err := utils.Mkdir(utils.DataPath, true); err != nil {
		fmt.Println(err)
		return
	}

	qbft.ProcessController()

	qbft.ProcessInstance()
	//utils.CleanSpecPath()

	fmt.Println("done")
}

// ########### SSV #####################################
