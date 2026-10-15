package validation

import (
	"context"
	"testing"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	pb "github.com/libp2p/go-libp2p-pubsub/pb"
	"github.com/libp2p/go-libp2p/core/peer"
	libp2ptest "github.com/libp2p/go-libp2p/core/test"
	"github.com/stretchr/testify/require"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/operator/duties/dutystore"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
)

type observation struct {
	peer peer.ID
	op   spectypes.OperatorID
}

type recordingObserver struct{ calls []observation }

func (r *recordingObserver) Observe(p peer.ID, op spectypes.OperatorID) {
	r.calls = append(r.calls, observation{peer: p, op: op})
}

func signedBy(ops ...spectypes.OperatorID) *queue.SSVMessage {
	return &queue.SSVMessage{SignedSSVMessage: &spectypes.SignedSSVMessage{OperatorIDs: ops}}
}

func randPeer(t *testing.T) peer.ID {
	t.Helper()
	p, err := libp2ptest.RandPeerID()
	require.NoError(t, err)
	return p
}

// authoredBy returns a pubsub message authored by author (its From field) and delivered by
// relay (ReceivedFrom), as gossipsub hands it to the validator.
func authoredBy(author, relay peer.ID) *pubsub.Message {
	return &pubsub.Message{Message: &pb.Message{From: []byte(author)}, ReceivedFrom: relay}
}

func TestObserveAccepted_SingleSignerIsRecorded(t *testing.T) {
	obs := &recordingObserver{}
	mv := &messageValidator{attribution: obs}
	p := randPeer(t)

	mv.observeAccepted(authoredBy(p, p), signedBy(7))

	require.Equal(t, []observation{{peer: p, op: 7}}, obs.calls)
}

func TestObserveAccepted_AuthorIsRecordedNotRelay(t *testing.T) {
	obs := &recordingObserver{}
	mv := &messageValidator{attribution: obs}
	author, relay := randPeer(t), randPeer(t)

	mv.observeAccepted(authoredBy(author, relay), signedBy(7))

	require.Equal(t, []observation{{peer: author, op: 7}}, obs.calls)
}

func TestObserveAccepted_MultiSignerIsSkipped(t *testing.T) {
	obs := &recordingObserver{}
	mv := &messageValidator{attribution: obs}
	p := randPeer(t)

	mv.observeAccepted(authoredBy(p, p), signedBy(1, 2, 3))

	require.Empty(t, obs.calls)
}

func TestObserveAccepted_NoObserver(t *testing.T) {
	mv := &messageValidator{}
	p := randPeer(t)
	require.NotPanics(t, func() { mv.observeAccepted(authoredBy(p, p), signedBy(7)) })
}

func TestObserveAccepted_NilMessagesAreSkipped(t *testing.T) {
	obs := &recordingObserver{}
	mv := &messageValidator{attribution: obs}
	p := randPeer(t)

	mv.observeAccepted(authoredBy(p, p), nil)
	mv.observeAccepted(authoredBy(p, p), &queue.SSVMessage{})
	mv.observeAccepted(nil, signedBy(7))
	mv.observeAccepted(&pubsub.Message{}, signedBy(7))

	require.Empty(t, obs.calls)
}

func TestObserveAccepted_MissingOrMalformedAuthorIsSkipped(t *testing.T) {
	obs := &recordingObserver{}
	mv := &messageValidator{attribution: obs}
	relay := randPeer(t)

	mv.observeAccepted(&pubsub.Message{Message: &pb.Message{}, ReceivedFrom: relay}, signedBy(7))
	mv.observeAccepted(&pubsub.Message{Message: &pb.Message{From: []byte("not-a-peer-id")}, ReceivedFrom: relay}, signedBy(7))

	require.Empty(t, obs.calls)
}

func TestObserveAccepted_SelfAuthorIsSkipped(t *testing.T) {
	obs := &recordingObserver{}
	self := randPeer(t)
	mv := &messageValidator{attribution: obs, selfPID: self}

	mv.observeAccepted(authoredBy(self, randPeer(t)), signedBy(7))

	require.Empty(t, obs.calls)
}

func TestValidate_RejectedMessageIsNotAttributed(t *testing.T) {
	obs := &recordingObserver{}
	mv := New(networkconfig.TestNetwork, nil, nil, dutystore.New(), nil, WithAttribution(obs))

	result := mv.Validate(context.Background(), randPeer(t), &pubsub.Message{})

	require.Equal(t, pubsub.ValidationReject, result)
	require.Empty(t, obs.calls)
}
