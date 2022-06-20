package testingutils

import (
	"github.com/bloxapp/ssv/spec/qbft"
	"github.com/bloxapp/ssv/spec/types"
	"github.com/herumi/bls-eth-go-binary/bls"
)

var MultiSignQBFTMsg = func(sks []*bls.SecretKey, ids []types.OperatorID, msg *qbft.Message) *qbft.SignedMessage {
	if len(sks) == 0 || len(ids) != len(sks) {
		panic("sks != ids")
	}
	var signed *qbft.SignedMessage
	for i, sk := range sks {
		if signed == nil {
			signed = SignQBFTMsg(sk, ids[i], msg)
		} else {
			if err := signed.Aggregate(SignQBFTMsg(sk, ids[i], msg)); err != nil {
				panic(err.Error())
			}
		}
	}

	return signed
}

var SignQBFTMsg = func(sk *bls.SecretKey, id types.OperatorID, msg *qbft.Message) *qbft.SignedMessage {
	domain := types.PrimusTestnet
	sigType := types.QBFTSignatureType

	r, _ := types.ComputeSigningRoot(msg, types.ComputeSignatureDomain(domain, sigType))
	sig := sk.SignByte(r)

	return &qbft.SignedMessage{
		Message:   msg,
		Signers:   []types.OperatorID{id},
		Signature: sig.Serialize(),
	}
}
var ProposalDataBytes = func(data []byte, rcj, pj []*qbft.SignedMessage) []byte {
	d := &qbft.ProposalData{
		Data:                     data,
		RoundChangeJustification: rcj,
		PrepareJustification:     pj,
	}
	ret, _ := d.Encode()
	return ret
}
var PrepareDataBytes = func(data []byte) []byte {
	d := &qbft.PrepareData{
		Data: data,
	}
	ret, _ := d.Encode()
	return ret
}
var CommitDataBytes = func(data []byte) []byte {
	d := &qbft.CommitData{
		Data: data,
	}
	ret, _ := d.Encode()
	return ret
}
var RoundChangeDataBytes = func(preparedValue []byte, preparedRound qbft.Round, nextProposalData []byte) []byte {
	return RoundChangePreparedDataBytes(preparedValue, preparedRound, nextProposalData, nil)
}
var RoundChangePreparedDataBytes = func(preparedValue []byte, preparedRound qbft.Round, nextProposalData []byte, justif []*qbft.SignedMessage) []byte {
	d := &qbft.RoundChangeData{
		PreparedValue:            preparedValue,
		PreparedRound:            preparedRound,
		NextProposalData:         nextProposalData,
		RoundChangeJustification: justif,
	}
	ret, _ := d.Encode()
	return ret
}
