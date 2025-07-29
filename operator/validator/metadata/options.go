package metadata

import "time"

type Option func(*Syncer)

func WithSyncInterval(interval time.Duration) Option {
	return func(u *Syncer) {
		u.syncInterval = interval
	}
}
