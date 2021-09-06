package validator

import (
	"github.com/bloxapp/ssv/beacon"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestVerifyPartialSignature(t *testing.T) {
	tests := []struct {
		name          string
		skByts        []byte
		root          []byte
		useWrongRoot  bool
		useEmptyRoot  bool
		ibftID        uint64
		expectedError string
	}{
		{
			"valid/ id 1",
			refSplitShares[0],
			[]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9},
			false,
			false,
			1,
			"",
		},
		{
			"valid/ id 2",
			refSplitShares[1],
			[]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 1},
			false,
			false,
			2,
			"",
		},
		{
			"valid/ id 3",
			refSplitShares[2],
			[]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 2},
			false,
			false,
			3,
			"",
		},
		{
			"wrong ibft id",
			refSplitShares[2],
			[]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 2},
			false,
			false,
			2,
			"could not verify signature from iBFT member 2",
		},
		{
			"wrong root",
			refSplitShares[2],
			[]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 2},
			true,
			false,
			3,
			"could not verify signature from iBFT member 3",
		},
		{
			"empty root",
			refSplitShares[2],
			[]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 2},
			false,
			true,
			3,
			"could not verify signature from iBFT member 3",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			identifier := _byteArray("6139636633363061613135666231643164333065653262353738646335383834383233633139363631383836616538623839323737356363623362643936623764373334353536396132616130623134653464303135633534613661306335345f4154544553544552")
			node := testingValidator(t, true, 4, identifier)

			sk := &bls.SecretKey{}
			require.NoError(t, sk.Deserialize(test.skByts))

			sig := sk.SignByte(test.root)

			usedRoot := test.root
			if test.useWrongRoot {
				usedRoot = []byte{0, 0, 0, 0, 0, 0, 0}
			}
			if test.useEmptyRoot {
				usedRoot = []byte{}
			}

			err := node.verifyPartialSignature(sig.Serialize(), usedRoot, test.ibftID, node.ibfts[beacon.RoleTypeAttester].GetIBFTCommittee()) // TODO need to fetch the committee from storage
			if len(test.expectedError) > 0 {
				require.EqualError(t, err, test.expectedError)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
