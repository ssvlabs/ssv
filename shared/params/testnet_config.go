package params

// UseTestnetConfig sets config for the testnet
func UseTestnetConfig() {
	ssvConfig = TestnetConfig()
}

// TestnetConfig defines the config for the testnet.
func TestnetConfig() *SsvNetworkConfig {
	return testnetSsvConfig
}

var testnetSsvConfig = &SsvNetworkConfig{
	OperatorContractAddress: "0x1970F94e594b28b3c4C19eF1DC83621b58304cCa",
	ContractABI:             `[{"anonymous":false,"inputs":[{"indexed":false,"internalType":"bytes","name":"validatorPublicKey","type":"bytes"},{"indexed":false,"internalType":"uint256","name":"index","type":"uint256"},{"indexed":false,"internalType":"bytes","name":"operatorPublicKey","type":"bytes"},{"indexed":false,"internalType":"bytes","name":"sharedPublicKey","type":"bytes"},{"indexed":false,"internalType":"bytes","name":"encryptedKey","type":"bytes"}],"name":"OessAdded","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"string","name":"name","type":"string"},{"indexed":false,"internalType":"address","name":"ownerAddress","type":"address"},{"indexed":false,"internalType":"bytes","name":"publicKey","type":"bytes"}],"name":"OperatorAdded","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"address","name":"ownerAddress","type":"address"},{"indexed":false,"internalType":"bytes","name":"publicKey","type":"bytes"},{"components":[{"internalType":"uint256","name":"index","type":"uint256"},{"internalType":"bytes","name":"operatorPublicKey","type":"bytes"},{"internalType":"bytes","name":"sharedPublicKey","type":"bytes"},{"internalType":"bytes","name":"encryptedKey","type":"bytes"}],"indexed":false,"internalType":"structISSVNetwork.Oess[]","name":"oessList","type":"tuple[]"}],"name":"ValidatorAdded","type":"event"},{"inputs":[{"internalType":"string","name":"_name","type":"string"},{"internalType":"address","name":"_ownerAddress","type":"address"},{"internalType":"bytes","name":"_publicKey","type":"bytes"}],"name":"addOperator","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"_ownerAddress","type":"address"},{"internalType":"bytes","name":"_publicKey","type":"bytes"},{"internalType":"bytes[]","name":"_operatorPublicKeys","type":"bytes[]"},{"internalType":"bytes[]","name":"_sharesPublicKeys","type":"bytes[]"},{"internalType":"bytes[]","name":"_encryptedKeys","type":"bytes[]"}],"name":"addValidator","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[],"name":"operatorCount","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"bytes","name":"","type":"bytes"}],"name":"operators","outputs":[{"internalType":"string","name":"name","type":"string"},{"internalType":"address","name":"ownerAddress","type":"address"},{"internalType":"bytes","name":"publicKey","type":"bytes"},{"internalType":"uint256","name":"score","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"validatorCount","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"}]`,
	OperatorPublicKey:       "2a1b7a4e12a5554bf00d74d0a4df5ef7420599574ee3eca102aee47bc14d5669",
}
