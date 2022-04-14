package v0

import (
	"encoding/json"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/pkg/errors"
)

// ToV1Message converts an old message to v1
func ToV1Message(msgV0 *network.Message) (*message.SSVMessage, error) {
	msg := message.SSVMessage{}

	switch msgV0.Type {
	case network.NetworkMsg_SyncType:
		msg.MsgType = message.SSVSyncMsgType
		if msgV0.SyncMessage != nil {
			msg.ID = msgV0.SyncMessage.GetLambda()
			syncMsg := new(message.SyncMessage)
			syncMsg.Params = new(message.SyncParams)
			syncMsg.Params.Identifier = msgV0.SyncMessage.GetLambda()
			params := msgV0.SyncMessage.GetParams()
			for i, p := range params {
				syncMsg.Params.Height[i] = message.Height(p)
			}
			for _, sm := range msgV0.SyncMessage.GetSignedMessages() {
				signed := toSignedMessageV1(sm)
				if signed.Message != nil {
					syncMsg.Data = append(syncMsg.Data, signed)
					msg.ID = signed.Message.Identifier
				}
			}
			if err := msgV0.SyncMessage.Error; len(err) > 0 {
				syncMsg.Status = message.StatusError
			}
			data, err := json.Marshal(syncMsg)
			if err != nil {
				return nil, err
			}
			msg.Data = data
			//msg.ID = msgV0.SyncMessage.GetLambda()
		}
		return &msg, nil
	case network.NetworkMsg_SignatureType:
		msg.MsgType = message.SSVPostConsensusMsgType
	case network.NetworkMsg_IBFTType:
		fallthrough
	case network.NetworkMsg_DecidedType:
		msg.MsgType = message.SSVConsensusMsgType
	}

	if msgV0.SignedMessage != nil {
		signed := toSignedMessageV1(msgV0.SignedMessage)
		data, err := json.Marshal(signed)
		if err != nil {
			return nil, err
		}
		msg.Data = data
		if signed.Message.Identifier != nil {
			msg.ID = signed.Message.Identifier
		}
	}

	return &msg, nil
}

func toSignedMessageV1(sm *proto.SignedMessage) *message.SignedMessage {
	signed := new(message.SignedMessage)
	signed.Signature = sm.GetSignature()
	signers := sm.GetSignerIds()
	for _, s := range signers {
		signed.Signers = append(signed.Signers, beacon.OperatorID(s))
	}
	if sm.GetMessage() != nil {
		signed.Message = new(message.ConsensusMessage)
		signed.Message.Data = sm.Message.GetValue()
		signed.Message.Round = message.Round(sm.Message.GetRound())
		signed.Message.Identifier = sm.Message.GetLambda()
		signed.Message.Height = message.Height(sm.Message.GetSeqNumber())
		switch sm.Message.GetType() {
		case proto.RoundState_NotStarted:
			// TODO
		case proto.RoundState_PrePrepare:
			signed.Message.MsgType = message.ProposalMsgType
		case proto.RoundState_Prepare:
			signed.Message.MsgType = message.PrepareMsgType
		case proto.RoundState_Commit:
			signed.Message.MsgType = message.CommitMsgType
		case proto.RoundState_ChangeRound:
			signed.Message.MsgType = message.RoundChangeMsgType
		//case proto.RoundState_Decided:
		//	signed.Message.MsgType = message.DecidedMsgType
		case proto.RoundState_Stopped:
			// TODO
		}
	}
	return signed
}

// ToV0Message converts v1 message to v0
func ToV0Message(msg *message.SSVMessage) (*network.Message, error) {
	v0Msg := &network.Message{}

	switch msg.GetType() {
	case message.SSVConsensusMsgType:
		v0Msg.Type = network.NetworkMsg_IBFTType
		signedMsg := &message.SignedMessage{}
		if err := signedMsg.Decode(msg.GetData()); err != nil {
			return nil, errors.Wrap(err, "could not decode consensus signed message")
		}

		v0Msg.SignedMessage = toSignedMessageV0(signedMsg, msg.ID)
		switch v0Msg.SignedMessage.GetMessage().GetType() {
		case proto.RoundState_ChangeRound:
			v0Msg.Type = network.NetworkMsg_IBFTType
		case proto.RoundState_Decided:
			v0Msg.Type = network.NetworkMsg_DecidedType
		default:
		}
		//return v.processConsensusMsg(dutyRunner, signedMsg)
	case message.SSVPostConsensusMsgType:
		v0Msg.Type = network.NetworkMsg_DecidedType // TODO need to provide the proper type (under consensus or post consensus?)
		v0Msg.Type = network.NetworkMsg_SignatureType
		signedMsg := &message.SignedPostConsensusMessage{}
		if err := signedMsg.Decode(msg.GetData()); err != nil {
			return nil, errors.Wrap(err, "could not get post consensus Message from network Message")
		}
		v0Msg.SignedMessage = toSignedMessagePostConsensusV0(signedMsg, msg.ID)

		//return v.processPostConsensusSig(dutyRunner, signedMsg)
	case message.SSVSyncMsgType:
		v0Msg.Type = network.NetworkMsg_SyncType
		syncMsg := &message.SyncMessage{}
		if err := syncMsg.Decode(msg.GetData()); err != nil {
			return nil, errors.Wrap(err, "could not decode consensus signed message")
		}
		v0Msg.SyncMessage.Params = make([]uint64, 0)
		if len(syncMsg.Params.Height) > 0 {
			v0Msg.SyncMessage.Params[0] = uint64(syncMsg.Params.Height[0])
			if len(syncMsg.Params.Height) > 1 {
				v0Msg.SyncMessage.Params[1] = uint64(syncMsg.Params.Height[1])
			}
		}
		v0Msg.SyncMessage.Lambda = syncMsg.Params.Identifier
		if syncMsg.Status == message.StatusSuccess {
			v0Msg.SyncMessage.SignedMessages = make([]*proto.SignedMessage, 0)
			for _, smsg := range syncMsg.Data {
				v0Msg.SyncMessage.SignedMessages = append(v0Msg.SyncMessage.SignedMessages, toSignedMessageV0(smsg, msg.ID))
			}
		} else {
			v0Msg.SyncMessage.Error = "error"
		}
	default:
		return nil, errors.New("unknown msg")
	}

	return v0Msg, nil
}

func toSignedMessageV0(signedMsg *message.SignedMessage, identifier message.Identifier) *proto.SignedMessage {
	signedMsgV0 := &proto.SignedMessage{}

	signedMsgV0.Message = &proto.Message{
		Round:     uint64(signedMsg.Message.Round),
		Lambda:    identifier,
		SeqNumber: uint64(signedMsg.Message.Height),
		Value:     signedMsg.Message.Data,
	}
	switch signedMsg.Message.MsgType {
	case message.ProposalMsgType:
		signedMsgV0.Message.Type = proto.RoundState_PrePrepare
	case message.PrepareMsgType:
		signedMsgV0.Message.Type = proto.RoundState_Prepare
	case message.CommitMsgType:
		signedMsgV0.Message.Type = proto.RoundState_Commit
	case message.RoundChangeMsgType:
		signedMsgV0.Message.Type = proto.RoundState_ChangeRound
		//case message.DecidedMsgType:
		//	signedMsgV0.Message.Type = proto.RoundState_Decided
	}
	signedMsgV0.Signature = signedMsg.GetSignature()
	for _, signer := range signedMsg.GetSigners() {
		signedMsgV0.SignerIds = append(signedMsgV0.SignerIds, uint64(signer))
	}
	return signedMsgV0
}

func toSignedMessagePostConsensusV0(signedMsg *message.SignedPostConsensusMessage, identifier []byte) *proto.SignedMessage {
	signedMsgV0 := &proto.SignedMessage{}
	signedMsgV0.Message = &proto.Message{
		Lambda:    identifier,
		SeqNumber: uint64(signedMsg.Message.Height),
		// TODO: complete
	}
	signedMsgV0.Signature = signedMsg.GetSignature()
	for _, signer := range signedMsg.Signers {
		signedMsgV0.SignerIds = append(signedMsgV0.SignerIds, uint64(signer))
	}
	return signedMsgV0
}
