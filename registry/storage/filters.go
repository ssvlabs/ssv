package storage

// ParticipationOptions provides a set of filters for querying validators based on their
// participation status and lifecycle state within the SSV network and Ethereum beacon chain.
// These options allow for precise selection of validators for various operational duties
// such as attestations, sync committee duties, or administrative tasks.
type ParticipationOptions struct {
	// IncludeLiquidated specifies whether to include validators whose clusters have been
	// liquidated.
	IncludeLiquidated bool

	// IncludeExited specifies whether to include validators that have voluntarily exited
	// the Ethereum beacon chain. Exited validators no longer perform validation duties.
	// Defaults to false.
	IncludeExited bool

	// OnlyAttesting filters for validators that are currently in their active attestation
	// lifecycle.
	OnlyAttesting bool

	// OnlySyncCommittee filters for validators that are part of the current sync committee.
	OnlySyncCommittee bool
}

// UpdateOptions provides flags to control the behavior of state update operations
// within the validator store. This allows for fine-grained control over side effects,
// such as triggering lifecycle callbacks.
type UpdateOptions struct {
	// TriggerCallbacks specifies whether to invoke registered lifecycle callbacks
	// (e.g., OnValidatorAdded, OnValidatorRemoved) after a state-modifying operation.
	// Setting this to false can be useful during bulk updates, migrations, or initial
	// setup to prevent a cascade of potentially expensive callback executions.
	// Defaults to false, requiring explicit activation of callbacks.
	TriggerCallbacks bool
}
