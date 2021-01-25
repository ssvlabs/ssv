package types

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"

	"github.com/herumi/bls-eth-go-binary/bls"
)

// Compare returns true if both messages are equal.
// DOES NOT compare signatures
func (msg Message) Compare(other Message) bool {
	if msg.Type != other.Type ||
		msg.Round != other.Round ||
		!bytes.Equal(msg.Lambda, other.Lambda) ||
		!bytes.Equal(msg.Value, other.Value) {
		return false
	}

	return true
}

// SigningRoot returns a signing root (bytes)
func (msg *Message) SigningRoot() ([]byte, error) {
	// TODO - consider moving to SSZ
	byts, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}

	hasher := sha256.New()
	_, err = hasher.Write(byts)
	if err != nil {
		return nil, err
	}
	return hasher.Sum(nil), nil
}

// Sign takes a secret key and signs the Message
func (msg *Message) Sign(sk *bls.SecretKey) (*bls.Sign, error) {
	root, err := msg.SigningRoot()
	if err != nil {
		return nil, err
	}
	return sk.SignByte(root), nil
}

// VerifySig returns true if the justification signed msg verifies against the public key, false if otherwise
func (msg *SignedMessage) VerifySig(pk *bls.PublicKey) (bool, error) {
	root, err := msg.Message.SigningRoot()
	if err != nil {
		return false, err
	}

	sig := &bls.Sign{}
	if err := sig.Deserialize(msg.Signature); err != nil {
		return false, err
	}
	return sig.VerifyByte(pk, root), nil
}

// VerifySig returns true if the justification signed msg verifies against the public key, false if otherwise
func (d *ChangeRoundData) VerifySig(pk bls.PublicKey) (bool, error) {
	root, err := d.JustificationMsg.SigningRoot()
	if err != nil {
		return false, err
	}

	sig := bls.Sign{}
	if err := sig.Deserialize(d.JustificationSig); err != nil {
		return false, err
	}

	return sig.VerifyByte(&pk, root), nil
}
