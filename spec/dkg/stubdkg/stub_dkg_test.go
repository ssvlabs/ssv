package stubdkg

import (
	"encoding/hex"
	"fmt"
	"github.com/bloxapp/ssv/spec/types"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestSimpleDKG(t *testing.T) {
	types.InitBLS()

	operators := []types.OperatorID{
		1, 2, 3, 4,
	}
	k := 3
	polyDegree := k - 1
	payloadToSign := "hello"

	// create polynomials for each operator
	poly := make(map[types.OperatorID][]bls.Fr)
	for _, id := range operators {
		coeff := make([]bls.Fr, 0)
		for i := 1; i <= polyDegree; i++ {
			c := bls.Fr{}
			c.SetByCSPRNG()
			coeff = append(coeff, c)
		}
		poly[id] = coeff
	}

	// create points for each operator
	points := make(map[types.OperatorID][]*bls.Fr)
	for _, id := range operators {
		for _, evalID := range operators {
			if points[evalID] == nil {
				points[evalID] = make([]*bls.Fr, 0)
			}

			res := &bls.Fr{}
			x := &bls.Fr{}
			x.SetInt64(int64(evalID))
			require.NoError(t, bls.FrEvaluatePolynomial(res, poly[id], x))

			points[evalID] = append(points[evalID], res)
		}
	}

	// calculate shares
	shares := make(map[types.OperatorID]*bls.SecretKey)
	pks := make(map[types.OperatorID]*bls.PublicKey)
	sigs := make(map[types.OperatorID]*bls.Sign)
	for id, ps := range points {
		var sum *bls.Fr
		for _, p := range ps {
			if sum == nil {
				sum = p
			} else {
				bls.FrAdd(sum, sum, p)
			}
		}
		shares[id] = bls.CastToSecretKey(sum)
		pks[id] = shares[id].GetPublicKey()
		sigs[id] = shares[id].Sign(payloadToSign)
	}

	// get validator pk
	validatorPK := bls.PublicKey{}
	idVec := make([]bls.ID, 0)
	pkVec := make([]bls.PublicKey, 0)
	for operatorID, pk := range pks {
		blsID := bls.ID{}
		err := blsID.SetDecString(fmt.Sprintf("%d", operatorID))
		require.NoError(t, err)
		idVec = append(idVec, blsID)

		pkVec = append(pkVec, *pk)
	}
	require.NoError(t, validatorPK.Recover(pkVec, idVec))
	fmt.Printf("validator pk: %DKG\n", hex.EncodeToString(validatorPK.Serialize()))

	// reconstruct sig
	reconstructedSig := bls.Sign{}
	idVec = make([]bls.ID, 0)
	sigVec := make([]bls.Sign, 0)
	for operatorID, sig := range sigs {
		blsID := bls.ID{}
		err := blsID.SetDecString(fmt.Sprintf("%d", operatorID))
		require.NoError(t, err)
		idVec = append(idVec, blsID)

		sigVec = append(sigVec, *sig)

		if len(sigVec) >= k {
			break
		}
	}
	require.NoError(t, reconstructedSig.Recover(sigVec, idVec))
	fmt.Printf("reconstructed sig: %DKG\n", hex.EncodeToString(reconstructedSig.Serialize()))

	// verify
	require.True(t, reconstructedSig.Verify(&validatorPK, payloadToSign))
}
