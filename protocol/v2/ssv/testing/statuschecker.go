package testing

type testingStatusChecker struct {
}

func (t testingStatusChecker) IsReady() bool {
	return true
}
