package validation

import (
	"fmt"

	"github.com/bloxapp/ssv/protocol/v2/ssv/queue"
	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
)

// event_validation.go contains methods for validating event messages

func (mv *MessageValidator) validateEventMessage(msg *queue.DecodedSSVMessage) error {
	_, ok := msg.Body.(*ssvtypes.EventMsg)
	if !ok {
		return fmt.Errorf("expected event message")
	}

	return ErrEventMessage
}
