package eth1client

import (
	"time"
)

const (
	finalizationOffset                 = 8
	defaultConnectionTimeout           = 500 * time.Millisecond
	defaultReconnectionInitialInterval = 1 * time.Second
	defaultReconnectionMaxInterval     = 64 * time.Second
)
