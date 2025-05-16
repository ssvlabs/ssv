package executionclient

const (
	SlotsPerEpoch    = 32
	FinalityDistance = SlotsPerEpoch * 2

	// FinalityForkEpoch is the epoch at which the finality fork is active.
	// TODO: This is a placeholder value and should be updated when the actual epoch is known.
	FinalityForkEpoch = 120
)
