package validators_test

import (
	"context"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	specssv "github.com/ssvlabs/ssv-spec/ssv"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	tests2 "github.com/ssvlabs/ssv/integration/qbft/tests"
	"github.com/ssvlabs/ssv/operator/validators"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/validator"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestUpdateCommitteeAtomic(t *testing.T) {
	t.Run("does nothing if committee not found", func(t *testing.T) {
		var cmtID spectypes.CommitteeID
		vm := validators.New(context.Background())
		updated := vm.UpdateCommitteeAtomic(cmtID, func(c *validator.Committee) {
			t.Fatal("should not be called")
		})
		assert.False(t, updated)
	})

	t.Run("removes committee if no shares left", func(t *testing.T) {
		ctx := context.Background()
		vm := validators.New(ctx)
		ctx, cancel := context.WithCancel(ctx)

		cmt := validator.NewCommittee(
			ctx,
			cancel,
			zap.NewNop(),
			tests2.NewTestingBeaconNodeWrapped().GetBeaconNetwork(),
			&new(specssv.Committee).CommitteeMember, nil,
			map[phase0.ValidatorIndex]*spectypes.Share{
				0: new(spectypes.Share),
			})

		var cmtID spectypes.CommitteeID
		vm.PutCommittee(cmtID, cmt)
		assert.Equal(t, 1, vm.SizeCommittees())

		updated := vm.UpdateCommitteeAtomic(cmtID, func(c *validator.Committee) {
			c.RemoveShare(0)
		})

		assert.True(t, updated)
		assert.True(t, cmt.Stopped())
		assert.Equal(t, context.Canceled, ctx.Err())
		assert.Equal(t, 0, vm.SizeCommittees())
	})

	t.Run("does not stop committee if it has shares left", func(t *testing.T) {
		ctx := context.Background()
		vm := validators.New(ctx)
		ctx, cancel := context.WithCancel(ctx)
		t.Cleanup(cancel)

		cmt := validator.NewCommittee(
			ctx,
			cancel,
			zap.NewNop(),
			tests2.NewTestingBeaconNodeWrapped().GetBeaconNetwork(),
			&new(specssv.Committee).CommitteeMember, nil,
			map[phase0.ValidatorIndex]*spectypes.Share{
				0: new(spectypes.Share),
				1: new(spectypes.Share),
			},
		)

		var cmtID spectypes.CommitteeID
		vm.PutCommittee(cmtID, cmt)

		updated := vm.UpdateCommitteeAtomic(cmtID, func(c *validator.Committee) {
			c.RemoveShare(0)
		})

		assert.True(t, updated)
		assert.False(t, cmt.Stopped())
		assert.Equal(t, 1, vm.SizeCommittees())
		assert.NoError(t, ctx.Err())
	})
}
