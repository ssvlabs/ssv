package v0

// WithMsgID implementation
func (v0 *ForkV0) WithMsgID() bool {
	return false
}

// WithScoring implementation
func (v0 *ForkV0) WithScoring() bool {
	return false
}

// Subnets returns the subnets count for this fork
func (v0 *ForkV0) Subnets() int64 {
	return 0
}
