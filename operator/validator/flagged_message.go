package validator

import "github.com/bloxapp/ssv/protocol/v1/message"

type flaggedMessage struct {
	message  message.SSVMessage
	readOnly bool
}
