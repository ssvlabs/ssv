package tests

import (
	"context"
	"github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/integration/qbft/scenarios"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func Test_Integration_QBFTScenarios4Committee(t *testing.T) {
	//_ = logging.SetLogLevelRegex("ssv/.*", "debug") // for debugging TODO: ssv/.* or ssv/*. ?
	f := 1

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sCtx, err := scenarios.Bootstrap(ctx, []types.OperatorID{1, 2, 3, 4})
	require.NoError(t, err)
	defer func() {
		_ = sCtx.Close()
		<-time.After(time.Second)
	}()

	require.NoError(t, scenarios.RegularAttester(f).Run(f, sCtx))
	require.NoError(t, scenarios.RegularAggregator().Run(f, sCtx))
	require.NoError(t, scenarios.RegularProposer().Run(f, sCtx))
	require.NoError(t, scenarios.RegularSyncCommittee().Run(f, sCtx))
	require.NoError(t, scenarios.RegularSyncCommitteeContribution().Run(f, sCtx))
	require.NoError(t, scenarios.RoundChange(types.BNRoleAttester).Run(f, sCtx))
	require.NoError(t, scenarios.F1Decided(types.BNRoleAttester).Run(f, sCtx))
}

func Test_Integration_QBFTScenarios7Committee(t *testing.T) {
	//_ = logging.SetLogLevelRegex("ssv/.*", "debug") // for debugging TODO: ssv/.* or ssv/*. ?

	f := 2

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sCtx, err := scenarios.Bootstrap(ctx, []types.OperatorID{1, 2, 3, 4, 5, 6, 7})
	require.NoError(t, err)
	defer func() {
		_ = sCtx.Close()
		<-time.After(time.Second)
	}()

	require.NoError(t, scenarios.RegularAttester(f).Run(f, sCtx))
}

func Test_Integration_QBFTScenarios10Committee(t *testing.T) {
	//_ = logging.SetLogLevelRegex("ssv/.*", "debug") // for debugging TODO: ssv/.* or ssv/*. ?

	f := 3

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sCtx, err := scenarios.Bootstrap(ctx, []types.OperatorID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10})
	require.NoError(t, err)
	defer func() {
		_ = sCtx.Close()
		<-time.After(time.Second)
	}()

	require.NoError(t, scenarios.RegularAttester(f).Run(f, sCtx))
}
