package discovery

import (
	"go.uber.org/zap"
)

// dv5Logger implements log.Handler to track logs of discv5
type dv5Logger struct {
	logger *zap.Logger // struct logger to implement log.Handler
}

// Log takes a record and uses the zap.Logger to print it
//func (dvl *dv5Logger) Log(r *log.Record) error {
//	logger := dvl.logger.With(zap.Any("context", r.Ctx))
//	for _, v := range r.Ctx {
//		logger = dvl.logger.With(zap.Any("v", v))
//	}
//	switch r.Lvl {
//	case log.LvlTrace:
//		logger.Debug(fmt.Sprintf("TRACE: %s", r.Msg))
//	case log.LvlDebug:
//		logger.Debug(r.Msg)
//	case log.LvlInfo:
//		logger.Info(r.Msg)
//	case log.LvlWarn:
//		logger.Warn(r.Msg)
//	case log.LvlError:
//		logger.Error(r.Msg)
//	case log.LvlCrit:
//		logger.Fatal(r.Msg)
//	default:
//	}
//	return nil
//}
