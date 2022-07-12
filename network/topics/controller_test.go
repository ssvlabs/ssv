package topics

import (
	"context"
	"encoding/hex"
	"fmt"
	"github.com/bloxapp/ssv/network/forks/genesis"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bloxapp/ssv/network/discovery"
	"github.com/bloxapp/ssv/network/forks"
	"github.com/bloxapp/ssv/protocol/v1/message"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p-core/host"
	libp2pnetwork "github.com/libp2p/go-libp2p-core/network"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestTopicManager(t *testing.T) {
	nPeers := 4

	pks := []string{"b768cdc2b2e0a859052bf04d1cd66383c96d95096a5287d08151494ce709556ba39c1300fbb902a0e2ebb7c31dc4e400",
		"824b9024767a01b56790a72afb5f18bb0f97d5bddb946a7bd8dd35cc607c35a4d76be21f24f484d0d478b99dc63ed170",
		"9340b7b80983a412bbb42cad6f992e06983d53deb41166ed5978dcbfa3761f347b237ad446d7cb4a4d0a5cca78c2ce8a",
		"a5abb232568fc869765da01688387738153f3ad6cc4e635ab282c5d5cfce2bba2351f03367103090804c5243dc8e229b",
		"a1169bd8407279d9e56b8cefafa37449afd6751f94d1da6bc8145b96d7ad2940184d506971291cd55ae152f9fc65b146",
		"80ff2cfb8fd80ceafbb3c331f271a9f9ce0ed3e360087e314d0a8775e86fa7cd19c999b821372ab6419cde376e032ff6",
		"a01909aac48337bab37c0dba395fb7495b600a53c58059a251d00b4160b9da74c62f9c4e9671125c59932e7bb864fd3d",
		"a4fc8c859ed5c10d7a1ff9fb111b76df3f2e0a6cbe7d0c58d3c98973c0ff160978bc9754a964b24929fff486ebccb629"}
	// genesis features includes msg_id, msg validator, subnets, scoring
	t.Run("features", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		f := genesis.New()
		peers := newPeers(ctx, t, nPeers, true, true, f)
		baseTest(ctx, t, peers, pks, f, 1, 2)
	})
}

func baseTest(ctx context.Context, t *testing.T, peers []*P, pks []string, f forks.Fork, minMsgCount, maxMsgCount int) {
	nValidators := len(pks)
	//nPeers := len(peers)

	validatorTopic := func(pkhex string) string {
		pk, err := hex.DecodeString(pkhex)
		if err != nil {
			return "invalid"
		}
		return f.ValidatorTopicID(pk)[0]
	}

	t.Log("subscribing to topics")
	// listen to topics
	for _, pk := range pks {
		for _, p := range peers {
			require.NoError(t, p.tm.Subscribe(validatorTopic(pk)))
			// simulate concurrency, by trying to subscribe multiple times
			go func(tm Controller, pk string) {
				require.NoError(t, tm.Subscribe(validatorTopic(pk)))
			}(p.tm, pk)
			go func(tm Controller, pk string) {
				<-time.After(100 * time.Millisecond)
				require.NoError(t, tm.Subscribe(validatorTopic(pk)))
			}(p.tm, pk)
		}
	}

	// wait for the peers to join topics
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		c, cancel := context.WithTimeout(ctx, time.Second*5)
		defer cancel()
	peersLoop:
		for _, p := range peers {
			// TODO: modify for subnets
			for len(p.tm.Topics()) < nValidators {
				if c.Err() != nil {
					break peersLoop
				}
				time.Sleep(time.Millisecond * 100)
			}
		}

		nPeers := len(peers)
		for _, p := range peers {
			topics := p.tm.Topics()
			for _, topic := range topics {
				topicPeers, err := p.tm.Peers(topic)
				require.NoError(t, err)
				// wait for min peers
				for c.Err() == nil && len(topicPeers) < nPeers-1 {
					time.Sleep(time.Millisecond * 100)
					topicPeers, err = p.tm.Peers(topic)
					require.NoError(t, err)
				}
			}
		}
		require.NoError(t, c.Err())
	}()
	wg.Wait()

	t.Log("broadcasting messages")
	// publish some messages
	for i := 0; i < nValidators; i++ {
		for j, p := range peers {
			wg.Add(1)
			go func(p *P, pk string, pi int) {
				defer wg.Done()
				msg, err := dummyMsg(pk, pi%4)
				require.NoError(t, err)
				raw, err := msg.MarshalJSON()
				require.NoError(t, err)
				require.NoError(t, p.tm.Broadcast(validatorTopic(pk), raw, time.Second*10))
				<-time.After(time.Second * 5)
			}(p, pks[i], j)
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
			go func(pk string) {
				ctxReadMessages, cancel := context.WithTimeout(ctx, time.Second*5)
				defer cancel()
				defer wg.Done()
				for _, p := range peers {
					// wait for messages
					for ctxReadMessages.Err() == nil && p.getCount(f.GetTopicFullName(validatorTopic(pk))) < minMsgCount {
						time.Sleep(time.Millisecond * 100)
					}
					require.NoError(t, ctxReadMessages.Err())
					c := p.getCount(f.GetTopicFullName(validatorTopic(pk)))
					require.GreaterOrEqual(t, c, minMsgCount)
					//require.LessOrEqual(t, c, maxMsgCount)
				}
			}(pks[i])
		}
	}()
	wg.Wait()

	t.Log("unsubscribing")
	// unsubscribing multiple times for each topic
	for i := 0; i < nValidators; i++ {
		for _, p := range peers {
			wg.Add(1)
			go func(p *P, pk string) {
				defer wg.Done()
				require.NoError(t, p.tm.Unsubscribe(validatorTopic(pk), false))
				go func(p *P) {
					<-time.After(time.Millisecond)
					require.NoError(t, p.tm.Unsubscribe(validatorTopic(pk), false))
				}(p)
				wg.Add(1)
				go func(p *P) {
					defer wg.Done()
					<-time.After(time.Millisecond * 50)
					require.NoError(t, p.tm.Unsubscribe(validatorTopic(pk), false))
				}(p)
			}(p, pks[i])
		}
	}
	wg.Wait()
}

type P struct {
	host host.Host
	ps   *pubsub.PubSub
	tm   *topicsCtrl

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
func newPeers(ctx context.Context, t *testing.T, n int, msgValidator, msgID bool, fork forks.Fork) []*P {
	peers := make([]*P, n)
	for i := 0; i < n; i++ {
		peers[i] = newPeer(ctx, t, msgValidator, msgID, fork)
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

func newPeer(ctx context.Context, t *testing.T, msgValidator, msgID bool, fork forks.Fork) *P {
	h, err := libp2p.New(ctx,
		libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/0"))
	require.NoError(t, err)
	ds, err := discovery.NewLocalDiscovery(ctx, zap.L(), h)
	require.NoError(t, err)

	var p *P
	//logger := zaptest.NewLogger(t)
	logger := zap.L()
	var midHandler MsgIDHandler
	if msgID {
		midHandler = NewMsgIDHandler(logger, fork, 2*time.Minute)
	}
	cfg := &PububConfig{
		Logger:       logger,
		Host:         h,
		TraceLog:     true,
		MsgIDHandler: midHandler,
		MsgHandler: func(topic string, msg *pubsub.Message) error {
			p.saveMsg(topic, msg)
			return nil
		},
		Scoring: &ScoringConfig{
			IPWhilelist:        nil,
			IPColocationWeight: 0,
			AppSpecificWeight:  1,
			OneEpochDuration:   time.Minute,
		},
		// TODO: add mock for peers.ScoreIndex
	}
	//
	if msgValidator {
		cfg.MsgValidatorFactory = func(s string) MsgValidatorFunc {
			return NewSSVMsgValidator(logger.With(zap.String("who", "MsgValidator")),
				fork, h.ID())
		}
	}
	ps, tm, err := NewPubsub(ctx, cfg, fork)
	require.NoError(t, err)

	p = &P{
		host:     h,
		ps:       ps,
		tm:       tm.(*topicsCtrl),
		msgs:     make(map[string][]*pubsub.Message),
		msgsLock: &sync.Mutex{},
	}
	h.Network().Notify(&libp2pnetwork.NotifyBundle{
		ConnectedF: func(network libp2pnetwork.Network, conn libp2pnetwork.Conn) {
			atomic.AddUint64(&p.connsCount, 1)
		},
	})
	require.NoError(t, ds.Bootstrap(func(e discovery.PeerEvent) {
		_ = h.Connect(ctx, e.AddrInfo)
	}))

	return p
}

func dummyMsg(pkHex string, height int) (*message.SSVMessage, error) {
	pk, err := hex.DecodeString(pkHex)
	if err != nil {
		return nil, err
	}
	id := message.NewIdentifier(pk, message.RoleTypeAttester)
	msgData := fmt.Sprintf(`{
	  "message": {
		"type": 3,
		"round": 2,
		"identifier": "%s",
		"height": %d,
		"value": "bk0iAAAAAAACAAAAAAAAAAbYXFSt2H7SQd5q5u+N0bp6PbbPTQjU25H1QnkbzTECahIBAAAAAADmi+NJfvXZ3iXp2cfs0vYVW+EgGD7DTTvr5EkLtiWq8WsSAQAAAAAAIC8dZTEdD3EvE38B9kDVWkSLy40j0T+TtSrrrBqVjo4="
	  },
	  "signature": "sVV0fsvqQlqliKv/ussGIatxpe8LDWhc9uoaM5WpjbiYvvxUr1eCpz0ja7UT1PGNDdmoGi6xbMC1g/ozhAt4uCdpy0Xdfqbv2hMf2iRL5ZPKOSmMifHbd8yg4PeeceyN",
	  "signer_ids": [1,3,4]
	}`, id, height)
	return &message.SSVMessage{
		MsgType: message.SSVConsensusMsgType,
		ID:      id,
		Data:    []byte(msgData),
	}, nil
}

//
//func createShares(n int) []*bls.SecretKey {
//	threshold.Init()
//
//	var res []*bls.SecretKey
//	for i := 0; i < n; i++ {
//		sk := bls.SecretKey{}
//		sk.SetByCSPRNG()
//		res = append(res, &sk)
//		fmt.Printf("\"%s\",", sk.GetPublicKey().SerializeToHexStr())
//	}
//	return res
//}
