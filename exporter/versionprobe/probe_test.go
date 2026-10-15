package versionprobe

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/network/peers"
	"github.com/ssvlabs/ssv/network/peers/connections"
	"github.com/ssvlabs/ssv/network/records"
	"github.com/ssvlabs/ssv/network/streams"
)

// newResponder starts a loopback host that answers /ssv/info/0.0.1 like a real SSV node:
// read the request (same framing as network/streams), reply with its own sealed NodeInfo.
func newResponder(t *testing.T, version string, meta bool) host.Host {
	t.Helper()
	priv, _, err := crypto.GenerateSecp256k1Key(rand.Reader)
	require.NoError(t, err)
	h, err := libp2p.New(libp2p.Identity(priv), libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Close() })
	ctrl := streams.NewStreamController(context.Background(), h, 5*time.Second, 5*time.Second)
	h.SetStreamHandler(peers.NodeInfoProtocol, func(s network.Stream) {
		req, respond, done, herr := ctrl.HandleStream(zap.NewNop(), s)
		defer done()
		if herr != nil {
			return
		}
		var theirs records.NodeInfo
		_ = theirs.Consume(req) // the real handler verifies only AFTER responding
		ni := &records.NodeInfo{NetworkID: "0x00000502"}
		if meta {
			ni.Metadata = &records.NodeMetadata{NodeVersion: version, Subnets: strings.Repeat("0", int(commons.SubnetsCount)/4)}
		}
		sealed, serr := ni.Seal(priv)
		if serr != nil {
			t.Errorf("seal responder node info: %v", serr)
			return
		}
		_ = respond(sealed)
	})
	return h
}

func addrInfo(h host.Host) peer.AddrInfo { return peer.AddrInfo{ID: h.ID(), Addrs: h.Addrs()} }

var testReq = Request{NetworkID: "0x00000502", NodeVersion: "v2.5.0-test"}

func TestProbeOK(t *testing.T) {
	r := newResponder(t, "v2.5.0", true)
	res := Probe(context.Background(), addrInfo(r), testReq)
	require.Equal(t, OutcomeOK, res.Outcome, res.Err)
	require.Equal(t, "v2.5.0", res.Version)
	require.Positive(t, res.Duration)
}

// The probing node's main host must gain no connection (spec §7.1).
func TestProbeLeavesMainHostUntouched(t *testing.T) {
	mainHost, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	defer mainHost.Close()
	r := newResponder(t, "v2.5.0", true)
	res := Probe(context.Background(), addrInfo(r), testReq)
	require.Equal(t, OutcomeOK, res.Outcome, res.Err)
	require.Empty(t, mainHost.Network().Peers())
	// and the responder's connection from the probe host is gone once Probe returns
	require.Eventually(t, func() bool { return len(r.Network().Peers()) == 0 }, 5*time.Second, 50*time.Millisecond)
}

func TestProbeBadResponseWithoutMetadata(t *testing.T) {
	r := newResponder(t, "", false)
	res := Probe(context.Background(), addrInfo(r), testReq)
	require.Equal(t, OutcomeBadResponse, res.Outcome, res.Err)
	require.Contains(t, res.Err, "metadata")
}

// newRawResponder starts a loopback host whose /ssv/info handler is h itself.
func newRawResponder(t *testing.T, handler network.StreamHandler) host.Host {
	t.Helper()
	h, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Close() })
	h.SetStreamHandler(peers.NodeInfoProtocol, handler)
	return h
}

// A reply that is not a sealed NodeInfo envelope is a bad response (spec §8).
func TestProbeBadResponseOnGarbage(t *testing.T) {
	r := newRawResponder(t, func(raw network.Stream) {
		s := streams.NewStream(raw) // same framing as StreamController.HandleStream
		defer func() { _ = s.Close() }()
		if _, err := s.ReadWithTimeout(5 * time.Second); err != nil {
			return
		}
		_ = s.WriteWithTimeout([]byte("garbage"), 5*time.Second)
	})
	res := Probe(context.Background(), addrInfo(r), testReq)
	require.Equal(t, OutcomeBadResponse, res.Outcome, res.Err)
	require.Contains(t, res.Err, "envelope")
}

// The remote accepts /ssv/info, reads the request and never replies: the whole stream phase is
// bounded by the caller's context, not by fresh per-operation deadlines (Review Focus 3 and 5).
func TestProbeStalledResponderHonoursCallerContext(t *testing.T) {
	release := make(chan struct{})
	r := newRawResponder(t, func(s network.Stream) {
		_, _ = io.ReadAll(s)
		<-release
		_ = s.Reset()
	})
	t.Cleanup(func() { close(release) }) // runs before the host's Close
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	start := time.Now()
	res := Probe(ctx, addrInfo(r), testReq)
	require.Equal(t, OutcomeTimeout, res.Outcome, res.Err)
	require.Less(t, time.Since(start), 2*time.Second)
}

func TestProbeRefusedWhenProtocolUnsupported(t *testing.T) {
	h, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	defer h.Close()
	res := Probe(context.Background(), addrInfo(h), testReq)
	require.Equal(t, OutcomeRefused, res.Outcome, res.Err)
}

// A raw TCP listener that accepts and never speaks: the dial's security handshake stalls.
func TestProbeTimeout(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			defer c.Close()
		}
	}()
	priv, _, _ := crypto.GenerateSecp256k1Key(rand.Reader)
	id, _ := peer.IDFromPrivateKey(priv)
	port := ln.Addr().(*net.TCPAddr).Port
	addr := ma.StringCast(fmt.Sprintf("/ip4/127.0.0.1/tcp/%d", port))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second) // keep the test fast
	defer cancel()
	start := time.Now()
	res := Probe(ctx, peer.AddrInfo{ID: id, Addrs: []ma.Multiaddr{addr}}, testReq)
	require.Equal(t, OutcomeTimeout, res.Outcome, res.Err)
	require.Less(t, time.Since(start), 5*time.Second)
}

// A closed port: the dial fails fast and is bounded (Review Focus 2).
func TestProbeUnreachableAddressIsBounded(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	priv, _, _ := crypto.GenerateSecp256k1Key(rand.Reader)
	id, _ := peer.IDFromPrivateKey(priv)
	start := time.Now()
	res := Probe(context.Background(), peer.AddrInfo{ID: id, Addrs: []ma.Multiaddr{ma.StringCast(fmt.Sprintf("/ip4/127.0.0.1/tcp/%d", port))}}, testReq)
	require.Contains(t, []Outcome{OutcomeDialFailed, OutcomeRefused}, res.Outcome, res.Err)
	require.Less(t, time.Since(start), dialTimeout+time.Second)
}

// The responder runs the real SSV handshake handler (network/peers/connections), so the probe
// is checked against the production respond-before-verify code path. A handshake filter
// captures the probe's request after the handler's own checks, proving the real verifier
// accepts what the probe sends.
func TestProbeAgainstRealHandshaker(t *testing.T) {
	priv, _, err := crypto.GenerateSecp256k1Key(rand.Reader)
	require.NoError(t, err)
	h, err := libp2p.New(libp2p.Identity(priv), libp2p.ListenAddrStrings("/ip4/127.0.0.1/tcp/0"))
	require.NoError(t, err)
	defer h.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	self := &records.NodeInfo{Metadata: &records.NodeMetadata{
		NodeVersion: "v2.5.0",
		Subnets:     strings.Repeat("0", int(commons.SubnetsCount)/4),
	}}
	idx := peers.NewPeersIndex(zap.NewNop(), h.Network(), self,
		func(string) int { return 60 },
		func() crypto.PrivKey { return priv },
		peers.NewGossipScoreIndex())

	type seen struct {
		sender peer.ID
		ni     *records.NodeInfo
	}
	got := make(chan seen, 1)
	capture := func(sender peer.ID, ni *records.NodeInfo) error {
		select {
		case got <- seen{sender, ni}:
		default:
		}
		return nil
	}
	hs := connections.NewHandshaker(ctx, zap.NewNop(), &connections.HandshakerCfg{
		Network:         h.Network(),
		Streams:         streams.NewStreamController(ctx, h, 5*time.Second, 5*time.Second),
		NodeInfos:       idx,
		PeerInfos:       idx,
		ConnIdx:         idx,
		SubnetsIdx:      idx,
		DomainTypeFn:    func() spectypes.DomainType { return spectypes.DomainType{0x0, 0x0, 0x5, 0x2} },
		SubnetsProvider: func() commons.Subnets { return commons.Subnets{} },
	}, func() []connections.HandshakeFilter { return []connections.HandshakeFilter{capture} })
	h.SetStreamHandler(peers.NodeInfoProtocol, hs.Handler())

	res := Probe(context.Background(), addrInfo(h), testReq)
	require.Equal(t, OutcomeOK, res.Outcome, res.Err)
	require.Equal(t, "v2.5.0", res.Version)

	select {
	case s := <-got:
		require.Equal(t, testReq.NetworkID, s.ni.NetworkID)
		require.NotNil(t, s.ni.Metadata)
		require.Equal(t, testReq.NodeVersion, s.ni.Metadata.NodeVersion)
		require.NotEqual(t, h.ID(), s.sender)
	case <-time.After(5 * time.Second):
		t.Fatal("real handshaker never verified the probe's request")
	}
}

func TestClassify(t *testing.T) {
	require.Equal(t, OutcomeTimeout, classify(context.DeadlineExceeded, phaseDial))
	require.Equal(t, OutcomeTimeout, classify(fmt.Errorf("read: %w", context.DeadlineExceeded), phaseStream))
	require.Equal(t, OutcomeRefused, classify(fmt.Errorf("failed to negotiate security protocol: EOF"), phaseDial))
	require.Equal(t, OutcomeRefused, classify(fmt.Errorf("dial: connection reset by peer"), phaseDial))
	require.Equal(t, OutcomeDialFailed, classify(fmt.Errorf("dial: connection refused"), phaseDial))
	require.Equal(t, OutcomeDialFailed, classify(fmt.Errorf("no good addresses"), phaseDial))
	require.Equal(t, OutcomeRefused, classify(fmt.Errorf("protocols not supported: [/ssv/info/0.0.1]"), phaseStream))
	require.Equal(t, OutcomeRefused, classify(fmt.Errorf("read: %w", network.ErrReset), phaseStream))
	require.Equal(t, OutcomeBadResponse, classify(fmt.Errorf("could not consume envelope"), phaseStream))
}
