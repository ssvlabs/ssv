package smr

import (
	"bytes"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"

	spectypes "github.com/ssvlabs/ssv-spec/types"
)

type OperatorID = spectypes.OperatorID
type CommitteeMember = spectypes.CommitteeMember

type TimeoutMessage struct {
	View      uint64
	Block     *Block // highest block voted for, or nil
	SignerID  OperatorID
	Signature []byte
}

type TimeoutCertificate struct {
	View     uint64
	Messages []*TimeoutMessage
}

func NewTimeoutCertificate(view uint64) *TimeoutCertificate {
	return &TimeoutCertificate{
		View:     view,
		Messages: make([]*TimeoutMessage, 0),
	}
}

func (tc *TimeoutCertificate) AddMessage(msg *TimeoutMessage) error {
	if tc == nil {
		return errors.New("nil timeout certificate")
	}
	if msg == nil {
		return errors.New("nil timeout message")
	}
	if msg.View != tc.View {
		return fmt.Errorf("timeout message view (%d) does not match certificate view (%d)", msg.View, tc.View)
	}
	if msg.SignerID == 0 {
		return errors.New("missing signer id")
	}
	if len(msg.Signature) == 0 {
		return errors.New("missing signature")
	}

	for _, existing := range tc.Messages {
		if existing != nil && existing.SignerID == msg.SignerID {
			return fmt.Errorf("duplicate signer id (%d)", msg.SignerID)
		}
	}

	tc.Messages = append(tc.Messages, msg)
	return nil
}

// HasQuorum returns true if the certificate contains >= 4f-1 unique timeout messages.
func (tc *TimeoutCertificate) HasQuorum(committeeMember *CommitteeMember) bool {
	if tc == nil || committeeMember == nil {
		return false
	}
	if committeeMember.FaultyNodes == 0 {
		return false
	}

	required := int(4*committeeMember.FaultyNodes - 1)
	if required <= 0 {
		return false
	}

	seen := make(map[OperatorID]struct{}, len(tc.Messages))
	for _, msg := range tc.Messages {
		if msg == nil {
			continue
		}
		if msg.View != tc.View {
			continue
		}
		if msg.SignerID == 0 {
			continue
		}
		seen[msg.SignerID] = struct{}{}
	}

	return len(seen) >= required
}

// IsValid performs full validation:
// - certificate has quorum (>=4f-1) for the given committee
// - all messages have consistent view
// - signers are unique and in committee
// - signatures verify against the committee public keys
func (tc *TimeoutCertificate) IsValid(committeeMember *CommitteeMember) bool {
	if tc == nil || committeeMember == nil {
		return false
	}
	if !tc.HasQuorum(committeeMember) {
		return false
	}

	seen := make(map[OperatorID]struct{}, len(tc.Messages))
	for _, msg := range tc.Messages {
		if msg == nil {
			return false
		}
		if msg.View != tc.View {
			return false
		}
		if msg.SignerID == 0 {
			return false
		}
		if _, ok := seen[msg.SignerID]; ok {
			return false
		}
		seen[msg.SignerID] = struct{}{}

		if !signerInCommittee(committeeMember, msg.SignerID) {
			return false
		}
		if err := verifyTimeoutMessageSignature(msg, committeeMember); err != nil {
			return false
		}
	}

	return true
}

// LocksBlock returns true if this certificate locks the given block under Condition 1 or Condition 2.
func (tc *TimeoutCertificate) LocksBlock(block *Block, leaderID OperatorID) bool {
	if tc == nil || block == nil {
		return false
	}

	msgs, ok := tc.uniqueMessagesForView()
	if !ok {
		return false
	}

	f := inferFaultyNodes(len(msgs))
	if f == 0 {
		return false
	}

	quorum := int(4*f - 1)
	if len(msgs) < quorum {
		return false
	}

	compatible := 0
	for _, msg := range msgs {
		if msg.Block == nil {
			continue
		}
		if BlockExtends(block, msg.Block) {
			compatible++
		}
	}

	// Condition 1:
	// - >= 2f-1 compatible messages
	// - no messages for blocks that conflict with B
	if compatible >= int(2*f-1) && !tc.hasConflictingMessage(block, msgs) {
		return true
	}

	// Condition 2:
	// - >= 2f compatible messages
	// - no timeout message from leader
	if compatible >= int(2*f) && !tc.hasLeaderMessage(leaderID, msgs) {
		return true
	}

	return false
}

// GetLockedBlock returns the highest block locked by this timeout certificate (or nil).
func (tc *TimeoutCertificate) GetLockedBlock(leaderID OperatorID) *Block {
	if tc == nil {
		return nil
	}

	msgs, ok := tc.uniqueMessagesForView()
	if !ok {
		return nil
	}

	candidates := make([]*Block, 0, len(msgs))
	seenBlocks := make(map[*Block]struct{}, len(msgs))
	for _, msg := range msgs {
		if msg.Block == nil {
			continue
		}
		if _, ok := seenBlocks[msg.Block]; ok {
			continue
		}
		seenBlocks[msg.Block] = struct{}{}
		candidates = append(candidates, msg.Block)
	}

	locked := make([]*Block, 0, len(candidates))
	for _, b := range candidates {
		if tc.LocksBlock(b, leaderID) {
			locked = append(locked, b)
		}
	}

	return HighestBlock(locked)
}

func (tc *TimeoutCertificate) uniqueMessagesForView() (map[OperatorID]*TimeoutMessage, bool) {
	if tc == nil {
		return nil, false
	}

	msgs := make(map[OperatorID]*TimeoutMessage, len(tc.Messages))
	for _, msg := range tc.Messages {
		if msg == nil || msg.SignerID == 0 {
			return nil, false
		}
		if msg.View != tc.View {
			return nil, false
		}
		if _, ok := msgs[msg.SignerID]; ok {
			return nil, false
		}
		msgs[msg.SignerID] = msg
	}
	return msgs, true
}

func (tc *TimeoutCertificate) hasLeaderMessage(leaderID OperatorID, msgs map[OperatorID]*TimeoutMessage) bool {
	_, ok := msgs[leaderID]
	return ok
}

func (tc *TimeoutCertificate) hasConflictingMessage(block *Block, msgs map[OperatorID]*TimeoutMessage) bool {
	for _, msg := range msgs {
		if msg.Block == nil {
			continue
		}
		if BlocksConflict(block, msg.Block) {
			return true
		}
	}
	return false
}

func inferFaultyNodes(uniqueMessages int) uint64 {
	if uniqueMessages < 3 { // minimum for f=1 where 4f-1 == 3
		return 0
	}
	// A TC contains >= 4f-1 messages; conservatively infer f as floor((m+1)/4).
	return uint64((uniqueMessages + 1) / 4)
}

var timeoutMessageSigningDomain = []byte("ssv.smr.timeout.v1")

func timeoutMessageSigningBytes(msg *TimeoutMessage) ([]byte, error) {
	if msg == nil {
		return nil, errors.New("nil timeout message")
	}

	buf := bytes.NewBuffer(make([]byte, 0, len(timeoutMessageSigningDomain)+1+8+1+8+8+32))
	buf.Write(timeoutMessageSigningDomain)

	var tmp [8]byte
	binary.BigEndian.PutUint64(tmp[:], msg.View)
	buf.Write(tmp[:])

	if msg.Block == nil {
		buf.WriteByte(0)
		return buf.Bytes(), nil
	}

	buf.WriteByte(1)
	binary.BigEndian.PutUint64(tmp[:], msg.Block.View)
	buf.Write(tmp[:])
	binary.BigEndian.PutUint64(tmp[:], msg.Block.Height)
	buf.Write(tmp[:])
	buf.Write(msg.Block.Root[:])

	return buf.Bytes(), nil
}

func signerInCommittee(cm *CommitteeMember, signerID OperatorID) bool {
	if cm == nil {
		return false
	}
	for _, op := range cm.Committee {
		if op != nil && op.OperatorID == signerID {
			return true
		}
	}
	return false
}

func operatorForSigner(cm *CommitteeMember, signerID OperatorID) *spectypes.Operator {
	if cm == nil {
		return nil
	}
	for _, op := range cm.Committee {
		if op != nil && op.OperatorID == signerID {
			return op
		}
	}
	return nil
}

func verifyTimeoutMessageSignature(msg *TimeoutMessage, cm *CommitteeMember) error {
	if msg == nil {
		return errors.New("nil timeout message")
	}
	if cm == nil {
		return errors.New("nil committee member")
	}
	if len(msg.Signature) == 0 {
		return errors.New("empty signature")
	}

	op := operatorForSigner(cm, msg.SignerID)
	if op == nil {
		return errors.New("unknown signer")
	}

	pk, err := spectypes.PemToPublicKey(op.SSVOperatorPubKey)
	if err != nil {
		return err
	}

	signingBytes, err := timeoutMessageSigningBytes(msg)
	if err != nil {
		return err
	}
	hash := sha256.Sum256(signingBytes)

	return rsa.VerifyPKCS1v15(pk, crypto.SHA256, hash[:], msg.Signature)
}
