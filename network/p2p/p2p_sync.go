package p2p

import (
	"encoding/hex"
	"encoding/json"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/utils/tasks"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

func peerToString(peerID peer.ID) string {
	return peer.Encode(peerID)
}

func peerFromString(peerStr string) (peer.ID, error) {
	return peer.Decode(peerStr)
}

// BroadcastSyncMessage broadcasts a sync message to peers.
// Peer list must not be nil or empty if stream is nil.
// returns a stream closed for writing
func (n *p2pNetwork) sendSyncMessage(stream network.SyncStream, peer peer.ID, msg *network.SyncMessage) (network.SyncStream, error) {
	if stream == nil {
		if len(peer) == 0 {
			return nil, errors.New("peer ID nil")
		}

		s, err := n.host.NewStream(n.ctx, peer, syncStreamProtocol)
		if err != nil {
			return nil, err
		}
		stream = &SyncStream{stream: s}
	}

	// message to bytes
	msgBytes, err := json.Marshal(network.Message{
		SyncMessage: msg,
		Type:        network.NetworkMsg_SyncType,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal message")
	}

	if _, err := stream.Write(msgBytes); err != nil {
		return nil, errors.Wrap(err, "could not write to stream")
	}
	if err := stream.CloseWrite(); err != nil {
		return nil, errors.Wrap(err, "could not close write stream")
	}
	return stream, nil
}

// sendAndReadResponse sends a reques sync msg, waits to a response and parses it. Includes timeout as well
func (n *p2pNetwork) sendAndReadSyncResponse(peer peer.ID, msg *network.SyncMessage) (*network.Message, error) {
	var err error
	stream, err := n.sendSyncMessage(nil, peer, msg)
	if err != nil {
		return nil, errors.Wrap(err, "could not send sync msg")
	}

	// close function for stream
	defer func() {
		if err := stream.Close(); err != nil {
			n.logger.Error("could not close peer stream", zap.Error(err))
		}
	}()

	var resMsg *network.Message
	readMsgData := func() {
		resMsg, err = readMessageData(stream)
	}
	if completed := tasks.ExecWithTimeout(readMsgData, n.cfg.RequestTimeout, n.ctx); !completed {
		n.logger.Debug("sync request timeout")
	} else if resMsg == nil { // no response
		err = errors.New("no response for sync request")
	} else {
		n.logger.Debug("got sync response",
			zap.String("ValidatorPk", hex.EncodeToString(resMsg.SyncMessage.GetValidatorPk())),
			zap.String("FromPeerID", resMsg.SyncMessage.GetFromPeerID()))
	}

	return resMsg, err
}

// GetHighestDecidedInstance asks peers for SyncMessage
func (n *p2pNetwork) GetHighestDecidedInstance(peerStr string, msg *network.SyncMessage) (*network.SyncMessage, error) {
	peerID, err := peerFromString(peerStr)
	if err != nil {
		return nil, err
	}

	res, err := n.sendAndReadSyncResponse(peerID, msg)
	if err != nil {
		return nil, err
	}
	return res.SyncMessage, nil
}

// RespondToHighestDecidedInstance responds to a GetHighestDecidedInstance
func (n *p2pNetwork) RespondToHighestDecidedInstance(stream network.SyncStream, msg *network.SyncMessage) error {
	msg.FromPeerID = n.host.ID().Pretty() // critical
	_, err := n.sendSyncMessage(stream, "", msg)
	return err
}

// GetDecidedByRange returns a list of decided signed messages up to 25 in a batch.
func (n *p2pNetwork) GetDecidedByRange(peerStr string, msg *network.SyncMessage) (*network.SyncMessage, error) {
	peerID, err := peerFromString(peerStr)
	if err != nil {
		return nil, err
	}

	res, err := n.sendAndReadSyncResponse(peerID, msg)
	if err != nil {
		return nil, err
	}
	return res.SyncMessage, nil
}

func (n *p2pNetwork) RespondToGetDecidedByRange(stream network.SyncStream, msg *network.SyncMessage) error {
	msg.FromPeerID = n.host.ID().Pretty() // critical
	_, err := n.sendSyncMessage(stream, "", msg)
	return err
}

// ReceivedSyncMsgChan returns the channel for sync messages
func (n *p2pNetwork) ReceivedSyncMsgChan() <-chan *network.SyncChanObj {
	ls := listener{
		syncCh: make(chan *network.SyncChanObj, MsgChanSize),
	}

	n.listenersLock.Lock()
	n.listeners = append(n.listeners, ls)
	n.listenersLock.Unlock()

	return ls.syncCh
}
