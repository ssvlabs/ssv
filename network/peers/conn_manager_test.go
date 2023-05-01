package peers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"math/rand"
	"sort"
	"testing"

	"github.com/bloxapp/ssv/logging"
	"github.com/bloxapp/ssv/network/records"
	connmgrcore "github.com/libp2p/go-libp2p/core/connmgr"
	libp2pnetwork "github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestTagBestPeers(t *testing.T) {
	logger := logging.TestLogger(t)
	connMgrMock := newConnMgr()

	allSubs, _ := records.Subnets{}.FromString(records.AllSubnets)
	si := newSubnetsIndex(len(allSubs))

	cm := NewConnManager(zap.NewNop(), connMgrMock, si).(*connManager)

	pids, err := createPeerIDs(50)
	require.NoError(t, err)

	for _, pid := range pids {
		r := rand.Intn(len(allSubs) / 3)
		si.UpdatePeerSubnets(pid, createRandomSubnets(r))
	}
	mySubnets := createRandomSubnets(40)

	best := cm.getBestPeers(40, mySubnets, pids, 10)
	require.Len(t, best, 40)

	cm.TagBestPeers(logger, 20, mySubnets, pids, 10)
	require.Equal(t, 20, len(connMgrMock.tags))
}

func createRandomSubnets(n int) records.Subnets {
	subnets, _ := records.Subnets{}.FromString(records.ZeroSubnets)
	size := len(subnets)
	for n > 0 {
		i := rand.Intn(size)
		for subnets[i] == byte(1) {
			i = rand.Intn(size)
		}
		subnets[i] = byte(1)
		n--
	}
	return subnets
}

type mockConnManager struct {
	tags map[peer.ID]string
}

var _ connmgrcore.ConnManager = (*mockConnManager)(nil)

func newConnMgr() *mockConnManager {
	return &mockConnManager{
		tags: map[peer.ID]string{},
	}
}

func (m mockConnManager) TagPeer(id peer.ID, s string, i int) {
}

func (m mockConnManager) UntagPeer(p peer.ID, tag string) {
}

func (m mockConnManager) UpsertTag(p peer.ID, tag string, upsert func(int) int) {
}

func (m mockConnManager) GetTagInfo(p peer.ID) *connmgrcore.TagInfo {
	return nil
}

func (m mockConnManager) TrimOpenConns(ctx context.Context) {
}

func (m mockConnManager) Notifee() libp2pnetwork.Notifiee {
	return nil
}

func (m mockConnManager) Protect(id peer.ID, tag string) {
	m.tags[id] = tag

}

func (m mockConnManager) Unprotect(id peer.ID, tag string) (protected bool) {
	_, ok := m.tags[id]
	if ok {
		delete(m.tags, id)
	}
	return ok
}

func (m mockConnManager) IsProtected(id peer.ID, tag string) (protected bool) {
	_, ok := m.tags[id]
	return ok
}

func (m mockConnManager) Close() error {
	return nil
}

func TestScorePeer(t *testing.T) {
	subnetScores := []float64{
		scoreSubnet(0, 5, 10),  // 0: Disconnected
		scoreSubnet(2, 5, 10),  // 1: Underconnected
		scoreSubnet(5, 5, 20),  // 2: Barely connected
		scoreSubnet(15, 5, 20), // 3: Well connected
		scoreSubnet(30, 5, 20), // 4: Overconnected
	}
	peers := []records.Subnets{
		{1, 1, 1, 1, 1},
		{0, 1, 1, 1, 1},
		{0, 0, 1, 1, 1},
		{0, 0, 0, 1, 1},
		{0, 0, 0, 0, 1},
		{1, 1, 1, 1, 0},
		{1, 1, 1, 0, 0},
		{1, 1, 0, 0, 0},
		{1, 0, 0, 0, 0},
		{0, 0, 0, 0, 0},
	}
	for peerIndex, p := range peers {
		score := scorePeer(p, subnetScores)
		var peerSubnets string
		for i, s := range p {
			if i > 0 {
				peerSubnets += ":"
			}
			peerSubnets += fmt.Sprint(s)
		}
		fmt.Printf("Peer #%d: Subnets(%s) -> Score(%f)\n", 1000+peerIndex, peerSubnets, score)
	}
}

func TestSimulation(t *testing.T) {
	var err error

	// s, err := records.Subnets{}.FromString("00004000400000000040004010000000")
	// require.NoError(t, err)
	// for subnet, ss := range s {
	// 	if ss == 1 {
	// 		fmt.Printf("Subnet %d\n", subnet)
	// 	}
	// }

	var dmp dump
	err = json.Unmarshal([]byte(ssv_2_18_30), &dmp)
	require.NoError(t, err)
	mySubnets, err := records.Subnets{}.FromString(dmp.MySubnets)
	require.NoError(t, err)
	_ = mySubnets

	mySubnets, err = records.Subnets{}.FromString("00000041000000008040000000400000")
	require.NoError(t, err)

	// for i, p := range dmp.Peers {
	// 	peerSubnets, err := records.Subnets{}.FromString(p.Subnets)
	// 	require.NoError(t, err)
	// 	if len(peerSubnets) == 128 {
	// 		for j := 100; j < 128; j++ {
	// 			peerSubnets[j] = 0
	// 			dmp.SubnetsConnections[j]--
	// 		}
	// 	}
	// 	dmp.Peers[i].Subnets = peerSubnets.String()
	// }

	allSubs, _ := records.Subnets{}.FromString(records.AllSubnets)
	activeSubnets := records.SharedSubnets(allSubs, mySubnets, 0)
	subnetScores := make([]float64, len(allSubs))
	for subnet, conns := range dmp.SubnetsConnections {
		active := false
		for _, s := range activeSubnets {
			if s == subnet {
				active = true
				break
			}
		}
		if active {
			subnetScores[subnet] = 0.2 + scoreSubnet(conns, 4, 10)
		}
	}
	// sort.Float64Slice(subnetScores).Sort()
	log.Printf("subnetScores: %#v", subnetScores)

	peers := dmp.Peers
	sort.Slice(peers, func(i, j int) bool {
		return peers[i].Score > peers[j].Score
	})

	var newPeers []struct {
		Peer  peer.ID
		Score PeerScore
	}
	for _, p := range peers {
		peerSubnets, err := records.Subnets{}.FromString(p.Subnets)
		require.NoError(t, err)

		peerScore := scorePeer(peerSubnets, subnetScores)
		newPeers = append(newPeers, struct {
			Peer  peer.ID
			Score PeerScore
		}{Peer: p.Peer, Score: peerScore})
	}

	sort.Slice(newPeers, func(i, j int) bool {
		return newPeers[i].Score > newPeers[j].Score
	})

	for newRank, newPeer := range newPeers {
		newScore := newPeer.Score
		oldRank := -1
		oldScore := PeerScore(0)
		var peer peerDump
		for rank, p := range peers {
			if p.Peer == newPeer.Peer {
				oldRank = rank
				oldScore = p.Score
				peer = p
				break
			}
		}
		if oldRank == -1 {
			panic("peer not found")
		}
		peerName := peer.Peer.String()
		if name, ok := peerNames[peerName]; ok {
			peerName = name
		}
		peerSubnets, err := records.Subnets{}.FromString(peer.Subnets)
		require.NoError(t, err)
		newSharedSubnets := records.SharedSubnets(peerSubnets, mySubnets, len(mySubnets))
		fmt.Printf("Peer %53s: SharedSubnets=%d NewSharedSubnets=%d Old(%.2f #%d) New(%.2f #%d) Subnets(%s)\n",
			peerName, peer.SharedSubnets, len(newSharedSubnets), oldScore, oldRank, newScore, newRank, peer.Subnets)
	}

	// If we were to keep only the top 50% of peers, how would the subnet connections look like
	// with the old vs. new scores?
	oldSubnetConnections := make([]int, 128)
	newSubnetConnections := make([]int, 128)
	for _, p := range peers[:len(peers)/2] {
		peerSubnets, err := records.Subnets{}.FromString(p.Subnets)
		require.NoError(t, err)
		for subnet, connected := range peerSubnets {
			if connected == 1 {
				oldSubnetConnections[subnet]++
			}
		}
	}
	for _, newPeer := range newPeers[:len(newPeers)/2] {
		var peer *peerDump
		for _, p := range peers {
			if p.Peer == newPeer.Peer {
				peer = &p
				break
			}
		}
		if peer == nil {
			panic("peer not found")
		}
		peerSubnets, err := records.Subnets{}.FromString(peer.Subnets)
		require.NoError(t, err)
		for subnet, connected := range peerSubnets {
			if connected == 1 {
				newSubnetConnections[subnet]++
			}
		}
	}

	// Print both old and new subnet connection distributions
	fmt.Println("Old subnet connections:")
	min := 0
	max := math.MinInt64
	sum := 0
	sort.Ints(oldSubnetConnections)
	for _, conns := range oldSubnetConnections {
		fmt.Printf("%d ", conns)
		if conns < min {
			min = conns
		}
		if conns > max {
			max = conns
		}
		sum += conns
	}
	fmt.Println()
	fmt.Printf("\nMin: %d, Max: %d, Avg: %.2f\n", min, max, float64(sum)/float64(len(oldSubnetConnections)))
	fmt.Println()
	fmt.Println("New subnet connections:")
	min = 0
	max = math.MinInt64
	sum = 0
	sort.Ints(newSubnetConnections)
	for _, conns := range newSubnetConnections {
		fmt.Printf("%d ", conns)
		if conns < min {
			min = conns
		}
		if conns > max {
			max = conns
		}
		sum += conns
	}
	fmt.Println()
	fmt.Printf("\nMin: %d, Max: %d, Avg: %.2f\n", min, max, float64(sum)/float64(len(newSubnetConnections)))
	fmt.Println()
}

var peerNames = map[string]string{
	"16Uiu2HAmMorRmGosQJzh3DUtiKF9BBspCWA6KQ3EPKWsUQMCmU2x": "ssv-node-1",
	"16Uiu2HAm4uu7WFUDdLwTkYYsZc3rqE2cvQBVZsfgkdPkFr5Bct9o": "ssv-node-2",
	"16Uiu2HAm4RuLkxdxXkRHBNm52aEo2FMAjSv947NhZkfR2xuwLWFM": "ssv-node-3",
	"16Uiu2HAmUHiQMemRv1AjyLid17Z3Rvbe9LekqjoWaVJyL3drmKh8": "ssv-node-4",
	"16Uiu2HAkwt9SuuNXKHjDCLvcX5LNVEvsBeLGUCtAKccDFuCrCqTC": "special-29",
	"16Uiu2HAkxLGN74DMAyCQXqtoZAXFfYnMJJjudwF9kcU6GaHdRE4i": "special-5",
	"16Uiu2HAmKD3qvyAb4moxhMiFkNmKHHni6aNdKbH8G7WT5mdaHM9Y": "1-shared",
}

type peerDump struct {
	Peer          peer.ID
	Score         PeerScore
	SharedSubnets int
	Subnets       string
}

type dump struct {
	MySubnets          string `json:"my_subnets"`
	Peers              []peerDump
	SubnetsConnections []int `json:"subnets_connections"`
}

var ssv_3_15_28 = `{"my_subnets":"ffffffffffffffffffffffffffffffff","peers":[{"Peer":"16Uiu2HAkwt9SuuNXKHjDCLvcX5LNVEvsBeLGUCtAKccDFuCrCqTC","Score":-0.14322916666666663,"SharedSubnets":29,"Subnets":"0008284002a280008903041527094885"},{"Peer":"16Uiu2HAmEe8GnSuE2syDCKqZ8EjuPSSGoC7Z3p3KXYNoM6ZEawYT","Score":-0.13020833333333334,"SharedSubnets":30,"Subnets":"2808a84012a10000a901041527114081"},{"Peer":"16Uiu2HAmJS8vouJxen9LGPcur2vtEGjvZPvu9tJ1YV9jCS32nPAc","Score":-0.11588541666666667,"SharedSubnets":28,"Subnets":"000c0a8d8210c462704005200a002100"},{"Peer":"16Uiu2HAkx6vuo9vAV85uRvxscecXWN7SNerAi6ETERGtDwBatW5j","Score":-0.10677083333333341,"SharedSubnets":101,"Subnets":"ab3ef6ef7fe9ffffbf9b6eeff3effefc"},{"Peer":"16Uiu2HAmHLrAJr75TukUswnvvfkzpFiNE8yoB9Vw9E6v6869J7y8","Score":-0.08593750000000001,"SharedSubnets":9,"Subnets":"08000004000000420020010020001800"},{"Peer":"16Uiu2HAm89btspBaQ6FLZJcYxS4GDFoAxgyGY6qCHSW95ooi38m8","Score":-0.08072916666666663,"SharedSubnets":15,"Subnets":"000098420a4000480480010040200000"},{"Peer":"16Uiu2HAm87sWQcCPTCPLr1xZ3TvyWqBAHRRCWKkrniqhrgeu9gbJ","Score":-0.0703125,"SharedSubnets":9,"Subnets":"08000004800000420020010020000002"},{"Peer":"16Uiu2HAm4uu7WFUDdLwTkYYsZc3rqE2cvQBVZsfgkdPkFr5Bct9o","Score":-0.057291666666666664,"SharedSubnets":128,"Subnets":"ffffffffffffffffffffffffffffffff"},{"Peer":"16Uiu2HAmMorRmGosQJzh3DUtiKF9BBspCWA6KQ3EPKWsUQMCmU2x","Score":-0.057291666666666664,"SharedSubnets":128,"Subnets":"ffffffffffffffffffffffffffffffff"},{"Peer":"16Uiu2HAmTNWo4henNoHdVwcA2FBzS7nh5upjHjstafCAvbtYT6g9","Score":-0.057291666666666664,"SharedSubnets":128,"Subnets":"ffffffffffffffffffffffffffffffff"},{"Peer":"16Uiu2HAmUHiQMemRv1AjyLid17Z3Rvbe9LekqjoWaVJyL3drmKh8","Score":-0.057291666666666664,"SharedSubnets":128,"Subnets":"ffffffffffffffffffffffffffffffff"},{"Peer":"16Uiu2HAmCH7wX7MvcH339VyqFo82C1Kf3GDnUDG9ZAK8YK9hiRCS","Score":-0.046875000000000014,"SharedSubnets":27,"Subnets":"040120180010124c16f0400348009401"},{"Peer":"16Uiu2HAm7EgFz3Zsg9Hk6HzbCPK9u97cVmFUoHzqnHkP4Zbrc3yj","Score":-0.013020833333333363,"SharedSubnets":6,"Subnets":"00000200000000000100000410084000"},{"Peer":"16Uiu2HAmCbi6DTk49BEakGLi9LysHuLkkjAas1mDMrtJhL9VoA6K","Score":-0.006510416666666664,"SharedSubnets":9,"Subnets":"00010808009001040000000020000002"},{"Peer":"16Uiu2HAmAcNyd6VaA1JDCjZ5QdWFdEWnnKBGbAYUWvckqApt8nr1","Score":0.0052083333333333036,"SharedSubnets":5,"Subnets":"00000000000000020200200010001000"},{"Peer":"16Uiu2HAmJa7gNYQCumkMAxWZT5kb99VJfPdM99ZZacv8kbqKzVk9","Score":0.011718750000000007,"SharedSubnets":6,"Subnets":"02000208800000000020100000000000"},{"Peer":"16Uiu2HAm2Z1eWNeNmaTeMHDPB4WB9HaHUHXzerrqei9SijQmJXXo","Score":0.014322916666666668,"SharedSubnets":17,"Subnets":"00401000604004480480010284a00001"},{"Peer":"16Uiu2HAmFhNizAZxBjKfry2vYxyidPtPH6QVZc6YVLGd76yxYYFi","Score":0.0169270833333333,"SharedSubnets":9,"Subnets":"80000021000020080800000100280000"},{"Peer":"16Uiu2HAmFQdGq8XHndxZo6VvUicBjvikarjVzdVFAbSnQ8GuJNxF","Score":0.018229166666666675,"SharedSubnets":9,"Subnets":"00048802880000000001800000000200"},{"Peer":"16Uiu2HAkyfXMGd18iGWQ8DDL8Vowx5XV1wxNzAtF8Yok5MWfo84y","Score":0.022135416666666654,"SharedSubnets":3,"Subnets":"00010000000000000020000000080000"},{"Peer":"16Uiu2HAm2e9cXq7HEUSGcZpbETwjhxdkFjvoiufR6KaZMcXUHdFg","Score":0.023437499999999962,"SharedSubnets":4,"Subnets":"00040080008000000000000020000000"},{"Peer":"16Uiu2HAm3BtFC7hzip7czpo6p15iTUPZ2T9KAf1ECue1akDPmewL","Score":0.026041666666666654,"SharedSubnets":4,"Subnets":"00004002000080000010000000000000"},{"Peer":"16Uiu2HAm41iFvN8CbnRnj4iNQJ3sLoWUEWBVDjQxshmQ6nHujPTG","Score":0.027343750000000017,"SharedSubnets":5,"Subnets":"00000000010000000200200010001000"},{"Peer":"16Uiu2HAmLsDqf3oGdqCU37TyAo3yRtEju7jfz8LuYMwtyzMcA2yL","Score":0.028645833333333297,"SharedSubnets":4,"Subnets":"00000000004010000000008002000000"},{"Peer":"16Uiu2HAkxLGN74DMAyCQXqtoZAXFfYnMJJjudwF9kcU6GaHdRE4i","Score":0.02864583333333331,"SharedSubnets":5,"Subnets":"00000041000000008040000000400000"},{"Peer":"16Uiu2HAkwW7i8B72U54LGWeruwHzZLwuUU66oY2SPFD4fpxfBvmJ","Score":0.02864583333333332,"SharedSubnets":5,"Subnets":"00004000400000000040004010000000"},{"Peer":"16Uiu2HAmKTSWRiRSUJetUSpArWfXiXve54YNwVC6VYzYi4KnhBse","Score":0.03385416666666664,"SharedSubnets":5,"Subnets":"00000022000000000040000080000008"},{"Peer":"16Uiu2HAmAz4Vws3Y8Yop3d9B8htSzZEEBCJ5TFXSvG9K7x2wRb1M","Score":0.036458333333333315,"SharedSubnets":1,"Subnets":"00002000000000000000000000000000"},{"Peer":"16Uiu2HAm3jRReG6udtc8Vro5qACYKjKPFdNhm8b5zCsVWNdj5r9C","Score":0.036458333333333315,"SharedSubnets":1,"Subnets":"00002000000000000000000000000000"},{"Peer":"16Uiu2HAm6A6NoPJqneZz3VJdHp72nXjXFDWd9qWJ1aJGzymhL4hn","Score":0.036458333333333315,"SharedSubnets":1,"Subnets":"00002000000000000000000000000000"},{"Peer":"16Uiu2HAmRMTiJjeUXcuqmntkzR2z77Ve6Ju5FKWAHz7evn36zTrx","Score":0.036458333333333315,"SharedSubnets":1,"Subnets":"00002000000000000000000000000000"},{"Peer":"16Uiu2HAmUGRH5NxVxTBuFGjcJ6KzZzPB9ifKSFcjbSVJRJSmcxHc","Score":0.03776041666666666,"SharedSubnets":7,"Subnets":"10001000004800000002004000001000"},{"Peer":"16Uiu2HAmEeveoth116XcpuNH5P6Vzezd11RcKTAyvLjsKJmxGj59","Score":0.03906249999999997,"SharedSubnets":2,"Subnets":"00000200000000200000000000000000"},{"Peer":"16Uiu2HAmQ7xseZ8Axu4WN7kWnpz1f2zHMZNZ3cEiCh4psf9wSXo9","Score":0.039062499999999986,"SharedSubnets":1,"Subnets":"00000000000000000000000400000000"},{"Peer":"16Uiu2HAm3opMtmf3DBNJMiHvenciUeEMjiTr6gdxHXwQkmn48y3Q","Score":0.039062499999999986,"SharedSubnets":1,"Subnets":"08000000000000000000000000000000"},{"Peer":"16Uiu2HAmAwcCrVSYYop7wAK2jJaWwd1NmkrRDyjTfNuFHqWaa9j8","Score":0.0390625,"SharedSubnets":9,"Subnets":"00000004040480040000002000012100"},{"Peer":"16Uiu2HAm913ANPqMEo25S2UBmZb62kasFg3k88mowF69UP9R4EQX","Score":0.0403645833333333,"SharedSubnets":1,"Subnets":"00000000000008000000000000000000"},{"Peer":"16Uiu2HAmBDFQc1edFKAT6PkyvvbH4PtoEVpkLuPUhqD4mJ4cijSu","Score":0.0403645833333333,"SharedSubnets":1,"Subnets":"00000000000008000000000000000000"},{"Peer":"16Uiu2HAm5y8gBegyzX9GqBrq58Abm9t8zPSDzyprabnGFsxEUCQo","Score":0.0403645833333333,"SharedSubnets":1,"Subnets":"00000000000008000000000000000000"},{"Peer":"16Uiu2HAkwoZZuLHktUXts3zjaEZLYM5noSvqEwkvvvELUhLwTVyk","Score":0.040364583333333315,"SharedSubnets":2,"Subnets":"00080000000000000000000040000000"},{"Peer":"16Uiu2HAmBBqPcJQcM6BeMAQ4bAXMdA32Lhxkho1tWn9chCmrziHv","Score":0.04036458333333333,"SharedSubnets":1,"Subnets":"00000000000000000001000000000000"},{"Peer":"16Uiu2HAmELHczR4KGMBwYBv9ew426iYp1Js11zGZkAQFboXdtkLU","Score":0.04166666666666664,"SharedSubnets":2,"Subnets":"20000000000000000000000400000000"},{"Peer":"16Uiu2HAmCVXoohQ5zSGbzSHVLYwBmNb5uDFZ8LJ36XSpzUELXeYV","Score":0.04296874999999996,"SharedSubnets":3,"Subnets":"80000000000108000000000000000000"},{"Peer":"16Uiu2HAkzWnqFg9LJFcCmPd9fsQifqqRM4njWApECFay5sHukJ8H","Score":0.0442708333333333,"SharedSubnets":3,"Subnets":"08000000000010100000000000000000"},{"Peer":"16Uiu2HAm7XHmHAwj7BTWyFtpyPEHqFLYqtmTc9HqrdqHQu9dAMS2","Score":0.0442708333333333,"SharedSubnets":4,"Subnets":"00000210000000200000200000000000"},{"Peer":"16Uiu2HAkuZjGXchx93zPaY9pa5vYRAQhu8uwDa2z27jieeM2d3zw","Score":0.04687499999999995,"SharedSubnets":3,"Subnets":"00000100000800400000000000000000"},{"Peer":"16Uiu2HAm1UReWCXi4gUtYzF5YE85CUyNBhmUJWrVnAeUwF9ReUsG","Score":0.046874999999999965,"SharedSubnets":3,"Subnets":"02000000000000000020100000000000"},{"Peer":"16Uiu2HAm5u7aGYiR5b2t1EJGhh2ZwCLEXWGKsAXPnvuqK7AG8Xno","Score":0.057291666666666664,"SharedSubnets":1,"Subnets":"00000000000000000000000000000002"},{"Peer":"16Uiu2HAmFbJk2puSWWuNDuLUEUwPjPTdkyg4j11EUJYEgT4iowZq","Score":0.057291666666666664,"SharedSubnets":2,"Subnets":"00010000000000000000000000000002"},{"Peer":"16Uiu2HAkwUX4ZaWA5GPr384QCgPQms8A4SmowdCKjwNuqPJufa5H","Score":0.057291666666666664,"SharedSubnets":2,"Subnets":"00004000000000000000000004000000"},{"Peer":"16Uiu2HAm6JxaiyD8rY6YXUmaPQmYNf1nSxmn4qzByv44gaPgCHZa","Score":0.057291666666666664,"SharedSubnets":1,"Subnets":"00000000000100000000000000000000"},{"Peer":"16Uiu2HAmGa5eobff1pHVh5ez6XZGyt7bdaEvE2WbZHsNqV6pD2Pf","Score":0.05989583333333332,"SharedSubnets":1,"Subnets":"00000000000000000000000008000000"},{"Peer":"16Uiu2HAm3YEQeDtaYfDbT2jABtsAgjXRTseqkuKRR6g3bANAaifH","Score":0.05989583333333332,"SharedSubnets":1,"Subnets":"00000000000000000000000000000100"},{"Peer":"16Uiu2HAmLeXb3PW85PYZCr5JGms3CcaBET6TbKz4WLSqkeT7c3NV","Score":0.05989583333333332,"SharedSubnets":1,"Subnets":"00000000000000000000000000000200"},{"Peer":"16Uiu2HAm11JQtkxDUMy7ke6YKSRbKrf7w3dBT6YtoSPtpTQuRvvA","Score":0.05989583333333335,"SharedSubnets":1,"Subnets":"00000000100000000000000000000000"},{"Peer":"16Uiu2HAmV6X7pCXmT8FDta5KPju7eJWdbzUsG5MaeYJaGVWBi55H","Score":0.05989583333333335,"SharedSubnets":1,"Subnets":"00000000000002000000000000000000"},{"Peer":"16Uiu2HAkyWM3sLHyDfrqT9eerTVzKHXt3GsJ14Ukm17yCVQk6cpd","Score":0.05989583333333335,"SharedSubnets":1,"Subnets":"00000000000000000000400000000000"},{"Peer":"16Uiu2HAkwamxFptQ8cWpWgtXbXAcsaCeBccgcDDpK242QhwPmmA5","Score":0.05989583333333335,"SharedSubnets":1,"Subnets":"00000000040000000000000000000000"},{"Peer":"16Uiu2HAm8WhQjnCi2dz3gY19gUFxEP3FmiXjjS69qJn7Fa6NESRe","Score":0.06250000000000001,"SharedSubnets":1,"Subnets":"00020000000000000000000000000000"},{"Peer":"16Uiu2HAmPRV6ThVp4j8PYBTmFVLwBudGKqHRPVGtvHgyK6wAvRaP","Score":0.06250000000000001,"SharedSubnets":1,"Subnets":"00000000000400000000000000000000"},{"Peer":"16Uiu2HAmPXA2jN2JBEmARutrCSmQWP4QkcUpgiuMqJqxFyNhnqQ3","Score":0.06510416666666669,"SharedSubnets":2,"Subnets":"00000000000000000000081000000000"}],"subnets_connections":[5,7,5,10,5,7,4,7,8,6,8,9,5,5,5,4,5,10,5,10,8,12,8,8,8,9,9,9,6,7,9,7,6,9,7,7,7,6,7,8,8,5,6,7,7,7,9,9,6,7,7,9,8,6,6,9,5,9,8,9,6,8,12,5,8,8,8,8,7,7,5,8,9,7,4,5,7,10,9,8,9,5,8,6,6,8,7,5,9,7,10,5,7,7,7,6,7,9,8,7,9,11,8,7,8,5,5,9,5,8,6,6,7,7,6,7,10,7,8,6,8,8,6,6,5,5,5,7]}`
var ssv_3_15_43 = `{"my_subnets":"ffffffffffffffffffffffffffffffff","peers":[{"Peer":"16Uiu2HAm3jRReG6udtc8Vro5qACYKjKPFdNhm8b5zCsVWNdj5r9C","Score":-0.38216145833333315,"SharedSubnets":1,"Subnets":"00002000000000000000000000000000"},{"Peer":"16Uiu2HAm6A6NoPJqneZz3VJdHp72nXjXFDWd9qWJ1aJGzymhL4hn","Score":-0.38216145833333315,"SharedSubnets":1,"Subnets":"00002000000000000000000000000000"},{"Peer":"16Uiu2HAmRMTiJjeUXcuqmntkzR2z77Ve6Ju5FKWAHz7evn36zTrx","Score":-0.38216145833333315,"SharedSubnets":1,"Subnets":"00002000000000000000000000000000"},{"Peer":"16Uiu2HAm5DNoR33xk885aU6SbNTksjUD4WTxagPTfEnFDw9fz33b","Score":-0.38216145833333315,"SharedSubnets":1,"Subnets":"00002000000000000000000000000000"},{"Peer":"16Uiu2HAmAz4Vws3Y8Yop3d9B8htSzZEEBCJ5TFXSvG9K7x2wRb1M","Score":-0.38216145833333315,"SharedSubnets":1,"Subnets":"00002000000000000000000000000000"},{"Peer":"16Uiu2HAmAcNyd6VaA1JDCjZ5QdWFdEWnnKBGbAYUWvckqApt8nr1","Score":-0.37304687499999994,"SharedSubnets":5,"Subnets":"00000000000000020200200010001000"},{"Peer":"16Uiu2HAm41iFvN8CbnRnj4iNQJ3sLoWUEWBVDjQxshmQ6nHujPTG","Score":-0.37044270833333326,"SharedSubnets":5,"Subnets":"00000000010000000200200010001000"},{"Peer":"16Uiu2HAkwW7i8B72U54LGWeruwHzZLwuUU66oY2SPFD4fpxfBvmJ","Score":-0.3678385416666667,"SharedSubnets":5,"Subnets":"00004000400000000040004010000000"},{"Peer":"16Uiu2HAm7EgFz3Zsg9Hk6HzbCPK9u97cVmFUoHzqnHkP4Zbrc3yj","Score":-0.36523437499999994,"SharedSubnets":6,"Subnets":"00000200000000000100000410084000"},{"Peer":"16Uiu2HAm6JxaiyD8rY6YXUmaPQmYNf1nSxmn4qzByv44gaPgCHZa","Score":-0.3613281249999999,"SharedSubnets":1,"Subnets":"00000000000100000000000000000000"},{"Peer":"16Uiu2HAm3opMtmf3DBNJMiHvenciUeEMjiTr6gdxHXwQkmn48y3Q","Score":-0.3613281249999999,"SharedSubnets":1,"Subnets":"08000000000000000000000000000000"},{"Peer":"16Uiu2HAmBDFQc1edFKAT6PkyvvbH4PtoEVpkLuPUhqD4mJ4cijSu","Score":-0.3613281249999999,"SharedSubnets":1,"Subnets":"00000000000008000000000000000000"},{"Peer":"16Uiu2HAm5y8gBegyzX9GqBrq58Abm9t8zPSDzyprabnGFsxEUCQo","Score":-0.3613281249999999,"SharedSubnets":1,"Subnets":"00000000000008000000000000000000"},{"Peer":"16Uiu2HAmQ7xseZ8Axu4WN7kWnpz1f2zHMZNZ3cEiCh4psf9wSXo9","Score":-0.3613281249999999,"SharedSubnets":1,"Subnets":"00000000000000000000000400000000"},{"Peer":"16Uiu2HAm913ANPqMEo25S2UBmZb62kasFg3k88mowF69UP9R4EQX","Score":-0.3613281249999999,"SharedSubnets":1,"Subnets":"00000000000008000000000000000000"},{"Peer":"16Uiu2HAmBBqPcJQcM6BeMAQ4bAXMdA32Lhxkho1tWn9chCmrziHv","Score":-0.3613281249999999,"SharedSubnets":1,"Subnets":"00000000000000000001000000000000"},{"Peer":"16Uiu2HAm5u7aGYiR5b2t1EJGhh2ZwCLEXWGKsAXPnvuqK7AG8Xno","Score":-0.35872395833333326,"SharedSubnets":1,"Subnets":"00000000000000000000000000000002"},{"Peer":"16Uiu2HAm11JQtkxDUMy7ke6YKSRbKrf7w3dBT6YtoSPtpTQuRvvA","Score":-0.35872395833333326,"SharedSubnets":1,"Subnets":"00000000100000000000000000000000"},{"Peer":"16Uiu2HAmLeXb3PW85PYZCr5JGms3CcaBET6TbKz4WLSqkeT7c3NV","Score":-0.35872395833333326,"SharedSubnets":1,"Subnets":"00000000000000000000000000000200"},{"Peer":"16Uiu2HAm6CWyfwBgYKQAcqBRpah1BhwRcf3EiY4YjFZtokEuCMpV","Score":-0.35872395833333326,"SharedSubnets":1,"Subnets":"00000001000000000000000000000000"},{"Peer":"16Uiu2HAmELHczR4KGMBwYBv9ew426iYp1Js11zGZkAQFboXdtkLU","Score":-0.3561197916666666,"SharedSubnets":2,"Subnets":"20000000000000000000000400000000"},{"Peer":"16Uiu2HAmEeveoth116XcpuNH5P6Vzezd11RcKTAyvLjsKJmxGj59","Score":-0.3561197916666666,"SharedSubnets":2,"Subnets":"00000200000000200000000000000000"},{"Peer":"16Uiu2HAmCVXoohQ5zSGbzSHVLYwBmNb5uDFZ8LJ36XSpzUELXeYV","Score":-0.3561197916666666,"SharedSubnets":3,"Subnets":"80000000000108000000000000000000"},{"Peer":"16Uiu2HAkwUX4ZaWA5GPr384QCgPQms8A4SmowdCKjwNuqPJufa5H","Score":-0.3561197916666666,"SharedSubnets":2,"Subnets":"00004000000000000000000004000000"},{"Peer":"16Uiu2HAkwamxFptQ8cWpWgtXbXAcsaCeBccgcDDpK242QhwPmmA5","Score":-0.3561197916666666,"SharedSubnets":1,"Subnets":"00000000040000000000000000000000"},{"Peer":"16Uiu2HAmV6X7pCXmT8FDta5KPju7eJWdbzUsG5MaeYJaGVWBi55H","Score":-0.3561197916666666,"SharedSubnets":1,"Subnets":"00000000000002000000000000000000"},{"Peer":"16Uiu2HAmFbJk2puSWWuNDuLUEUwPjPTdkyg4j11EUJYEgT4iowZq","Score":-0.35351562499999994,"SharedSubnets":2,"Subnets":"00010000000000000000000000000002"},{"Peer":"16Uiu2HAkyfXMGd18iGWQ8DDL8Vowx5XV1wxNzAtF8Yok5MWfo84y","Score":-0.35351562499999994,"SharedSubnets":3,"Subnets":"00010000000000000020000000080000"},{"Peer":"16Uiu2HAm8WhQjnCi2dz3gY19gUFxEP3FmiXjjS69qJn7Fa6NESRe","Score":-0.35351562499999994,"SharedSubnets":1,"Subnets":"00020000000000000000000000000000"},{"Peer":"16Uiu2HAkyWM3sLHyDfrqT9eerTVzKHXt3GsJ14Ukm17yCVQk6cpd","Score":-0.35351562499999994,"SharedSubnets":1,"Subnets":"00000000000000000000400000000000"},{"Peer":"16Uiu2HAmPRV6ThVp4j8PYBTmFVLwBudGKqHRPVGtvHgyK6wAvRaP","Score":-0.35351562499999994,"SharedSubnets":1,"Subnets":"00000000000400000000000000000000"},{"Peer":"16Uiu2HAm3YEQeDtaYfDbT2jABtsAgjXRTseqkuKRR6g3bANAaifH","Score":-0.3535156249999999,"SharedSubnets":1,"Subnets":"00000000000000000000000000000100"},{"Peer":"16Uiu2HAm2e9cXq7HEUSGcZpbETwjhxdkFjvoiufR6KaZMcXUHdFg","Score":-0.35091145833333326,"SharedSubnets":4,"Subnets":"00040080008000000000000020000000"},{"Peer":"16Uiu2HAmGa5eobff1pHVh5ez6XZGyt7bdaEvE2WbZHsNqV6pD2Pf","Score":-0.35091145833333326,"SharedSubnets":1,"Subnets":"00000000000000000000000008000000"},{"Peer":"16Uiu2HAkwoZZuLHktUXts3zjaEZLYM5noSvqEwkvvvELUhLwTVyk","Score":-0.3509114583333332,"SharedSubnets":2,"Subnets":"00080000000000000000000040000000"},{"Peer":"16Uiu2HAmQ63Khb7FnvhPY7EXXuT19Ccytzhf1gZxMr5LvwqLjFqP","Score":-0.3509114583333332,"SharedSubnets":5,"Subnets":"00000022000000000040000080000008"},{"Peer":"16Uiu2HAkzWnqFg9LJFcCmPd9fsQifqqRM4njWApECFay5sHukJ8H","Score":-0.3509114583333332,"SharedSubnets":3,"Subnets":"08000000000010100000000000000000"},{"Peer":"16Uiu2HAmKTSWRiRSUJetUSpArWfXiXve54YNwVC6VYzYi4KnhBse","Score":-0.3509114583333332,"SharedSubnets":5,"Subnets":"00000022000000000040000080000008"},{"Peer":"16Uiu2HAm3BtFC7hzip7czpo6p15iTUPZ2T9KAf1ECue1akDPmewL","Score":-0.34830729166666663,"SharedSubnets":4,"Subnets":"00004002000080000010000000000000"},{"Peer":"16Uiu2HAm1UReWCXi4gUtYzF5YE85CUyNBhmUJWrVnAeUwF9ReUsG","Score":-0.3483072916666666,"SharedSubnets":3,"Subnets":"02000000000000000020100000000000"},{"Peer":"16Uiu2HAmV3SQqKrjVxNgXVswcZuzNN6D6zYPZqRiXH5pJqagScz2","Score":-0.3483072916666666,"SharedSubnets":9,"Subnets":"05042004480004000000000080000000"},{"Peer":"16Uiu2HAm7XHmHAwj7BTWyFtpyPEHqFLYqtmTc9HqrdqHQu9dAMS2","Score":-0.345703125,"SharedSubnets":4,"Subnets":"00000210000000200000200000000000"},{"Peer":"16Uiu2HAmPXA2jN2JBEmARutrCSmQWP4QkcUpgiuMqJqxFyNhnqQ3","Score":-0.34570312499999994,"SharedSubnets":2,"Subnets":"00000000000000000000081000000000"},{"Peer":"16Uiu2HAmLsDqf3oGdqCU37TyAo3yRtEju7jfz8LuYMwtyzMcA2yL","Score":-0.34570312499999983,"SharedSubnets":4,"Subnets":"00000000004010000000008002000000"},{"Peer":"16Uiu2HAkuZjGXchx93zPaY9pa5vYRAQhu8uwDa2z27jieeM2d3zw","Score":-0.34309895833333326,"SharedSubnets":3,"Subnets":"00000100000800400000000000000000"},{"Peer":"16Uiu2HAm87sWQcCPTCPLr1xZ3TvyWqBAHRRCWKkrniqhrgeu9gbJ","Score":-0.34049479166666663,"SharedSubnets":9,"Subnets":"08000004800000420020010020000002"},{"Peer":"16Uiu2HAmJa7gNYQCumkMAxWZT5kb99VJfPdM99ZZacv8kbqKzVk9","Score":-0.34049479166666663,"SharedSubnets":6,"Subnets":"02000208800000000020100000000000"},{"Peer":"16Uiu2HAkxLGN74DMAyCQXqtoZAXFfYnMJJjudwF9kcU6GaHdRE4i","Score":-0.3404947916666666,"SharedSubnets":5,"Subnets":"00000041000000008040000000400000"},{"Peer":"16Uiu2HAmUGRH5NxVxTBuFGjcJ6KzZzPB9ifKSFcjbSVJRJSmcxHc","Score":-0.33528645833333326,"SharedSubnets":7,"Subnets":"10001000004800000002004000001000"},{"Peer":"16Uiu2HAmFQdGq8XHndxZo6VvUicBjvikarjVzdVFAbSnQ8GuJNxF","Score":-0.32747395833333337,"SharedSubnets":9,"Subnets":"00048802880000000001800000000200"},{"Peer":"16Uiu2HAmFhNizAZxBjKfry2vYxyidPtPH6QVZc6YVLGd76yxYYFi","Score":-0.32486979166666663,"SharedSubnets":9,"Subnets":"80000021000020080800000100280000"},{"Peer":"16Uiu2HAmCbi6DTk49BEakGLi9LysHuLkkjAas1mDMrtJhL9VoA6K","Score":-0.3196614583333334,"SharedSubnets":9,"Subnets":"00010808009001040000000020000002"},{"Peer":"16Uiu2HAmAwcCrVSYYop7wAK2jJaWwd1NmkrRDyjTfNuFHqWaa9j8","Score":-0.3092447916666667,"SharedSubnets":9,"Subnets":"00000004040480040000002000012100"},{"Peer":"16Uiu2HAm2Z1eWNeNmaTeMHDPB4WB9HaHUHXzerrqei9SijQmJXXo","Score":-0.2753906250000001,"SharedSubnets":17,"Subnets":"00401000604004480480010284a00001"},{"Peer":"16Uiu2HAm2mL8PBS9xQEzV53jFi7eu41NophErcUKkfYzSs38xFsa","Score":-0.2688802083333333,"SharedSubnets":36,"Subnets":"441830809141120ec633244114041680"},{"Peer":"16Uiu2HAmEe8GnSuE2syDCKqZ8EjuPSSGoC7Z3p3KXYNoM6ZEawYT","Score":-0.25195312500000017,"SharedSubnets":30,"Subnets":"2808a84012a10000a901041527114081"},{"Peer":"16Uiu2HAkx6vuo9vAV85uRvxscecXWN7SNerAi6ETERGtDwBatW5j","Score":0.14257812500000003,"SharedSubnets":101,"Subnets":"ab3ef6ef7fe9ffffbf9b6eeff3effefc"},{"Peer":"16Uiu2HAm4uu7WFUDdLwTkYYsZc3rqE2cvQBVZsfgkdPkFr5Bct9o","Score":0.3613281249999999,"SharedSubnets":128,"Subnets":"ffffffffffffffffffffffffffffffff"},{"Peer":"16Uiu2HAmUHiQMemRv1AjyLid17Z3Rvbe9LekqjoWaVJyL3drmKh8","Score":0.3613281249999999,"SharedSubnets":128,"Subnets":"ffffffffffffffffffffffffffffffff"},{"Peer":"16Uiu2HAmMorRmGosQJzh3DUtiKF9BBspCWA6KQ3EPKWsUQMCmU2x","Score":0.3613281249999999,"SharedSubnets":128,"Subnets":"ffffffffffffffffffffffffffffffff"}],"subnets_connections":[5,6,5,8,4,6,4,6,6,5,7,7,5,4,4,3,4,8,4,6,7,12,7,6,7,8,7,6,4,7,6,6,6,5,6,6,7,5,7,7,8,3,5,6,4,5,8,7,5,6,6,8,7,5,4,6,4,7,7,7,5,6,7,4,6,7,6,6,4,5,4,7,8,6,3,4,6,8,7,5,5,4,6,5,5,8,5,4,7,5,8,4,5,5,7,5,5,6,7,4,9,8,5,8,6,4,5,7,4,6,5,5,5,7,5,4,8,5,6,4,5,7,4,6,4,4,4,6]}`
var ssv_3_16_13 = `{"my_subnets":"ffffffffffffffffffffffffffffffff","peers":[{"Peer":"16Uiu2HAm3opMtmf3DBNJMiHvenciUeEMjiTr6gdxHXwQkmn48y3Q","Score":-0.23697916666666669,"SharedSubnets":1,"Subnets":"08000000000000000000000000000000"},{"Peer":"16Uiu2HAm3jRReG6udtc8Vro5qACYKjKPFdNhm8b5zCsVWNdj5r9C","Score":-0.23697916666666669,"SharedSubnets":1,"Subnets":"00002000000000000000000000000000"},{"Peer":"16Uiu2HAmVnDy2D4CHxrDKQtdHHh3kvsyuoXYnNQqgm1AQCsXhz3k","Score":-0.23697916666666669,"SharedSubnets":1,"Subnets":"00002000000000000000000000000000"},{"Peer":"16Uiu2HAm7xH8hhmxsF4j3Bz6G7cFNS1kP5x2BA5zsXB9wz7QJ2ge","Score":-0.23567708333333337,"SharedSubnets":2,"Subnets":"00000200000010000000000000000000"},{"Peer":"16Uiu2HAm1XscCE7r3wR24ernj1qZJQeJ7SvhDNPXNeAG2NDfeCP6","Score":-0.23567708333333337,"SharedSubnets":8,"Subnets":"00042000090080002000000004008000"},{"Peer":"16Uiu2HAm3BtFC7hzip7czpo6p15iTUPZ2T9KAf1ECue1akDPmewL","Score":-0.23567708333333334,"SharedSubnets":4,"Subnets":"00004002000080000010000000000000"},{"Peer":"16Uiu2HAmBBqPcJQcM6BeMAQ4bAXMdA32Lhxkho1tWn9chCmrziHv","Score":-0.23567708333333331,"SharedSubnets":1,"Subnets":"00000000000000000001000000000000"},{"Peer":"16Uiu2HAmQ63Khb7FnvhPY7EXXuT19Ccytzhf1gZxMr5LvwqLjFqP","Score":-0.23437500000000003,"SharedSubnets":5,"Subnets":"00000022000000000040000080000008"},{"Peer":"16Uiu2HAmHkxX6KiF7Q4agUSEQmzQMC1eDDt9DPdVFuEPNMPKfsEU","Score":-0.23437500000000003,"SharedSubnets":5,"Subnets":"00000022000000000040000080000008"},{"Peer":"16Uiu2HAkwW7i8B72U54LGWeruwHzZLwuUU66oY2SPFD4fpxfBvmJ","Score":-0.23437500000000003,"SharedSubnets":5,"Subnets":"00004000400000000040004010000000"},{"Peer":"16Uiu2HAmKD3qvyAb4moxhMiFkNmKHHni6aNdKbH8G7WT5mdaHM9Y","Score":-0.23437500000000003,"SharedSubnets":5,"Subnets":"00004000400000000040004010000000"},{"Peer":"16Uiu2HAmQ7xseZ8Axu4WN7kWnpz1f2zHMZNZ3cEiCh4psf9wSXo9","Score":-0.234375,"SharedSubnets":1,"Subnets":"00000000000000000000000400000000"},{"Peer":"16Uiu2HAm11JQtkxDUMy7ke6YKSRbKrf7w3dBT6YtoSPtpTQuRvvA","Score":-0.234375,"SharedSubnets":1,"Subnets":"00000000100000000000000000000000"},{"Peer":"16Uiu2HAm323QWZ5LM8reJx6MBuTiVvGvwCWWL3dRKWrMMuxHrqS4","Score":-0.234375,"SharedSubnets":0,"Subnets":"00000000000000000000000000000000"},{"Peer":"16Uiu2HAmHLrAJr75TukUswnvvfkzpFiNE8yoB9Vw9E6v6869J7y8","Score":-0.23307291666666669,"SharedSubnets":9,"Subnets":"08000004000000420020010020001800"},{"Peer":"16Uiu2HAkyfXMGd18iGWQ8DDL8Vowx5XV1wxNzAtF8Yok5MWfo84y","Score":-0.23307291666666666,"SharedSubnets":3,"Subnets":"00010000000000000020000000080000"},{"Peer":"16Uiu2HAmVHni9pncdYrbRgAar9NiRgEa4SpbHLmHU5wMaNDedWkc","Score":-0.23177083333333337,"SharedSubnets":5,"Subnets":"08000000000000000008000008008001"},{"Peer":"16Uiu2HAmBDFQc1edFKAT6PkyvvbH4PtoEVpkLuPUhqD4mJ4cijSu","Score":-0.23177083333333334,"SharedSubnets":1,"Subnets":"00000000000008000000000000000000"},{"Peer":"16Uiu2HAm876jaEMj5MNDQdftgUxyQwuuArwpVFrnveqXZCxbMprd","Score":-0.23177083333333334,"SharedSubnets":1,"Subnets":"00000000000008000000000000000000"},{"Peer":"16Uiu2HAmGa5eobff1pHVh5ez6XZGyt7bdaEvE2WbZHsNqV6pD2Pf","Score":-0.23177083333333331,"SharedSubnets":1,"Subnets":"00000000000000000000000008000000"},{"Peer":"16Uiu2HAmFhNizAZxBjKfry2vYxyidPtPH6QVZc6YVLGd76yxYYFi","Score":-0.23177083333333331,"SharedSubnets":9,"Subnets":"80000021000020080800000100280000"},{"Peer":"16Uiu2HAmQkYATgANrpdDyZ5SgGhxEraiWqGt1TBC4aJJovUw39SD","Score":-0.23177083333333331,"SharedSubnets":13,"Subnets":"80048802880080400001800000100200"},{"Peer":"16Uiu2HAkxxNv28fAnAtAUf2sqdYP64h8MmD3jc1fh8qTQug8C5Wr","Score":-0.23177083333333331,"SharedSubnets":1,"Subnets":"00000000000000000000000000100000"},{"Peer":"16Uiu2HAkzWnqFg9LJFcCmPd9fsQifqqRM4njWApECFay5sHukJ8H","Score":-0.23046875000000003,"SharedSubnets":3,"Subnets":"08000000000010100000000000000000"},{"Peer":"16Uiu2HAmG8SvC3HBtuwnTszcgpySMSKVxMEm4CSn7Acr62eczLFi","Score":-0.23046874999999997,"SharedSubnets":4,"Subnets":"00200000000000000000006000000400"},{"Peer":"16Uiu2HAm7EgFz3Zsg9Hk6HzbCPK9u97cVmFUoHzqnHkP4Zbrc3yj","Score":-0.23046874999999997,"SharedSubnets":6,"Subnets":"00000200000000000100000410084000"},{"Peer":"16Uiu2HAmLsDqf3oGdqCU37TyAo3yRtEju7jfz8LuYMwtyzMcA2yL","Score":-0.2291666666666667,"SharedSubnets":4,"Subnets":"00000000004010000000008002000000"},{"Peer":"16Uiu2HAmKtPJc3kokeWuRosHKHdJMTT2odi3Mcz4JzryAhaTFvkU","Score":-0.2291666666666667,"SharedSubnets":3,"Subnets":"80000000000000080000020000000000"},{"Peer":"16Uiu2HAm41iFvN8CbnRnj4iNQJ3sLoWUEWBVDjQxshmQ6nHujPTG","Score":-0.22916666666666669,"SharedSubnets":5,"Subnets":"00000000010000000200200010001000"},{"Peer":"16Uiu2HAmV6X7pCXmT8FDta5KPju7eJWdbzUsG5MaeYJaGVWBi55H","Score":-0.22916666666666669,"SharedSubnets":1,"Subnets":"00000000000002000000000000000000"},{"Peer":"16Uiu2HAmPRV6ThVp4j8PYBTmFVLwBudGKqHRPVGtvHgyK6wAvRaP","Score":-0.22916666666666669,"SharedSubnets":1,"Subnets":"00000000000400000000000000000000"},{"Peer":"16Uiu2HAkwamxFptQ8cWpWgtXbXAcsaCeBccgcDDpK242QhwPmmA5","Score":-0.22916666666666669,"SharedSubnets":1,"Subnets":"00000000040000000000000000000000"},{"Peer":"16Uiu2HAm5u7aGYiR5b2t1EJGhh2ZwCLEXWGKsAXPnvuqK7AG8Xno","Score":-0.22916666666666669,"SharedSubnets":1,"Subnets":"00000000000000000000000000000002"},{"Peer":"16Uiu2HAkyWM3sLHyDfrqT9eerTVzKHXt3GsJ14Ukm17yCVQk6cpd","Score":-0.22916666666666669,"SharedSubnets":1,"Subnets":"00000000000000000000400000000000"},{"Peer":"16Uiu2HAkvupTFCYj7v22niTAwsFQYFVkYUSNsTzPmk79dnYcsdvf","Score":-0.22916666666666669,"SharedSubnets":1,"Subnets":"00000000000000000000000000000040"},{"Peer":"16Uiu2HAmEeveoth116XcpuNH5P6Vzezd11RcKTAyvLjsKJmxGj59","Score":-0.22916666666666669,"SharedSubnets":2,"Subnets":"00000200000000200000000000000000"},{"Peer":"16Uiu2HAm8WhQjnCi2dz3gY19gUFxEP3FmiXjjS69qJn7Fa6NESRe","Score":-0.22916666666666669,"SharedSubnets":1,"Subnets":"00020000000000000000000000000000"},{"Peer":"16Uiu2HAmKSKKHNcUhRQF4dK3bA8aPtM5SYEDEPt9gdbkdxwJYFB2","Score":-0.22916666666666666,"SharedSubnets":5,"Subnets":"00000000000000020000002000013000"},{"Peer":"16Uiu2HAmFQdGq8XHndxZo6VvUicBjvikarjVzdVFAbSnQ8GuJNxF","Score":-0.22916666666666663,"SharedSubnets":9,"Subnets":"00048802880000000001800000000200"},{"Peer":"16Uiu2HAmCVXoohQ5zSGbzSHVLYwBmNb5uDFZ8LJ36XSpzUELXeYV","Score":-0.22786458333333337,"SharedSubnets":3,"Subnets":"80000000000108000000000000000000"},{"Peer":"16Uiu2HAmJBiGQHDoUB57GQqU17hWtKznYbYiMezLuPRwfNoRXK2y","Score":-0.22786458333333334,"SharedSubnets":5,"Subnets":"00000041000000008040000000400000"},{"Peer":"16Uiu2HAmUGRH5NxVxTBuFGjcJ6KzZzPB9ifKSFcjbSVJRJSmcxHc","Score":-0.22656250000000006,"SharedSubnets":7,"Subnets":"10001000004800000002004000001000"},{"Peer":"16Uiu2HAmFbJk2puSWWuNDuLUEUwPjPTdkyg4j11EUJYEgT4iowZq","Score":-0.22656250000000003,"SharedSubnets":2,"Subnets":"00010000000000000000000000000002"},{"Peer":"16Uiu2HAm7XHmHAwj7BTWyFtpyPEHqFLYqtmTc9HqrdqHQu9dAMS2","Score":-0.22395833333333334,"SharedSubnets":4,"Subnets":"00000210000000200000200000000000"},{"Peer":"16Uiu2HAm1UReWCXi4gUtYzF5YE85CUyNBhmUJWrVnAeUwF9ReUsG","Score":-0.22135416666666669,"SharedSubnets":3,"Subnets":"02000000000000000020100000000000"},{"Peer":"16Uiu2HAm6FPFWfYkZvu51GzSoMtbx8sg4r7EEezv1KgKCy1FmXzh","Score":-0.22135416666666666,"SharedSubnets":18,"Subnets":"80000021090820480800000124280610"},{"Peer":"16Uiu2HAkuicjxoqeDDUrsdiY4E3ceqprCgK5iexhFSo7f2gSKXBt","Score":-0.22005208333333343,"SharedSubnets":14,"Subnets":"002400442040100400000000c6800800"},{"Peer":"16Uiu2HAm8dg33RBB7hQpMXVeUNjNT2TaLDWhmWg4DoQMounPdoVu","Score":-0.21875000000000003,"SharedSubnets":4,"Subnets":"00000010000080800004000000000000"},{"Peer":"16Uiu2HAmKPZnTQL89qebAkZkJNm4PrW8mmC3LteUgDhhcHJsiPEh","Score":-0.21484375,"SharedSubnets":8,"Subnets":"10201000000000001040800000002040"},{"Peer":"16Uiu2HAmAwcCrVSYYop7wAK2jJaWwd1NmkrRDyjTfNuFHqWaa9j8","Score":-0.21093749999999997,"SharedSubnets":9,"Subnets":"00000004040480040000002000012100"},{"Peer":"16Uiu2HAmFA7dF56C3Ca3Tqimyq5fyDwfvimKrfdgDQeWG7z2L3TZ","Score":-0.2044270833333334,"SharedSubnets":16,"Subnets":"08028000100801020018014800409001"},{"Peer":"16Uiu2HAm2Z1eWNeNmaTeMHDPB4WB9HaHUHXzerrqei9SijQmJXXo","Score":-0.1979166666666667,"SharedSubnets":17,"Subnets":"00401000604004480480010284a00001"},{"Peer":"16Uiu2HAmQCWth9HD1pnu8w3jZ2y77FxLPDhgAud37zUGhhN7y641","Score":-0.18749999999999997,"SharedSubnets":18,"Subnets":"12000504b04220000210200204000420"},{"Peer":"16Uiu2HAmBzsKGKSDCxbnthySpbFw6LUwvfH77rQ1mkXZHoS2ozzR","Score":-0.18098958333333337,"SharedSubnets":27,"Subnets":"040120180010124c16f0400348009401"},{"Peer":"16Uiu2HAkwt9SuuNXKHjDCLvcX5LNVEvsBeLGUCtAKccDFuCrCqTC","Score":-0.17968750000000003,"SharedSubnets":29,"Subnets":"0008284002a280008903041527094885"},{"Peer":"16Uiu2HAmEe8GnSuE2syDCKqZ8EjuPSSGoC7Z3p3KXYNoM6ZEawYT","Score":-0.1783854166666667,"SharedSubnets":30,"Subnets":"2808a84012a10000a901041527114081"},{"Peer":"16Uiu2HAmMorRmGosQJzh3DUtiKF9BBspCWA6KQ3EPKWsUQMCmU2x","Score":0.234375,"SharedSubnets":128,"Subnets":"ffffffffffffffffffffffffffffffff"},{"Peer":"16Uiu2HAmUHiQMemRv1AjyLid17Z3Rvbe9LekqjoWaVJyL3drmKh8","Score":0.234375,"SharedSubnets":128,"Subnets":"ffffffffffffffffffffffffffffffff"},{"Peer":"16Uiu2HAm7TxnEk1GgbRfQqviWwtE3HhofSMemA8Db4RAXbTcnhhU","Score":0.234375,"SharedSubnets":128,"Subnets":"ffffffffffffffffffffffffffffffff"},{"Peer":"16Uiu2HAmLNPY62BphZRxmnbw74kvvi4At7TCCd3yse8U5foYRd1X","Score":0.234375,"SharedSubnets":128,"Subnets":"ffffffffffffffffffffffffffffffff"}],"subnets_connections":[4,6,5,10,7,5,4,9,7,6,8,6,4,7,5,4,5,8,5,8,7,10,7,8,7,9,8,5,7,8,8,4,7,6,6,8,8,7,7,7,6,6,6,7,5,6,9,6,5,6,5,7,9,7,4,10,4,7,7,9,5,6,9,5,7,7,6,8,6,6,4,7,9,6,5,6,8,8,11,6,7,5,6,4,5,7,6,7,9,7,8,5,6,7,9,5,6,8,11,7,8,8,6,8,8,4,4,9,7,7,6,6,5,7,8,7,10,7,7,8,10,6,5,6,5,5,6,6]}`
var ssv_2_18_30 = `{"my_subnets":"ffffffffffffffffffffffffffffffff","peers":[{"Peer":"16Uiu2HAmFQdGq8XHndxZo6VvUicBjvikarjVzdVFAbSnQ8GuJNxF","Score":-0.16536458333333331,"SharedSubnets":9,"Subnets":"00048802880000000001800000000200"},{"Peer":"16Uiu2HAmCaJ1E7Pd2mAKHGyDqXinbNFqcjrw4zbwakJmdKWbY9xY","Score":-0.1653645833333333,"SharedSubnets":2,"Subnets":"00000040000010000000000000000000"},{"Peer":"16Uiu2HAkwUX4ZaWA5GPr384QCgPQms8A4SmowdCKjwNuqPJufa5H","Score":-0.1653645833333333,"SharedSubnets":2,"Subnets":"00004000000000000000000004000000"},{"Peer":"16Uiu2HAm913ANPqMEo25S2UBmZb62kasFg3k88mowF69UP9R4EQX","Score":-0.1653645833333333,"SharedSubnets":1,"Subnets":"00000000000008000000000000000000"},{"Peer":"16Uiu2HAm876jaEMj5MNDQdftgUxyQwuuArwpVFrnveqXZCxbMprd","Score":-0.1653645833333333,"SharedSubnets":1,"Subnets":"00000000000008000000000000000000"},{"Peer":"16Uiu2HAmVHni9pncdYrbRgAar9NiRgEa4SpbHLmHU5wMaNDedWkc","Score":-0.1653645833333333,"SharedSubnets":5,"Subnets":"08000000000000000008000008008001"},{"Peer":"16Uiu2HAm5y8gBegyzX9GqBrq58Abm9t8zPSDzyprabnGFsxEUCQo","Score":-0.1653645833333333,"SharedSubnets":1,"Subnets":"00000000000008000000000000000000"},{"Peer":"16Uiu2HAmEeveoth116XcpuNH5P6Vzezd11RcKTAyvLjsKJmxGj59","Score":-0.16536458333333326,"SharedSubnets":2,"Subnets":"00000200000000200000000000000000"},{"Peer":"16Uiu2HAmLCNsY7fqsrzxsThRRABf7a9fscMELBbhXaivvVC99Wjw","Score":-0.1640625,"SharedSubnets":8,"Subnets":"00004202800000000011000000022000"},{"Peer":"16Uiu2HAm2e9cXq7HEUSGcZpbETwjhxdkFjvoiufR6KaZMcXUHdFg","Score":-0.16406249999999997,"SharedSubnets":4,"Subnets":"00040080008000000000000020000000"},{"Peer":"16Uiu2HAmKD3qvyAb4moxhMiFkNmKHHni6aNdKbH8G7WT5mdaHM9Y","Score":-0.16406249999999994,"SharedSubnets":5,"Subnets":"00004000400000000040004010000000"},{"Peer":"16Uiu2HAm5rC4Y1LzvNM2YRiUyfNptCrsMiAEdzF1XvA3q77gd48K","Score":-0.16406249999999994,"SharedSubnets":3,"Subnets":"00000000000000000000800200000002"},{"Peer":"16Uiu2HAmJa7gNYQCumkMAxWZT5kb99VJfPdM99ZZacv8kbqKzVk9","Score":-0.16406249999999994,"SharedSubnets":6,"Subnets":"02000208800000000020100000000000"},{"Peer":"16Uiu2HAm5u7aGYiR5b2t1EJGhh2ZwCLEXWGKsAXPnvuqK7AG8Xno","Score":-0.16406249999999994,"SharedSubnets":1,"Subnets":"00000000000000000000000000000002"},{"Peer":"16Uiu2HAmQ7xseZ8Axu4WN7kWnpz1f2zHMZNZ3cEiCh4psf9wSXo9","Score":-0.16406249999999994,"SharedSubnets":1,"Subnets":"00000000000000000000000400000000"},{"Peer":"16Uiu2HAmGa5eobff1pHVh5ez6XZGyt7bdaEvE2WbZHsNqV6pD2Pf","Score":-0.16406249999999994,"SharedSubnets":1,"Subnets":"00000000000000000000000008000000"},{"Peer":"16Uiu2HAm11JQtkxDUMy7ke6YKSRbKrf7w3dBT6YtoSPtpTQuRvvA","Score":-0.16406249999999994,"SharedSubnets":1,"Subnets":"00000000100000000000000000000000"},{"Peer":"16Uiu2HAmKSKKHNcUhRQF4dK3bA8aPtM5SYEDEPt9gdbkdxwJYFB2","Score":-0.16406249999999992,"SharedSubnets":5,"Subnets":"00000000000000020000002000013000"},{"Peer":"16Uiu2HAmCVXoohQ5zSGbzSHVLYwBmNb5uDFZ8LJ36XSpzUELXeYV","Score":-0.16276041666666663,"SharedSubnets":3,"Subnets":"80000000000108000000000000000000"},{"Peer":"16Uiu2HAm1UReWCXi4gUtYzF5YE85CUyNBhmUJWrVnAeUwF9ReUsG","Score":-0.16276041666666663,"SharedSubnets":3,"Subnets":"02000000000000000020100000000000"},{"Peer":"16Uiu2HAm1XscCE7r3wR24ernj1qZJQeJ7SvhDNPXNeAG2NDfeCP6","Score":-0.16276041666666663,"SharedSubnets":8,"Subnets":"00042000090080002000000004008000"},{"Peer":"16Uiu2HAkyfXMGd18iGWQ8DDL8Vowx5XV1wxNzAtF8Yok5MWfo84y","Score":-0.1627604166666666,"SharedSubnets":3,"Subnets":"00010000000000000020000000080000"},{"Peer":"16Uiu2HAm6JxaiyD8rY6YXUmaPQmYNf1nSxmn4qzByv44gaPgCHZa","Score":-0.16145833333333331,"SharedSubnets":1,"Subnets":"00000000000100000000000000000000"},{"Peer":"16Uiu2HAm6CWyfwBgYKQAcqBRpah1BhwRcf3EiY4YjFZtokEuCMpV","Score":-0.16145833333333331,"SharedSubnets":1,"Subnets":"00000001000000000000000000000000"},{"Peer":"16Uiu2HAmPRV6ThVp4j8PYBTmFVLwBudGKqHRPVGtvHgyK6wAvRaP","Score":-0.16145833333333331,"SharedSubnets":1,"Subnets":"00000000000400000000000000000000"},{"Peer":"16Uiu2HAmV6X7pCXmT8FDta5KPju7eJWdbzUsG5MaeYJaGVWBi55H","Score":-0.16145833333333331,"SharedSubnets":1,"Subnets":"00000000000002000000000000000000"},{"Peer":"16Uiu2HAkwamxFptQ8cWpWgtXbXAcsaCeBccgcDDpK242QhwPmmA5","Score":-0.16145833333333331,"SharedSubnets":1,"Subnets":"00000000040000000000000000000000"},{"Peer":"16Uiu2HAkyWM3sLHyDfrqT9eerTVzKHXt3GsJ14Ukm17yCVQk6cpd","Score":-0.1614583333333333,"SharedSubnets":1,"Subnets":"00000000000000000000400000000000"},{"Peer":"16Uiu2HAm8WhQjnCi2dz3gY19gUFxEP3FmiXjjS69qJn7Fa6NESRe","Score":-0.1614583333333333,"SharedSubnets":1,"Subnets":"00020000000000000000000000000000"},{"Peer":"16Uiu2HAkvupTFCYj7v22niTAwsFQYFVkYUSNsTzPmk79dnYcsdvf","Score":-0.16145833333333326,"SharedSubnets":1,"Subnets":"00000000000000000000000000000040"},{"Peer":"16Uiu2HAm3YEQeDtaYfDbT2jABtsAgjXRTseqkuKRR6g3bANAaifH","Score":-0.16145833333333326,"SharedSubnets":1,"Subnets":"00000000000000000000000000000100"},{"Peer":"16Uiu2HAmLeXb3PW85PYZCr5JGms3CcaBET6TbKz4WLSqkeT7c3NV","Score":-0.16145833333333326,"SharedSubnets":1,"Subnets":"00000000000000000000000000000200"},{"Peer":"16Uiu2HAmCbi6DTk49BEakGLi9LysHuLkkjAas1mDMrtJhL9VoA6K","Score":-0.16015624999999997,"SharedSubnets":9,"Subnets":"00010808009001040000000020000002"},{"Peer":"16Uiu2HAm7XHmHAwj7BTWyFtpyPEHqFLYqtmTc9HqrdqHQu9dAMS2","Score":-0.16015624999999997,"SharedSubnets":4,"Subnets":"00000210000000200000200000000000"},{"Peer":"16Uiu2HAkzWnqFg9LJFcCmPd9fsQifqqRM4njWApECFay5sHukJ8H","Score":-0.16015624999999994,"SharedSubnets":3,"Subnets":"08000000000010100000000000000000"},{"Peer":"16Uiu2HAmNc8KdfiNXeaVA6rVJ5fj1nKRmx7VRA97C6AMZFYRUuUv","Score":-0.15885416666666669,"SharedSubnets":14,"Subnets":"82008080184000202015000820000000"},{"Peer":"16Uiu2HAkxLGN74DMAyCQXqtoZAXFfYnMJJjudwF9kcU6GaHdRE4i","Score":-0.15885416666666663,"SharedSubnets":5,"Subnets":"00000041000000008040000000400000"},{"Peer":"16Uiu2HAkwoZZuLHktUXts3zjaEZLYM5noSvqEwkvvvELUhLwTVyk","Score":-0.15885416666666663,"SharedSubnets":2,"Subnets":"00080000000000000000000040000000"},{"Peer":"16Uiu2HAmELHczR4KGMBwYBv9ew426iYp1Js11zGZkAQFboXdtkLU","Score":-0.15885416666666663,"SharedSubnets":2,"Subnets":"20000000000000000000000400000000"},{"Peer":"16Uiu2HAmG8SvC3HBtuwnTszcgpySMSKVxMEm4CSn7Acr62eczLFi","Score":-0.15885416666666663,"SharedSubnets":4,"Subnets":"00200000000000000000006000000400"},{"Peer":"16Uiu2HAkxxNv28fAnAtAUf2sqdYP64h8MmD3jc1fh8qTQug8C5Wr","Score":-0.15885416666666663,"SharedSubnets":1,"Subnets":"00000000000000000000000000100000"},{"Peer":"16Uiu2HAmKtPJc3kokeWuRosHKHdJMTT2odi3Mcz4JzryAhaTFvkU","Score":-0.1588541666666666,"SharedSubnets":3,"Subnets":"80000000000000080000020000000000"},{"Peer":"16Uiu2HAkurFn5k8V8pwffutzHcpXqRtVAGBVYhnkaWL1psLHbL4b","Score":-0.1575520833333333,"SharedSubnets":4,"Subnets":"00000000000000000200200010001000"},{"Peer":"16Uiu2HAmLsDqf3oGdqCU37TyAo3yRtEju7jfz8LuYMwtyzMcA2yL","Score":-0.1575520833333333,"SharedSubnets":4,"Subnets":"00000000004010000000008002000000"},{"Peer":"16Uiu2HAmQ63Khb7FnvhPY7EXXuT19Ccytzhf1gZxMr5LvwqLjFqP","Score":-0.15625,"SharedSubnets":5,"Subnets":"00000022000000000040000080000008"},{"Peer":"16Uiu2HAm8dg33RBB7hQpMXVeUNjNT2TaLDWhmWg4DoQMounPdoVu","Score":-0.15624999999999994,"SharedSubnets":2,"Subnets":"00000000000000800004000000000000"},{"Peer":"16Uiu2HAmPXA2jN2JBEmARutrCSmQWP4QkcUpgiuMqJqxFyNhnqQ3","Score":-0.15624999999999994,"SharedSubnets":2,"Subnets":"00000000000000000000081000000000"},{"Peer":"16Uiu2HAkuZjGXchx93zPaY9pa5vYRAQhu8uwDa2z27jieeM2d3zw","Score":-0.15624999999999994,"SharedSubnets":3,"Subnets":"00000100000800400000000000000000"},{"Peer":"16Uiu2HAmV3SQqKrjVxNgXVswcZuzNN6D6zYPZqRiXH5pJqagScz2","Score":-0.1549479166666666,"SharedSubnets":9,"Subnets":"05042004480004000000000080000000"},{"Peer":"16Uiu2HAmAwcCrVSYYop7wAK2jJaWwd1NmkrRDyjTfNuFHqWaa9j8","Score":-0.15494791666666657,"SharedSubnets":9,"Subnets":"00000004040480040000002000012100"},{"Peer":"16Uiu2HAmKPZnTQL89qebAkZkJNm4PrW8mmC3LteUgDhhcHJsiPEh","Score":-0.15234374999999992,"SharedSubnets":8,"Subnets":"10201000000000001040800000002040"},{"Peer":"16Uiu2HAmFA7dF56C3Ca3Tqimyq5fyDwfvimKrfdgDQeWG7z2L3TZ","Score":-0.1497395833333333,"SharedSubnets":16,"Subnets":"08028000100801020018014800409001"},{"Peer":"16Uiu2HAmCH7wX7MvcH339VyqFo82C1Kf3GDnUDG9ZAK8YK9hiRCS","Score":-0.14583333333333334,"SharedSubnets":27,"Subnets":"040120180010124c16f0400348009401"},{"Peer":"16Uiu2HAm2Z1eWNeNmaTeMHDPB4WB9HaHUHXzerrqei9SijQmJXXo","Score":-0.13411458333333331,"SharedSubnets":17,"Subnets":"00401000604004480480010284a00001"},{"Peer":"16Uiu2HAkwt9SuuNXKHjDCLvcX5LNVEvsBeLGUCtAKccDFuCrCqTC","Score":-0.09374999999999999,"SharedSubnets":29,"Subnets":"0008284002a280008903041527094885"},{"Peer":"16Uiu2HAmMorRmGosQJzh3DUtiKF9BBspCWA6KQ3EPKWsUQMCmU2x","Score":0.16406249999999994,"SharedSubnets":128,"Subnets":"ffffffffffffffffffffffffffffffff"},{"Peer":"16Uiu2HAm4RuLkxdxXkRHBNm52aEo2FMAjSv947NhZkfR2xuwLWFM","Score":0.16406249999999994,"SharedSubnets":128,"Subnets":"ffffffffffffffffffffffffffffffff"},{"Peer":"16Uiu2HAmUHiQMemRv1AjyLid17Z3Rvbe9LekqjoWaVJyL3drmKh8","Score":0.16406249999999994,"SharedSubnets":128,"Subnets":"ffffffffffffffffffffffffffffffff"},{"Peer":"16Uiu2HAmLNPY62BphZRxmnbw74kvvi4At7TCCd3yse8U5foYRd1X","Score":0.16406249999999994,"SharedSubnets":128,"Subnets":"ffffffffffffffffffffffffffffffff"},{"Peer":"16Uiu2HAm7TxnEk1GgbRfQqviWwtE3HhofSMemA8Db4RAXbTcnhhU","Score":0.16406249999999994,"SharedSubnets":128,"Subnets":"ffffffffffffffffffffffffffffffff"}],"subnets_connections":[6,8,7,8,6,6,5,8,8,7,9,7,5,7,6,5,6,9,5,8,7,9,8,8,7,8,7,8,7,6,8,7,6,6,7,9,8,6,8,8,7,6,7,7,7,6,8,8,7,7,7,9,9,5,5,8,5,7,8,8,6,8,8,6,6,7,7,6,7,7,5,7,9,6,7,7,9,9,10,7,7,6,6,6,7,7,7,8,7,8,8,7,7,8,8,6,6,7,9,8,7,9,7,8,8,6,5,7,6,6,7,6,7,7,7,6,9,9,6,9,10,8,6,6,5,5,7,6]}`
