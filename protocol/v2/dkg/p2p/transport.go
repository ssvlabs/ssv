// Package p2p provides the SSV-P2P-backed implementation of dkg.Transport.
// One instance per active DKG ceremony (clusterID + generation): outbound
// envelope bytes are wrapped in a signed SignedSSVMessage and broadcast
// via SSV's existing pubsub layer; inbound bytes are pushed in via Deliver
// (called by the per-node dispatcher).
//
// MsgID design choice: DKG is per-committee, not per-validator, but P2P
// subnet routing in SSV is per-validator-pubkey. Callers therefore supply
// a MessageID at construction time — typically derived from one of the
// committee's validator pubkeys (any choice works as long as every
// cluster operator is subscribed to that subnet, which they are by
// definition of operating that validator).
package p2p

import (
	"errors"
	"fmt"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	ssvmessage "github.com/ssvlabs/ssv/protocol/v2/message"
	protocolp2p "github.com/ssvlabs/ssv/protocol/v2/p2p"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

// defaultInboxBuffer sizes the inbound channel. DKG bandwidth is bursty
// at phase boundaries (n DealBundles arriving near-simultaneously); a
// per-cluster cap of 4n upper-bounds the per-phase peak comfortably for
// any cluster size SSV runs today (n ≤ 13).
const defaultInboxBuffer = 64

// Transport implements dkg.Transport over `protocolp2p.Network`. Inbound
// envelope bytes are pushed in via Deliver; outbound envelope bytes are
// wrapped in a signed SignedSSVMessage and broadcast via the network.
type Transport struct {
	network protocolp2p.Broadcaster
	signer  ssvtypes.OperatorSigner
	msgID   spectypes.MessageID
	inbox   chan []byte
}

// Options parameterises a Transport. Network, Signer, and MsgID are
// required; InboxBuffer defaults to 64 if 0.
type Options struct {
	Network     protocolp2p.Broadcaster
	Signer      ssvtypes.OperatorSigner
	MsgID       spectypes.MessageID
	InboxBuffer int
}

// New constructs a Transport from the given options.
func New(opts Options) (*Transport, error) {
	if opts.Network == nil {
		return nil, errors.New("dkg p2p: nil network broadcaster")
	}
	if opts.Signer == nil {
		return nil, errors.New("dkg p2p: nil operator signer")
	}
	buf := opts.InboxBuffer
	if buf <= 0 {
		buf = defaultInboxBuffer
	}
	return &Transport{
		network: opts.Network,
		signer:  opts.Signer,
		msgID:   opts.MsgID,
		inbox:   make(chan []byte, buf),
	}, nil
}

// Broadcast wraps `envelope` in a SignedSSVMessage with MsgType =
// SSVDKGMsgType and the per-ceremony MsgID, signs with the operator key,
// and publishes via the network. Errors propagate from the signing or
// publish steps; callers may retry.
func (t *Transport) Broadcast(envelope []byte) error {
	if len(envelope) == 0 {
		return errors.New("dkg p2p: empty envelope")
	}
	ssvMsg := &spectypes.SSVMessage{
		MsgType: ssvmessage.SSVDKGMsgType,
		MsgID:   t.msgID,
		Data:    envelope,
	}
	sig, err := t.signer.SignSSVMessage(ssvMsg)
	if err != nil {
		return fmt.Errorf("dkg p2p: sign envelope: %w", err)
	}
	signed := &spectypes.SignedSSVMessage{
		Signatures:  [][]byte{sig},
		OperatorIDs: []spectypes.OperatorID{t.signer.GetOperatorID()},
		SSVMessage:  ssvMsg,
	}
	if err := t.network.Broadcast(t.msgID, signed); err != nil {
		return fmt.Errorf("dkg p2p: publish: %w", err)
	}
	return nil
}

// Inbox returns the channel inbound DKG envelope bytes are delivered to.
// The channel is drained by the dkg.Coordinator; the dispatcher feeds it
// via Deliver.
func (t *Transport) Inbox() <-chan []byte {
	return t.inbox
}

// Deliver pushes `envelope` into the inbox. Called by the per-node
// DKG dispatcher after it has identified that the envelope's
// clusterID matches this Transport's ceremony.
//
// Returns an error if the inbox is full — callers may drop or backoff.
// The buffer is sized generously enough that this only fires under
// pathological backpressure (the Coordinator's pumpInbox goroutine
// drains continuously while Run is active).
func (t *Transport) Deliver(envelope []byte) error {
	if len(envelope) == 0 {
		return errors.New("dkg p2p: empty envelope")
	}
	select {
	case t.inbox <- envelope:
		return nil
	default:
		return errors.New("dkg p2p: inbox full")
	}
}
