package goclient

import (
	"time"
)

type metrics interface {
	ConsensusClientReady()
	ConsensusClientSyncing()
	ConsensusClientUnknown()
	AttesterDataRequest(duration time.Duration)
	AggregatorDataRequest(duration time.Duration)
	ProposerDataRequest(duration time.Duration)
	SyncCommitteeDataRequest(duration time.Duration)
	SyncCommitteeContributionDataRequest(duration time.Duration)
}

type nopMetrics struct{}

func (n nopMetrics) ConsensusClientReady()                                       {}
func (n nopMetrics) ConsensusClientSyncing()                                     {}
func (n nopMetrics) ConsensusClientUnknown()                                     {}
func (n nopMetrics) AttesterDataRequest(duration time.Duration)                  {}
func (n nopMetrics) AggregatorDataRequest(duration time.Duration)                {}
func (n nopMetrics) ProposerDataRequest(duration time.Duration)                  {}
func (n nopMetrics) SyncCommitteeDataRequest(duration time.Duration)             {}
func (n nopMetrics) SyncCommitteeContributionDataRequest(duration time.Duration) {}
