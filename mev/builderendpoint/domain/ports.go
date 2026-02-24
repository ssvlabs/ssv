package domain

import (
	"context"
	"io"

	"github.com/attestantio/go-eth2-client/api"
	"github.com/attestantio/go-eth2-client/spec/phase0"
)

// Bid represents a versioned Builder API bid body along with the associated consensus version.
// The concrete payload shape will be implemented in Step 2 (v1 getHeader).
type Bid struct {
	ConsensusVersion string
	Body             any
}

// BidProvider provides Builder API bids (headers) for a given (slot, parent_hash, pubkey).
// Returning (nil, nil) means "no bid available" and should map to HTTP 204.
type BidProvider interface {
	BuilderBid(ctx context.Context, slot phase0.Slot, parentHash phase0.Hash32, pubkey phase0.BLSPubKey) (*Bid, error)
}

// Unblinder reveals/unblinds a signed blinded beacon block by calling relays/builders.
// The concrete implementation will be added in Step 4.
type Unblinder interface {
	UnblindBlock(ctx context.Context, block *api.VersionedSignedBlindedBeaconBlock) (*api.VersionedSignedProposal, error)
}

// RegistrationForwarder forwards validator registrations to relays/builders.
// Step 7 will implement parsing and forwarding logic; Step 1 keeps it as a passthrough port.
type RegistrationForwarder interface {
	ForwardValidatorRegistrations(ctx context.Context, body io.ReadCloser) ([]string, error)
}
