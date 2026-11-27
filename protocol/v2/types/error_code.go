package types

// Error codes of the validator-registration runner, retired at Gloas, which the Gloas-only ssv-spec
// removed. The first two keep the numbers the spec reserves for them. The spec reused the third's
// number, so it takes one outside the spec's range.
const (
	ValidatorRegistrationNoConsensusPhaseErrorCode     = 35
	ValidatorRegistrationNoPostConsensusPhaseErrorCode = 36
	ValidatorRegistrationDeprecatedErrorCode           = 1000
)
