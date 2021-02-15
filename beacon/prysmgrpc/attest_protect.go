package prysmgrpc

import (
	"context"

	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
)

// slashableAttestationCheck checks if an attestation is slashable by comparing it with the attesting
// history for the given public key in our DB. If it is not, we then update the history
// with new values and save it to the database.
func (b *prysmGRPC) slashableAttestationCheck(ctx context.Context, indexedAtt *ethpb.IndexedAttestation, pubKey []byte, signingRoot [32]byte) error {
	// TODO: Implement
	return nil
}
