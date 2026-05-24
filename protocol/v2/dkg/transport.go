package dkg

// Transport is the message-passing channel the Coordinator uses to talk
// to peers in the cluster during a DKG ceremony.
//
// Production wires SSV's P2P broadcaster behind this. Tests
// use an in-memory fan-out implementation.
//
// The transport handles raw envelope bytes; envelope parsing
// (wire.Unwrap) is the Coordinator's responsibility.
type Transport interface {
	// Broadcast sends `envelope` to all other cluster members. The
	// envelope is the byte sequence produced by wire.WrapExchange /
	// WrapDeal / WrapResponse / WrapJustification. The transport itself
	// does not interpret it.
	Broadcast(envelope []byte) error

	// Inbox returns a channel delivering envelopes received from cluster
	// peers. Implementations should drop or buffer-with-loss rather than
	// block on a slow consumer; the Coordinator drains the channel
	// continuously while a ceremony is in flight.
	Inbox() <-chan []byte
}
