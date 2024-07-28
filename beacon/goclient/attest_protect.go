package goclient

import (
	"context"
)

// slashableAttestationCheck checks if an attestation is slashable by comparing it with the attesting
// history for the given public key in our DB. If it is not, we then update the history
// with new values and save it to the database.
func (gc *GoClient) slashableAttestationCheck(ctx context.Context, signingRoot [32]byte) error {
	// TODO: Implement
	return nil
}
