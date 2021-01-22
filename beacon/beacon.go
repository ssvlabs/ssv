package beacon

import (
	"context"

	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	ikeymanager "github.com/prysmaticlabs/prysm/validator/keymanager"
)

// Beacon represents the behavior of the beacon node connector
type Beacon interface {
	// StreamDuties returns channel with duties stream
	StreamDuties(ctx context.Context, pubKey []byte) (<-chan *ethpb.DutiesResponse_Duty, error)

	// SubmitAttestation submits attestation fo the given slot using the given public key
	SubmitAttestation(ctx context.Context, slot uint64, duty *ethpb.DutiesResponse_Duty, keyManager ikeymanager.IKeymanager) error
}
