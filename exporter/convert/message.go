package convert

import (
	"encoding/binary"
	"encoding/hex"
	"math"

	spectypes "github.com/ssvlabs/ssv-spec/types"
)

const (
	domainSize             = 4
	domainStartPos         = 0
	roleTypeSize           = 4
	roleTypeStartPos       = domainStartPos + domainSize
	dutyExecutorIDSize     = 48
	dutyExecutorIDStartPos = roleTypeStartPos + roleTypeSize
)

// MessageID is used to identify and route messages to the right validator and Runner
type MessageID [56]byte

func (msg MessageID) GetDomain() []byte {
	return msg[domainStartPos : domainStartPos+domainSize]
}

func (msg MessageID) GetDutyExecutorID() []byte {
	return msg[dutyExecutorIDStartPos : dutyExecutorIDStartPos+dutyExecutorIDSize]
}

func (msg MessageID) GetRoleType() spectypes.RunnerRole {
	roleByts := msg[roleTypeStartPos : roleTypeStartPos+roleTypeSize]
	roleValue := binary.LittleEndian.Uint32(roleByts)

	// Sanitize RoleValue
	if roleValue > math.MaxInt32 {
		return spectypes.RoleUnknown
	}

	return spectypes.RunnerRole(roleValue)
}

func NewMsgID(domain spectypes.DomainType, dutyExecutorID []byte, role spectypes.RunnerRole) MessageID {
	// Sanitize role. If bad role, return an empty MessageID
	roleValue := int32(role)
	if roleValue < 0 {
		return MessageID{}
	}
	roleByts := make([]byte, 4)
	binary.LittleEndian.PutUint32(roleByts, uint32(roleValue))
	return newMessageID(domain[:], roleByts, dutyExecutorID)
}

func (msgID MessageID) String() string {
	return hex.EncodeToString(msgID[:])
}

func MessageIDFromBytes(mid []byte) MessageID {
	if len(mid) < domainSize+dutyExecutorIDSize+roleTypeSize {
		return MessageID{}
	}
	return newMessageID(
		mid[domainStartPos:domainStartPos+domainSize],
		mid[roleTypeStartPos:roleTypeStartPos+roleTypeSize],
		mid[dutyExecutorIDStartPos:dutyExecutorIDStartPos+dutyExecutorIDSize],
	)
}

func newMessageID(domain, roleByts, dutyExecutorID []byte) MessageID {
	mid := MessageID{}
	copy(mid[domainStartPos:domainStartPos+domainSize], domain[:])
	copy(mid[roleTypeStartPos:roleTypeStartPos+roleTypeSize], roleByts)
	prefixLen := dutyExecutorIDSize - len(dutyExecutorID)
	copy(mid[dutyExecutorIDStartPos+prefixLen:dutyExecutorIDStartPos+dutyExecutorIDSize], dutyExecutorID)
	return mid
}
