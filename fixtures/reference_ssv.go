package fixtures

import "encoding/hex"

func _byteArray(input string) []byte {
	res, _ := hex.DecodeString(input)
	return res
}

var (
	// RefSk is a reference testing private key
	RefSk = _byteArray("2c083f2c8fc923fa2bd32a70ab72b4b46247e8c1f347adc30b2f8036a355086c")
	// RefPk is the PK of RefSk
	RefPk = _byteArray("a9cf360aa15fb1d1d30ee2b578dc5884823c19661886ae8b892775ccb3bd96b7d7345569a2aa0b14e4d015c54a6a0c54")

	// RefSplitShares is RefSk split into 4 shares
	RefSplitShares = [][]byte{ // sk split to 4: 2c083f2c8fc923fa2bd32a70ab72b4b46247e8c1f347adc30b2f8036a355086c
		_byteArray("1a1b411e54ebb0973dc0f133c8b192cc4320fd464cbdcfe3be38b77f821f30bc"),
		_byteArray("6a93d37661cfe9cbaff9f051f2dd1d1995905932375e09357be1a50f7f4de323"),
		_byteArray("3596a78e633ad5071c0a77bb16b1a391b21ab47fb32ba1ba442a48e89ae11f9f"),
		_byteArray("62ff0c0cac676cd9e866377f4772d63f403b5734c02351701712a308d4d8e632"),
	}
	// RefSplitSharesPubKeys is the PK for RefSplitShares
	RefSplitSharesPubKeys = [][]byte{
		_byteArray("84d90424a5511e3741ac3c99ee1dba39007a290410e805049d0ae40cde74191d785d7848f08b2dfb99b742ebfe846e3b"),
		_byteArray("b6ac738a09a6b7f3fb4f85bac26d8965f6329d431f484e8b43633f7b7e9afce0085bb592ea90df6176b2f2bd97dfd7f3"),
		_byteArray("a261c25548320f1aabfc2aac5da3737a0b8bbc992a5f4f937259d22d39fbf6ebf8ec561720de3a04f661c9772fcace96"),
		_byteArray("85dd2d89a3e320995507c46320f371dc85eb16f349d1c56d71b58663b5b6a5fd390fcf41cf9098471eb5437fd95be1ac"),
	}
)
