package versionprobe

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/connmgr"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	rcmgr "github.com/libp2p/go-libp2p/p2p/host/resource-manager"
	"github.com/libp2p/go-libp2p/p2p/muxer/yamux"
	"github.com/libp2p/go-libp2p/p2p/security/noise"
	"github.com/libp2p/go-libp2p/p2p/transport/tcp"

	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/network/peers"
	"github.com/ssvlabs/ssv/network/records"
	"github.com/ssvlabs/ssv/network/streams"
)

// probeHostLimit caps the throwaway host's connections and streams. One probe needs a single
// connection carrying identify and one /ssv/info stream; a few more leave room for identify
// pushes and the remote's own identify request.
const probeHostLimit = 8

// Classification phases.
const (
	phaseDial   = "dial"
	phaseStream = "stream"
)

// Probe dials target from a fresh, throwaway libp2p host, sends one SSV handshake request
// (/ssv/info/0.0.1) and returns the version the remote reports in its NodeInfo. The host is
// closed, with all its connections, before Probe returns, whatever the outcome. The caller's
// own host is never used. Result.AddrSource is left for the caller to fill.
func Probe(ctx context.Context, target peer.AddrInfo, req Request) (res Result) {
	start := time.Now()
	defer func() {
		if r := recover(); r != nil {
			res = Result{Outcome: OutcomeBadResponse, Err: fmt.Sprintf("panic: %v", r)}
		}
		res.Duration = time.Since(start)
	}()

	h, priv, err := newProbeHost(req)
	if err != nil {
		return Result{Outcome: OutcomeDialFailed, Err: fmt.Sprintf("create probe host: %v", err)}
	}
	defer func() { _ = h.Close() }()

	dialCtx, cancelDial := context.WithTimeout(ctx, dialTimeout)
	err = h.Connect(dialCtx, target)
	cancelDial()
	if err != nil {
		return failure(err, phaseDial)
	}

	sealed, err := sealedRequest(priv, req)
	if err != nil {
		return Result{Outcome: OutcomeBadResponse, Err: err.Error()}
	}

	data, err := request(ctx, h, target.ID, sealed)
	if err != nil {
		return failure(err, phaseStream)
	}

	ni := new(records.NodeInfo)
	if err := ni.Consume(data); err != nil {
		return Result{Outcome: OutcomeBadResponse, Err: err.Error()}
	}
	if ni.Metadata == nil {
		return Result{Outcome: OutcomeBadResponse, Err: "node info has no metadata"}
	}
	if ni.Metadata.NodeVersion == "" {
		return Result{Outcome: OutcomeBadResponse, Err: "node info has an empty node version"}
	}
	return Result{Outcome: OutcomeOK, Version: ni.Metadata.NodeVersion}
}

// request opens one /ssv/info stream and runs the same exchange as streams.StreamController.Request
// (write, close write, read, close), without the controller's node-wide metrics.
func request(ctx context.Context, h host.Host, id peer.ID, msg []byte) ([]byte, error) {
	streamCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	raw, err := h.NewStream(streamCtx, id, peers.NodeInfoProtocol)
	if err != nil {
		return nil, err
	}
	s := streams.NewStream(raw)
	defer func() { _ = s.Close() }()

	if err := s.WriteWithTimeout(msg, requestTimeout); err != nil {
		return nil, fmt.Errorf("write request: %w", err)
	}
	if err := s.CloseWrite(); err != nil {
		return nil, fmt.Errorf("close write: %w", err)
	}
	data, err := s.ReadWithTimeout(requestTimeout)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	return data, nil
}

func failure(err error, phase string) Result {
	return Result{Outcome: classify(err, phase), Err: err.Error()}
}

// newProbeHost builds an isolated, dial-only libp2p host with a fresh identity: no listen
// addresses, no relay, no NAT traversal, no metrics, a null connection manager and a small
// resource budget.
func newProbeHost(req Request) (host.Host, crypto.PrivKey, error) {
	priv, _, err := crypto.GenerateSecp256k1Key(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate key: %w", err)
	}

	small := rcmgr.ResourceLimits{
		Conns:           probeHostLimit,
		ConnsInbound:    probeHostLimit,
		ConnsOutbound:   probeHostLimit,
		Streams:         probeHostLimit,
		StreamsInbound:  probeHostLimit,
		StreamsOutbound: probeHostLimit,
		FD:              probeHostLimit,
	}
	limits := rcmgr.PartialLimitConfig{System: small, Transient: small}.Build(rcmgr.DefaultLimits.AutoScale())
	rm, err := rcmgr.NewResourceManager(rcmgr.NewFixedLimiter(limits))
	if err != nil {
		return nil, nil, fmt.Errorf("resource manager: %w", err)
	}

	h, err := libp2p.New(
		libp2p.Identity(priv),
		libp2p.NoListenAddrs,
		libp2p.Transport(tcp.NewTCPTransport),
		libp2p.Security(noise.ID, noise.New),
		libp2p.Muxer(yamux.ID, yamux.DefaultTransport),
		libp2p.DisableRelay(),
		libp2p.ForceReachabilityPrivate(),
		libp2p.ConnectionManager(connmgr.NullConnMgr{}),
		libp2p.ResourceManager(rm),
		libp2p.UserAgent("SSV-Node:"+req.NodeVersion+"-version-probe"),
		libp2p.Ping(false),
		libp2p.DisableMetrics(),
	)
	if err != nil {
		_ = rm.Close()
		return nil, nil, fmt.Errorf("new host: %w", err)
	}
	return h, priv, nil
}

// sealedRequest builds the probe's own NodeInfo, signed with the probe identity.
func sealedRequest(priv crypto.PrivKey, req Request) ([]byte, error) {
	ni := &records.NodeInfo{
		NetworkID: req.NetworkID,
		Metadata: &records.NodeMetadata{
			NodeVersion: req.NodeVersion,
			Subnets:     strings.Repeat("0", int(commons.SubnetsCount)/4),
		},
	}
	sealed, err := ni.Seal(priv)
	if err != nil {
		return nil, fmt.Errorf("seal request: %w", err)
	}
	return sealed, nil
}

// classify maps a dial or stream error to an outcome. It is best-effort: libp2p wraps errors
// in strings as often as in types, so it also matches on messages. Callers keep err.Error().
func classify(err error, phase string) Outcome {
	if isTimeout(err) {
		return OutcomeTimeout
	}
	msg := err.Error()
	switch phase {
	case phaseStream:
		if errors.Is(err, network.ErrReset) || containsAny(msg,
			"protocols not supported", "protocol not supported",
			"connection reset", "stream reset", "EOF") {
			return OutcomeRefused
		}
		return OutcomeBadResponse
	default:
		if containsAny(msg, "connection reset", "EOF", "closed", "failed to negotiate security protocol") {
			return OutcomeRefused
		}
		return OutcomeDialFailed
	}
}

func isTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) {
		return true
	}
	var te interface{ Timeout() bool }
	return errors.As(err, &te) && te.Timeout()
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
