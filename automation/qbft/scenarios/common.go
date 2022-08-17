package scenarios

import (
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	ibftinstance "github.com/bloxapp/ssv/protocol/v1/qbft/instance"
	"github.com/bloxapp/ssv/protocol/v1/validator"
)

func startNode(val validator.IValidator, h specqbft.Height, value []byte, logger *zap.Logger) error {
	ibftControllers := val.(*validator.Validator).Ibfts()

	for _, ibftc := range ibftControllers {
		res, err := ibftc.StartInstance(ibftinstance.ControllerStartInstanceOptions{
			Logger:    logger,
			SeqNumber: h,
			Value:     value,
		})

		if err != nil {
			return err
		} else if !res.Decided {
			return errors.New("instance could not decide")
		} else {
			logger.Info("decided with value", zap.String("decided value", string(res.Msg.Message.Data)))
		}
	}

	return nil
}
