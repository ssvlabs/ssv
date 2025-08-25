package storage

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_storageShare_encoding_decoding(t *testing.T) {
	// this test checks current Share struct is backward-compatible with encoded []byte
	// representation of Share stored in our DB, if this test fails it means we need to
	// update encoded []byte representation of Share stored in our DB (via DB migration)

	// use this code snippet to generate new encoded []byte representation of Share struct
	// we'll be expecting from now on
	// t.Run("generating new encoding for Share", func(t *testing.T) {
	// 	stShareOld := &Share{
	// 		ValidatorIndex:      1680836,
	// 		ValidatorPubKey:     []uint8{131, 207, 238, 243, 110, 159, 17, 79, 36, 50, 179, 159, 243, 244, 127, 139, 21, 211, 144, 116, 169, 105, 96, 55, 123, 59, 14, 95, 14, 66, 32, 153, 238, 84, 164, 237, 218, 248, 129, 92, 143, 226, 218, 184, 149, 40, 200, 110},
	// 		SharePubKey:         []uint8{143, 95, 128, 60, 53, 49, 202, 142, 235, 182, 44, 248, 88, 194, 40, 168, 114, 157, 86, 139, 175, 201, 224, 125, 121, 125, 108, 131, 54, 132, 116, 245, 194, 23, 15, 101, 56, 145, 170, 213, 95, 218, 116, 168, 37, 229, 209, 228},
	// 		Committee:           []*storageOperator{{OperatorID: 275, PubKey: []uint8{167, 84, 254, 250, 155, 244, 185, 103, 217, 88, 26, 199, 169, 71, 109, 198, 90, 128, 110, 66, 87, 60, 99, 34, 173, 158, 185, 14, 53, 39, 252, 22, 52, 175, 235, 235, 63, 117, 50, 215, 200, 11, 132, 121, 75, 41, 146, 189}}, {OperatorID: 276, PubKey: []uint8{143, 95, 128, 60, 53, 49, 202, 142, 235, 182, 44, 248, 88, 194, 40, 168, 114, 157, 86, 139, 175, 201, 224, 125, 121, 125, 108, 131, 54, 132, 116, 245, 194, 23, 15, 101, 56, 145, 170, 213, 95, 218, 116, 168, 37, 229, 209, 228}}, {OperatorID: 277, PubKey: []uint8{177, 210, 143, 196, 201, 131, 9, 232, 36, 155, 177, 191, 183, 55, 204, 215, 228, 168, 153, 108, 230, 146, 149, 142, 132, 138, 167, 208, 238, 65, 255, 172, 197, 98, 197, 185, 206, 134, 157, 111, 59, 105, 215, 42, 27, 68, 64, 142}}, {OperatorID: 278, PubKey: []uint8{151, 233, 189, 2, 126, 151, 105, 163, 79, 183, 48, 255, 245, 203, 168, 120, 35, 80, 130, 103, 179, 58, 168, 67, 244, 75, 69, 10, 206, 6, 240, 233, 165, 170, 237, 244, 221, 171, 37, 87, 55, 225, 232, 75, 123, 184, 253, 74}}},
	// 		DomainType:          types.DomainType{0, 0, 49, 19},
	// 		FeeRecipientAddress: [20]uint8{209, 220, 134, 149, 86, 241, 243, 2, 125, 239, 2, 157, 35, 205, 9, 104, 207, 8, 203, 14},
	// 		Graffiti:            []uint8{131, 207, 238, 243, 110, 159, 17, 79, 36, 50, 179, 159, 243, 244, 127, 139, 21, 211, 144, 116, 169, 105, 96, 55, 123, 59},
	// 		Status:              3,
	// 		ActivationEpoch:     47275,
	// 		ExitEpoch:           math.MaxUint64,
	// 		OwnerAddress:        common.Address{92, 192, 221, 225, 78, 114, 86, 52, 12, 200, 32, 65, 90, 96, 34, 167, 209, 201, 58, 53},
	// 		Liquidated:          true,
	// 	}
	// 	stShareEncodedOld, err := stShareOld.Encode()
	// 	require.NoError(t, err)
	// 	fmt.Printf("stShareEncodedOld: %s", hex.EncodeToString(stShareEncodedOld))
	// })

	t.Run("verify Share is backward-compatible with old encoding", func(t *testing.T) {
		stShareEncodedOldStr := "c4a519000000000083cfeef36e9f114f2432b39ff3f47f8b15d39074a96960377b3b0e5f0e422099ee54a4eddaf8815c8fe2dab89528c86e89000000b900000000003113d1dc869556f1f3027def029d23cd0968cf08cb0eb90100000300000000000000abb8000000000000ffffffffffffffff5cc0dde14e7256340cc820415a6022a7d1c93a35018f5f803c3531ca8eebb62cf858c228a8729d568bafc9e07d797d6c83368474f5c2170f653891aad55fda74a825e5d1e4100000004c00000088000000c400000013010000000000000c000000a754fefa9bf4b967d9581ac7a9476dc65a806e42573c6322ad9eb90e3527fc1634afebeb3f7532d7c80b84794b2992bd14010000000000000c0000008f5f803c3531ca8eebb62cf858c228a8729d568bafc9e07d797d6c83368474f5c2170f653891aad55fda74a825e5d1e415010000000000000c000000b1d28fc4c98309e8249bb1bfb737ccd7e4a8996ce692958e848aa7d0ee41ffacc562c5b9ce869d6f3b69d72a1b44408e16010000000000000c00000097e9bd027e9769a34fb730fff5cba87823508267b33aa843f44b450ace06f0e9a5aaedf4ddab255737e1e84b7bb8fd4a83cfeef36e9f114f2432b39ff3f47f8b15d39074a96960377b3b"
		stShareEncodedOld, err := hex.DecodeString(stShareEncodedOldStr)
		require.NoError(t, err)

		stShareOld := &Share{}
		err = stShareOld.Decode(stShareEncodedOld)
		require.NoError(t, err)
	})
}
