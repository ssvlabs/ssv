package main

import (
	"fmt"
	"github.com/bloxapp/ssv/scripts/spec_align_report/qbft"
	"github.com/bloxapp/ssv/scripts/spec_align_report/utils"
)

func main() {
	if err := utils.CloneSpec("main", "7e96c8b781915faaa12d29eba94e702445bd5945"); err != nil {
		fmt.Println(err)
		return
	}
	if err := utils.Mkdir(utils.DataPath, true); err != nil {
		fmt.Println(err)
		return
	}

	qbft.ProcessController()

	//qbft.ProcessInstance()

	//ssv.ProcessRunner()
	//utils.CleanSpecPath()

	fmt.Println("done")
}

// ########### SSV #####################################
