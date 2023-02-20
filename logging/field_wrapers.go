package logging

import (
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"go.uber.org/zap"
)

func OperatorID(operatorId spectypes.OperatorID) zap.Field {
	return zap.Uint64("operator-id", uint64(operatorId))
}

func Height(height specqbft.Height) zap.Field {
	return zap.Uint64("height", uint64(height))
}

func Round(round specqbft.Round) zap.Field {
	return zap.Uint64("round", uint64(round))
}
