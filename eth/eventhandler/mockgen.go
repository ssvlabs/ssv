package eventhandler

//go:generate go tool -modfile=../../tool.mod mockgen -package=mocks -destination=./mocks/task_executor.go github.com/ssvlabs/ssv/eth/eventhandler TaskExecutor

// TaskExecutor adapts package-private interface for mockgen to generate properly capitalized mock-name.
// The mockgen.go cannot be a mockgen_test.go for mockgen to work (if we want Golang to ignore mockgen.go file
// during builds, we probably need to do it with build-tags).
type TaskExecutor = taskExecutor
