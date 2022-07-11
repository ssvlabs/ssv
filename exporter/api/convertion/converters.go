package conversion

import (
	"encoding/hex"
	"encoding/json"

	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/exporter/api/network"
	"github.com/bloxapp/ssv/exporter/api/proto"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/utils/format"
	"github.com/bloxapp/ssv/utils/logex"
)

// converters are used to encapsulate the struct of the messages
// that are passed in the network (v0), until v1 fork

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
			syncMsg.Status = message.StatusSuccess
			syncMsg.Params = new(message.SyncParams)
			syncMsg.Params.Identifier = identifier
			params := msgV0.SyncMessage.GetParams()
			syncMsg.Params.Height = make([]message.Height, 0)
			for _, p := range params {
				syncMsg.Params.Height = append(syncMsg.Params.Height, message.Height(p))
			}
			for _, sm := range msgV0.SyncMessage.GetSignedMessages() {
				signed, err := ToSignedMessageV1(sm)
				if err != nil {
					return nil, err
				}
				if signed.Message != nil {
					syncMsg.Data = append(syncMsg.Data, signed)
				}
			}
			if len(syncMsg.Data) == 0 {
				syncMsg.Status = message.StatusNotFound
			}
			if err := msgV0.SyncMessage.Error; len(err) > 0 {
				if err == message.EntryNotFoundError {
					syncMsg.Status = message.StatusNotFound
				} else {
					logex.GetLogger().Warn("sync message error", zap.String("fromPeers", msgV0.SyncMessage.FromPeerID), zap.String("symcType", msgV0.SyncMessage.Type.String()), zap.String("err", err))
					syncMsg.Status = message.StatusError
				}
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
		panic("convert signature type is not supported!!!")
		return &msg, nil
	case network.NetworkMsg_IBFTType:
		msg.MsgType = message.SSVConsensusMsgType
	case network.NetworkMsg_DecidedType:
		msg.MsgType = message.SSVDecidedMsgType
	}

	if msgV0.SignedMessage != nil {
		signed, err := ToSignedMessageV1(msgV0.SignedMessage)
		if err != nil {
			return nil, err
		}
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

// ToSignedMessageV1 converts a signed message from v0 to v1
func ToSignedMessageV1(sm *proto.SignedMessage) (*message.SignedMessage, error) {
	signed := new(message.SignedMessage)
	signed.Signature = sm.GetSignature()
	signers := sm.GetSignerIds()
	for _, s := range signers {
		signed.Signers = message.AppendSigners(signed.Signers, message.OperatorID(s))
	}
	if msg := sm.GetMessage(); msg != nil {
		signed.Message = new(message.ConsensusMessage)
		data := msg.GetValue()
		signed.Message.Round = message.Round(msg.GetRound())
		signed.Message.Identifier = toIdentifierV1(msg.GetLambda())
		signed.Message.Height = message.Height(msg.GetSeqNumber())
		switch msg.GetType() {
		case proto.RoundState_NotStarted:
			// TODO
		case proto.RoundState_PrePrepare:
			signed.Message.MsgType = message.ProposalMsgType
			p, err := (&message.ProposalData{Data: data}).Encode()
			if err != nil {
				return nil, err
			}
			signed.Message.Data = p
		case proto.RoundState_Prepare:
			signed.Message.MsgType = message.PrepareMsgType
			p, err := (&message.PrepareData{Data: data}).Encode()
			if err != nil {
				return nil, err
			}
			signed.Message.Data = p
		case proto.RoundState_Commit:
			signed.Message.MsgType = message.CommitMsgType
			c, err := (&message.CommitData{Data: data}).Encode()
			if err != nil {
				return nil, err
			}
			signed.Message.Data = c
		case proto.RoundState_ChangeRound:
			signed.Message.MsgType = message.RoundChangeMsgType
			rcd, err := toV1ChangeRound(data)
			if err != nil {
				return nil, err
			}
			signed.Message.Data = rcd
		case proto.RoundState_Stopped:
			// TODO
		}
	}
	return signed, nil
}

func toV1ChangeRound(changeRoundData []byte) ([]byte, error) {
	ret := &proto.ChangeRoundData{}
	if err := json.Unmarshal(changeRoundData, ret); err != nil {
		logex.GetLogger().Warn("failed to unmarshal v0 change round struct", zap.Error(err))
		return new(message.RoundChangeData).Encode() // should return empty struct
	}

	var signers []message.OperatorID
	for _, signer := range ret.GetSignerIds() {
		signers = message.AppendSigners(signers, message.OperatorID(signer))
	}

	consensusMsg := &message.ConsensusMessage{}
	if ret.GetJustificationMsg() != nil {
		consensusMsg.Height = message.Height(ret.GetJustificationMsg().SeqNumber)
		consensusMsg.Round = message.Round(ret.GetJustificationMsg().Round)
		consensusMsg.Identifier = toIdentifierV1(ret.GetJustificationMsg().Lambda)
		pd := message.PrepareData{Data: ret.GetJustificationMsg().Value}
		encodedPrepare, err := pd.Encode()
		if err != nil {
			return nil, errors.Wrap(err, "failed to encode prepare data")
		}
		consensusMsg.Data = encodedPrepare
		consensusMsg.MsgType = message.PrepareMsgType // can be only prepare
	}

	crm := &message.RoundChangeData{
		PreparedValue:    ret.GetPreparedValue(),
		Round:            message.Round(ret.GetPreparedRound()),
		NextProposalData: nil,
		RoundChangeJustification: []*message.SignedMessage{{
			Signature: ret.GetJustificationSig(),
			Signers:   signers,
			Message:   consensusMsg,
		}},
	}

	encoded, err := crm.Encode()
	if err != nil {
		return nil, err
	}
	return encoded, nil
}

// ToV0Message converts v1 message to v0
func ToV0Message(msg *message.SSVMessage) (*network.Message, error) {
	v0Msg := &network.Message{}
	identifierV0 := toIdentifierV0(msg.GetIdentifier())
	if msg.GetType() == message.SSVDecidedMsgType {
		v0Msg.Type = network.NetworkMsg_DecidedType // TODO need to provide the proper type (under consensus or post consensus?)
	}
	switch msg.GetType() {
	case message.SSVConsensusMsgType, message.SSVDecidedMsgType:
		if v0Msg.Type != network.NetworkMsg_DecidedType {
			v0Msg.Type = network.NetworkMsg_IBFTType
		}
		signedMsg := &message.SignedMessage{}
		if err := signedMsg.Decode(msg.GetData()); err != nil {
			return nil, errors.Wrap(err, "could not decode consensus signed message")
		}

		sm, err := ToSignedMessageV0(signedMsg, identifierV0)
		if err != nil {
			return nil, err
		}
		v0Msg.SignedMessage = sm
		switch v0Msg.SignedMessage.GetMessage().GetType() {
		case proto.RoundState_ChangeRound:
			v0Msg.Type = network.NetworkMsg_IBFTType
		case proto.RoundState_Decided:
			v0Msg.Type = network.NetworkMsg_DecidedType
		default:
		}
	case message.SSVPostConsensusMsgType:
		panic("convert post consensus type is not supported!!!")
	case message.SSVSyncMsgType:
		v0Msg.Type = network.NetworkMsg_SyncType
		syncMsg := &message.SyncMessage{}
		if err := syncMsg.Decode(msg.GetData()); err != nil {
			return nil, errors.Wrap(err, "could not decode consensus signed message")
		}
		if syncMsg.Status == message.StatusUnknown {
			syncMsg.Status = message.StatusSuccess
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
				sm, err := ToSignedMessageV0(smsg, identifierV0)
				if err != nil {
					return nil, err
				}
				v0Msg.SyncMessage.SignedMessages = append(v0Msg.SyncMessage.SignedMessages, sm)
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

// ToSignedMessageV0 converts a signed message from v1 to v0
func ToSignedMessageV0(signedMsg *message.SignedMessage, identifierV0 []byte) (*proto.SignedMessage, error) {
	signedMsgV0 := &proto.SignedMessage{}
	signedMsgV0.Message = &proto.Message{
		Round:     uint64(signedMsg.Message.Round),
		Lambda:    identifierV0,
		SeqNumber: uint64(signedMsg.Message.Height),
		Value:     nil,
	}

	switch signedMsg.Message.MsgType {
	case message.ProposalMsgType:
		signedMsgV0.Message.Type = proto.RoundState_PrePrepare
		p, err := signedMsg.Message.GetProposalData()
		if err != nil {
			return nil, err
		}
		signedMsgV0.Message.Value = p.Data
	case message.PrepareMsgType:
		signedMsgV0.Message.Type = proto.RoundState_Prepare
		p, err := signedMsg.Message.GetPrepareData()
		if err != nil {
			return nil, err
		}
		signedMsgV0.Message.Value = p.Data
	case message.CommitMsgType:
		signedMsgV0.Message.Type = proto.RoundState_Commit
		c, err := signedMsg.Message.GetCommitData()
		if err != nil {
			return nil, err
		}
		signedMsgV0.Message.Value = c.Data

	case message.RoundChangeMsgType:
		signedMsgV0.Message.Type = proto.RoundState_ChangeRound
		cr, err := signedMsg.Message.GetRoundChangeData()
		if err != nil {
			return nil, err
		}
		if cr.GetPreparedValue() != nil && len(cr.GetPreparedValue()) > 0 {
			crV0 := proto.ChangeRoundData{
				PreparedRound:    uint64(cr.GetPreparedRound()),
				PreparedValue:    cr.GetPreparedValue(),
				JustificationMsg: nil,
				JustificationSig: nil,
				SignerIds:        nil,
			}
			if len(cr.GetRoundChangeJustification()) > 0 {
				m := cr.GetRoundChangeJustification()[0]

				prepareData := new(message.PrepareData)
				if err := prepareData.Decode(m.Message.Data); err != nil {
					return nil, err
				}

				crV0.JustificationMsg = &proto.Message{
					Type:      proto.RoundState_Prepare,
					Round:     uint64(m.Message.Round),
					Lambda:    []byte(format.IdentifierFormat(m.Message.Identifier.GetValidatorPK(), m.Message.Identifier.GetRoleType().String())),
					SeqNumber: uint64(m.Message.Height),
					Value:     prepareData.Data,
				}

				crV0.JustificationSig = m.Signature

				var signers []uint64
				for _, id := range m.Signers {
					signers = append(signers, uint64(id))
				}
				crV0.SignerIds = signers
			}

			if encoded, err := json.Marshal(crV0); err == nil {
				signedMsgV0.Message.Value = encoded
			} else {
				return nil, errors.Wrap(err, "failed to encode proto msg")
			}

		} else {
			v := make(map[string]interface{})
			marshaledV, err := json.Marshal(v)
			if err != nil {
				return nil, errors.Wrap(err, "failed to marshal empty map")
			}
			signedMsgV0.Message.Value = marshaledV // adding empty json in order to support v0 root
		}
	}

	signedMsgV0.Signature = signedMsg.GetSignature()
	for _, signer := range signedMsg.GetSigners() {
		signedMsgV0.SignerIds = append(signedMsgV0.SignerIds, uint64(signer))
	}
	return signedMsgV0, nil
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
