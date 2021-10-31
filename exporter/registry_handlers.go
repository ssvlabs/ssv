package exporter

import (
	"encoding/hex"
	"github.com/bloxapp/ssv/eth1"
	"github.com/bloxapp/ssv/exporter/api"
	"github.com/bloxapp/ssv/exporter/storage"
	"github.com/bloxapp/ssv/pubsub"
	"github.com/bloxapp/ssv/validator"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// ListenToEth1Events register for eth1 events
func (exp *exporter) listenToEth1Events(cn pubsub.SubjectChannel) chan error {
	cnErr := make(chan error)
	go func() {
		for e := range cn {
			if event, ok := e.(eth1.Event); ok {
				if err := exp.handleEth1Event(event); err != nil {
					cnErr <- err
				}
			}
		}
	}()
	return cnErr
}

// ListenToEth1Events register for eth1 events
func (exp *exporter) handleEth1Event(e eth1.Event) error {
	var err error = nil
	if validatorAddedEvent, ok := e.Data.(eth1.ValidatorAddedEvent); ok {
		err = exp.handleValidatorAddedEvent(validatorAddedEvent)
	} else if opertaorAddedEvent, ok := e.Data.(eth1.OperatorAddedEvent); ok {
		err = exp.handleOperatorAddedEvent(opertaorAddedEvent)
	}
	return err
}

// handleValidatorAddedEvent parses the given event and sync the ibft-data of the validator
func (exp *exporter) handleValidatorAddedEvent(event eth1.ValidatorAddedEvent) error {
	pubKeyHex := hex.EncodeToString(event.PublicKey)
	logger := exp.logger.With(zap.String("pubKey", pubKeyHex))
	logger.Info("validator added event")
	// save the share to be able to reuse IBFT functionality
	validatorShare, _, err := validator.ShareFromValidatorAddedEvent(event, "")
	if err != nil {
		return errors.Wrap(err, "could not create a share from ValidatorAddedEvent")
	}
	if err := exp.validatorStorage.SaveValidatorShare(validatorShare); err != nil {
		return errors.Wrap(err, "failed to save validator share")
	}
	logger.Debug("validator share was saved")
	// save information for exporting validators
	vi, err := toValidatorInformation(event)
	if err != nil {
		return errors.Wrap(err, "could not create ValidatorInformation")
	}
	if err := exp.storage.SaveValidatorInformation(vi); err != nil {
		return errors.Wrap(err, "failed to save validator information")
	}
	logger.Debug("validator information was saved", zap.Any("value", *vi))

	// TODO: aggregate validators in sync scenario
	// otherwise the network will be overloaded with multiple messages
	exp.ws.OutboundSubject().Notify(api.NetworkMessage{Msg: api.Message{
		Type:   api.TypeValidator,
		Filter: api.MessageFilter{From: vi.Index, To: vi.Index},
		Data:   []storage.ValidatorInformation{*vi},
	}, Conn: nil})

	// triggers a sync for the given validator
	if err = exp.triggerValidator(validatorShare.PublicKey); err != nil {
		return errors.Wrap(err, "failed to trigger ibft sync")
	}

	return nil
}

// handleOperatorAddedEvent parses the given event and saves operator information
func (exp *exporter) handleOperatorAddedEvent(event eth1.OperatorAddedEvent) error {
	l := exp.logger.With(zap.String("pubKey", string(event.PublicKey)))
	l.Info("operator added event")
	oi := storage.OperatorInformation{
		PublicKey:    string(event.PublicKey),
		Name:         event.Name,
		OwnerAddress: event.OwnerAddress,
	}
	err := exp.storage.SaveOperatorInformation(&oi)
	if err != nil {
		return err
	}
	l.Debug("managed to save operator information", zap.Any("value", oi))
	reportOperatorIndex(exp.logger, &oi)

	exp.ws.OutboundSubject().Notify(api.NetworkMessage{Msg: api.Message{
		Type:   api.TypeOperator,
		Filter: api.MessageFilter{From: oi.Index, To: oi.Index},
		Data:   []storage.OperatorInformation{oi},
	}, Conn: nil})

	return nil
}

// toValidatorInformation converts raw event to ValidatorInformation
func toValidatorInformation(validatorAddedEvent eth1.ValidatorAddedEvent) (*storage.ValidatorInformation, error) {
	pubKey := &bls.PublicKey{}
	if err := pubKey.Deserialize(validatorAddedEvent.PublicKey); err != nil {
		return nil, errors.Wrap(err, "failed to deserialize validator public key")
	}

	var operators []storage.OperatorNodeLink
	for i := range validatorAddedEvent.OessList {
		oess := validatorAddedEvent.OessList[i]
		nodeID := oess.Index.Uint64() + 1
		operators = append(operators, storage.OperatorNodeLink{
			ID: nodeID, PublicKey: string(oess.OperatorPublicKey),
		})
	}

	vi := storage.ValidatorInformation{
		PublicKey: pubKey.SerializeToHexStr(),
		Operators: operators,
	}

	return &vi, nil
}
