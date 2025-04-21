package mocks

import (
	"time"

	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/operator/slotticker"
)

func NewSlotTickerProvider() slotticker.SlotTicker {
	return slotticker.New(zap.NewNop(), slotticker.Config{
		SlotDuration: 12 * time.Second,
		GenesisTime:  time.Now(),
	})
}
