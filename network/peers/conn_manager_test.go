package peers

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"strings"
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
	lines := strings.Split(ssvNode2Dump13_08, "\n")
	mySubnetsStr, peersStr, subnetConnectionsStr := lines[0], lines[1], lines[2]
	mySubnets, err := records.Subnets{}.FromString(mySubnetsStr)
	require.NoError(t, err)
	var peers []peerDump
	err = json.Unmarshal([]byte(peersStr), &peers)
	require.NoError(t, err)
	var subnetConnections []int
	err = json.Unmarshal([]byte(subnetConnectionsStr), &subnetConnections)
	require.NoError(t, err)
	_ = mySubnets

	var subnetScores []float64
	for _, conns := range subnetConnections {
		subnetScores = append(subnetScores, scoreSubnet(conns, 2, 10))
	}

	sort.Slice(peers, func(i, j int) bool {
		return peers[i].Score > peers[j].Score
	})

	var newPeers []struct {
		Peer  peer.ID
		Score float64
	}
	for _, p := range peers {
		peerSubnets, err := records.Subnets{}.FromString(p.Subnets)
		require.NoError(t, err)
		peerScore := scorePeer(peerSubnets, subnetScores)
		newPeers = append(newPeers, struct {
			Peer  peer.ID
			Score float64
		}{Peer: p.Peer, Score: peerScore})
	}

	sort.Slice(newPeers, func(i, j int) bool {
		return newPeers[i].Score > newPeers[j].Score
	})

	for oldRank, peer := range peers {
		newRank := -1
		newScore := float64(0)
		for i, p := range newPeers {
			if p.Peer == peer.Peer {
				newRank = i
				newScore = p.Score
				break
			}
		}
		if newRank == -1 {
			panic("peer not found")
		}
		peerName := peer.Peer.String()
		if name, ok := peerNames[peerName]; ok {
			peerName = name
		}
		fmt.Printf("Peer %53s: SharedSubnets=%d OldScore(%d #%d) NewScore(%.2f #%d)\n",
			peerName, peer.SharedSubnets, peer.Score, oldRank, newScore, newRank)
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
}

type peerDump struct {
	Peer          peer.ID
	Score         int
	SharedSubnets int
	Subnets       string
}

var ssvNode4Dump12_53 = `ffffffffffffffffffffffffffffffff
[{"Peer":"16Uiu2HAmK13BqMguC4oeVDr5bqQaDojF7hijaAQDAmyobwXMcyWm","Score":0,"SharedSubnets":0,"Subnets":"00000000000000000000000000000000"},{"Peer":"16Uiu2HAm3jRReG6udtc8Vro5qACYKjKPFdNhm8b5zCsVWNdj5r9C","Score":42,"SharedSubnets":1,"Subnets":"00002000000000000000000000000000"},{"Peer":"16Uiu2HAm6JxaiyD8rY6YXUmaPQmYNf1nSxmn4qzByv44gaPgCHZa","Score":42,"SharedSubnets":1,"Subnets":"00000000000100000000000000000000"},{"Peer":"16Uiu2HAmV6X7pCXmT8FDta5KPju7eJWdbzUsG5MaeYJaGVWBi55H","Score":42,"SharedSubnets":1,"Subnets":"00000000000002000000000000000000"},{"Peer":"16Uiu2HAm913ANPqMEo25S2UBmZb62kasFg3k88mowF69UP9R4EQX","Score":42,"SharedSubnets":1,"Subnets":"00000000000008000000000000000000"},{"Peer":"16Uiu2HAm3YEQeDtaYfDbT2jABtsAgjXRTseqkuKRR6g3bANAaifH","Score":42,"SharedSubnets":1,"Subnets":"00000000000000000000000000000100"},{"Peer":"16Uiu2HAmBDFQc1edFKAT6PkyvvbH4PtoEVpkLuPUhqD4mJ4cijSu","Score":42,"SharedSubnets":1,"Subnets":"00000000000008000000000000000000"},{"Peer":"16Uiu2HAkvupTFCYj7v22niTAwsFQYFVkYUSNsTzPmk79dnYcsdvf","Score":42,"SharedSubnets":1,"Subnets":"00000000000000000000000000000040"},{"Peer":"16Uiu2HAm11JQtkxDUMy7ke6YKSRbKrf7w3dBT6YtoSPtpTQuRvvA","Score":42,"SharedSubnets":1,"Subnets":"00000000100000000000000000000000"},{"Peer":"16Uiu2HAmQ7xseZ8Axu4WN7kWnpz1f2zHMZNZ3cEiCh4psf9wSXo9","Score":42,"SharedSubnets":1,"Subnets":"00000000000000000000000400000000"},{"Peer":"16Uiu2HAmBLvt41t3rDd2PUvMovAtp4vrNi2e312nRgurr1vK2S6v","Score":42,"SharedSubnets":1,"Subnets":"00000000000000000000080000000000"},{"Peer":"16Uiu2HAmGa5eobff1pHVh5ez6XZGyt7bdaEvE2WbZHsNqV6pD2Pf","Score":42,"SharedSubnets":1,"Subnets":"00000000000000000000000008000000"},{"Peer":"16Uiu2HAm8WhQjnCi2dz3gY19gUFxEP3FmiXjjS69qJn7Fa6NESRe","Score":42,"SharedSubnets":1,"Subnets":"00020000000000000000000000000000"},{"Peer":"16Uiu2HAmG8SvC3HBtuwnTszcgpySMSKVxMEm4CSn7Acr62eczLFi","Score":42,"SharedSubnets":1,"Subnets":"00200000000000000000000000000000"},{"Peer":"16Uiu2HAmJa7gNYQCumkMAxWZT5kb99VJfPdM99ZZacv8kbqKzVk9","Score":43,"SharedSubnets":2,"Subnets":"00000008800000000000000000000000"},{"Peer":"16Uiu2HAmKtPJc3kokeWuRosHKHdJMTT2odi3Mcz4JzryAhaTFvkU","Score":43,"SharedSubnets":3,"Subnets":"80000000000000080000020000000000"},{"Peer":"16Uiu2HAmEeveoth116XcpuNH5P6Vzezd11RcKTAyvLjsKJmxGj59","Score":43,"SharedSubnets":2,"Subnets":"00000200000000200000000000000000"},{"Peer":"16Uiu2HAkuZjGXchx93zPaY9pa5vYRAQhu8uwDa2z27jieeM2d3zw","Score":43,"SharedSubnets":3,"Subnets":"00000100000800400000000000000000"},{"Peer":"16Uiu2HAm7xH8hhmxsF4j3Bz6G7cFNS1kP5x2BA5zsXB9wz7QJ2ge","Score":43,"SharedSubnets":2,"Subnets":"00000200000010000000000000000000"},{"Peer":"16Uiu2HAkzWnqFg9LJFcCmPd9fsQifqqRM4njWApECFay5sHukJ8H","Score":43,"SharedSubnets":3,"Subnets":"08000000000010100000000000000000"},{"Peer":"16Uiu2HAkyfXMGd18iGWQ8DDL8Vowx5XV1wxNzAtF8Yok5MWfo84y","Score":43,"SharedSubnets":3,"Subnets":"00010000000000000020000000080000"},{"Peer":"16Uiu2HAmFbJk2puSWWuNDuLUEUwPjPTdkyg4j11EUJYEgT4iowZq","Score":43,"SharedSubnets":2,"Subnets":"00010000000000000000000000000002"},{"Peer":"16Uiu2HAmCaJ1E7Pd2mAKHGyDqXinbNFqcjrw4zbwakJmdKWbY9xY","Score":43,"SharedSubnets":2,"Subnets":"00000040000010000000000000000000"},{"Peer":"16Uiu2HAm8dg33RBB7hQpMXVeUNjNT2TaLDWhmWg4DoQMounPdoVu","Score":43,"SharedSubnets":2,"Subnets":"00000000000000800004000000000000"},{"Peer":"16Uiu2HAkwoZZuLHktUXts3zjaEZLYM5noSvqEwkvvvELUhLwTVyk","Score":43,"SharedSubnets":2,"Subnets":"00080000000000000000000040000000"},{"Peer":"16Uiu2HAmELHczR4KGMBwYBv9ew426iYp1Js11zGZkAQFboXdtkLU","Score":43,"SharedSubnets":2,"Subnets":"20000000000000000000000400000000"},{"Peer":"16Uiu2HAkxwYwawPqrcF61c8DBq8TZqXjiizig6V5bheUobMHRaEx","Score":43,"SharedSubnets":2,"Subnets":"00000000000008000000000000100000"},{"Peer":"16Uiu2HAm5rC4Y1LzvNM2YRiUyfNptCrsMiAEdzF1XvA3q77gd48K","Score":43,"SharedSubnets":3,"Subnets":"00000000000000000000800200000002"},{"Peer":"16Uiu2HAmCVXoohQ5zSGbzSHVLYwBmNb5uDFZ8LJ36XSpzUELXeYV","Score":43,"SharedSubnets":3,"Subnets":"80000000000108000000000000000000"},{"Peer":"16Uiu2HAm1XscCE7r3wR24ernj1qZJQeJ7SvhDNPXNeAG2NDfeCP6","Score":44,"SharedSubnets":8,"Subnets":"00042000090080002000000004008000"},{"Peer":"16Uiu2HAm2e9cXq7HEUSGcZpbETwjhxdkFjvoiufR6KaZMcXUHdFg","Score":44,"SharedSubnets":4,"Subnets":"00040080008000000000000020000000"},{"Peer":"16Uiu2HAmPhc4cCBbeVayDp5jmWgs6GseqPMbh6GJn4f6VcrJUfeZ","Score":44,"SharedSubnets":5,"Subnets":"00000041000000008040000000400000"},{"Peer":"16Uiu2HAmLsDqf3oGdqCU37TyAo3yRtEju7jfz8LuYMwtyzMcA2yL","Score":44,"SharedSubnets":4,"Subnets":"00000000004010000000008002000000"},{"Peer":"16Uiu2HAm7XHmHAwj7BTWyFtpyPEHqFLYqtmTc9HqrdqHQu9dAMS2","Score":44,"SharedSubnets":4,"Subnets":"00000210000000200000200000000000"},{"Peer":"16Uiu2HAmP5ipPcMp2YhfVzZZYN9NMSFKeQKn3FU3ya8rUJUxbqfe","Score":44,"SharedSubnets":4,"Subnets":"00010004000008000020000000000000"},{"Peer":"16Uiu2HAmHkxX6KiF7Q4agUSEQmzQMC1eDDt9DPdVFuEPNMPKfsEU","Score":44,"SharedSubnets":5,"Subnets":"00000022000000000040000080000008"},{"Peer":"16Uiu2HAkurFn5k8V8pwffutzHcpXqRtVAGBVYhnkaWL1psLHbL4b","Score":44,"SharedSubnets":4,"Subnets":"00000000000000000200200010001000"},{"Peer":"16Uiu2HAmKSKKHNcUhRQF4dK3bA8aPtM5SYEDEPt9gdbkdxwJYFB2","Score":44,"SharedSubnets":5,"Subnets":"00000000000000020000002000013000"},{"Peer":"16Uiu2HAmQ63Khb7FnvhPY7EXXuT19Ccytzhf1gZxMr5LvwqLjFqP","Score":44,"SharedSubnets":5,"Subnets":"00000022000000000040000080000008"},{"Peer":"16Uiu2HAm9Eo76CBEX9fnsx3fy2PGmpnw7TSEyf7K8q8UMKyPqye4","Score":45,"SharedSubnets":11,"Subnets":"10000100500200000080200284000400"},{"Peer":"16Uiu2HAm7EgFz3Zsg9Hk6HzbCPK9u97cVmFUoHzqnHkP4Zbrc3yj","Score":45,"SharedSubnets":6,"Subnets":"00000200000000000100000410084000"},{"Peer":"16Uiu2HAkwr7G5BEks3gjhtoU3WkDRNXUhiBpzW48tQTr8XvQhCLX","Score":45,"SharedSubnets":7,"Subnets":"82000000000084000000000000410008"},{"Peer":"16Uiu2HAmUGRH5NxVxTBuFGjcJ6KzZzPB9ifKSFcjbSVJRJSmcxHc","Score":45,"SharedSubnets":7,"Subnets":"10001000004800000002004000001000"},{"Peer":"16Uiu2HAm3v7wh42EwULT9ftdY1TvmRt8giVWpwjiLdajp2kjQEbi","Score":46,"SharedSubnets":9,"Subnets":"80000021000020080800000100280000"},{"Peer":"16Uiu2HAmFQdGq8XHndxZo6VvUicBjvikarjVzdVFAbSnQ8GuJNxF","Score":46,"SharedSubnets":9,"Subnets":"00048802880000000001800000000200"},{"Peer":"16Uiu2HAmAwcCrVSYYop7wAK2jJaWwd1NmkrRDyjTfNuFHqWaa9j8","Score":46,"SharedSubnets":9,"Subnets":"00000004040480040000002000012100"},{"Peer":"16Uiu2HAmLCNsY7fqsrzxsThRRABf7a9fscMELBbhXaivvVC99Wjw","Score":46,"SharedSubnets":8,"Subnets":"00004202800000000011000000022000"},{"Peer":"16Uiu2HAmKPZnTQL89qebAkZkJNm4PrW8mmC3LteUgDhhcHJsiPEh","Score":46,"SharedSubnets":8,"Subnets":"10201000000000001040800000002040"},{"Peer":"16Uiu2HAmCbi6DTk49BEakGLi9LysHuLkkjAas1mDMrtJhL9VoA6K","Score":46,"SharedSubnets":9,"Subnets":"00010808009001040000000020000002"},{"Peer":"16Uiu2HAmV3SQqKrjVxNgXVswcZuzNN6D6zYPZqRiXH5pJqagScz2","Score":46,"SharedSubnets":9,"Subnets":"05042004480004000000000080000000"},{"Peer":"16Uiu2HAm87sWQcCPTCPLr1xZ3TvyWqBAHRRCWKkrniqhrgeu9gbJ","Score":46,"SharedSubnets":9,"Subnets":"08000004800000420020010020000002"},{"Peer":"16Uiu2HAkuicjxoqeDDUrsdiY4E3ceqprCgK5iexhFSo7f2gSKXBt","Score":47,"SharedSubnets":14,"Subnets":"002400442040100400000000c6800800"},{"Peer":"16Uiu2HAmS5FJGXa5tZtRbHTinyy7vcPjixBNJ8VcR5YxZnVE1YTQ","Score":47,"SharedSubnets":14,"Subnets":"12000104704200000210000004200400"},{"Peer":"16Uiu2HAm2Z1eWNeNmaTeMHDPB4WB9HaHUHXzerrqei9SijQmJXXo","Score":48,"SharedSubnets":17,"Subnets":"00401000604004480480010284a00001"},{"Peer":"16Uiu2HAmQkYATgANrpdDyZ5SgGhxEraiWqGt1TBC4aJJovUw39SD","Score":48,"SharedSubnets":13,"Subnets":"80048802880080400001800000100200"},{"Peer":"16Uiu2HAm6FPFWfYkZvu51GzSoMtbx8sg4r7EEezv1KgKCy1FmXzh","Score":49,"SharedSubnets":18,"Subnets":"80000021090820480800000124280610"},{"Peer":"16Uiu2HAmFA7dF56C3Ca3Tqimyq5fyDwfvimKrfdgDQeWG7z2L3TZ","Score":50,"SharedSubnets":16,"Subnets":"08028000100801020018014800409001"},{"Peer":"16Uiu2HAmEe8GnSuE2syDCKqZ8EjuPSSGoC7Z3p3KXYNoM6ZEawYT","Score":50,"SharedSubnets":20,"Subnets":"2808804010010040a801000505104081"},{"Peer":"16Uiu2HAm92C2wkqaMDP6Aijw7jUazY3z2Fic4TeKaK2ADMXiQPKF","Score":52,"SharedSubnets":21,"Subnets":"0208000200004180cc11801700006081"},{"Peer":"16Uiu2HAm2mL8PBS9xQEzV53jFi7eu41NophErcUKkfYzSs38xFsa","Score":56,"SharedSubnets":36,"Subnets":"441830809141120ec633244114041680"},{"Peer":"16Uiu2HAm3easq5K4juU6Q5W815p2uqaSKwpKtLjJai6hhTaD4PXz","Score":58,"SharedSubnets":101,"Subnets":"f6fff99eeffffe5bdddfb2dafa5fffdf"}]
[1,4,3,4,6,3,2,7,5,3,7,5,2,4,2,1,4,5,0,4,5,5,2,5,3,7,7,3,3,4,4,3,4,1,2,6,6,4,6,7,5,3,2,5,2,1,7,3,3,3,4,6,7,3,2,6,1,5,4,6,3,2,7,2,2,3,4,5,2,2,3,5,7,3,2,2,6,4,5,3,3,2,1,1,1,5,0,6,5,5,5,2,2,3,5,2,1,3,8,2,4,5,4,7,4,2,2,5,4,4,4,2,3,5,6,2,6,6,4,3,5,5,1,4,2,0,3,4]`

var ssvNode3Dump12_53 = `ffffffffffffffffffffffffffffffff
[{"Peer":"16Uiu2HAkyWM3sLHyDfrqT9eerTVzKHXt3GsJ14Ukm17yCVQk6cpd","Score":30,"SharedSubnets":1,"Subnets":"00000000000000000000400000000000"},{"Peer":"16Uiu2HAm8WhQjnCi2dz3gY19gUFxEP3FmiXjjS69qJn7Fa6NESRe","Score":30,"SharedSubnets":1,"Subnets":"00020000000000000000000000000000"},{"Peer":"16Uiu2HAmV6X7pCXmT8FDta5KPju7eJWdbzUsG5MaeYJaGVWBi55H","Score":30,"SharedSubnets":1,"Subnets":"00000000000002000000000000000000"},{"Peer":"16Uiu2HAmGa5eobff1pHVh5ez6XZGyt7bdaEvE2WbZHsNqV6pD2Pf","Score":30,"SharedSubnets":1,"Subnets":"00000000000000000000000008000000"},{"Peer":"16Uiu2HAmBDFQc1edFKAT6PkyvvbH4PtoEVpkLuPUhqD4mJ4cijSu","Score":30,"SharedSubnets":1,"Subnets":"00000000000008000000000000000000"},{"Peer":"16Uiu2HAm5u7aGYiR5b2t1EJGhh2ZwCLEXWGKsAXPnvuqK7AG8Xno","Score":30,"SharedSubnets":1,"Subnets":"00000000000000000000000000000002"},{"Peer":"16Uiu2HAm5y8gBegyzX9GqBrq58Abm9t8zPSDzyprabnGFsxEUCQo","Score":30,"SharedSubnets":1,"Subnets":"00000000000008000000000000000000"},{"Peer":"16Uiu2HAm3opMtmf3DBNJMiHvenciUeEMjiTr6gdxHXwQkmn48y3Q","Score":30,"SharedSubnets":1,"Subnets":"08000000000000000000000000000000"},{"Peer":"16Uiu2HAm913ANPqMEo25S2UBmZb62kasFg3k88mowF69UP9R4EQX","Score":30,"SharedSubnets":1,"Subnets":"00000000000008000000000000000000"},{"Peer":"16Uiu2HAm11JQtkxDUMy7ke6YKSRbKrf7w3dBT6YtoSPtpTQuRvvA","Score":30,"SharedSubnets":1,"Subnets":"00000000100000000000000000000000"},{"Peer":"16Uiu2HAm6A6NoPJqneZz3VJdHp72nXjXFDWd9qWJ1aJGzymhL4hn","Score":30,"SharedSubnets":1,"Subnets":"00002000000000000000000000000000"},{"Peer":"16Uiu2HAm1XscCE7r3wR24ernj1qZJQeJ7SvhDNPXNeAG2NDfeCP6","Score":30,"SharedSubnets":8,"Subnets":"00042000090080002000000004008000"},{"Peer":"16Uiu2HAmVHni9pncdYrbRgAar9NiRgEa4SpbHLmHU5wMaNDedWkc","Score":30,"SharedSubnets":5,"Subnets":"08000000000000000008000008008001"},{"Peer":"16Uiu2HAmQ7xseZ8Axu4WN7kWnpz1f2zHMZNZ3cEiCh4psf9wSXo9","Score":30,"SharedSubnets":1,"Subnets":"00000000000000000000000400000000"},{"Peer":"16Uiu2HAm3YEQeDtaYfDbT2jABtsAgjXRTseqkuKRR6g3bANAaifH","Score":30,"SharedSubnets":1,"Subnets":"00000000000000000000000000000100"},{"Peer":"16Uiu2HAm876jaEMj5MNDQdftgUxyQwuuArwpVFrnveqXZCxbMprd","Score":30,"SharedSubnets":1,"Subnets":"00000000000008000000000000000000"},{"Peer":"16Uiu2HAmG8SvC3HBtuwnTszcgpySMSKVxMEm4CSn7Acr62eczLFi","Score":30,"SharedSubnets":1,"Subnets":"00200000000000000000000000000000"},{"Peer":"16Uiu2HAmLeXb3PW85PYZCr5JGms3CcaBET6TbKz4WLSqkeT7c3NV","Score":30,"SharedSubnets":1,"Subnets":"00000000000000000000000000000200"},{"Peer":"16Uiu2HAmBBqPcJQcM6BeMAQ4bAXMdA32Lhxkho1tWn9chCmrziHv","Score":30,"SharedSubnets":1,"Subnets":"00000000000000000001000000000000"},{"Peer":"16Uiu2HAm6JxaiyD8rY6YXUmaPQmYNf1nSxmn4qzByv44gaPgCHZa","Score":30,"SharedSubnets":1,"Subnets":"00000000000100000000000000000000"},{"Peer":"16Uiu2HAmELHczR4KGMBwYBv9ew426iYp1Js11zGZkAQFboXdtkLU","Score":31,"SharedSubnets":2,"Subnets":"20000000000000000000000400000000"},{"Peer":"16Uiu2HAmFbJk2puSWWuNDuLUEUwPjPTdkyg4j11EUJYEgT4iowZq","Score":31,"SharedSubnets":2,"Subnets":"00010000000000000000000000000002"},{"Peer":"16Uiu2HAmJa7gNYQCumkMAxWZT5kb99VJfPdM99ZZacv8kbqKzVk9","Score":31,"SharedSubnets":2,"Subnets":"00000008800000000000000000000000"},{"Peer":"16Uiu2HAmCVXoohQ5zSGbzSHVLYwBmNb5uDFZ8LJ36XSpzUELXeYV","Score":31,"SharedSubnets":3,"Subnets":"80000000000108000000000000000000"},{"Peer":"16Uiu2HAmEeveoth116XcpuNH5P6Vzezd11RcKTAyvLjsKJmxGj59","Score":31,"SharedSubnets":2,"Subnets":"00000200000000200000000000000000"},{"Peer":"16Uiu2HAmCaJ1E7Pd2mAKHGyDqXinbNFqcjrw4zbwakJmdKWbY9xY","Score":31,"SharedSubnets":2,"Subnets":"00000040000010000000000000000000"},{"Peer":"16Uiu2HAkyfXMGd18iGWQ8DDL8Vowx5XV1wxNzAtF8Yok5MWfo84y","Score":31,"SharedSubnets":3,"Subnets":"00010000000000000020000000080000"},{"Peer":"16Uiu2HAkxwYwawPqrcF61c8DBq8TZqXjiizig6V5bheUobMHRaEx","Score":31,"SharedSubnets":2,"Subnets":"00000000000008000000000000100000"},{"Peer":"16Uiu2HAm1UReWCXi4gUtYzF5YE85CUyNBhmUJWrVnAeUwF9ReUsG","Score":31,"SharedSubnets":3,"Subnets":"02000000000000000020100000000000"},{"Peer":"16Uiu2HAkwoZZuLHktUXts3zjaEZLYM5noSvqEwkvvvELUhLwTVyk","Score":31,"SharedSubnets":2,"Subnets":"00080000000000000000000040000000"},{"Peer":"16Uiu2HAm7xH8hhmxsF4j3Bz6G7cFNS1kP5x2BA5zsXB9wz7QJ2ge","Score":31,"SharedSubnets":2,"Subnets":"00000200000010000000000000000000"},{"Peer":"16Uiu2HAmLsDqf3oGdqCU37TyAo3yRtEju7jfz8LuYMwtyzMcA2yL","Score":32,"SharedSubnets":4,"Subnets":"00000000004010000000008002000000"},{"Peer":"16Uiu2HAmKSKKHNcUhRQF4dK3bA8aPtM5SYEDEPt9gdbkdxwJYFB2","Score":32,"SharedSubnets":5,"Subnets":"00000000000000020000002000013000"},{"Peer":"16Uiu2HAmPhc4cCBbeVayDp5jmWgs6GseqPMbh6GJn4f6VcrJUfeZ","Score":32,"SharedSubnets":5,"Subnets":"00000041000000008040000000400000"},{"Peer":"16Uiu2HAm2e9cXq7HEUSGcZpbETwjhxdkFjvoiufR6KaZMcXUHdFg","Score":32,"SharedSubnets":4,"Subnets":"00040080008000000000000020000000"},{"Peer":"16Uiu2HAmTk643ZrDfZH4izW5xFH8UwHtvJwgvmd2iwU8rprJkZtv","Score":32,"SharedSubnets":21,"Subnets":"02800002098801402908000025108211"},{"Peer":"16Uiu2HAmHLrAJr75TukUswnvvfkzpFiNE8yoB9Vw9E6v6869J7y8","Score":32,"SharedSubnets":9,"Subnets":"08000004000000420020010020001800"},{"Peer":"16Uiu2HAm3BtFC7hzip7czpo6p15iTUPZ2T9KAf1ECue1akDPmewL","Score":32,"SharedSubnets":4,"Subnets":"00004002000080000010000000000000"},{"Peer":"16Uiu2HAm2Z1eWNeNmaTeMHDPB4WB9HaHUHXzerrqei9SijQmJXXo","Score":32,"SharedSubnets":17,"Subnets":"00401000604004480480010284a00001"},{"Peer":"16Uiu2HAmHkxX6KiF7Q4agUSEQmzQMC1eDDt9DPdVFuEPNMPKfsEU","Score":32,"SharedSubnets":5,"Subnets":"00000022000000000040000080000008"},{"Peer":"16Uiu2HAmLLRhcZ8eLV4TSn2peoxLCGyxi8Nzd7q1fMsjqD91hHf2","Score":32,"SharedSubnets":5,"Subnets":"00000041000000008040000000400000"},{"Peer":"16Uiu2HAmV3SQqKrjVxNgXVswcZuzNN6D6zYPZqRiXH5pJqagScz2","Score":32,"SharedSubnets":9,"Subnets":"05042004480004000000000080000000"},{"Peer":"16Uiu2HAm7XHmHAwj7BTWyFtpyPEHqFLYqtmTc9HqrdqHQu9dAMS2","Score":32,"SharedSubnets":4,"Subnets":"00000210000000200000200000000000"},{"Peer":"16Uiu2HAkwr7G5BEks3gjhtoU3WkDRNXUhiBpzW48tQTr8XvQhCLX","Score":33,"SharedSubnets":7,"Subnets":"82000000000084000000000000410008"},{"Peer":"16Uiu2HAm6FPFWfYkZvu51GzSoMtbx8sg4r7EEezv1KgKCy1FmXzh","Score":33,"SharedSubnets":18,"Subnets":"80000021090820480800000124280610"},{"Peer":"16Uiu2HAm9Eo76CBEX9fnsx3fy2PGmpnw7TSEyf7K8q8UMKyPqye4","Score":33,"SharedSubnets":11,"Subnets":"10000100500200000080200284000400"},{"Peer":"16Uiu2HAm7EgFz3Zsg9Hk6HzbCPK9u97cVmFUoHzqnHkP4Zbrc3yj","Score":33,"SharedSubnets":6,"Subnets":"00000200000000000100000410084000"},{"Peer":"16Uiu2HAm89btspBaQ6FLZJcYxS4GDFoAxgyGY6qCHSW95ooi38m8","Score":33,"SharedSubnets":15,"Subnets":"000098420a4000480480010040200000"},{"Peer":"16Uiu2HAm9nRGV7wUfWeHAUie6iS198RMq6EJEGex1b489KRn4p76","Score":33,"SharedSubnets":19,"Subnets":"160030062c4c00400202004000001001"},{"Peer":"16Uiu2HAmKPZnTQL89qebAkZkJNm4PrW8mmC3LteUgDhhcHJsiPEh","Score":34,"SharedSubnets":8,"Subnets":"10201000000000001040800000002040"},{"Peer":"16Uiu2HAm3v7wh42EwULT9ftdY1TvmRt8giVWpwjiLdajp2kjQEbi","Score":34,"SharedSubnets":9,"Subnets":"80000021000020080800000100280000"},{"Peer":"16Uiu2HAmEe8GnSuE2syDCKqZ8EjuPSSGoC7Z3p3KXYNoM6ZEawYT","Score":34,"SharedSubnets":20,"Subnets":"2808804010010040a801000505104081"},{"Peer":"16Uiu2HAmCbi6DTk49BEakGLi9LysHuLkkjAas1mDMrtJhL9VoA6K","Score":34,"SharedSubnets":9,"Subnets":"00010808009001040000000020000002"},{"Peer":"16Uiu2HAmAwcCrVSYYop7wAK2jJaWwd1NmkrRDyjTfNuFHqWaa9j8","Score":34,"SharedSubnets":9,"Subnets":"00000004040480040000002000012100"},{"Peer":"16Uiu2HAmNc8KdfiNXeaVA6rVJ5fj1nKRmx7VRA97C6AMZFYRUuUv","Score":35,"SharedSubnets":14,"Subnets":"82008080184000202015000820000000"},{"Peer":"16Uiu2HAmFA7dF56C3Ca3Tqimyq5fyDwfvimKrfdgDQeWG7z2L3TZ","Score":36,"SharedSubnets":16,"Subnets":"08028000100801020018014800409001"},{"Peer":"16Uiu2HAmLNPY62BphZRxmnbw74kvvi4At7TCCd3yse8U5foYRd1X","Score":38,"SharedSubnets":128,"Subnets":"ffffffffffffffffffffffffffffffff"},{"Peer":"16Uiu2HAm3VoGtru359raxR2w8z6Up9uPg8TwL1S7nWXm7CaDJ2kR","Score":40,"SharedSubnets":28,"Subnets":"013101000511d120044020118401ca03"},{"Peer":"16Uiu2HAm92C2wkqaMDP6Aijw7jUazY3z2Fic4TeKaK2ADMXiQPKF","Score":42,"SharedSubnets":28,"Subnets":"0228018204004580cc11841780006081"},{"Peer":"16Uiu2HAm2mL8PBS9xQEzV53jFi7eu41NophErcUKkfYzSs38xFsa","Score":44,"SharedSubnets":36,"Subnets":"441830809141120ec633244114041680"}]
[3,7,4,6,5,3,2,6,5,3,4,5,3,5,2,2,4,5,1,3,6,6,2,5,5,7,5,3,2,4,6,5,6,2,5,8,7,3,5,3,6,2,3,5,3,1,7,4,6,3,5,7,6,3,3,6,1,5,4,6,2,5,8,2,3,3,6,6,2,5,3,6,6,3,2,4,6,5,6,4,5,1,3,1,2,5,2,3,7,4,6,3,3,4,5,2,3,2,9,3,3,7,4,7,5,1,2,5,4,5,5,2,3,6,5,3,6,5,5,6,9,5,1,3,3,1,2,4]`

var ssvNode2Dump13_04 = `ffffffffffffffffffffffffffffffff
[{"Peer":"16Uiu2HAkuZjGXchx93zPaY9pa5vYRAQhu8uwDa2z27jieeM2d3zw","Score":56,"SharedSubnets":3,"Subnets":"00000100000800400000000000000000"},{"Peer":"16Uiu2HAmLLRhcZ8eLV4TSn2peoxLCGyxi8Nzd7q1fMsjqD91hHf2","Score":57,"SharedSubnets":5,"Subnets":"00000041000000008040000000400000"},{"Peer":"16Uiu2HAmBLvt41t3rDd2PUvMovAtp4vrNi2e312nRgurr1vK2S6v","Score":57,"SharedSubnets":1,"Subnets":"00000000000000000000080000000000"},{"Peer":"16Uiu2HAm2zCJ3fG5vam56Bgrr32WmSDUkUBTCL5tM3Kj6TwBkzYM","Score":57,"SharedSubnets":5,"Subnets":"00000022000000000040000080000008"},{"Peer":"16Uiu2HAm11JQtkxDUMy7ke6YKSRbKrf7w3dBT6YtoSPtpTQuRvvA","Score":57,"SharedSubnets":1,"Subnets":"00000000100000000000000000000000"},{"Peer":"16Uiu2HAmPhc4cCBbeVayDp5jmWgs6GseqPMbh6GJn4f6VcrJUfeZ","Score":57,"SharedSubnets":5,"Subnets":"00000041000000008040000000400000"},{"Peer":"16Uiu2HAm5u7aGYiR5b2t1EJGhh2ZwCLEXWGKsAXPnvuqK7AG8Xno","Score":57,"SharedSubnets":1,"Subnets":"00000000000000000000000000000002"},{"Peer":"16Uiu2HAmGa5eobff1pHVh5ez6XZGyt7bdaEvE2WbZHsNqV6pD2Pf","Score":57,"SharedSubnets":1,"Subnets":"00000000000000000000000008000000"},{"Peer":"16Uiu2HAmBDFQc1edFKAT6PkyvvbH4PtoEVpkLuPUhqD4mJ4cijSu","Score":57,"SharedSubnets":1,"Subnets":"00000000000008000000000000000000"},{"Peer":"16Uiu2HAm6JxaiyD8rY6YXUmaPQmYNf1nSxmn4qzByv44gaPgCHZa","Score":57,"SharedSubnets":1,"Subnets":"00000000000100000000000000000000"},{"Peer":"16Uiu2HAm8WhQjnCi2dz3gY19gUFxEP3FmiXjjS69qJn7Fa6NESRe","Score":57,"SharedSubnets":1,"Subnets":"00020000000000000000000000000000"},{"Peer":"16Uiu2HAmHkxX6KiF7Q4agUSEQmzQMC1eDDt9DPdVFuEPNMPKfsEU","Score":57,"SharedSubnets":5,"Subnets":"00000022000000000040000080000008"},{"Peer":"16Uiu2HAmVHni9pncdYrbRgAar9NiRgEa4SpbHLmHU5wMaNDedWkc","Score":57,"SharedSubnets":5,"Subnets":"08000000000000000008000008008001"},{"Peer":"16Uiu2HAm6A6NoPJqneZz3VJdHp72nXjXFDWd9qWJ1aJGzymhL4hn","Score":57,"SharedSubnets":1,"Subnets":"00002000000000000000000000000000"},{"Peer":"16Uiu2HAm3jRReG6udtc8Vro5qACYKjKPFdNhm8b5zCsVWNdj5r9C","Score":57,"SharedSubnets":1,"Subnets":"00002000000000000000000000000000"},{"Peer":"16Uiu2HAmV6X7pCXmT8FDta5KPju7eJWdbzUsG5MaeYJaGVWBi55H","Score":57,"SharedSubnets":1,"Subnets":"00000000000002000000000000000000"},{"Peer":"16Uiu2HAmSjCmXGkqjziiM3rKtMEuTExBfKVzYwvYGhsMNzmrVvi6","Score":57,"SharedSubnets":1,"Subnets":"00002000000000000000000000000000"},{"Peer":"16Uiu2HAm3opMtmf3DBNJMiHvenciUeEMjiTr6gdxHXwQkmn48y3Q","Score":57,"SharedSubnets":1,"Subnets":"08000000000000000000000000000000"},{"Peer":"16Uiu2HAm876jaEMj5MNDQdftgUxyQwuuArwpVFrnveqXZCxbMprd","Score":57,"SharedSubnets":1,"Subnets":"00000000000008000000000000000000"},{"Peer":"16Uiu2HAmEeveoth116XcpuNH5P6Vzezd11RcKTAyvLjsKJmxGj59","Score":58,"SharedSubnets":2,"Subnets":"00000200000000200000000000000000"},{"Peer":"16Uiu2HAkxwYwawPqrcF61c8DBq8TZqXjiizig6V5bheUobMHRaEx","Score":58,"SharedSubnets":2,"Subnets":"00000000000008000000000000100000"},{"Peer":"16Uiu2HAmCaJ1E7Pd2mAKHGyDqXinbNFqcjrw4zbwakJmdKWbY9xY","Score":58,"SharedSubnets":2,"Subnets":"00000040000010000000000000000000"},{"Peer":"16Uiu2HAm1UReWCXi4gUtYzF5YE85CUyNBhmUJWrVnAeUwF9ReUsG","Score":58,"SharedSubnets":3,"Subnets":"02000000000000000020100000000000"},{"Peer":"16Uiu2HAkwoZZuLHktUXts3zjaEZLYM5noSvqEwkvvvELUhLwTVyk","Score":58,"SharedSubnets":2,"Subnets":"00080000000000000000000040000000"},{"Peer":"16Uiu2HAmELHczR4KGMBwYBv9ew426iYp1Js11zGZkAQFboXdtkLU","Score":58,"SharedSubnets":2,"Subnets":"20000000000000000000000400000000"},{"Peer":"16Uiu2HAmJa7gNYQCumkMAxWZT5kb99VJfPdM99ZZacv8kbqKzVk9","Score":58,"SharedSubnets":2,"Subnets":"00000008800000000000000000000000"},{"Peer":"16Uiu2HAmCVXoohQ5zSGbzSHVLYwBmNb5uDFZ8LJ36XSpzUELXeYV","Score":58,"SharedSubnets":3,"Subnets":"80000000000108000000000000000000"},{"Peer":"16Uiu2HAm7xH8hhmxsF4j3Bz6G7cFNS1kP5x2BA5zsXB9wz7QJ2ge","Score":58,"SharedSubnets":2,"Subnets":"00000200000010000000000000000000"},{"Peer":"16Uiu2HAm8dg33RBB7hQpMXVeUNjNT2TaLDWhmWg4DoQMounPdoVu","Score":58,"SharedSubnets":2,"Subnets":"00000000000000800004000000000000"},{"Peer":"16Uiu2HAmFbJk2puSWWuNDuLUEUwPjPTdkyg4j11EUJYEgT4iowZq","Score":58,"SharedSubnets":2,"Subnets":"00010000000000000000000000000002"},{"Peer":"16Uiu2HAm5rC4Y1LzvNM2YRiUyfNptCrsMiAEdzF1XvA3q77gd48K","Score":58,"SharedSubnets":3,"Subnets":"00000000000000000000800200000002"},{"Peer":"16Uiu2HAkyfXMGd18iGWQ8DDL8Vowx5XV1wxNzAtF8Yok5MWfo84y","Score":58,"SharedSubnets":3,"Subnets":"00010000000000000020000000080000"},{"Peer":"16Uiu2HAmG8SvC3HBtuwnTszcgpySMSKVxMEm4CSn7Acr62eczLFi","Score":59,"SharedSubnets":4,"Subnets":"00200000000000000000006000000400"},{"Peer":"16Uiu2HAmKSKKHNcUhRQF4dK3bA8aPtM5SYEDEPt9gdbkdxwJYFB2","Score":59,"SharedSubnets":5,"Subnets":"00000000000000020000002000013000"},{"Peer":"16Uiu2HAmKPZnTQL89qebAkZkJNm4PrW8mmC3LteUgDhhcHJsiPEh","Score":59,"SharedSubnets":8,"Subnets":"10201000000000001040800000002040"},{"Peer":"16Uiu2HAm7XHmHAwj7BTWyFtpyPEHqFLYqtmTc9HqrdqHQu9dAMS2","Score":59,"SharedSubnets":4,"Subnets":"00000210000000200000200000000000"},{"Peer":"16Uiu2HAm2e9cXq7HEUSGcZpbETwjhxdkFjvoiufR6KaZMcXUHdFg","Score":59,"SharedSubnets":4,"Subnets":"00040080008000000000000020000000"},{"Peer":"16Uiu2HAmLsDqf3oGdqCU37TyAo3yRtEju7jfz8LuYMwtyzMcA2yL","Score":59,"SharedSubnets":4,"Subnets":"00000000004010000000008002000000"},{"Peer":"16Uiu2HAm3BtFC7hzip7czpo6p15iTUPZ2T9KAf1ECue1akDPmewL","Score":59,"SharedSubnets":4,"Subnets":"00004002000080000010000000000000"},{"Peer":"16Uiu2HAm7EgFz3Zsg9Hk6HzbCPK9u97cVmFUoHzqnHkP4Zbrc3yj","Score":60,"SharedSubnets":6,"Subnets":"00000200000000000100000410084000"},{"Peer":"16Uiu2HAkwr7G5BEks3gjhtoU3WkDRNXUhiBpzW48tQTr8XvQhCLX","Score":60,"SharedSubnets":7,"Subnets":"82000000000084000000000000410008"},{"Peer":"16Uiu2HAmFQdGq8XHndxZo6VvUicBjvikarjVzdVFAbSnQ8GuJNxF","Score":61,"SharedSubnets":9,"Subnets":"00048802880000000001800000000200"},{"Peer":"16Uiu2HAm2Z1eWNeNmaTeMHDPB4WB9HaHUHXzerrqei9SijQmJXXo","Score":61,"SharedSubnets":17,"Subnets":"00401000604004480480010284a00001"},{"Peer":"16Uiu2HAm3v7wh42EwULT9ftdY1TvmRt8giVWpwjiLdajp2kjQEbi","Score":61,"SharedSubnets":9,"Subnets":"80000021000020080800000100280000"},{"Peer":"16Uiu2HAmCbi6DTk49BEakGLi9LysHuLkkjAas1mDMrtJhL9VoA6K","Score":61,"SharedSubnets":9,"Subnets":"00010808009001040000000020000002"},{"Peer":"16Uiu2HAmV3SQqKrjVxNgXVswcZuzNN6D6zYPZqRiXH5pJqagScz2","Score":61,"SharedSubnets":9,"Subnets":"05042004480004000000000080000000"},{"Peer":"16Uiu2HAmFhNizAZxBjKfry2vYxyidPtPH6QVZc6YVLGd76yxYYFi","Score":61,"SharedSubnets":9,"Subnets":"80000021000020080800000100280000"},{"Peer":"16Uiu2HAmAwcCrVSYYop7wAK2jJaWwd1NmkrRDyjTfNuFHqWaa9j8","Score":61,"SharedSubnets":9,"Subnets":"00000004040480040000002000012100"},{"Peer":"16Uiu2HAm9Eo76CBEX9fnsx3fy2PGmpnw7TSEyf7K8q8UMKyPqye4","Score":62,"SharedSubnets":11,"Subnets":"10000100500200000080200284000400"},{"Peer":"16Uiu2HAm9nRGV7wUfWeHAUie6iS198RMq6EJEGex1b489KRn4p76","Score":62,"SharedSubnets":19,"Subnets":"160030062c4c00400202004000001001"},{"Peer":"16Uiu2HAmFA7dF56C3Ca3Tqimyq5fyDwfvimKrfdgDQeWG7z2L3TZ","Score":63,"SharedSubnets":16,"Subnets":"08028000100801020018014800409001"},{"Peer":"16Uiu2HAm6FPFWfYkZvu51GzSoMtbx8sg4r7EEezv1KgKCy1FmXzh","Score":64,"SharedSubnets":18,"Subnets":"80000021090820480800000124280610"},{"Peer":"16Uiu2HAkuicjxoqeDDUrsdiY4E3ceqprCgK5iexhFSo7f2gSKXBt","Score":64,"SharedSubnets":14,"Subnets":"002400442040100400000000c6800800"},{"Peer":"16Uiu2HAmT3NrtubcBLDBzVfZoBSmr5Q72s11KTYULfs7WmJxNQzQ","Score":64,"SharedSubnets":27,"Subnets":"040120180010124c16f0400348009401"},{"Peer":"16Uiu2HAmCH7wX7MvcH339VyqFo82C1Kf3GDnUDG9ZAK8YK9hiRCS","Score":64,"SharedSubnets":27,"Subnets":"040120180010124c16f0400348009401"},{"Peer":"16Uiu2HAm92C2wkqaMDP6Aijw7jUazY3z2Fic4TeKaK2ADMXiQPKF","Score":65,"SharedSubnets":21,"Subnets":"0208000200004180cc11801700006081"},{"Peer":"16Uiu2HAmP1GNde7uC5UQA6rX1akEFRG53DhYiuddDSjDTffbjdb5","Score":66,"SharedSubnets":22,"Subnets":"020000000020a4c8842010300d30400c"},{"Peer":"16Uiu2HAmQCWth9HD1pnu8w3jZ2y77FxLPDhgAud37zUGhhN7y641","Score":66,"SharedSubnets":18,"Subnets":"12000504b04220000210200204000420"},{"Peer":"16Uiu2HAm3VoGtru359raxR2w8z6Up9uPg8TwL1S7nWXm7CaDJ2kR","Score":67,"SharedSubnets":28,"Subnets":"013101000511d120044020118401ca03"},{"Peer":"16Uiu2HAmAHvwQtgcKzoJfHbDLN8ydh4wQVHDGiCrMXAcRuL2jTjt","Score":71,"SharedSubnets":38,"Subnets":"aa8104423c21c260420568a843020826"}]
[2,7,4,4,5,2,0,6,7,2,4,2,1,4,1,1,4,4,2,2,4,7,1,2,5,7,5,4,4,5,5,1,2,0,4,5,5,5,4,3,4,2,2,4,4,2,5,2,4,4,4,4,7,5,3,7,0,2,5,7,1,4,8,3,1,5,6,4,3,0,2,4,3,1,2,2,6,5,8,4,2,0,0,2,2,5,3,4,7,7,3,2,3,5,3,2,2,3,7,5,2,3,6,7,4,1,0,5,2,5,4,2,1,3,6,3,5,4,4,5,8,6,2,4,1,2,1,1]`

var ssvNode2Dump13_08 = `ffffffffffffffffffffffffffffffff
[{"Peer":"16Uiu2HAmTNWo4henNoHdVwcA2FBzS7nh5upjHjstafCAvbtYT6g9","Score":14,"SharedSubnets":128,"Subnets":"ffffffffffffffffffffffffffffffff"},{"Peer":"16Uiu2HAmUHiQMemRv1AjyLid17Z3Rvbe9LekqjoWaVJyL3drmKh8","Score":14,"SharedSubnets":128,"Subnets":"ffffffffffffffffffffffffffffffff"},{"Peer":"16Uiu2HAmT3NrtubcBLDBzVfZoBSmr5Q72s11KTYULfs7WmJxNQzQ","Score":17,"SharedSubnets":27,"Subnets":"040120180010124c16f0400348009401"},{"Peer":"16Uiu2HAmCH7wX7MvcH339VyqFo82C1Kf3GDnUDG9ZAK8YK9hiRCS","Score":17,"SharedSubnets":27,"Subnets":"040120180010124c16f0400348009401"},{"Peer":"16Uiu2HAmDjpSXor9wGaFbvsTtHoz2afRGPBhSCdEaQBcww5aELov","Score":22,"SharedSubnets":4,"Subnets":"00000000000000000200200010001000"},{"Peer":"16Uiu2HAm41iFvN8CbnRnj4iNQJ3sLoWUEWBVDjQxshmQ6nHujPTG","Score":22,"SharedSubnets":5,"Subnets":"00000000010000000200200010001000"},{"Peer":"16Uiu2HAmQCWth9HD1pnu8w3jZ2y77FxLPDhgAud37zUGhhN7y641","Score":23,"SharedSubnets":18,"Subnets":"12000504b04220000210200204000420"},{"Peer":"16Uiu2HAmSjCmXGkqjziiM3rKtMEuTExBfKVzYwvYGhsMNzmrVvi6","Score":24,"SharedSubnets":1,"Subnets":"00002000000000000000000000000000"},{"Peer":"16Uiu2HAm6A6NoPJqneZz3VJdHp72nXjXFDWd9qWJ1aJGzymhL4hn","Score":24,"SharedSubnets":1,"Subnets":"00002000000000000000000000000000"},{"Peer":"16Uiu2HAkyfXMGd18iGWQ8DDL8Vowx5XV1wxNzAtF8Yok5MWfo84y","Score":25,"SharedSubnets":3,"Subnets":"00010000000000000020000000080000"},{"Peer":"16Uiu2HAmCVXoohQ5zSGbzSHVLYwBmNb5uDFZ8LJ36XSpzUELXeYV","Score":25,"SharedSubnets":3,"Subnets":"80000000000108000000000000000000"},{"Peer":"16Uiu2HAkwr7G5BEks3gjhtoU3WkDRNXUhiBpzW48tQTr8XvQhCLX","Score":25,"SharedSubnets":7,"Subnets":"82000000000084000000000000410008"},{"Peer":"16Uiu2HAm9Eo76CBEX9fnsx3fy2PGmpnw7TSEyf7K8q8UMKyPqye4","Score":25,"SharedSubnets":11,"Subnets":"10000100500200000080200284000400"},{"Peer":"16Uiu2HAmCaJ1E7Pd2mAKHGyDqXinbNFqcjrw4zbwakJmdKWbY9xY","Score":25,"SharedSubnets":2,"Subnets":"00000040000010000000000000000000"},{"Peer":"16Uiu2HAm1UReWCXi4gUtYzF5YE85CUyNBhmUJWrVnAeUwF9ReUsG","Score":25,"SharedSubnets":3,"Subnets":"02000000000000000020100000000000"},{"Peer":"16Uiu2HAmFbJk2puSWWuNDuLUEUwPjPTdkyg4j11EUJYEgT4iowZq","Score":25,"SharedSubnets":2,"Subnets":"00010000000000000000000000000002"},{"Peer":"16Uiu2HAm7xH8hhmxsF4j3Bz6G7cFNS1kP5x2BA5zsXB9wz7QJ2ge","Score":25,"SharedSubnets":2,"Subnets":"00000200000010000000000000000000"},{"Peer":"16Uiu2HAm9nRGV7wUfWeHAUie6iS198RMq6EJEGex1b489KRn4p76","Score":25,"SharedSubnets":19,"Subnets":"160030062c4c00400202004000001001"},{"Peer":"16Uiu2HAm5rC4Y1LzvNM2YRiUyfNptCrsMiAEdzF1XvA3q77gd48K","Score":25,"SharedSubnets":3,"Subnets":"00000000000000000000800200000002"},{"Peer":"16Uiu2HAm6JxaiyD8rY6YXUmaPQmYNf1nSxmn4qzByv44gaPgCHZa","Score":26,"SharedSubnets":1,"Subnets":"00000000000100000000000000000000"},{"Peer":"16Uiu2HAm3YEQeDtaYfDbT2jABtsAgjXRTseqkuKRR6g3bANAaifH","Score":26,"SharedSubnets":1,"Subnets":"00000000000000000000000000000100"},{"Peer":"16Uiu2HAm3BtFC7hzip7czpo6p15iTUPZ2T9KAf1ECue1akDPmewL","Score":26,"SharedSubnets":4,"Subnets":"00004002000080000010000000000000"},{"Peer":"16Uiu2HAmHkxX6KiF7Q4agUSEQmzQMC1eDDt9DPdVFuEPNMPKfsEU","Score":26,"SharedSubnets":5,"Subnets":"00000022000000000040000080000008"},{"Peer":"16Uiu2HAm8WhQjnCi2dz3gY19gUFxEP3FmiXjjS69qJn7Fa6NESRe","Score":26,"SharedSubnets":1,"Subnets":"00020000000000000000000000000000"},{"Peer":"16Uiu2HAmBLvt41t3rDd2PUvMovAtp4vrNi2e312nRgurr1vK2S6v","Score":26,"SharedSubnets":1,"Subnets":"00000000000000000000080000000000"},{"Peer":"16Uiu2HAmPhc4cCBbeVayDp5jmWgs6GseqPMbh6GJn4f6VcrJUfeZ","Score":26,"SharedSubnets":5,"Subnets":"00000041000000008040000000400000"},{"Peer":"16Uiu2HAm5u7aGYiR5b2t1EJGhh2ZwCLEXWGKsAXPnvuqK7AG8Xno","Score":26,"SharedSubnets":1,"Subnets":"00000000000000000000000000000002"},{"Peer":"16Uiu2HAmGa5eobff1pHVh5ez6XZGyt7bdaEvE2WbZHsNqV6pD2Pf","Score":26,"SharedSubnets":1,"Subnets":"00000000000000000000000008000000"},{"Peer":"16Uiu2HAm11JQtkxDUMy7ke6YKSRbKrf7w3dBT6YtoSPtpTQuRvvA","Score":26,"SharedSubnets":1,"Subnets":"00000000100000000000000000000000"},{"Peer":"16Uiu2HAmKSKKHNcUhRQF4dK3bA8aPtM5SYEDEPt9gdbkdxwJYFB2","Score":26,"SharedSubnets":5,"Subnets":"00000000000000020000002000013000"},{"Peer":"16Uiu2HAm3v7wh42EwULT9ftdY1TvmRt8giVWpwjiLdajp2kjQEbi","Score":26,"SharedSubnets":9,"Subnets":"80000021000020080800000100280000"},{"Peer":"16Uiu2HAmG8SvC3HBtuwnTszcgpySMSKVxMEm4CSn7Acr62eczLFi","Score":26,"SharedSubnets":4,"Subnets":"00200000000000000000006000000400"},{"Peer":"16Uiu2HAmVHni9pncdYrbRgAar9NiRgEa4SpbHLmHU5wMaNDedWkc","Score":26,"SharedSubnets":5,"Subnets":"08000000000000000008000008008001"},{"Peer":"16Uiu2HAmLsDqf3oGdqCU37TyAo3yRtEju7jfz8LuYMwtyzMcA2yL","Score":26,"SharedSubnets":4,"Subnets":"00000000004010000000008002000000"},{"Peer":"16Uiu2HAm92C2wkqaMDP6Aijw7jUazY3z2Fic4TeKaK2ADMXiQPKF","Score":26,"SharedSubnets":21,"Subnets":"0208000200004180cc11801700006081"},{"Peer":"16Uiu2HAm7XHmHAwj7BTWyFtpyPEHqFLYqtmTc9HqrdqHQu9dAMS2","Score":26,"SharedSubnets":4,"Subnets":"00000210000000200000200000000000"},{"Peer":"16Uiu2HAkvupTFCYj7v22niTAwsFQYFVkYUSNsTzPmk79dnYcsdvf","Score":26,"SharedSubnets":1,"Subnets":"00000000000000000000000000000040"},{"Peer":"16Uiu2HAm876jaEMj5MNDQdftgUxyQwuuArwpVFrnveqXZCxbMprd","Score":26,"SharedSubnets":1,"Subnets":"00000000000008000000000000000000"},{"Peer":"16Uiu2HAmFhNizAZxBjKfry2vYxyidPtPH6QVZc6YVLGd76yxYYFi","Score":26,"SharedSubnets":9,"Subnets":"80000021000020080800000100280000"},{"Peer":"16Uiu2HAm3opMtmf3DBNJMiHvenciUeEMjiTr6gdxHXwQkmn48y3Q","Score":26,"SharedSubnets":1,"Subnets":"08000000000000000000000000000000"},{"Peer":"16Uiu2HAmBDFQc1edFKAT6PkyvvbH4PtoEVpkLuPUhqD4mJ4cijSu","Score":26,"SharedSubnets":1,"Subnets":"00000000000008000000000000000000"},{"Peer":"16Uiu2HAmLLRhcZ8eLV4TSn2peoxLCGyxi8Nzd7q1fMsjqD91hHf2","Score":26,"SharedSubnets":5,"Subnets":"00000041000000008040000000400000"},{"Peer":"16Uiu2HAkxwYwawPqrcF61c8DBq8TZqXjiizig6V5bheUobMHRaEx","Score":27,"SharedSubnets":2,"Subnets":"00000000000008000000000000100000"},{"Peer":"16Uiu2HAmNc8KdfiNXeaVA6rVJ5fj1nKRmx7VRA97C6AMZFYRUuUv","Score":27,"SharedSubnets":14,"Subnets":"82008080184000202015000820000000"},{"Peer":"16Uiu2HAm8dg33RBB7hQpMXVeUNjNT2TaLDWhmWg4DoQMounPdoVu","Score":27,"SharedSubnets":2,"Subnets":"00000000000000800004000000000000"},{"Peer":"16Uiu2HAmELHczR4KGMBwYBv9ew426iYp1Js11zGZkAQFboXdtkLU","Score":27,"SharedSubnets":2,"Subnets":"20000000000000000000000400000000"},{"Peer":"16Uiu2HAmEeveoth116XcpuNH5P6Vzezd11RcKTAyvLjsKJmxGj59","Score":27,"SharedSubnets":2,"Subnets":"00000200000000200000000000000000"},{"Peer":"16Uiu2HAkwoZZuLHktUXts3zjaEZLYM5noSvqEwkvvvELUhLwTVyk","Score":27,"SharedSubnets":2,"Subnets":"00080000000000000000000040000000"},{"Peer":"16Uiu2HAmJa7gNYQCumkMAxWZT5kb99VJfPdM99ZZacv8kbqKzVk9","Score":27,"SharedSubnets":2,"Subnets":"00000008800000000000000000000000"},{"Peer":"16Uiu2HAmCbi6DTk49BEakGLi9LysHuLkkjAas1mDMrtJhL9VoA6K","Score":28,"SharedSubnets":9,"Subnets":"00010808009001040000000020000002"},{"Peer":"16Uiu2HAmKPZnTQL89qebAkZkJNm4PrW8mmC3LteUgDhhcHJsiPEh","Score":28,"SharedSubnets":8,"Subnets":"10201000000000001040800000002040"},{"Peer":"16Uiu2HAm2e9cXq7HEUSGcZpbETwjhxdkFjvoiufR6KaZMcXUHdFg","Score":28,"SharedSubnets":4,"Subnets":"00040080008000000000000020000000"},{"Peer":"16Uiu2HAmV3SQqKrjVxNgXVswcZuzNN6D6zYPZqRiXH5pJqagScz2","Score":28,"SharedSubnets":9,"Subnets":"05042004480004000000000080000000"},{"Peer":"16Uiu2HAm3VoGtru359raxR2w8z6Up9uPg8TwL1S7nWXm7CaDJ2kR","Score":28,"SharedSubnets":28,"Subnets":"013101000511d120044020118401ca03"},{"Peer":"16Uiu2HAmFA7dF56C3Ca3Tqimyq5fyDwfvimKrfdgDQeWG7z2L3TZ","Score":28,"SharedSubnets":16,"Subnets":"08028000100801020018014800409001"},{"Peer":"16Uiu2HAm6FPFWfYkZvu51GzSoMtbx8sg4r7EEezv1KgKCy1FmXzh","Score":29,"SharedSubnets":18,"Subnets":"80000021090820480800000124280610"},{"Peer":"16Uiu2HAm7EgFz3Zsg9Hk6HzbCPK9u97cVmFUoHzqnHkP4Zbrc3yj","Score":29,"SharedSubnets":6,"Subnets":"00000200000000000100000410084000"},{"Peer":"16Uiu2HAmFQdGq8XHndxZo6VvUicBjvikarjVzdVFAbSnQ8GuJNxF","Score":30,"SharedSubnets":9,"Subnets":"00048802880000000001800000000200"},{"Peer":"16Uiu2HAmAwcCrVSYYop7wAK2jJaWwd1NmkrRDyjTfNuFHqWaa9j8","Score":30,"SharedSubnets":9,"Subnets":"00000004040480040000002000012100"},{"Peer":"16Uiu2HAkuicjxoqeDDUrsdiY4E3ceqprCgK5iexhFSo7f2gSKXBt","Score":31,"SharedSubnets":14,"Subnets":"002400442040100400000000c6800800"}]
[4,8,6,5,7,3,2,8,8,4,6,4,3,6,2,2,5,6,3,4,5,8,3,5,7,7,7,6,6,6,6,4,5,2,5,7,7,5,5,5,5,4,4,5,6,2,7,4,6,4,4,6,9,6,4,7,2,4,7,7,3,6,6,4,3,8,6,6,5,3,3,5,5,3,4,4,9,6,9,5,3,2,2,3,3,8,4,6,9,8,5,4,4,5,5,3,2,4,7,6,6,6,7,7,6,2,2,7,3,5,6,3,4,5,8,4,9,6,5,7,9,7,2,4,3,3,4,3]`
