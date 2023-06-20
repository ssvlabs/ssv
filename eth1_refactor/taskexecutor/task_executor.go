package taskexecutor

import (
	"github.com/bloxapp/ssv/eth1_refactor/eventbatcher"
)

type TaskExecutor struct{}

func NewTaskExecutor() *TaskExecutor {
	return &TaskExecutor{}
}

func (te *TaskExecutor) ExecuteTasks(tasks ...eventbatcher.EventTask) error {
	for _, task := range tasks {
		if err := te.executeTask(task); err != nil {
			return err
		}
	}

	return nil
}

func (te *TaskExecutor) executeTask(task eventbatcher.EventTask) error {
	// TODO: implement

	return nil
}
