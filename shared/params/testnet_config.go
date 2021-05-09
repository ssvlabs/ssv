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
	OperatorContractAddress: "0x555fe4a050Bb5f392fD80dCAA2b6FCAf829f21e9",
	ContractABI:             `[{"anonymous":false,"inputs":[{"indexed":false,"internalType":"string","name":"name","type":"string"},{"indexed":false,"internalType":"bytes","name":"pubkey","type":"bytes"},{"indexed":false,"internalType":"address","name":"paymentAddress","type":"address"}],"name":"OperatorAdded","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"bytes","name":"pubkey","type":"bytes"},{"indexed":false,"internalType":"address","name":"ownerAddress","type":"address"},{"indexed":false,"internalType":"bytes","name":"oess","type":"bytes"}],"name":"ValidatorAdded","type":"event"},{"inputs":[{"internalType":"string","name":"_name","type":"string"},{"internalType":"string","name":"_pubkey","type":"string"},{"internalType":"address","name":"_paymentAddress","type":"address"}],"name":"addOperator","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"string","name":"pubkey","type":"string"},{"internalType":"string[]","name":"operatorPubKeys","type":"string[]"},{"internalType":"uint256[]","name":"indexes","type":"uint256[]"},{"internalType":"string[]","name":"sharePubKeys","type":"string[]"},{"internalType":"string[]","name":"encryptedKeys","type":"string[]"},{"internalType":"address","name":"ownerAddress","type":"address"}],"name":"addValidator","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"uint8","name":"c","type":"uint8"}],"name":"fromHexChar","outputs":[{"internalType":"uint8","name":"","type":"uint8"}],"stateMutability":"pure","type":"function"},{"inputs":[],"name":"operatorCount","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"validatorCount","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"}]`,
	OessABI:                 `[{"name":"tuple","type":"function","outputs":[{"type":"tuple","name":"ret","components":[{"type":"bytes","name":"operatorPubKey"},{"type":"uint256","name":"index"},{"type":"bytes","name":"sharePubKey"},{"type":"bytes","name":"encryptedKey"}]}]}]`,
	OessSeparator:           "6f6573732d736570617261746f72",
	OperatorPublicKey:       "2a1b7a4e12a5554bf00d74d0a4df5ef7420599574ee3eca102aee47bc14d5669",
}
