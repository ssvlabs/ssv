package validation

import (
	spectypes "github.com/bloxapp/ssv-spec/alan/types"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
)

func (mv *messageValidator) decodeSignedSSVMessage(pMsg *pubsub.Message) (*spectypes.SignedSSVMessage, error) {
	signedSSVMessage := &spectypes.SignedSSVMessage{}
	if err := signedSSVMessage.Decode(pMsg.GetData()); err != nil {
		e := ErrMalformedPubSubMessage
		e.innerErr = err
		return nil, e
	}

	return signedSSVMessage, nil
}

func (mv *messageValidator) validateSignedSSVMessage(signedSSVMessage *spectypes.SignedSSVMessage) error {
	if signedSSVMessage == nil {
		return ErrEmptyPubSubMessage
	}

	if err := signedSSVMessage.Validate(); err != nil {
		e := ErrSignedSSVMessageValidation
		e.innerErr = err
		return e
	}

	return nil
}
