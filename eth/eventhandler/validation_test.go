package eventhandler

import (
	"encoding/binary"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/ssvlabs/ssv/networkconfig"
	operatorstorage "github.com/ssvlabs/ssv/operator/storage"
	"github.com/ssvlabs/ssv/registry/storage"
	"github.com/ssvlabs/ssv/storage/basedb"
	"github.com/ssvlabs/ssv/storage/kv"
)

func Test_validateValidatorAddedEvent(t *testing.T) {
	logger := zaptest.NewLogger(t)

	db, err := kv.NewInMemory(logger, basedb.Options{})
	require.NoError(t, err)

	nodeStorage, err := operatorstorage.NewNodeStorage(networkconfig.TestNetwork, logger, db)
	require.NoError(t, err)

	eh := &EventHandler{
		nodeStorage: nodeStorage,
	}

	generateOperators := func(count uint64) []uint64 {
		result := make([]uint64, 0)
		for i := uint64(1); i <= count; i++ {
			result = append(result, i)
		}
		return result
	}

	tt := []struct {
		name      string
		operators []uint64
		saved     bool
		err       string
	}{
		{
			name:      "4 operators",
			operators: generateOperators(4),
			saved:     true,
			err:       "",
		},
		{
			name:      "7 operators",
			operators: generateOperators(7),
			saved:     true,
			err:       "",
		},
		{
			name:      "10 operators",
			operators: generateOperators(10),
			saved:     true,
			err:       "",
		},
		{
			name:      "13 operators",
			operators: generateOperators(13),
			saved:     true,
			err:       "",
		},
		{
			name:      "14 operators",
			operators: generateOperators(14),
			saved:     true,
			err:       "too many operators (14)",
		},
		{
			name:      "0 operators",
			operators: generateOperators(0),
			saved:     true,
			err:       "no operators",
		},
		{
			name:      "1 operator",
			operators: generateOperators(1),
			saved:     true,
			err:       "given operator count (1) cannot build a 3f+1 quorum",
		},
		{
			name:      "2 operators",
			operators: generateOperators(2),
			saved:     true,
			err:       "given operator count (2) cannot build a 3f+1 quorum",
		},
		{
			name:      "3 operators",
			operators: generateOperators(3),
			saved:     true,
			err:       "given operator count (3) cannot build a 3f+1 quorum",
		},
		{
			name:      "5 operators",
			operators: generateOperators(5),
			saved:     true,
			err:       "given operator count (5) cannot build a 3f+1 quorum",
		},
		{
			name:      "duplicated operator",
			operators: []uint64{1, 2, 3, 3},
			saved:     true,
			err:       "duplicated operator ID (3)",
		},
		{
			name:      "4 operators not saved",
			operators: generateOperators(4),
			saved:     false,
			err:       "not all operators exist",
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if tc.saved {
				for _, operator := range tc.operators {
					od := &storage.OperatorData{
						PublicKey:    binary.LittleEndian.AppendUint64(nil, operator),
						OwnerAddress: common.Address{},
						ID:           operator,
					}

					_, err := nodeStorage.SaveOperatorData(nil, od)
					require.NoError(t, err)
				}

				defer func() {
					require.NoError(t, nodeStorage.DropOperators())
				}()
			}

			err := eh.validateOperators(nil, tc.operators)
			if tc.err == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.err)
			}
		})
	}
}
