package operator

import (
	"errors"
	"math/big"
	"testing"

	ethcommon "github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ssvlabs/ssv/eth/executionclient"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/observability/log"
	operatorstorage "github.com/ssvlabs/ssv/operator/storage"
	kv "github.com/ssvlabs/ssv/storage/badger"
	"github.com/ssvlabs/ssv/storage/basedb"
)

func newResyncTestStorage(t *testing.T) operatorstorage.Storage {
	db, err := kv.NewInMemory(log.TestLogger(t), basedb.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	ns, err := operatorstorage.NewNodeStorage(networkconfig.TestNetwork.Beacon, log.TestLogger(t), db)
	require.NoError(t, err)
	return ns
}

func TestPrepareRegistryResync_NoFlag(t *testing.T) {
	ns := newResyncTestStorage(t)
	require.NoError(t, ns.SaveLastProcessedBlock(nil, big.NewInt(123)))

	resync, err := prepareRegistryResync(log.TestLogger(t), ns)
	require.NoError(t, err)
	require.False(t, resync)

	// Without the flag set, nothing is dropped.
	lpb, found, err := ns.GetLastProcessedBlock(nil)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, uint64(123), lpb.Uint64())
}

func TestPrepareRegistryResync_FlagSet(t *testing.T) {
	ns := newResyncTestStorage(t)
	require.NoError(t, ns.SaveLastProcessedBlock(nil, big.NewInt(123)))
	require.NoError(t, ns.SaveUnverifiedRange(nil, operatorstorage.UnverifiedRange{From: 1, To: 9, Cursor: 1}))
	require.NoError(t, ns.SaveBlockLogIDs(nil, 5, []byte{1, 2}))
	require.NoError(t, ns.SaveUnsettledBlock(nil, 7))
	require.NoError(t, ns.SetResyncRequired(nil))

	resync, err := prepareRegistryResync(log.TestLogger(t), ns)
	require.NoError(t, err)
	require.True(t, resync)

	// Registry marker and the verification journal are dropped for a clean rebuild...
	_, found, err := ns.GetLastProcessedBlock(nil)
	require.NoError(t, err)
	require.False(t, found)

	ranges, err := ns.ListUnverifiedRanges(nil)
	require.NoError(t, err)
	require.Empty(t, ranges)

	_, idsFound, err := ns.GetBlockLogIDs(nil, 5)
	require.NoError(t, err)
	require.False(t, idsFound)

	unsettled, err := ns.ListUnsettledBlocks(nil)
	require.NoError(t, err)
	require.Empty(t, unsettled)

	// ...the resync-required flag stays set until completion (cleared by the caller)...
	stillSet, err := ns.IsResyncRequired(nil)
	require.NoError(t, err)
	require.True(t, stillSet)

	// ...and the resync is now marked in progress, so an interruption resumes rather than re-drops.
	inProgress, err := ns.IsResyncInProgress(nil)
	require.NoError(t, err)
	require.True(t, inProgress)
}

func TestPrepareRegistryResync_ResumesInProgress(t *testing.T) {
	ns := newResyncTestStorage(t)
	// A previous repair got interrupted: it dropped and rebuilt partway (marker at 500) and left
	// the in-progress flag set. The resync must resume, NOT drop the partial progress.
	require.NoError(t, ns.SaveLastProcessedBlock(nil, big.NewInt(500)))
	require.NoError(t, ns.SetResyncRequired(nil))
	require.NoError(t, ns.SetResyncInProgress(nil))

	resync, err := prepareRegistryResync(log.TestLogger(t), ns)
	require.NoError(t, err)
	require.True(t, resync)

	// The partial progress is preserved (not re-dropped) so the resync continues from the marker.
	lpb, found, err := ns.GetLastProcessedBlock(nil)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, uint64(500), lpb.Uint64())
}

func TestShouldVerifyCatchUpInline(t *testing.T) {
	const followDistance = executionclient.FollowDistance

	type receiptsProbe int
	const (
		notProbed receiptsProbe = iota
		receiptsServed
		receiptsUnavailable
		probeFails
	)

	tests := []struct {
		name      string
		fromBlock uint64
		head      *big.Int // nil => HeaderByNumber errors
		emptyHead bool     // the head block has no transactions, so no receipts to probe
		probe     receiptsProbe
		want      bool
	}{
		{
			name:      "small catch-up verified inline",
			fromBlock: 100,
			head:      big.NewInt(100 + followDistance + 1_000), // ~1k blocks to sync
			probe:     receiptsServed,
			want:      true,
		},
		{
			name:      "at the cap verified inline",
			fromBlock: 100,
			head:      big.NewInt(100 + followDistance + maxInlineVerifyCatchUp - 1), // exactly the cap
			probe:     receiptsServed,
			want:      true,
		},
		{
			name:      "above the cap stays optimistic",
			fromBlock: 100,
			head:      big.NewInt(100 + followDistance + maxInlineVerifyCatchUp), // one over the cap
			want:      false,
		},
		{
			name:      "cold sync from offset stays optimistic",
			fromBlock: 1,
			head:      big.NewInt(10_000_000),
			want:      false,
		},
		{
			name:      "nothing to sync verifies (moot) rather than blocks",
			fromBlock: 5_000,
			head:      big.NewInt(1_000),
			want:      true,
		},
		{
			name:      "head fetch error falls back to optimistic",
			fromBlock: 100,
			head:      nil,
			want:      false,
		},
		{
			name:      "no receipts source stays optimistic",
			fromBlock: 100,
			head:      big.NewInt(100 + followDistance + 1_000),
			probe:     receiptsUnavailable,
			want:      false,
		},
		{
			name:      "receipts probe error falls back to optimistic",
			fromBlock: 100,
			head:      big.NewInt(100 + followDistance + 1_000),
			probe:     probeFails,
			want:      false,
		},
		{
			name:      "empty head block skips the probe",
			fromBlock: 100,
			head:      big.NewInt(100 + followDistance + 1_000),
			emptyHead: true,
			want:      true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ec := executionclient.NewMockProvider(gomock.NewController(t))
			if tc.head == nil {
				ec.EXPECT().HeaderByNumber(gomock.Any(), gomock.Any()).Return(nil, errors.New("boom"))
			} else {
				txHash := ethcommon.Hash{1}
				if tc.emptyHead {
					txHash = ethtypes.EmptyTxsHash
				}
				ec.EXPECT().HeaderByNumber(gomock.Any(), gomock.Any()).Return(&ethtypes.Header{Number: tc.head, TxHash: txHash}, nil)
			}
			switch tc.probe {
			case notProbed:
			case receiptsServed:
				ec.EXPECT().BlockContractLogs(gomock.Any(), tc.head.Uint64()).Return(nil, true, nil)
			case receiptsUnavailable:
				ec.EXPECT().BlockContractLogs(gomock.Any(), tc.head.Uint64()).Return(nil, false, nil)
			case probeFails:
				ec.EXPECT().BlockContractLogs(gomock.Any(), tc.head.Uint64()).Return(nil, false, errors.New("boom"))
			}

			got := shouldVerifyCatchUpInline(t.Context(), log.TestLogger(t), ec, tc.fromBlock)
			require.Equal(t, tc.want, got)
		})
	}
}
