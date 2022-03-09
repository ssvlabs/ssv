package topics

import (
	"context"
	"encoding/hex"
	"fmt"
	"github.com/bloxapp/ssv/beacon"
	"github.com/bloxapp/ssv/network/forks"
	forksv1 "github.com/bloxapp/ssv/network/forks/v1"
	"github.com/bloxapp/ssv/network/p2p/discovery"
	"github.com/bloxapp/ssv/protocol"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p-core/host"
	libp2pnetwork "github.com/libp2p/go-libp2p-core/network"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestTopicManager(t *testing.T) {
	nPeers := 8

	pks := []string{"b768cdc2b2e0a859052bf04d1cd66383c96d95096a5287d08151494ce709556ba39c1300fbb902a0e2ebb7c31dc4e400",
		"824b9024767a01b56790a72afb5f18bb0f97d5bddb946a7bd8dd35cc607c35a4d76be21f24f484d0d478b99dc63ed170",
		"9340b7b80983a412bbb42cad6f992e06983d53deb41166ed5978dcbfa3761f347b237ad446d7cb4a4d0a5cca78c2ce8a",
		"a5abb232568fc869765da01688387738153f3ad6cc4e635ab282c5d5cfce2bba2351f03367103090804c5243dc8e229b",
		"a1169bd8407279d9e56b8cefafa37449afd6751f94d1da6bc8145b96d7ad2940184d506971291cd55ae152f9fc65b146",
		"80ff2cfb8fd80ceafbb3c331f271a9f9ce0ed3e360087e314d0a8775e86fa7cd19c999b821372ab6419cde376e032ff6",
		"a01909aac48337bab37c0dba395fb7495b600a53c58059a251d00b4160b9da74c62f9c4e9671125c59932e7bb864fd3d",
		"a4fc8c859ed5c10d7a1ff9fb111b76df3f2e0a6cbe7d0c58d3c98973c0ff160978bc9754a964b24929fff486ebccb629"}
	//shares := createShares(nValidators)

	t.Run("v0 features", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		f := forksv1.New()
		peers := newPeers(ctx, t, nPeers, false, false, f)
		baseTest(ctx, t, peers, pks, f, 8)
	})

	t.Run("v1 features", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		f := forksv1.New()
		peers := newPeers(ctx, t, nPeers, true, true, f)
		baseTest(ctx, t, peers, pks, f, 2)
	})

}

func baseTest(ctx context.Context, t *testing.T, peers []*P, pks []string, f forks.Fork, msgCountTopic int) {
	nValidators := len(pks)
	nPeers := len(pks)

	topicName := func(pkhex string) string {
		pk, err := hex.DecodeString(pkhex)
		if err != nil {
			return "invalid"
		}
		return f.ValidatorTopicID(pk)
	}
	subTopic := func(p *P, i int, potentialErrs ...error) {
		tname := topicName(pks[i])
		in, err := p.tm.Subscribe(tname)
		if len(potentialErrs) == 0 {
			require.NoError(t, err)
		} else if err != nil {
			var errP *error
			for _, e := range potentialErrs {
				if e.Error() == err.Error() {
					errP = &e
					break
				}
			}
			require.Nil(t, errP, "got err", errP)
		}
		if in == nil {
			return
		}
		for ctx.Err() == nil {
			next := <-in
			p.saveMsg(tname, next)
		}
	}

	// listen to topics
	for i := 0; i < nValidators; i++ {
		for _, p := range peers {
			go subTopic(p, i)
			// simulate concurrency, by trying to subscribe twice
			<-time.After(time.Millisecond)
			go subTopic(p, i, ErrInProcess, errTopicAlreadyExists)
		}
	}

	// let the peers join topics
	<-time.After(time.Second * 4)

	// publish some messages
	for i := 0; i < nValidators; i++ {
		for j, p := range peers {
			go func(p *P, pk string, pi int) {
				msg, err := dummyMsg(pk, 1)
				if pi%2 == 0 { // ensuring that 2 messages will be created across peers
					msg, err = dummyMsg(pk, 2)
				}
				require.NoError(t, err)
				raw, err := msg.Encode()
				require.NoError(t, err)
				require.NoError(t, p.tm.Broadcast(topicName(pk), raw, time.Second*5))
			}(p, pks[i], j)
		}
	}

	// let the messages propagate
	<-time.After(time.Second * 5)

	// check number of topics
	for _, p := range peers {
		require.Len(t, p.tm.Topics(), nValidators)
	}

	// check number of peers and messages
	for i := 0; i < nValidators; i++ {
		for _, p := range peers {
			pk := pks[i]
			peers, err := p.tm.Peers(topicName(pk))
			require.NoError(t, err)
			require.Len(t, peers, nPeers-1)
			c := p.getCount(topicName(pk))
			require.Equal(t, msgCountTopic, c) // expecting only 2 messages, as the rest were filtered by duplicated id
		}
	}

	// unsubscribe
	var wg sync.WaitGroup
	for i := 0; i < nValidators; i++ {
		for _, p := range peers {
			wg.Add(1)
			go func(p *P, pk string) {
				defer wg.Done()
				require.NoError(t, p.tm.Unsubscribe(topicName(pk)))
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
	host, err := libp2p.New(ctx,
		libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/0"))
	require.NoError(t, err)
	require.NoError(t, discovery.SetupMdnsDiscovery(ctx, zap.L(), host))

	//logger := zaptest.NewLogger(t)
	logger := zap.L()
	psBundle, err := NewPubsub(ctx, &PububConfig{
		Logger:   logger,
		Host:     host,
		TraceLog: false,
		MsgValidatorFactory: func(s string) MsgValidatorFunc {
			return NewSSVMsgValidator(logger.With(zap.String("who", "MsgValidator")),
				fork, host.ID())
		},
		UseMsgID: msgID,
		Fork:     fork,
	})
	require.NoError(t, err)
	ps := psBundle.PS
	tm := psBundle.TopicsCtrl

	p := &P{
		host:     host,
		ps:       ps,
		tm:       tm.(*topicsCtrl),
		msgs:     make(map[string][]*pubsub.Message),
		msgsLock: &sync.Mutex{},
	}
	host.Network().Notify(&libp2pnetwork.NotifyBundle{
		ConnectedF: func(network libp2pnetwork.Network, conn libp2pnetwork.Conn) {
			atomic.AddUint64(&p.connsCount, 1)
		},
	})
	return p
}

func dummyMsg(pkHex string, height int) (*protocol.SSVMessage, error) {
	pk, err := hex.DecodeString(pkHex)
	if err != nil {
		return nil, err
	}
	id := protocol.NewIdentifier(pk, beacon.RoleTypeAttester)
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
	return &protocol.SSVMessage{
		MsgType: protocol.SSVConsensusMsgType,
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
