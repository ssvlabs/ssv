package validation

import (
	"sync"
	"time"

	"github.com/bloxapp/ssv-spec/qbft"
	"github.com/bloxapp/ssv-spec/types"
	"github.com/cornelk/hashmap"
	"github.com/libp2p/go-libp2p/core/peer"
	"go.uber.org/zap"
)

type marksCollection struct {
	m *hashmap.Map[messageID, *messageMark]
}

func newMarksCollection() *marksCollection {
	return &marksCollection{
		m: hashmap.New[messageID, *messageMark](),
	}
}

func (c *marksCollection) Get(msgID messageID) (*messageMark, bool) {
	return c.m.Get(msgID)
}

func (c *marksCollection) GetSignerMark(msgID messageID, signer types.OperatorID) (*signerMark, bool) {
	mm, exists := c.m.Get(msgID)
	if !exists {
		return nil, false
	}
	return mm.signerMarks.Get(signer)
}

func (c *marksCollection) GetOrCreateSignerMark(msgID messageID, signer types.OperatorID) (*signerMark, bool) {
	mm, exists := c.m.GetOrInsert(msgID, &messageMark{})
	if !exists {
		mm.signerMarks = hashmap.New[types.OperatorID, *signerMark]()
	}
	return mm.signerMarks.GetOrInsert(signer, &signerMark{})
}

type messageMark struct {
	signerMarks *hashmap.Map[types.OperatorID, *signerMark]
}

type signerMark struct {
	mark
	peerMarks *hashmap.Map[peer.ID, *mark]
}

// TODO: should seperate consensus mark and decided mark? This may take more memory but may remove the need for locks
type mark struct {
	mu sync.RWMutex

	//should be reset when a decided message is received with a larger height
	HighestRound    qbft.Round
	FirstMsgInRound *hashmap.Map[qbft.Round, time.Time]
	MsgTypesInRound *hashmap.Map[qbft.Round, *hashmap.Map[qbft.MessageType, int]]

	HighestDecided    qbft.Height
	DecidedRound      qbft.Round
	Last2DecidedTimes [2]time.Time
	MarkedDecided     int
	// the seen signers of the last decided message
	signers                 map[types.OperatorID]struct{}
	betterOrSimilarMsgCount int
}

// DurationFromLast2Decided returns duration for the last 3 decided including a new decided added at time.Now()
func (mark *mark) DurationFromLast2Decided() time.Duration {
	// If Last2DecidedTimes[1] is empty then it is fine to return a very long duration
	return time.Since(mark.Last2DecidedTimes[1])
}

func (mark *mark) addDecidedMark() {
	copy(mark.Last2DecidedTimes[1:], mark.Last2DecidedTimes[:1]) // shift array one up to make room at index 0
	mark.Last2DecidedTimes[0] = time.Now()

	mark.MarkedDecided++
}

// ResetForNewRound prepares round for new round.
// don't forget to lock mark before calling
func (mark *mark) ResetForNewRound(round qbft.Round, msgType qbft.MessageType) {
	mark.HighestRound = round
	if mark.FirstMsgInRound == nil {
		mark.FirstMsgInRound = hashmap.New[qbft.Round, time.Time]()
	}
	mark.FirstMsgInRound.Set(round, time.Now())
	if mark.MsgTypesInRound == nil {
		mark.MsgTypesInRound = hashmap.New[qbft.Round, *hashmap.Map[qbft.MessageType, int]]()
	}
	typesToOccurrences, exists := mark.MsgTypesInRound.Get(round)
	if exists {
		// we clear map instead of creating a new one to avoid memory allocation
		typesToOccurrences.Range(func(key qbft.MessageType, value int) bool {
			return typesToOccurrences.Del(key)
		})
	} else {
		typesToOccurrences = hashmap.New[qbft.MessageType, int]()
		mark.MsgTypesInRound.Insert(round, typesToOccurrences)
	}
	typesToOccurrences.Insert(msgType, 1)
}

func (mark *mark) updateDecidedMark(height qbft.Height, round qbft.Round, msgSignatures []types.OperatorID, plog *zap.Logger) {
	mark.mu.Lock()
	defer mark.mu.Unlock()

	if mark.HighestDecided > height {
		// In staging this condition never happened
		plog.Info("dropping an attempt to update decided mark", zap.Int("highestHeight", int(mark.HighestDecided)),
			zap.Int("height", int(height)))
		return
	}
	// TODO: Here we have an ugly hack to reset the mark for the first height
	// be aware it doesn't cause bugs
	if mark.HighestDecided < height || height == qbft.FirstHeight && mark.MarkedDecided == 0 {
		// reset for new height
		mark.HighestDecided = height
		mark.DecidedRound = round
		// maybe clear map instead of creating a new one
		mark.signers = make(map[types.OperatorID]struct{})
		mark.MarkedDecided = 0
		mark.HighestRound = qbft.NoRound
		mark.betterOrSimilarMsgCount = 0
		mark.FirstMsgInRound = hashmap.New[qbft.Round, time.Time]()
		// TODO: maybe we should clear the map instead of creating a new one..
		// However, clearing a large map may take a lot of time. Also no memory will ever be freed.. which may open an attack vector.
		mark.MsgTypesInRound = hashmap.New[qbft.Round, *hashmap.Map[qbft.MessageType, int]]()
		//TODO maybe clear current array. Probably yes, if 2 different duties come close together
		//mark.Last2DecidedTimes = [2]time.Time{}
	}

	// only update for highest decided
	if mark.HighestDecided == height {
		mark.addDecidedMark()
		for _, signer := range msgSignatures {
			// no need to insert if exists
			mark.signers[signer] = struct{}{}
		}
	}
}
