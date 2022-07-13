package threshold

import (
	"fmt"
	"log"
	"strconv"
	"testing"

	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"
)

type shareSet struct {
	sharesCount uint64
	threshold   uint64
	message     []byte
	sk          bls.SecretKey
	skSig       *bls.Sign
	shares      map[spectypes.OperatorID]*bls.SecretKey
}

func generateShares(n uint64, k uint64, message string) (*shareSet, error) {
	set := shareSet{
		sharesCount: n,
		threshold:   k,
		message:     []byte(message),
		sk:          bls.SecretKey{},
	}
	// generate random secret key
	set.sk.SetByCSPRNG()
	set.skSig = set.sk.SignByte(set.message)
	shares, err := Create(set.sk.Serialize(), set.threshold, set.sharesCount)
	set.shares = shares
	return &set, err
}
func TestSplitAndReconstruct(t *testing.T) {
	Init()
	shareSet, err := generateShares(4, 3, "bloxRocks!")
	require.NoError(t, err)

	// partial sigs
	sigVec := make(map[spectypes.OperatorID][]byte)
	for i, s := range shareSet.shares {
		partialSig := s.SignByte(shareSet.message)
		sigVec[i] = partialSig.Serialize()
	}

	// reconstruct
	sig, _ := ReconstructSignatures(sigVec)
	require.True(t, shareSet.skSig.IsEqual(sig))
	require.NoError(t, shareSet.skSig.Deserialize(sig.Serialize()))
	require.True(t, shareSet.skSig.VerifyByte(shareSet.sk.GetPublicKey(), shareSet.message))
}

func TestIncorrectShare(t *testing.T) {
	Init()
	shareSet, err := generateShares(4, 3, "bloxRocks!")
	require.NoError(t, err)

	// replace one share with random one that was not created from sk
	randomShare := bls.SecretKey{}
	randomShare.SetByCSPRNG()
	shareSet.shares[2] = &randomShare

	// partial sigs
	sigVec := make(map[spectypes.OperatorID][]byte)
	for i, s := range shareSet.shares {
		partialSig := s.SignByte(shareSet.message)
		sigVec[i] = partialSig.Serialize()
	}

	// reconstruct
	sig, _ := ReconstructSignatures(sigVec)
	require.False(t, shareSet.skSig.IsEqual(sig))
	require.False(t, sig.VerifyByte(shareSet.sk.GetPublicKey(), shareSet.message))
}

// plain library example
func TestSplitAndReconstructHerumi(t *testing.T) {
	Init()
	count := uint64(4)
	msg := "this is message"
	// generate random secret and split
	msk := make([]bls.SecretKey, count-1)
	mpk := make([]bls.PublicKey, count-1)

	idVec := make([]bls.ID, count)
	secVec := make([]bls.SecretKey, count)
	pubVec := make([]bls.PublicKey, count)
	sigVec := make([]bls.Sign, count)

	for i := uint64(0); i < count-1; i++ {
		sk := bls.SecretKey{}
		sk.SetByCSPRNG()
		msk[i] = sk
		mpk[i] = *sk.GetPublicKey()
	}
	sig := msk[0].Sign(msg)
	log.Printf("master sk: %s", msk[0].SerializeToHexStr())
	log.Printf("master pk: %s", mpk[0].SerializeToHexStr())
	log.Printf("master message: %s \n verify %t", sig.SerializeToHexStr(), sig.Verify(&mpk[0], msg))

	for id := uint64(0); id < count; id++ {
		idVec[id] = bls.ID{}
		// staring from node id 1
		idVec[id].SetLittleEndian([]byte(strconv.Itoa(int(id + 1))))
		//idVec[id].SetDecString(string(id))
		//fmt.Println(idVec[id].GetHexString())

		sk := bls.SecretKey{}
		sk.Set(msk, &idVec[id])
		secVec[id] = sk

		pk := bls.PublicKey{}
		pk.Set(mpk, &idVec[id])
		pubVec[id] = pk

		signature := sk.Sign(msg)
		sigVec[id] = *signature

		log.Printf("sigVec[%d]: \n verify %t", id, signature.Verify(&pk, msg))
	}

	idxVec := [3]uint64{1, 2, 4}

	subIDVec := make([]bls.ID, 3)
	subSecVec := make([]bls.SecretKey, 3)
	subPubVec := make([]bls.PublicKey, 3)
	subSigVec := make([]bls.Sign, 3)

	for i := uint64(0); i < 3; i++ {
		idx := idxVec[i]
		fmt.Println(idx)
		blsID := bls.ID{}
		blsID.SetLittleEndian([]byte(strconv.Itoa(int(i + 1))))
		//blsID.SetDecString(string(idx))
		fmt.Println(blsID.GetHexString())
		subIDVec[i] = blsID
		subSecVec[i] = secVec[i]
		subPubVec[i] = pubVec[i]
		subSigVec[i] = sigVec[i]
	}

	sk := bls.SecretKey{}
	pk := bls.PublicKey{}
	recoverdSig := bls.Sign{}

	sk.Recover(subSecVec, subIDVec)
	pk.Recover(subPubVec, subIDVec)
	recoverdSig.Recover(subSigVec, subIDVec)

	log.Printf("recoverd sk: %s", sk.SerializeToHexStr())
	log.Printf("recoverd pk: %s", pk.SerializeToHexStr())
	log.Printf("recoverd sig: %s", recoverdSig.SerializeToHexStr())
	log.Printf("is sig equal: %t", recoverdSig.SerializeToHexStr() == sig.SerializeToHexStr())
}
