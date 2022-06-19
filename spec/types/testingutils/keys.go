package testingutils

import (
	"encoding/hex"
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/bloxapp/ssv/spec/types"
	"github.com/bloxapp/ssv/utils/threshold"
	"github.com/herumi/bls-eth-go-binary/bls"
)

var TestingValidatorPubKey = func() spec.BLSPubKey {
	// sk - 3515c7d08e5affd729e9579f7588d30f2342ee6f6a9334acf006345262162c6f
	pk, _ := hex.DecodeString("8e80066551a81b318258709edaf7dd1f63cd686a0e4db8b29bbb7acfe65608677af5a527d9448ee47835485e02b50bc0")
	blsPK := spec.BLSPubKey{}
	copy(blsPK[:], pk)
	return blsPK
}()

var TestingWrongValidatorPubKey = func() spec.BLSPubKey {
	// sk - 3515c7d08e5affd729e9579f7588d30f2342ee6f6a9334acf006345262162c6f
	pk, _ := hex.DecodeString("948fb44582ce25336fdb17122eac64fe5a1afc39174ce92d6013becac116766dc5a778c880dd47de7dfff6a0f86ba42b")
	blsPK := spec.BLSPubKey{}
	copy(blsPK[:], pk)
	return blsPK
}()

/**
SK: 3515c7d08e5affd729e9579f7588d30f2342ee6f6a9334acf006345262162c6f
Validator PK: 8e80066551a81b318258709edaf7dd1f63cd686a0e4db8b29bbb7acfe65608677af5a527d9448ee47835485e02b50bc0
*/

type TestKeySet struct {
	SK                                      *bls.SecretKey
	PK                                      *bls.PublicKey
	ShareCount, Threshold, PartialThreshold uint64
	Shares                                  map[types.OperatorID]*bls.SecretKey
}

func Testing4SharesSet() *TestKeySet {
	return &TestKeySet{
		SK:               skFromHex("3515c7d08e5affd729e9579f7588d30f2342ee6f6a9334acf006345262162c6f"),
		PK:               pkFromHex("8e80066551a81b318258709edaf7dd1f63cd686a0e4db8b29bbb7acfe65608677af5a527d9448ee47835485e02b50bc0"),
		ShareCount:       4,
		Threshold:        3,
		PartialThreshold: 2,
		Shares: map[types.OperatorID]*bls.SecretKey{
			1: skFromHex("5f4711a796c1116b5118ec35279fb64d551d9b38813d2939954dd2df5160d3d9"),
			2: skFromHex("48e4c0a38e90f9352d1d09489446443ebd17b1904f4f0002fe894c2c3f62457a"),
			3: skFromHex("65dc7c179f68347cf12f86e1c51e54e8aeeed579d4c715082bb8a0382c1a8153"),
			4: skFromHex("42409cb09fa945fa6a168cf8b0861045d6e562f211a70c4a1cdbcf0417898763"),
		},
	}
}

func Testing7SharesSet() *TestKeySet {
	return &TestKeySet{
		SK:               skFromHex("3515c7d08e5affd729e9579f7588d30f2342ee6f6a9334acf006345262162c6f"),
		PK:               pkFromHex("8e80066551a81b318258709edaf7dd1f63cd686a0e4db8b29bbb7acfe65608677af5a527d9448ee47835485e02b50bc0"),
		ShareCount:       7,
		Threshold:        5,
		PartialThreshold: 3,
		Shares: map[types.OperatorID]*bls.SecretKey{
			1: skFromHex("49ba32c9899ce444b41dca4c2feadb4503250607f927ce58ec3dec80519d0237"),
			2: skFromHex("63e2e0ca62d9fd14b1a901b9be377308fd354e64663be4da2bfa969e45f977d7"),
			3: skFromHex("4eeeab014330c78d6d53459b075bf8271a588111618f7088388cc88432af05fd"),
			4: skFromHex("3eae7b5de03ccc1db4bfcbaefb50c6d7b155f5f3b71cab850e8e3a37152478d0"),
			5: skFromHex("5b77a53e52792114a532dfb8897761ba1319ff454f19b5be1de1c5e9f4a3efef"),
			6: skFromHex("4e363e1beba2ed5978580b7696f899cf36f845922dfa38ed49b367fee25ade70"),
			7: skFromHex("28486c3189f462fbeab5c6b412083e8462270fbe746c2096e8783f04f95a0ae2"),
		},
	}
}

func Testing10SharesSet() *TestKeySet {
	return &TestKeySet{
		SK:               skFromHex("3515c7d08e5affd729e9579f7588d30f2342ee6f6a9334acf006345262162c6f"),
		PK:               pkFromHex("8e80066551a81b318258709edaf7dd1f63cd686a0e4db8b29bbb7acfe65608677af5a527d9448ee47835485e02b50bc0"),
		ShareCount:       10,
		Threshold:        7,
		PartialThreshold: 4,
		Shares: map[types.OperatorID]*bls.SecretKey{
			1:  skFromHex("37a822a012706efb37521c42a6e3efd8a1efb66e0060eb473785f92539fbedb8"),
			2:  skFromHex("1000b6658c8f62cafd41ac89321ab498a0b5986e39099c248e3e171e170379c9"),
			3:  skFromHex("62020e335b03f3ec3734868dbd072256f8fdb1859e5c9e74d039e8bf0f031aaf"),
			4:  skFromHex("73d8d8e7ed8d454d980636e29d7d7ad654ba2021cc0585e3cef540523fde1ff6"),
			5:  skFromHex("60bc0a6cf4afeb553d9e319928ba3ff722b97610103d9a76addd5bf299adf2a1"),
			6:  skFromHex("6b3fb8aebce28a51ed6b7f4c5481f2833fb429b0fd2cb7df5f40e7bbb3dfc228"),
			7:  skFromHex("26cca03eaf459be04d0a4e5ee3bda0824a85de69eaefbc4041af0e1aad46ca82"),
			8:  skFromHex("2fce3c93f1974af54a6e789d585f53369a045c666a3aae59ddc5963a17233341"),
			9:  skFromHex("1b984d5ddec68c9ae6692cd323b48c7840153f7fa7ad9330c46e1096eb1d87b2"),
			10: skFromHex("52cbe56e1b9ac72ac86114dbb8e62bc8a99a417cbfc00f1a8d8a11ad8c36c812"),
		},
	}
}

func Testing13SharesSet() *TestKeySet {
	return &TestKeySet{
		SK:               skFromHex("3515c7d08e5affd729e9579f7588d30f2342ee6f6a9334acf006345262162c6f"),
		PK:               pkFromHex("8e80066551a81b318258709edaf7dd1f63cd686a0e4db8b29bbb7acfe65608677af5a527d9448ee47835485e02b50bc0"),
		ShareCount:       13,
		Threshold:        9,
		PartialThreshold: 5,
		Shares: map[types.OperatorID]*bls.SecretKey{
			1:  skFromHex("25ea42411deae9e469315d5dd125472e0c519937a1888c667c5f09cbe4618200"),
			2:  skFromHex("310e664cff4c805b5ac0d8bb26df2b3389a7aad91a0ad5714ce9480da353bf85"),
			3:  skFromHex("3095464731c90e5f2c604a3db04c141732aaffa1b411f8885f7a7c35eb265356"),
			4:  skFromHex("472355db4ba7998f7ca18abbc55b513dde6efb994b5378183224b3d00ec56172"),
			5:  skFromHex("1f86be6c4c773f3723e6081e294b8b691b5bd33379e169103a4e0a11e69dd5c3"),
			6:  skFromHex("325aa74cc517ae4371e1b682f55ab720aaeba5a5435e34a54933a36a281ca657"),
			7:  skFromHex("243e354d1ba0e12f2031b031af2a5c9173cc1974cd6a4377089a1c2c67ef61a1"),
			8:  skFromHex("4b1c190ffb475a9e27dfca9fdbd5ba40c47c75e77b688b8e372d354aba7fa586"),
			9:  skFromHex("4c9b41f6fff651976068a8fa387cc2467981d167f5827fdfc8d05063078c9c78"),
			10: skFromHex("37b306c020f2d722ef55f87d8dac4d2955d1db0ed44a15038be11288472f1e93"),
			11: skFromHex("168abf41572818f57b95d54f249c67ec8b883affdc1a3bfd712d4b7ffffe9534"),
			12: skFromHex("6a785563a5921f9004b8320347eeb60b955b673de4de7506111d05597f764029"),
			13: skFromHex("3e8c386cf0756a5ec4212ea598362abfcea66036bc9ea9678551668c6723fc25"),
		},
	}
}

var skFromHex = func(str string) *bls.SecretKey {
	threshold.Init()
	ret := &bls.SecretKey{}
	if err := ret.DeserializeHexStr(str); err != nil {
		panic(err.Error())
	}
	return ret
}

var pkFromHex = func(str string) *bls.PublicKey {
	threshold.Init()
	ret := &bls.PublicKey{}
	byts, err := hex.DecodeString(str)
	if err != nil {
		panic(err.Error())
	}
	if err := ret.Deserialize(byts); err != nil {
		panic(err.Error())
	}
	return ret
}

func (ks *TestKeySet) Committee() []*types.Operator {
	committee := make([]*types.Operator, len(ks.Shares))
	for i, s := range ks.Shares {
		committee[i-1] = &types.Operator{
			OperatorID: i,
			PubKey:     s.GetPublicKey().Serialize(),
		}
	}
	return committee
}
