package mpc

import (
	"github.com/bloxapp/ssv/eth1/abiparser"
	mpcstorage "github.com/bloxapp/ssv/mpc/storage"
	"github.com/pkg/errors"
	"math/big"
)

// createRequest creates a new request object from event
func createRequest(
	distributedKeyRequestedEvent abiparser.DistributedKeyRequestedEvent,
	operatorPubKey string,
) (*mpcstorage.DkgRequest, error) {
	validatorShare, err := RequestFromDistributedRequestEvent(distributedKeyRequestedEvent, operatorPubKey)
	if err != nil {
		return nil, errors.Wrap(err, "could not extract dkg request from event")
	}

	return validatorShare, nil
}

func RequestFromDistributedRequestEvent(
	distributedKeyRequestedEvent abiparser.DistributedKeyRequestedEvent,
	operatorPubKey string,
) (*mpcstorage.DkgRequest, error) {
	dkgRequest := mpcstorage.DkgRequest{}

	dkgRequest.OwnerAddress = distributedKeyRequestedEvent.OwnerAddress.String()

	pk := new(big.Int)
	pk.SetString(operatorPubKey, 16)

	for i := range distributedKeyRequestedEvent.OperatorIds {
		nodeID := uint64(i + 1)
		if distributedKeyRequestedEvent.OperatorIds[i] == pk {
			dkgRequest.NodeID = nodeID
		}
	}

	return &dkgRequest, nil
}
