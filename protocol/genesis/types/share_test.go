package types

import (
	"bytes"
	"testing"

	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"
)

func TestConvertShare(t *testing.T) {
	// Step 1: Create an Alan share with all fields populated
	originalShare := &spectypes.Share{
		ValidatorPubKey: [48]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35, 36, 37, 38, 39, 40, 41, 42, 43, 44, 45, 46, 47, 48},
		SharePubKey:     []byte{49, 50, 51, 52, 53, 54, 55, 56, 57, 58, 59, 60},
		Committee: []*spectypes.ShareMember{
			{SharePubKey: []byte{61, 62, 63, 64, 65, 66, 67, 68, 69, 70, 71, 72}, Signer: 1},
			{SharePubKey: []byte{73, 74, 75, 76, 77, 78, 79, 80, 81, 82, 83, 84}, Signer: 2},
			{SharePubKey: []byte{85, 86, 87, 88, 89, 90, 91, 92, 93, 94, 95, 96}, Signer: 3},
			{SharePubKey: []byte{97, 98, 99, 100, 101, 102, 103, 104, 105, 106, 107, 108}, Signer: 4},
		},
		DomainType:          [4]byte{109, 110, 111, 112},
		FeeRecipientAddress: [20]byte{113, 114, 115, 116, 117, 118, 119, 120, 121, 122, 123, 124, 125, 126, 127, 128, 129, 130, 131, 132},
		Graffiti:            []byte{133, 134, 135, 136, 137, 138, 139, 140, 141, 142, 143, 144},
	}

	// Step 2: Convert to Genesis share
	genesisShare := ConvertToGenesisShare(originalShare, &spectypes.CommitteeMember{OperatorID: 1})

	// Step 3: Convert share back to Alan share
	convertedBackShare := convertFromGenesisShare(genesisShare)

	// Step 4: Expect equality with original share
	require.True(t, sharesEqual(originalShare, convertedBackShare), "The converted back share should be equal to the original share")

	// Step 5: Modify final share and expect inequality
	convertedBackShare.Graffiti[0] = 200
	require.False(t, sharesEqual(originalShare, convertedBackShare), "The modified converted back share should not be equal to the original share")

	// Step 6: Modify original share in the same way and expect equality again
	originalShare.Graffiti[0] = 200
	require.True(t, sharesEqual(originalShare, convertedBackShare), "The modified original share should be equal to the modified converted back share")

	// Step 7: Modify committee member's signer in the final share and expect inequality
	convertedBackShare.Committee[0].Signer = 420
	require.False(t, sharesEqual(originalShare, convertedBackShare), "The modified converted back share with changed committee signer should not be equal to the original share")

	// Step 8: Modify original share's committee member signer in the same way and expect equality again
	originalShare.Committee[0].Signer = 420
	require.True(t, sharesEqual(originalShare, convertedBackShare), "The modified original share with changed committee signer should be equal to the modified converted back share")

}

func sharesEqual(share1, share2 *spectypes.Share) bool {
	if !bytes.Equal(share1.ValidatorPubKey[:], share2.ValidatorPubKey[:]) {
		return false
	}
	if !bytes.Equal(share1.SharePubKey, share2.SharePubKey) {
		return false
	}
	if !bytes.Equal(share1.DomainType[:], share2.DomainType[:]) {
		return false
	}
	if !bytes.Equal(share1.FeeRecipientAddress[:], share2.FeeRecipientAddress[:]) {
		return false
	}
	if !bytes.Equal(share1.Graffiti, share2.Graffiti) {
		return false
	}
	if len(share1.Committee) != len(share2.Committee) {
		return false
	}
	for i := range share1.Committee {
		if share1.Committee[i].Signer != share2.Committee[i].Signer {
			return false
		}
		if !bytes.Equal(share1.Committee[i].SharePubKey, share2.Committee[i].SharePubKey) {
			return false
		}
	}
	return true
}

func convertFromGenesisShare(genesisShare *genesisspectypes.Share) *spectypes.Share {
	var key spectypes.ValidatorPK
	copy(key[:], genesisShare.ValidatorPubKey[:])

	share := &spectypes.Share{
		ValidatorPubKey:     key,
		SharePubKey:         make([]byte, len(genesisShare.SharePubKey)),
		Committee:           make([]*spectypes.ShareMember, 0, len(genesisShare.Committee)),
		DomainType:          spectypes.DomainType(genesisShare.DomainType),
		FeeRecipientAddress: genesisShare.FeeRecipientAddress,
		Graffiti:            make([]byte, len(genesisShare.Graffiti)),
	}

	copy(share.SharePubKey, genesisShare.SharePubKey)
	copy(share.Graffiti, genesisShare.Graffiti)

	for _, c := range genesisShare.Committee {
		newMember := &spectypes.ShareMember{
			SharePubKey: make([]byte, len(c.PubKey)),
			Signer:      c.OperatorID,
		}
		copy(newMember.SharePubKey, c.PubKey)
		share.Committee = append(share.Committee, newMember)
	}

	return share
}
