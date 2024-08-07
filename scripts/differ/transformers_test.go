package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNoPackageNames(t *testing.T) {
	input :=
		`types.One
*qbft.Two
types.One, qbft.Two
(types.One, &qbft.Two)

mtypes.One
mqbft.Two
typesm.One
qbftm.Two
types, One, spectypes, Two
types+One, spectypes+Two
types.One qbft.Two`
	expected :=
		`One
*Two
One, Two
(One, &Two)

mtypes.One
mqbft.Two
typesm.One
qbftm.Two
types, One, spectypes, Two
types+One, spectypes+Two
One qbft.Two`
	actual := NoPackageNames([]string{"types", "qbft"})(input)
	require.Equal(t, expected, actual)

	input2 := `msgToBroadcast := &spectypes.SignedSSVMessage{
		Signatures:  [][]byte{sig},
		OperatorIDs: []spectypes.OperatorID{r.operatorSigner.GetOperatorID()},
		SSVMessage:  ssvMsg,
	}`
	expected2 := `msgToBroadcast := &SignedSSVMessage{
		Signatures:  [][]byte{sig},
		OperatorIDs: []OperatorID{r.operatorSigner.GetOperatorID()},
		SSVMessage:  ssvMsg,
	}`
	actual2 := NoPackageNames([]string{"spectypes"})(input2)
	require.Equal(t, expected2, actual2)
}
