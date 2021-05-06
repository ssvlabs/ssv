package threshold

import (
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestSplitAndReconstruct(t *testing.T) {
	Init()
	sharesCount := uint64(4)
	threshold := uint64(3)
	message := []byte("bloxRocks!")

	// generate random secret and split
	sk := bls.SecretKey{}
	sk.SetByCSPRNG()

	originalSig := sk.SignByte(message)

	shares, err := Create(sk.Serialize(), threshold, sharesCount)
	require.NoError(t, err)

	// partial sigs
	sigVec := make(map[uint64][]byte)
	for i, s := range shares {
		partialSig := s.SignByte(message)
		sigVec[i] = partialSig.Serialize()
	}

	// reconstruct
	sig, _ := ReconstructSignatures(sigVec)
	require.True(t, originalSig.IsEqual(sig))
	require.NoError(t, originalSig.Deserialize(sig.Serialize()))
	require.True(t, originalSig.VerifyByte(sk.GetPublicKey(), message))
}

// plain library example
//func TestSplitAndReconstructHerumi(t *testing.T) {
//	Init()
//	count := uint64(4)
//	msg := "this is message"
//	// generate random secret and split
//	msk := make([]bls.SecretKey, count-1)
//	mpk := make([]bls.PublicKey, count-1)
//
//	idVec := make([]bls.ID, count)
//	secVec := make([]bls.SecretKey, count)
//	pubVec := make([]bls.PublicKey, count)
//	sigVec := make([]bls.Sign, count)
//
//	for i := uint64(0); i < count-1; i++ {
//		sk := bls.SecretKey{}
//		sk.SetByCSPRNG()
//		msk[i] = sk
//		mpk[i] = *sk.GetPublicKey()
//	}
//	sig := msk[0].Sign(msg)
//	log.Println(fmt.Sprintf("master sk: %s", msk[0].SerializeToHexStr()))
//	log.Println(fmt.Sprintf("master pk: %s", mpk[0].SerializeToHexStr()))
//	log.Println(fmt.Sprintf("master message: %s \n verify %t", sig.SerializeToHexStr(), sig.Verify(&mpk[0], msg)))
//
//	for id := uint64(0); id < count; id++ {
//		idVec[id] = bls.ID{}
//		// staring from node id 1
//		idVec[id].SetLittleEndian([]byte(strconv.Itoa(int(id + 1))))
//		//idVec[id].SetDecString(string(id))
//		//fmt.Println(idVec[id].GetHexString())
//
//		sk := bls.SecretKey{}
//		sk.Set(msk, &idVec[id])
//		secVec[id] = sk
//
//		pk := bls.PublicKey{}
//		pk.Set(mpk, &idVec[id])
//		pubVec[id] = pk
//
//		sig := sk.Sign(msg)
//		sigVec[id] = *sig
//
//		log.Println(fmt.Sprintf("sigVec[%d]: \n verify %t", id, sig.Verify(&pk, msg)))
//	}
//
//	idxVec := [3]uint64{1, 2, 4}
//
//	subIdVec := make([]bls.ID, 3)
//	subSecVec := make([]bls.SecretKey, 3)
//	subPubVec := make([]bls.PublicKey, 3)
//	subSigVec := make([]bls.Sign, 3)
//
//	for i := uint64(0); i < 3; i++ {
//		idx := idxVec[i]
//		fmt.Println(idx)
//		blsID := bls.ID{}
//		blsID.SetLittleEndian([]byte(strconv.Itoa(int(i + 1))))
//		//blsID.SetDecString(string(idx))
//		fmt.Println(blsID.GetHexString())
//		subIdVec[i] = blsID
//		subSecVec[i] = secVec[i]
//		subPubVec[i] = pubVec[i]
//		subSigVec[i] = sigVec[i]
//	}
//
//	sk := bls.SecretKey{}
//	pk := bls.PublicKey{}
//	recoverdSig := bls.Sign{}
//
//	sk.Recover(subSecVec, subIdVec)
//	pk.Recover(subPubVec, subIdVec)
//	recoverdSig.Recover(subSigVec, subIdVec)
//
//	log.Println(fmt.Sprintf("recoverd sk: %s", sk.SerializeToHexStr()))
//	log.Println(fmt.Sprintf("recoverd pk: %s", pk.SerializeToHexStr()))
//	log.Println(fmt.Sprintf("recoverd sig: %s", recoverdSig.SerializeToHexStr()))
//	log.Println(fmt.Sprintf("is sig equal: %t", recoverdSig.SerializeToHexStr() == sig.SerializeToHexStr()))
//}
