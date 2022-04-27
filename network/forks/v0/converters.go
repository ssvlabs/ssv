package v0

import (
	"encoding/hex"
	"encoding/json"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/utils/format"
	"github.com/pkg/errors"
)

// ToV1Message converts an old message to v1
func ToV1Message(msgV0 *network.Message) (*message.SSVMessage, error) {
	msg := message.SSVMessage{}

	switch msgV0.Type {
	case network.NetworkMsg_SyncType:
		msg.MsgType = message.SSVSyncMsgType
		if msgV0.SyncMessage != nil {
			identifier := toIdentifierV1(msgV0.SyncMessage.GetLambda())
			msg.ID = identifier
			syncMsg := new(message.SyncMessage)
			syncMsg.Params = new(message.SyncParams)
			syncMsg.Params.Identifier = identifier
			params := msgV0.SyncMessage.GetParams()
			syncMsg.Params.Height = make([]message.Height, 0)
			for _, p := range params {
				syncMsg.Params.Height = append(syncMsg.Params.Height, message.Height(p))
			}
			for _, sm := range msgV0.SyncMessage.GetSignedMessages() {
				signed := ToSignedMessageV1(sm)
				if signed.Message != nil {
					syncMsg.Data = append(syncMsg.Data, signed)
				}
			}
			if err := msgV0.SyncMessage.Error; len(err) > 0 {
				syncMsg.Status = message.StatusError
			}
			switch msgV0.SyncMessage.Type {
			case network.Sync_GetHighestType:
				syncMsg.Protocol = message.LastDecidedType
			case network.Sync_GetLatestChangeRound:
				syncMsg.Protocol = message.LastChangeRoundType
			case network.Sync_GetInstanceRange:
				syncMsg.Protocol = message.DecidedHistoryType
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
		postConMsg := toSignedPostConsensusMessageV1(msgV0.SignedMessage)
		data, err := postConMsg.Encode()
		if err != nil {
			return nil, err
		}
		msg.Data = data
		if identifierV0 := msgV0.SignedMessage.GetMessage().GetLambda(); identifierV0 != nil {
			msg.ID = toIdentifierV1(identifierV0)
		}
		return &msg, nil
	case network.NetworkMsg_IBFTType:
		msg.MsgType = message.SSVConsensusMsgType
	case network.NetworkMsg_DecidedType:
		msg.MsgType = message.SSVDecidedMsgType
	}

	if msgV0.SignedMessage != nil {
		signed := ToSignedMessageV1(msgV0.SignedMessage)
		data, err := signed.Encode()
		if err != nil {
			return nil, err
		}
		msg.Data = data
		if len(msg.ID) == 0 {
			msg.ID = signed.Message.Identifier
		}
	}

	return &msg, nil
}

func toSignedPostConsensusMessageV1(sm *proto.SignedMessage) *message.SignedPostConsensusMessage {
	signed := new(message.SignedPostConsensusMessage)
	consensus := &message.PostConsensusMessage{
		Height:          message.Height(sm.Message.SeqNumber),
		DutySignature:   sm.GetSignature(),
		DutySigningRoot: nil,
	}

	var signers []message.OperatorID
	for _, signer := range sm.GetSignerIds() {
		signers = append(signers, message.OperatorID(signer))
	}
	consensus.Signers = signers

	signed.Message = consensus
	signed.Signers = signers
	signed.Signature = sm.GetSignature() // TODO should be message sign and not duty sign

	return signed
}

func ToSignedMessageV1(sm *proto.SignedMessage) *message.SignedMessage {
	signed := new(message.SignedMessage)
	signed.Signature = sm.GetSignature()
	signers := sm.GetSignerIds()
	for _, s := range signers {
		signed.Signers = append(signed.Signers, message.OperatorID(s))
	}
	if msg := sm.GetMessage(); msg != nil {
		signed.Message = new(message.ConsensusMessage)
		data := msg.GetValue()
		target := make([]byte, len(data))
		copy(target, data)
		signed.Message.Data = target
		signed.Message.Round = message.Round(msg.GetRound())
		signed.Message.Identifier = toIdentifierV1(msg.GetLambda())
		signed.Message.Height = message.Height(msg.GetSeqNumber())
		switch msg.GetType() {
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
	identifierV0 := toIdentifierV0(msg.GetIdentifier())
	switch msg.GetType() {
	case message.SSVDecidedMsgType:
		v0Msg.Type = network.NetworkMsg_DecidedType // TODO need to provide the proper type (under consensus or post consensus?)
		fallthrough
	case message.SSVConsensusMsgType:
		if v0Msg.Type != network.NetworkMsg_DecidedType {
			v0Msg.Type = network.NetworkMsg_IBFTType
		}
		signedMsg := &message.SignedMessage{}
		if err := signedMsg.Decode(msg.GetData()); err != nil {
			return nil, errors.Wrap(err, "could not decode consensus signed message")
		}

		v0Msg.SignedMessage = toSignedMessageV0(signedMsg, identifierV0)
		switch v0Msg.SignedMessage.GetMessage().GetType() {
		case proto.RoundState_ChangeRound:
			v0Msg.Type = network.NetworkMsg_IBFTType
		case proto.RoundState_Decided:
			v0Msg.Type = network.NetworkMsg_DecidedType
		default:
		}
		break // cause of fallthrough
	//return v.processConsensusMsg(dutyRunner, signedMsg)
	case message.SSVPostConsensusMsgType:
		v0Msg.Type = network.NetworkMsg_SignatureType
		signedMsg := &message.SignedPostConsensusMessage{}
		if err := signedMsg.Decode(msg.GetData()); err != nil {
			return nil, errors.Wrap(err, "could not get post consensus Message from network Message")
		}
		v0Msg.SignedMessage = toSignedMessagePostConsensusV0(signedMsg, identifierV0)
	//return v.processPostConsensusSig(dutyRunner, signedMsg)
	case message.SSVSyncMsgType:
		v0Msg.Type = network.NetworkMsg_SyncType
		syncMsg := &message.SyncMessage{}
		if err := syncMsg.Decode(msg.GetData()); err != nil {
			return nil, errors.Wrap(err, "could not decode consensus signed message")
		}
		v0Msg.SyncMessage = new(network.SyncMessage)
		if len(syncMsg.Params.Height) > 0 {
			v0Msg.SyncMessage.Params = make([]uint64, 1)
			v0Msg.SyncMessage.Params[0] = uint64(syncMsg.Params.Height[0])
			if len(syncMsg.Params.Height) > 1 {
				v0Msg.SyncMessage.Params = append(v0Msg.SyncMessage.Params, uint64(syncMsg.Params.Height[1]))
			}
		}
		v0Msg.SyncMessage.Lambda = identifierV0
		switch syncMsg.Status {
		case message.StatusSuccess:
			v0Msg.SyncMessage.SignedMessages = make([]*proto.SignedMessage, 0)
			for _, smsg := range syncMsg.Data {
				v0Msg.SyncMessage.SignedMessages = append(v0Msg.SyncMessage.SignedMessages, toSignedMessageV0(smsg, identifierV0))
			}
		case message.StatusNotFound:
			v0Msg.SyncMessage.SignedMessages = make([]*proto.SignedMessage, 0)
		default:
			v0Msg.SyncMessage.Error = "error"
		}
		switch syncMsg.Protocol {
		case message.LastDecidedType:
			v0Msg.SyncMessage.Type = network.Sync_GetHighestType
		case message.LastChangeRoundType:
			v0Msg.SyncMessage.Type = network.Sync_GetLatestChangeRound
		case message.DecidedHistoryType:
			v0Msg.SyncMessage.Type = network.Sync_GetInstanceRange
		}
	default:
		return nil, errors.New("unknown msg")
	}

	return v0Msg, nil
}

func toSignedMessageV0(signedMsg *message.SignedMessage, identifierV0 []byte) *proto.SignedMessage {
	signedMsgV0 := &proto.SignedMessage{}
	signedMsgV0.Message = &proto.Message{
		Round:     uint64(signedMsg.Message.Round),
		Lambda:    identifierV0,
		SeqNumber: uint64(signedMsg.Message.Height),
		Value:     make([]byte, len(signedMsg.Message.Data)),
	}
	copy(signedMsgV0.Message.Value, signedMsg.Message.Data)

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

func toSignedMessagePostConsensusV0(signedMsg *message.SignedPostConsensusMessage, identifierV0 []byte) *proto.SignedMessage {
	signedMsgV0 := &proto.SignedMessage{}
	signedMsgV0.Message = &proto.Message{
		Lambda:    identifierV0,
		SeqNumber: uint64(signedMsg.Message.Height),
		// TODO: complete
	}
	signedMsgV0.Signature = signedMsg.Message.DutySignature
	for _, signer := range signedMsg.Signers {
		signedMsgV0.SignerIds = append(signedMsgV0.SignerIds, uint64(signer))
	}
	return signedMsgV0
}

func toIdentifierV0(mid message.Identifier) []byte {
	return []byte(format.IdentifierFormat(mid.GetValidatorPK(), mid.GetRoleType().String()))
}

func toIdentifierV1(old []byte) message.Identifier {
	pk, rt := format.IdentifierUnformat(string(old))
	pkraw, err := hex.DecodeString(pk)
	if err != nil {
		return nil
	}
	return message.NewIdentifier(pkraw, message.RoleTypeFromString(rt))
}

// non network convertors

// ConsensusToV0ProtoMessage converts consensus msg to proto msg
func ConsensusToV0ProtoMessage(cm *message.ConsensusMessage) ([]byte, error) {
	oldMsg := &proto.Message{
		Round:     uint64(cm.Round),
		Lambda:    toIdentifierV0(cm.Identifier),
		SeqNumber: uint64(cm.Height),
	}

	switch cm.MsgType {
	case message.ProposalMsgType:
		oldMsg.Type = proto.RoundState_PrePrepare
		if p, err := cm.GetProposalData(); err != nil {
			return nil, err
		} else {
			oldMsg.Value = p.Data
		}
	case message.PrepareMsgType:
		oldMsg.Type = proto.RoundState_Prepare
		if p, err := cm.GetPrepareData(); err != nil {
			return nil, err
		} else {
			oldMsg.Value = p.Data
		}
	case message.CommitMsgType:
		oldMsg.Type = proto.RoundState_Commit
		if c, err := cm.GetCommitData(); err != nil {
			return nil, err
		} else {
			oldMsg.Value = c.Data
		}
	case message.RoundChangeMsgType:
		oldMsg.Type = proto.RoundState_ChangeRound
		if cr, err := cm.GetRoundChangeData(); err != nil {
			return nil, err
		} else {
			oldMsg.Value = cr.PreparedValue
		}
	default:
		return nil, errors.Errorf("consensus type is not known. type - %s", cm.MsgType.String())
	}

	return oldMsg.SigningRoot()
}

// PostConsensusToV0ProtoMessage converts consensus msg to proto msg
func PostConsensusToV0ProtoMessage(pcm *message.PostConsensusMessage) ([]byte, error) {
	/*oldMsg := &proto.Message{
		Round:     uint64(pcm.Height),
		Lambda:    toIdentifierV0(pcm.Identifier),
		SeqNumber: uint64(pcm.Height),
	}

	switch pcm.MsgType {
	case message.ProposalMsgType:
		oldMsg.Type = proto.RoundState_PrePrepare
		if p, err := pcm.GetProposalData(); err != nil {
			return nil, err
		} else {
			oldMsg.Value = p.Data
		}
	case message.PrepareMsgType:
		oldMsg.Type = proto.RoundState_Prepare
		if p, err := pcm.GetPrepareData(); err != nil {
			return nil, err
		} else {
			oldMsg.Value = p.Data
		}
	case message.CommitMsgType:
		oldMsg.Type = proto.RoundState_Commit
		if c, err := pcm.GetCommitData(); err != nil {
			return nil, err
		} else {
			oldMsg.Value = c.Data
		}
	case message.RoundChangeMsgType:
		oldMsg.Type = proto.RoundState_ChangeRound
		if cr, err := pcm.GetRoundChangeData(); err != nil {
			return nil, err
		} else {
			oldMsg.Value = cr.PreparedValue
		}
	default:
		return nil, errors.Errorf("consensus type is not known. type - %s", pcm.MsgType.String())
	}

	return oldMsg.SigningRoot()*/
	panic("not implemented yet")
}
