package topics_test

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/libp2p/go-libp2p"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/host"
	libp2pnetwork "github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/message/signatureverifier"
	"github.com/ssvlabs/ssv/message/validation"
	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/network/discovery"
	"github.com/ssvlabs/ssv/network/topics"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/operator/duties/dutystore"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
	"github.com/ssvlabs/ssv/registry/storage/mocks"
	"github.com/ssvlabs/ssv/storage/basedb"
	"github.com/ssvlabs/ssv/storage/kv"
)

// TODO: fix this test to run post-fork
func TestTopicManager(t *testing.T) {
	// TODO: reduce running time of this test, use channels instead of long timeouts
	logger := logging.TestLogger(t)

	// TODO: rework this test to use message validation
	t.Run("happy flow", func(t *testing.T) {
		nPeers := 4

		cids := []string{"b768cdc2b2e0a859052bf04d1cd66383c96d95096a5287d08151494ce709556ba39c1300fbb902a0e2ebb7c31dc4e400",
			"824b9024767a01b56790a72afb5f18bb0f97d5bddb946a7bd8dd35cc607c35a4d76be21f24f484d0d478b99dc63ed170",
			"9340b7b80983a412bbb42cad6f992e06983d53deb41166ed5978dcbfa3761f347b237ad446d7cb4a4d0a5cca78c2ce8a",
			"a5abb232568fc869765da01688387738153f3ad6cc4e635ab282c5d5cfce2bba2351f03367103090804c5243dc8e229b",
			"a1169bd8407279d9e56b8cefafa37449afd6751f94d1da6bc8145b96d7ad2940184d506971291cd55ae152f9fc65b146",
			"80ff2cfb8fd80ceafbb3c331f271a9f9ce0ed3e360087e314d0a8775e86fa7cd19c999b821372ab6419cde376e032ff6",
			"a01909aac48337bab37c0dba395fb7495b600a53c58059a251d00b4160b9da74c62f9c4e9671125c59932e7bb864fd3d",
			"a4fc8c859ed5c10d7a1ff9fb111b76df3f2e0a6cbe7d0c58d3c98973c0ff160978bc9754a964b24929fff486ebccb629"}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		validator := &DummyMessageValidator{}

		peers := newPeers(ctx, logger, t, nPeers, validator, true, nil)
		baseTest(t, ctx, logger, peers, cids, 1)
	})

	t.Run("banning peer", func(t *testing.T) {
		t.Skip() // TODO: finish the test

		cids := []string{
			"b768cdc2b2e0a859052bf04d1cd66383c96d95096a5287d08151494ce709556ba39c1300fbb902a0e2ebb7c31dc4e400",
			"824b9024767a01b56790a72afb5f18bb0f97d5bddb946a7bd8dd35cc607c35a4d76be21f24f484d0d478b99dc63ed170",
			"9340b7b80983a412bbb42cad6f992e06983d53deb41166ed5978dcbfa3761f347b237ad446d7cb4a4d0a5cca78c2ce8a",
			"a5abb232568fc869765da01688387738153f3ad6cc4e635ab282c5d5cfce2bba2351f03367103090804c5243dc8e229b",
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		ctrl := gomock.NewController(t)

		dutyStore := dutystore.New()
		validatorStore := mocks.NewMockValidatorStore(ctrl)
		operators := mocks.NewMockOperators(ctrl)
		signatureVerifier := signatureverifier.NewMockSignatureVerifier(ctrl)

		validator := validation.New(
			networkconfig.TestNetwork,
			validatorStore,
			operators,
			dutyStore,
			signatureVerifier,
			phase0.Epoch(0))
		scoreMap := map[peer.ID]*pubsub.PeerScoreSnapshot{}
		var scoreMapMu sync.Mutex

		scoreInspector := func(m map[peer.ID]*pubsub.PeerScoreSnapshot) {
			b, _ := json.Marshal(m)
			t.Logf("peer scores: %v", string(b))

			scoreMapMu.Lock()
			defer scoreMapMu.Unlock()

			scoreMap = m
		}

		const nPeers = 4
		peers := newPeers(ctx, logger, t, nPeers, validator, true, scoreInspector)
		banningTest(t, logger, peers, cids, scoreMap, &scoreMapMu)
	})
}

func baseTest(t *testing.T, ctx context.Context, logger *zap.Logger, peers []*P, cids []string, minMsgCount int) {
	nValidators := len(cids)
	// nPeers := len(peers)

	t.Log("subscribing to topics")
	// listen to topics
	for _, cid := range cids {
		for _, p := range peers {
			require.NoError(t, p.tm.Subscribe(logger, committeeTopic(cid)))
			// simulate concurrency, by trying to subscribe multiple times
			go func(tm topics.Controller, cid string) {
				require.NoError(t, tm.Subscribe(logger, committeeTopic(cid)))
			}(p.tm, cid)
			go func(tm topics.Controller, cid string) {
				<-time.After(100 * time.Millisecond)
				require.NoError(t, tm.Subscribe(logger, committeeTopic(cid)))
			}(p.tm, cid)
		}
	}

	// wait for the peers to join topics
	<-time.After(3 * time.Second)

	t.Log("broadcasting messages")

	var wg sync.WaitGroup
	// publish some messages
	for i := 0; i < nValidators; i++ {
		for j, p := range peers {
			wg.Add(1)
			go func(p *P, cid string, pi int) {
				defer wg.Done()
				msg, err := dummyMsg(cid, pi%4, false)
				require.NoError(t, err)
				raw, err := msg.Encode()
				require.NoError(t, err)
				require.NoError(t, p.tm.Broadcast(committeeTopic(cid), raw, time.Second*10))
				<-time.After(time.Second * 5)
			}(p, cids[i], j)
		}
	}
	wg.Wait()

	// let the messages propagate
	wg.Add(1)
	go func() {
		defer wg.Done()
		// check number of peers and messages
		for i := 0; i < nValidators; i++ {
			wg.Add(1)
			go func(cid string) {
				ctxReadMessages, cancel := context.WithTimeout(ctx, time.Second*5)
				defer cancel()
				defer wg.Done()
				for _, p := range peers {
					// wait for messages
					for ctxReadMessages.Err() == nil && p.getCount(commons.GetTopicFullName(committeeTopic(cid))) < minMsgCount {
						time.Sleep(time.Millisecond * 100)
					}
					require.NoError(t, ctxReadMessages.Err())
					c := p.getCount(commons.GetTopicFullName(committeeTopic(cid)))
					require.GreaterOrEqual(t, c, minMsgCount)
					// require.LessOrEqual(t, c, maxMsgCount)
				}
			}(cids[i])
		}
	}()
	wg.Wait()

	t.Log("unsubscribing")
	// unsubscribing multiple times for each topic
	for i := 0; i < nValidators; i++ {
		for _, p := range peers {
			wg.Add(1)
			go func(p *P, cid string) {
				defer wg.Done()

				topic := committeeTopic(cid)
				topicFullName := commons.GetTopicFullName(topic)

				err := p.tm.Unsubscribe(logger, topic, false)
				require.NoError(t, err)

				go func(p *P) {
					<-time.After(time.Millisecond)

					err := p.tm.Unsubscribe(logger, topic, false)
					require.ErrorContains(t, err, fmt.Sprintf("failed to unsubscribe from topic %s: not subscribed", topicFullName))
				}(p)

				wg.Add(1)
				go func(p *P) {
					defer wg.Done()
					<-time.After(time.Millisecond * 50)

					err := p.tm.Unsubscribe(logger, topic, false)
					require.ErrorContains(t, err, fmt.Sprintf("failed to unsubscribe from topic %s: not subscribed", topicFullName))
				}(p)
			}(p, cids[i])
		}
	}
	wg.Wait()
}

func banningTest(t *testing.T, logger *zap.Logger, peers []*P, cids []string, scoreMap map[peer.ID]*pubsub.PeerScoreSnapshot, scoreMapMu *sync.Mutex) {
	t.Log("subscribing to topics")

	for _, cid := range cids {
		for _, p := range peers {
			require.NoError(t, p.tm.Subscribe(logger, committeeTopic(cid)))
		}
	}

	// wait for the peers to join topics
	<-time.After(3 * time.Second)

	t.Log("checking initial scores")
	for _, pk := range cids {
		for _, p := range peers {
			peerList, err := p.tm.Peers(pk)
			require.NoError(t, err)

			for _, pid := range peerList {
				scoreMapMu.Lock()
				v, ok := scoreMap[pid]
				scoreMapMu.Unlock()

				require.True(t, ok)
				require.Equal(t, 0, v.Score)
			}
		}
	}

	t.Log("broadcasting messages")

	const invalidMessagesCount = 10

	// TODO: get current default score, send an invalid rejected message, check the score; then run 10 of them and check the score; then check valid message

	invalidMessages, err := msgSequence(cids[0], invalidMessagesCount, len(cids), true)
	require.NoError(t, err)

	var wg sync.WaitGroup
	// publish some messages
	for i, msg := range invalidMessages {
		wg.Add(1)
		go func(p *P, cid string, msg *spectypes.SignedSSVMessage) {
			defer wg.Done()

			raw, err := msg.Encode()
			require.NoError(t, err)

			require.NoError(t, p.tm.Broadcast(committeeTopic(cid), raw, time.Second*10))

			<-time.After(time.Second * 5)
		}(peers[0], cids[i%len(cids)], msg)
	}
	wg.Wait()

	<-time.After(5 * time.Second)

	t.Log("checking final scores")
	for _, pk := range cids {
		for _, p := range peers {
			peerList, err := p.tm.Peers(pk)
			require.NoError(t, err)

			for _, pid := range peerList {
				scoreMapMu.Lock()
				v, ok := scoreMap[pid]
				scoreMapMu.Unlock()

				require.True(t, ok)
				require.Equal(t, 0, v.Score) // TODO: score should change
			}
		}
	}

	//t.Log("unsubscribing")
	//// unsubscribing multiple times for each topic
	//wg.Add(1)
	//go func(p *P, cid string) {
	//	defer wg.Done()
	//	require.NoError(t, p.tm.Unsubscribe(logger, committeeTopic(cid), false))
	//	go func(p *P) {
	//		<-time.After(time.Millisecond)
	//		require.NoError(t, p.tm.Unsubscribe(logger, committeeTopic(cid), false))
	//	}(p)
	//	wg.Add(1)
	//	go func(p *P) {
	//		defer wg.Done()
	//		<-time.After(time.Millisecond * 50)
	//		require.NoError(t, p.tm.Unsubscribe(logger, committeeTopic(cid), false))
	//	}(p)
	//}(peer, cid)
	//
	//wg.Wait()
}

func committeeTopic(cidHex string) string {
	cid, err := hex.DecodeString(cidHex)
	if err != nil {
		return "invalid"
	}

	return commons.CommitteeTopicID(spectypes.CommitteeID(cid))[0]
}

type P struct {
	host host.Host
	ps   *pubsub.PubSub
	tm   topics.Controller

	connsCount uint64

	msgsLock sync.Locker
	msgs     map[string][]*pubsub.Message
}

func (p *P) getCount(t string) int {
	p.msgsLock.Lock()
	defer p.msgsLock.Unlock()

	msgs, ok := p.msgs[t]
	if !ok {
		return 0
	}
	return len(msgs)
}

func (p *P) saveMsg(t string, msg *pubsub.Message) {
	p.msgsLock.Lock()
	defer p.msgsLock.Unlock()

	msgs, ok := p.msgs[t]
	if !ok {
		msgs = make([]*pubsub.Message, 0)
	}
	msgs = append(msgs, msg)
	p.msgs[t] = msgs
}

// TODO: use p2p/testing
func newPeers(ctx context.Context, logger *zap.Logger, t *testing.T, n int, msgValidator validation.MessageValidator, msgID bool, scoreInspector pubsub.ExtendedPeerScoreInspectFn) []*P {
	peers := make([]*P, n)
	for i := 0; i < n; i++ {
		peers[i] = newPeer(ctx, logger, t, msgValidator, msgID, scoreInspector)
	}
	t.Logf("%d peers were created", n)
	th := uint64(n/2) + uint64(n/4)
	for ctx.Err() == nil {
		done := 0
		for _, p := range peers {
			if atomic.LoadUint64(&p.connsCount) >= th {
				done++
			}
		}
		if done == len(peers) {
			break
		}
	}
	t.Log("peers are connected")
	return peers
}

func newPeer(ctx context.Context, logger *zap.Logger, t *testing.T, msgValidator validation.MessageValidator, msgID bool, scoreInspector pubsub.ExtendedPeerScoreInspectFn) *P {
	h, err := libp2p.New(libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/0"))
	require.NoError(t, err)
	ds, err := discovery.NewLocalDiscovery(ctx, logger, h)
	require.NoError(t, err)

	var p *P
	var midHandler topics.MsgIDHandler
	if msgID {
		midHandler = topics.NewMsgIDHandler(ctx, networkconfig.TestNetwork, 2*time.Minute)
		go midHandler.Start()
	}
	cfg := &topics.PubSubConfig{
		Host:         h,
		TraceLog:     false,
		MsgIDHandler: midHandler,
		MsgHandler: func(_ context.Context, topic string, msg *pubsub.Message) error {
			p.saveMsg(topic, msg)
			return nil
		},
		Scoring: &topics.ScoringConfig{
			IPWhitelist:        nil,
			IPColocationWeight: 0,
			OneEpochDuration:   time.Minute,
		},
		MsgValidator:           msgValidator,
		ScoreInspector:         scoreInspector,
		ScoreInspectorInterval: 100 * time.Millisecond,
		// TODO: add mock for peers.ScoreIndex
	}
	db, err := kv.NewInMemory(logger, basedb.Options{})
	require.NoError(t, err)

	_, validatorStore, err := registrystorage.NewSharesStorage(networkconfig.TestNetwork, db, []byte("test"))
	if err != nil {
		t.Fatal(err)
	}

	ps, tm, err := topics.NewPubSub(ctx, logger, cfg, validatorStore, nil)
	require.NoError(t, err)

	p = &P{
		host:     h,
		ps:       ps,
		tm:       tm,
		msgs:     make(map[string][]*pubsub.Message),
		msgsLock: &sync.Mutex{},
	}
	h.Network().Notify(&libp2pnetwork.NotifyBundle{
		ConnectedF: func(network libp2pnetwork.Network, conn libp2pnetwork.Conn) {
			atomic.AddUint64(&p.connsCount, 1)
		},
	})
	require.NoError(t, ds.Bootstrap(logger, func(e discovery.PeerEvent) {
		_ = h.Connect(ctx, e.AddrInfo)
	}))

	return p
}

func msgSequence(pkHex string, n, committeeSize int, malformed bool) ([]*spectypes.SignedSSVMessage, error) {
	var messages []*spectypes.SignedSSVMessage

	for i := 0; i < n; i++ {
		height := i * committeeSize
		msg, err := dummyMsg(pkHex, height, malformed)
		if err != nil {
			return nil, err
		}

		messages = append(messages, msg)
	}

	return messages, nil
}

func dummyMsg(pkHex string, height int, malformed bool) (*spectypes.SignedSSVMessage, error) {
	pk, err := hex.DecodeString(pkHex)
	if err != nil {
		return nil, err
	}

	id := spectypes.NewMsgID(networkconfig.TestNetwork.DomainType, pk, spectypes.RoleCommittee)
	signature, err := base64.StdEncoding.DecodeString("sVV0fsvqQlqliKv/ussGIatxpe8LDWhc9uoaM5WpjbiYvvxUr1eCpz0ja7UT1PGNDdmoGi6xbMC1g/ozhAt4uCdpy0Xdfqbv2hMf2iRL5ZPKOSmMifHbd8yg4PeeceyN")
	if err != nil {
		return nil, err
	}

	qbftMessage := &specqbft.Message{
		MsgType:    specqbft.RoundChangeMsgType,
		Height:     specqbft.Height(height),
		Round:      2,
		Identifier: id[:],
		Root:       [32]byte{},
	}

	msgData, err := qbftMessage.Encode()
	if err != nil {
		return nil, err
	}

	signedMessage := &spectypes.SignedSSVMessage{
		Signatures:  [][]byte{signature},
		OperatorIDs: []spectypes.OperatorID{1, 3, 4},
		SSVMessage: &spectypes.SSVMessage{
			MsgType: spectypes.SSVConsensusMsgType,
			MsgID:   id,
			Data:    msgData,
		},
		FullData: nil,
	}

	if malformed {
		signedMessage.SSVMessage.MsgType = math.MaxUint64
	}

	return signedMessage, nil
}

type DummyMessageValidator struct {
}

func (m *DummyMessageValidator) ValidatorForTopic(topic string) func(ctx context.Context, p peer.ID, pmsg *pubsub.Message) pubsub.ValidationResult {
	return func(ctx context.Context, p peer.ID, pmsg *pubsub.Message) pubsub.ValidationResult {
		return pubsub.ValidationAccept
	}
}

func (m *DummyMessageValidator) Validate(ctx context.Context, p peer.ID, pmsg *pubsub.Message) pubsub.ValidationResult {
	return pubsub.ValidationAccept
}
