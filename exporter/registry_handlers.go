package exporter

import (
	"encoding/hex"
	"github.com/bloxapp/ssv/eth1"
	"github.com/bloxapp/ssv/eth1/abiparser"
	"github.com/bloxapp/ssv/exporter/api"
	"github.com/bloxapp/ssv/exporter/storage"
	"github.com/bloxapp/ssv/validator"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/prysm/async/event"
	"go.uber.org/zap"
)

// ListenToEth1Events register for eth1 events
func (exp *exporter) listenToEth1Events(eventsFeed *event.Feed) <-chan error {
	cn := make(chan *eth1.Event)
	sub := eventsFeed.Subscribe(cn)
	cnErr := make(chan error, 10)
	go func() {
		defer sub.Unsubscribe()
		for {
			select {
			case event := <-cn:
				if err := exp.handleEth1Event(*event); err != nil {
					cnErr <- err
				}
			case err := <-sub.Err():
				cnErr <- err
			}
		}
	}()
	return cnErr
}

// ListenToEth1Events register for eth1 events
func (exp *exporter) handleEth1Event(e eth1.Event) error {
	var err error = nil
	if validatorAddedEvent, ok := e.Data.(abiparser.ValidatorAddedEvent); ok {
		err = exp.handleValidatorAddedEvent(validatorAddedEvent)
	} else if opertaorAddedEvent, ok := e.Data.(abiparser.OperatorAddedEvent); ok {
		err = exp.handleOperatorAddedEvent(opertaorAddedEvent)
	}
	return err
}

// handleValidatorAddedEvent parses the given event and sync the ibft-data of the validator
func (exp *exporter) handleValidatorAddedEvent(event abiparser.ValidatorAddedEvent) error {
	pubKeyHex := hex.EncodeToString(event.PublicKey)
	logger := exp.logger.With(zap.String("eventType", "ValidatorAdded"), zap.String("pubKey", pubKeyHex))
	logger.Info("validator added event")
	// save the share to be able to reuse IBFT functionality
	validatorShare, _, err := validator.ShareFromValidatorAddedEvent(event, "")
	if err != nil {
		return errors.Wrap(err, "could not create a share from ValidatorAddedEvent")
	}
	// add metadata
	if updated, err := validator.UpdateShareMetadata(validatorShare, exp.beacon); err != nil {
		logger.Warn("could not add validator metadata", zap.Error(err))
	} else if !updated {
		logger.Warn("could not find validator metadata")
	} else {
		logger.Debug("validator metadata was updated")
	}
	if err := exp.validatorStorage.SaveValidatorShare(validatorShare); err != nil {
		return errors.Wrap(err, "failed to save validator share")
	}
	// save information for exporting validators
	vi, err := toValidatorInformation(event)
	if err != nil {
		return errors.Wrap(err, "could not create ValidatorInformation")
	}
	if err := exp.storage.SaveValidatorInformation(vi); err != nil {
		return errors.Wrap(err, "failed to save validator information")
	}
	// TODO: aggregate validators in sync scenario
	go func() {
		n := exp.ws.BroadcastFeed().Send(api.Message{
			Type:   api.TypeValidator,
			Filter: api.MessageFilter{From: vi.Index, To: vi.Index},
			Data:   []storage.ValidatorInformation{*vi},
		})
		logger.Debug("msg was sent on outbound feed", zap.Int("num of subscribers", n))
	}()

	// triggers a sync for the given validator
	if _, err := exp.triggerValidator(validatorShare.PublicKey); err != nil {
		return errors.Wrap(err, "failed to trigger ibft sync")
	}

	return nil
}

// handleOperatorAddedEvent parses the given event and saves operator information
func (exp *exporter) handleOperatorAddedEvent(event abiparser.OperatorAddedEvent) error {
	logger := exp.logger.With(zap.String("eventType", "OperatorAdded"),
		zap.String("pubKey", string(event.PublicKey)))
	logger.Info("operator added event")
	oi := storage.OperatorInformation{
		PublicKey:    string(event.PublicKey),
		Name:         event.Name,
		OwnerAddress: event.OwnerAddress,
	}
	err := exp.storage.SaveOperatorInformation(&oi)
	if err != nil {
		return err
	}
	logger.Debug("managed to save operator information", zap.Any("value", oi))
	reportOperatorIndex(exp.logger, &oi)

	go func() {
		n := exp.ws.BroadcastFeed().Send(api.Message{
			Type:   api.TypeOperator,
			Filter: api.MessageFilter{From: oi.Index, To: oi.Index},
			Data:   []storage.OperatorInformation{oi},
		})
		logger.Debug("msg was sent on outbound feed", zap.Int("num of subscribers", n))
	}()

	return nil
}

// toValidatorInformation converts raw event to ValidatorInformation
func toValidatorInformation(validatorAddedEvent abiparser.ValidatorAddedEvent) (*storage.ValidatorInformation, error) {
	pubKey := &bls.PublicKey{}
	if err := pubKey.Deserialize(validatorAddedEvent.PublicKey); err != nil {
		return nil, errors.Wrap(err, "failed to deserialize validator public key")
	}

	var operators []storage.OperatorNodeLink
	for i, operatorPublicKey := range validatorAddedEvent.OperatorPublicKeys {
		nodeID := uint64(i + 1)
		operators = append(operators, storage.OperatorNodeLink{
			ID: nodeID, PublicKey: string(operatorPublicKey),
		})
	}

	vi := storage.ValidatorInformation{
		PublicKey: pubKey.SerializeToHexStr(),
		Operators: operators,
	}

	return &vi, nil
}
