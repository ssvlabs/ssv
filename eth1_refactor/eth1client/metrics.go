package eth1client

type metrics interface {
	Eth1Ready()
	Eth1Syncing()
	Eth1Failure()
	LastFetchedBlock(block uint64)
}

// nopMetrics is no-op metrics.
type nopMetrics struct{}

func (nopMetrics) Eth1Ready()                {}
func (nopMetrics) Eth1Syncing()              {}
func (nopMetrics) Eth1Failure()              {}
func (nopMetrics) LastFetchedBlock(_ uint64) {}
