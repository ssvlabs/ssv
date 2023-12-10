package logs_catcher

import (
	"fmt"
	"go.uber.org/zap"
	"os"
)

// TODO: Parse validators.json file into this config

func DefaultFataler(logger *zap.Logger) func(s string) {
	return func(s string) {
		logger.Error("fatal error", zap.Error(fmt.Errorf(s)))
		fmt.Fprintf(os.Stderr, "fatal error: %v\n", s)
		os.Exit(1)
	}
}

func DefaultApprover(logger *zap.Logger, count int) func(s string) {
	goodLogs := make([]string, 0)

	return func(s string) {
		logger.Info("Found log I was looking for!!!")
		goodLogs = append(goodLogs, s)
		if len(
			goodLogs,
		) == count { // is it enough? maybe we need a log for each validator in each operator for ex
			// todo: Condition satisfied. what should we do? exit happily?
			logger.Info("All approvers logged successfully")
			for _, log := range goodLogs {
				logger.Info(log)
			}
			os.Exit(0)
		}
	}
}

type Config struct {
	IgnoreContainers []string
	Fatalers         []map[string]any
	FatalerFunc      func(string)
	Approvers        []map[string]any
	ApproverFunc     func(string)
}
