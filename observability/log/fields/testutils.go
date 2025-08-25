package fields

import (
	"net/url"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func ENRStr(val string) zapcore.Field {
	return zap.String(FieldENR, val)
}

func AddressURL(val url.URL) zapcore.Field {
	return zap.Stringer(FieldAddress, &val)
}
