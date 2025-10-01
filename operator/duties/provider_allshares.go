package duties

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"

	"github.com/ssvlabs/ssv/protocol/v2/types"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
)

// allSharesProvider adapts a ValidatorStore to act as a ValidatorProvider
// where "self" is considered to be the entire validator set.
type allSharesProvider struct {
	store registrystorage.ValidatorStore
}

func NewAllSharesProvider(store registrystorage.ValidatorStore) *allSharesProvider {
	return &allSharesProvider{store: store}
}

func (p *allSharesProvider) Validators() []*types.SSVShare {
	return p.store.Validators()
}

func (p *allSharesProvider) SelfValidators() []*types.SSVShare {
	return p.store.Validators()
}

func (p *allSharesProvider) SelfParticipatingValidators(epoch phase0.Epoch) []*types.SSVShare {
	return p.store.ParticipatingValidators(epoch)
}

func (p *allSharesProvider) Validator(pubKey []byte) (*types.SSVShare, bool) {
	return p.store.Validator(pubKey)
}

// Ensure interface conformance.
var _ ValidatorProvider = (*allSharesProvider)(nil)
