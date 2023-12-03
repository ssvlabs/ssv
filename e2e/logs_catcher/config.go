package logs_catcher

import (
	"fmt"
	"os"
)

// TODO: Parse validators.json file into this config

var DefaultFataler = func(s string) {
	fmt.Fprintf(os.Stderr, "fatal error: %v\n", s)
	os.Exit(1)
}

func DefaultApprover(count int) func(s string) {
	goodLogs := make([]string, 0)

	return func(s string) {
		goodLogs = append(goodLogs, s)
		if len(goodLogs) == count { // is it enough? maybe we need a log for each validator in each operator for ex
			// todo: Condition satisfied. what should we do? exit happily?
			fmt.Println("All validators logged successfully")
			for _, log := range goodLogs {
				fmt.Println(log)
			}
			os.Exit(0)
		}
	}
}

type Config struct {
	IgnoreContainers []string
	Fatalers         []map[string]string
	FatalerFunc      func(string)
	Approvers        []map[string]string
	ApproverFunc     func(string)
}
