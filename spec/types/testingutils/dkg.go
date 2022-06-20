package testingutils

import (
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/hex"
	"github.com/bloxapp/ssv/spec/dkg"
	"github.com/bloxapp/ssv/spec/dkg/stubdkg"
	"github.com/bloxapp/ssv/spec/types"
)

var TestingWithdrawalCredentials, _ = hex.DecodeString("0x010000000000000000000000535953b5a6040074948cf185eaa7d2abbd66808f")

var TestingDKGNode = func(keySet *TestKeySet) *dkg.Node {
	network := NewTestingNetwork()
	config := &dkg.Config{
		Protocol: func(network dkg.Network, operatorID types.OperatorID, identifier dkg.RequestID) dkg.Protocol {
			ret := stubdkg.New(network, operatorID, identifier)
			ret.(*stubdkg.DKG).SetOperators(
				Testing4SharesSet().ValidatorPK.Serialize(),
				Testing4SharesSet().Shares,
			)
			return ret
		},
		Network:             network,
		Storage:             NewTestingStorage(),
		SignatureDomainType: types.PrimusTestnet,
		Signer:              NewTestingKeyManager(),
	}

	return dkg.NewNode(&dkg.Operator{
		OperatorID:       1,
		ETHAddress:       keySet.DKGOperators[1].ETHAddress,
		EncryptionPubKey: &keySet.DKGOperators[1].EncryptionKey.PublicKey,
	}, config)
}

var SignDKGMsg = func(sk *ecdsa.PrivateKey, id types.OperatorID, msg *dkg.Message) *dkg.SignedMessage {
	domain := types.PrimusTestnet
	sigType := types.DKGSignatureType

	r, _ := types.ComputeSigningRoot(msg, types.ComputeSignatureDomain(domain, sigType))
	sig, _ := sk.Sign(rand.Reader, r, nil)

	return &dkg.SignedMessage{
		Message:   msg,
		Signer:    id,
		Signature: sig,
	}
}

var InitMessageDataBytes = func(operators []types.OperatorID, threshold uint16, withdrawalCred []byte) []byte {
	m := &dkg.Init{
		OperatorIDs:           operators,
		Threshold:             threshold,
		WithdrawalCredentials: withdrawalCred,
	}
	byts, _ := m.Encode()
	return byts
}
