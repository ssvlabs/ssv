package validators_test

import (
	"context"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	ethcommon "github.com/ethereum/go-ethereum/common"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv/integration/qbft/tests"
	"github.com/ssvlabs/ssv/operator/validators"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/validator"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

const valPk = "b24454393691331ee6eba4ffa2dbb2600b9859f908c3e648b6c6de9e1dea3e9329866015d08355c8d451427762b913d1"

func TestAddShareToCommittee(t *testing.T) {
	t.Run("does nothing if committee not found", func(t *testing.T) {
		vm := validators.New(context.Background())
		share := &ssvtypes.SSVShare{
			Share: spectypes.Share{
				ValidatorPubKey: spectypes.ValidatorPK(ethcommon.Hex2Bytes(valPk)),
			},
		}

		vc, added := vm.AddShareToCommittee(share, func() *validator.Committee { return nil })

		assert.Nil(t, vc)
		assert.False(t, added)
	})

	t.Run("adds share to committee", func(t *testing.T) {
		ctx := context.Background()
		ctx, cancel := context.WithCancel(ctx)
		vm := validators.New(ctx)

		cmt := validator.NewCommittee(
			ctx,
			cancel,
			zap.NewNop(),
			tests.NewTestingBeaconNodeWrapped().GetBeaconNetwork(),
			&spectypes.CommitteeMember{}, nil,
			map[phase0.ValidatorIndex]*spectypes.Share{}, validator.NewCommitteeDutyGuard())

		share := &ssvtypes.SSVShare{
			Share: spectypes.Share{
				ValidatorPubKey: spectypes.ValidatorPK(ethcommon.Hex2Bytes(valPk)),
			},
		}

		var cmtID = share.CommitteeID()
		vm.PutCommitteeUnsafe(cmtID, cmt)
		assert.Equal(t, 1, vm.SizeCommittees())

		vc, added := vm.AddShareToCommittee(share, func() *validator.Committee { return nil })

		assert.Equal(t, cmt, vc)
		assert.True(t, added)
		assert.Equal(t, 1, vm.SizeCommittees())
	})
}

func TestRemoveShareFromCommittee(t *testing.T) {
	t.Run("removes committee if no shares left", func(t *testing.T) {
		ctx := context.Background()
		vm := validators.New(ctx)
		ctx, cancel := context.WithCancel(ctx)

		share := &ssvtypes.SSVShare{
			Share: spectypes.Share{
				ValidatorPubKey: spectypes.ValidatorPK(ethcommon.Hex2Bytes(valPk)),
			},
		}

		cmt := validator.NewCommittee(
			ctx,
			cancel,
			zap.NewNop(),
			tests.NewTestingBeaconNodeWrapped().GetBeaconNetwork(),
			&spectypes.CommitteeMember{}, nil,
			map[phase0.ValidatorIndex]*spectypes.Share{
				0: &share.Share,
			}, validator.NewCommitteeDutyGuard())

		var cmtID = share.CommitteeID()
		vm.PutCommitteeUnsafe(cmtID, cmt)
		assert.Equal(t, 1, vm.SizeCommittees())

		_, removed := vm.RemoveShareFromCommittee(share)

		assert.True(t, removed)
		assert.True(t, cmt.Stopped())
		assert.Equal(t, context.Canceled, ctx.Err())
		assert.Equal(t, 0, vm.SizeCommittees())
	})

	t.Run("does not stop committee if it has shares left", func(t *testing.T) {
		ctx := context.Background()
		vm := validators.New(ctx)
		ctx, cancel := context.WithCancel(ctx)
		t.Cleanup(cancel)

		share := &ssvtypes.SSVShare{
			Share: spectypes.Share{
				ValidatorPubKey: spectypes.ValidatorPK(ethcommon.Hex2Bytes(valPk)),
			},
		}

		cmt := validator.NewCommittee(
			ctx,
			cancel,
			zap.NewNop(),
			tests.NewTestingBeaconNodeWrapped().GetBeaconNetwork(),
			&spectypes.CommitteeMember{}, nil,
			map[phase0.ValidatorIndex]*spectypes.Share{
				0: &share.Share,
				1: new(spectypes.Share),
			}, validator.NewCommitteeDutyGuard(),
		)

		var cmtID = share.CommitteeID()
		vm.PutCommitteeUnsafe(cmtID, cmt)
		assert.Equal(t, 1, vm.SizeCommittees())

		_, removed := vm.RemoveShareFromCommittee(share)

		assert.True(t, removed)
		assert.False(t, cmt.Stopped())
		assert.Equal(t, 1, vm.SizeCommittees())
		assert.NoError(t, ctx.Err())
	})
}
