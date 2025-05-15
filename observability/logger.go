package observability

import "go.uber.org/zap"

var logger = zap.L().Named("Observability")
