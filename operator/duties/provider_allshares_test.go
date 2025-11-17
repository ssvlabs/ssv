package duties

import (
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	types "github.com/ssvlabs/ssv/protocol/v2/types"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
	"github.com/ssvlabs/ssv/registry/storage/mocks"
)

func TestAllSharesProviderDelegates(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	store := mocks.NewMockValidatorStore(ctrl)

	share := &types.SSVShare{Share: spectypes.Share{ValidatorIndex: phase0.ValidatorIndex(1)}}
	store.EXPECT().Validators().Return([]*types.SSVShare{share}).Times(2)
	store.EXPECT().ParticipatingValidators(phase0.Epoch(5)).Return([]*types.SSVShare{share})
	store.EXPECT().Validator(gomock.Any()).Return(share, true)

	provider := NewAllSharesProvider(store)

	assert.Equal(t, []*types.SSVShare{share}, provider.Validators())
	assert.Equal(t, []*types.SSVShare{share}, provider.SelfValidators())
	assert.Equal(t, []*types.SSVShare{share}, provider.SelfParticipatingValidators(5))
	got, ok := provider.Validator([]byte("key"))
	assert.True(t, ok)
	assert.Equal(t, share, got)

	var _ ValidatorProvider = provider
	var _ registrystorage.ValidatorStore = store
}
