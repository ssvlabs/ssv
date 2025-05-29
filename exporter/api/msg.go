package api

import (
	"encoding/binary"
	"encoding/hex"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/pkg/errors"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	qbftstorage "github.com/ssvlabs/ssv/protocol/v2/qbft/storage"
)

// Message represents an exporter message
type Message struct {
	// Type is the type of message
	Type MessageType `json:"type"`
	// Filter
	Filter MessageFilter `json:"filter"`
	// Values holds the results, optional as it's relevant for response
	Data interface{} `json:"data,omitempty"`
}

type ParticipantsAPI struct {
	Signers     []spectypes.OperatorID
	Slot        phase0.Slot
	Identifier  []byte
	ValidatorPK string
	Role        string
	Message     specqbft.Message
	FullData    *spectypes.ValidatorConsensusData
}

// NewParticipantsAPIMsg creates a new message in a new format from the given message.
func NewParticipantsAPIMsg(domainType spectypes.DomainType, msg qbftstorage.Participation) Message {
	data, err := ParticipantsAPIData(domainType, msg)
	if err != nil {
		return Message{
			Type: TypeParticipants,
			Data: []string{},
		}
	}

	return Message{
		Type: TypeDecided,
		Filter: MessageFilter{
			PublicKey: hex.EncodeToString(msg.PubKey[:]),
			From:      uint64(msg.Slot),
			To:        uint64(msg.Slot),
			Role:      msg.Role.String(),
		},
		Data: data,
	}
}

// ParticipantsAPIData creates a new message from the given message in a new format.
func ParticipantsAPIData(domainType spectypes.DomainType, msgs ...qbftstorage.Participation) (interface{}, error) {
	if len(msgs) == 0 {
		return nil, errors.New("no messages")
	}

	apiMsgs := make([]*ParticipantsAPI, 0)
	for _, msg := range msgs {
		var msgID = legacyNewMsgID(domainType, msg.PubKey[:], msg.Role)
		blsPubKey := phase0.BLSPubKey{}
		copy(blsPubKey[:], msg.PubKey[:])
		apiMsg := &ParticipantsAPI{
			Signers:     msg.Signers,
			Slot:        msg.Slot,
			Identifier:  msgID[:],
			ValidatorPK: hex.EncodeToString(msg.PubKey[:]),
			Role:        msg.Role.String(),
			Message: specqbft.Message{
				MsgType:    specqbft.CommitMsgType,
				Height:     specqbft.Height(msg.Slot),
				Identifier: msgID[:],
				Round:      specqbft.FirstRound,
			},
			FullData: &spectypes.ValidatorConsensusData{
				Duty: spectypes.ValidatorDuty{
					PubKey: blsPubKey,
					Slot:   msg.Slot,
				},
			},
		}

		apiMsgs = append(apiMsgs, apiMsg)
	}

	return apiMsgs, nil
}

// MessageFilter is a criteria for query in request messages and projection in responses
type MessageFilter struct {
	// From is the starting index of the desired data
	From uint64 `json:"from"`
	// To is the ending index of the desired data
	To uint64 `json:"to"`
	// Role is the duty type, optional as it's relevant for IBFT data
	Role string `json:"role,omitempty"`
	// PublicKey is optional, used for fetching decided messages or information about specific validator/operator
	PublicKey string `json:"publicKey,omitempty"`
}

// MessageType is the type of message being sent
type MessageType string

const (
	// TypeValidator is an enum for validator type messages
	TypeValidator MessageType = "validator"
	// TypeOperator is an enum for operator type messages
	TypeOperator MessageType = "operator"
	// TypeDecided is an enum for ibft type messages
	TypeDecided MessageType = "decided"
	// TypeError is an enum for error type messages
	TypeError MessageType = "error"
	// TypeParticipants is an enum for participants type messages
	TypeParticipants MessageType = "participants"
)

const (
	domainSize       = 4
	domainStartPos   = 0
	pubKeySize       = 48
	pubKeyStartPos   = domainStartPos + domainSize
	roleTypeSize     = 4
	roleTypeStartPos = pubKeyStartPos + pubKeySize
)

// legacyNewMsgID is needed only for API backwards-compatibility.
func legacyNewMsgID(domain spectypes.DomainType, pk []byte, role spectypes.BeaconRole) (mid spectypes.MessageID) {
	roleByts := make([]byte, 4)
	binary.LittleEndian.PutUint32(roleByts, uint32(role)) // #nosec G115
	copy(mid[domainStartPos:domainStartPos+domainSize], domain[:])
	copy(mid[pubKeyStartPos:pubKeyStartPos+pubKeySize], pk)
	copy(mid[roleTypeStartPos:roleTypeStartPos+roleTypeSize], roleByts)
	return mid
}
