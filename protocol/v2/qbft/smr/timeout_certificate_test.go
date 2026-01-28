package smr

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/binary"
	"testing"

	spectesting "github.com/ssvlabs/ssv-spec/types/testingutils"
)

func newTestBlock(view, height uint64, parent *Block, salt byte) *Block {
	h := sha256.New()
	h.Write([]byte{salt})
	var tmp [8]byte
	binary.BigEndian.PutUint64(tmp[:], view)
	h.Write(tmp[:])
	binary.BigEndian.PutUint64(tmp[:], height)
	h.Write(tmp[:])
	if parent != nil {
		h.Write(parent.Root[:])
	}
	sum := h.Sum(nil)

	var root [32]byte
	copy(root[:], sum)
	return &Block{
		View:   view,
		Height: height,
		Root:   root,
		Parent: parent,
	}
}

func signTimeoutMessage(t *testing.T, msg *TimeoutMessage, sk *rsa.PrivateKey) []byte {
	t.Helper()
	b, err := timeoutMessageSigningBytes(msg)
	if err != nil {
		t.Fatalf("signing bytes: %v", err)
	}
	hash := sha256.Sum256(b)
	sig, err := rsa.SignPKCS1v15(rand.Reader, sk, crypto.SHA256, hash[:])
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return sig
}

func TestBlockExtendsAndConflict(t *testing.T) {
	gen := (*Block)(nil)
	a := newTestBlock(1, 1, gen, 0)
	b := newTestBlock(1, 2, a, 0)

	forkA := newTestBlock(1, 1, gen, 1)
	forkB := newTestBlock(1, 2, forkA, 1)

	if !BlockExtends(b, a) {
		t.Fatalf("expected b to extend a")
	}
	if BlockExtends(a, b) {
		t.Fatalf("expected a not to extend b")
	}
	if !BlocksConflict(b, forkB) {
		t.Fatalf("expected blocks on different forks to conflict")
	}
	if BlocksConflict(a, gen) {
		t.Fatalf("nil should not conflict")
	}
}

func TestTimeoutCertificate_AddMessageValidationAndNoDuplicates(t *testing.T) {
	tc := NewTimeoutCertificate(10)

	if err := tc.AddMessage(nil); err == nil {
		t.Fatalf("expected error for nil message")
	}

	msg := &TimeoutMessage{View: 9, SignerID: 1, Signature: []byte{1}}
	if err := tc.AddMessage(msg); err == nil {
		t.Fatalf("expected error for view mismatch")
	}

	msg = &TimeoutMessage{View: 10, SignerID: 0, Signature: []byte{1}}
	if err := tc.AddMessage(msg); err == nil {
		t.Fatalf("expected error for missing signer")
	}

	msg = &TimeoutMessage{View: 10, SignerID: 1, Signature: nil}
	if err := tc.AddMessage(msg); err == nil {
		t.Fatalf("expected error for missing signature")
	}

	msg = &TimeoutMessage{View: 10, SignerID: 1, Signature: []byte{1}}
	if err := tc.AddMessage(msg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	msg2 := &TimeoutMessage{View: 10, SignerID: 1, Signature: []byte{2}}
	if err := tc.AddMessage(msg2); err == nil {
		t.Fatalf("expected error for duplicate signer")
	}
}

func TestTimeoutCertificate_IsValidAndHasQuorum(t *testing.T) {
	keys := spectesting.Testing4SharesSet()
	cm := spectesting.TestingCommitteeMember(keys)

	// f=1 => quorum is 4f-1 == 3
	tc := NewTimeoutCertificate(1)

	b1 := newTestBlock(1, 1, nil, 0)

	for _, id := range []OperatorID{1, 2, 3} {
		msg := &TimeoutMessage{
			View:     1,
			Block:    b1,
			SignerID: id,
		}
		msg.Signature = signTimeoutMessage(t, msg, keys.OperatorKeys[id])
		if err := tc.AddMessage(msg); err != nil {
			t.Fatalf("AddMessage(%d): %v", id, err)
		}
	}

	if !tc.HasQuorum(cm) {
		t.Fatalf("expected quorum")
	}
	if !tc.IsValid(cm) {
		t.Fatalf("expected valid TC")
	}

	// Corrupt one signature and expect validation failure.
	tc.Messages[0].Signature = []byte{1, 2, 3}
	if tc.IsValid(cm) {
		t.Fatalf("expected invalid TC due to bad signature")
	}
}

func TestTimeoutCertificate_IsValidFailsOnDuplicateSigner(t *testing.T) {
	keys := spectesting.Testing4SharesSet()
	cm := spectesting.TestingCommitteeMember(keys)

	tc := &TimeoutCertificate{
		View: 1,
		Messages: []*TimeoutMessage{
			{View: 1, SignerID: 1, Signature: []byte{1}},
			{View: 1, SignerID: 1, Signature: []byte{2}},
			{View: 1, SignerID: 2, Signature: []byte{3}},
		},
	}
	if tc.IsValid(cm) {
		t.Fatalf("expected invalid TC due to duplicate signer")
	}
}

func TestTimeoutCertificate_LockingCondition1(t *testing.T) {
	keys := spectesting.Testing7SharesSet()
	cm := spectesting.TestingCommitteeMember(keys) // FaultyNodes == 2

	// Build chain: a -> b -> c
	a := newTestBlock(1, 1, nil, 0)
	b := newTestBlock(1, 2, a, 0)
	c := newTestBlock(1, 3, b, 0)

	tc := NewTimeoutCertificate(7)
	for _, id := range []OperatorID{1, 2, 3} {
		msg := &TimeoutMessage{View: 7, Block: b, SignerID: id}
		msg.Signature = signTimeoutMessage(t, msg, keys.OperatorKeys[id])
		if err := tc.AddMessage(msg); err != nil {
			t.Fatalf("AddMessage(%d): %v", id, err)
		}
	}
	for _, id := range []OperatorID{4, 5, 6, 7} {
		msg := &TimeoutMessage{View: 7, Block: c, SignerID: id}
		msg.Signature = signTimeoutMessage(t, msg, keys.OperatorKeys[id])
		if err := tc.AddMessage(msg); err != nil {
			t.Fatalf("AddMessage(%d): %v", id, err)
		}
	}

	if !tc.IsValid(cm) {
		t.Fatalf("expected valid TC")
	}

	if !tc.LocksBlock(c, 1) {
		t.Fatalf("expected block c to be locked by condition 1")
	}
	if tc.GetLockedBlock(1) != c {
		t.Fatalf("expected locked block to be c (highest)")
	}

	// Introduce a conflicting block message; condition 1 should fail and condition 2 should fail due to leader presence.
	fork := newTestBlock(1, 2, a, 1)
	tc2 := NewTimeoutCertificate(7)
	// Keep quorum at 7 messages so inferred f stays 2.
	for _, id := range []OperatorID{1, 2, 3, 4, 5, 6} {
		msg := &TimeoutMessage{View: 7, Block: c, SignerID: id}
		msg.Signature = signTimeoutMessage(t, msg, keys.OperatorKeys[id])
		if err := tc2.AddMessage(msg); err != nil {
			t.Fatalf("AddMessage(%d): %v", id, err)
		}
	}
	msg7 := &TimeoutMessage{View: 7, Block: fork, SignerID: 7}
	msg7.Signature = signTimeoutMessage(t, msg7, keys.OperatorKeys[7])
	if err := tc2.AddMessage(msg7); err != nil {
		t.Fatalf("AddMessage(7): %v", err)
	}

	if tc2.LocksBlock(c, 1) {
		t.Fatalf("expected block c not to be locked due to conflict + leader message")
	}
	if tc2.GetLockedBlock(1) != nil {
		t.Fatalf("expected no locked block")
	}
}

func TestTimeoutCertificate_LockingCondition2(t *testing.T) {
	keys := spectesting.Testing10SharesSet()
	cm := spectesting.TestingCommitteeMember(keys)
	cm.FaultyNodes = 2 // override to allow quorum without leader (4f-1 == 7)

	a := newTestBlock(1, 1, nil, 0)
	b := newTestBlock(1, 2, a, 0)

	forkA := newTestBlock(1, 1, nil, 1)
	forkB := newTestBlock(1, 2, forkA, 1)

	leaderID := OperatorID(1)
	tc := NewTimeoutCertificate(8)

	// 4 compatible messages for b, 3 for conflicting forkB, and exclude leader.
	for _, id := range []OperatorID{2, 3, 4, 5} {
		msg := &TimeoutMessage{View: 8, Block: b, SignerID: id}
		msg.Signature = signTimeoutMessage(t, msg, keys.OperatorKeys[id])
		if err := tc.AddMessage(msg); err != nil {
			t.Fatalf("AddMessage(%d): %v", id, err)
		}
	}
	for _, id := range []OperatorID{6, 7, 8} {
		msg := &TimeoutMessage{View: 8, Block: forkB, SignerID: id}
		msg.Signature = signTimeoutMessage(t, msg, keys.OperatorKeys[id])
		if err := tc.AddMessage(msg); err != nil {
			t.Fatalf("AddMessage(%d): %v", id, err)
		}
	}

	if !tc.IsValid(cm) {
		t.Fatalf("expected valid TC")
	}

	if !tc.LocksBlock(b, leaderID) {
		t.Fatalf("expected block b to be locked by condition 2")
	}
	if tc.GetLockedBlock(leaderID) != b {
		t.Fatalf("expected locked block to be b")
	}

	// Same messages but include leader => condition 2 fails; conflicts mean condition 1 fails too.
	tc2 := NewTimeoutCertificate(8)
	for _, id := range []OperatorID{1, 2, 3, 4} {
		msg := &TimeoutMessage{View: 8, Block: b, SignerID: id}
		msg.Signature = signTimeoutMessage(t, msg, keys.OperatorKeys[id])
		if err := tc2.AddMessage(msg); err != nil {
			t.Fatalf("AddMessage(%d): %v", id, err)
		}
	}
	for _, id := range []OperatorID{6, 7, 8} {
		msg := &TimeoutMessage{View: 8, Block: forkB, SignerID: id}
		msg.Signature = signTimeoutMessage(t, msg, keys.OperatorKeys[id])
		if err := tc2.AddMessage(msg); err != nil {
			t.Fatalf("AddMessage(%d): %v", id, err)
		}
	}

	if tc2.LocksBlock(b, leaderID) {
		t.Fatalf("expected block b not to be locked when leader timeout is present and conflicts exist")
	}
	if tc2.GetLockedBlock(leaderID) != nil {
		t.Fatalf("expected no locked block")
	}
}

func TestTimeoutCertificate_EdgeCases(t *testing.T) {
	tc := NewTimeoutCertificate(1)
	if tc.LocksBlock(newTestBlock(1, 1, nil, 0), 1) {
		t.Fatalf("expected empty TC to not lock")
	}
	if tc.GetLockedBlock(1) != nil {
		t.Fatalf("expected empty TC to have no locked block")
	}
}
