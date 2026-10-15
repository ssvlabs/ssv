// Package versionprobe learns the node version of identified sender peers that the exporter
// isn't connected to. It dials them from a throwaway libp2p host, sends one SSV handshake
// request (/ssv/info/0.0.1), reads the remote's NodeInfo and disconnects. The main host's
// connections, peer slots and gossipsub mesh are never touched.
package versionprobe

import "time"

const (
	pickInterval   = 30 * time.Second
	minTargetCount = 20
	maxConcurrent  = 2
	minSpacing     = 5 * time.Second
	dialTimeout    = 10 * time.Second
	requestTimeout = 10 * time.Second
	refreshAfter   = time.Hour
	maxEntries     = 2048
)

// backoffSchedule is the wait after the 1st, 2nd, 3rd, 4th+ consecutive failure.
var backoffSchedule = []time.Duration{15 * time.Minute, 30 * time.Minute, time.Hour, 2 * time.Hour}

// Outcome classifies a probe attempt.
type Outcome string

const (
	OutcomeOK          Outcome = "ok"
	OutcomeNoAddr      Outcome = "no_addr"
	OutcomeDialFailed  Outcome = "dial_failed"
	OutcomeRefused     Outcome = "refused"
	OutcomeTimeout     Outcome = "timeout"
	OutcomeBadResponse Outcome = "bad_response"
)

// Request is what the probe sends about itself in its NodeInfo.
type Request struct {
	NetworkID   string
	NodeVersion string
}

// Result is the outcome of one probe attempt.
type Result struct {
	Version    string
	Outcome    Outcome
	Err        string
	AddrSource string
	Duration   time.Duration
}
