package v1

// WithMsgID implementation
func (v1 *ForkV1) WithMsgID() bool {
	return true
}

// WithScoring implementation
func (v1 *ForkV1) WithScoring() bool {
	return true
}

// Subnets returns the subnets count for this fork
func (v1 *ForkV1) Subnets() int64 {
	return int64(SubnetsCount)
}
