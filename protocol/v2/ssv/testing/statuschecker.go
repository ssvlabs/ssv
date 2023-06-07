package testing

type StatusChecker struct {
}

func (t StatusChecker) IsReady() bool {
	return true
}
