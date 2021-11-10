package v1

// ValidatorTopicID - version 1 implementation
func (v1 *ForkV1) ValidatorTopicID(pkByts []byte) string {
	return v1.forkV0.ValidatorTopicID(pkByts)
}
