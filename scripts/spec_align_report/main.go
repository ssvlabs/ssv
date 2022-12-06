package main

import (
	"fmt"
	"github.com/bloxapp/ssv/scripts/spec_align_report/qbft"
	"github.com/bloxapp/ssv/scripts/spec_align_report/ssv"
	"github.com/bloxapp/ssv/scripts/spec_align_report/utils"
)

func main() {
	if err := utils.CloneSpec("main", "675c93af14b3867f52d34e394f3843a2e8bce4e9"); err != nil {
		fmt.Println(err)
		return
	}
	if err := utils.Mkdir(utils.DataPath, true); err != nil {
		fmt.Println(err)
		return
	}

	qbft.ProcessController()

	qbft.ProcessInstance()

	ssv.ProcessRunner()

	ssv.ProcessValidator()
	//utils.CleanSpecPath()

	fmt.Println("done")
}

// ########### SSV #####################################
