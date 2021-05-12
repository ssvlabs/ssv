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
	OperatorContractAddress: "0x423ce2e31Ae8A2752588f067dAB23C8A75A07365",
	ContractABI:             `[{"inputs":[{"internalType":"address","name":"_logic","type":"address"},{"internalType":"address","name":"_admin","type":"address"},{"internalType":"bytes","name":"_data","type":"bytes"}],"stateMutability":"payable","type":"constructor"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"address","name":"previousAdmin","type":"address"},{"indexed":false,"internalType":"address","name":"newAdmin","type":"address"}],"name":"AdminChanged","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"implementation","type":"address"}],"name":"Upgraded","type":"event"},{"stateMutability":"payable","type":"fallback"},{"inputs":[],"name":"admin","outputs":[{"internalType":"address","name":"","type":"address"}],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"newAdmin","type":"address"}],"name":"changeAdmin","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[],"name":"implementation","outputs":[{"internalType":"address","name":"","type":"address"}],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"newImplementation","type":"address"}],"name":"upgradeTo","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"newImplementation","type":"address"},{"internalType":"bytes","name":"data","type":"bytes"}],"name":"upgradeToAndCall","outputs":[],"stateMutability":"payable","type":"function"},{"stateMutability":"payable","type":"receive"}]`,
	OessABI:                 `[{"name":"tuple","type":"function","outputs":[{"type":"tuple","name":"ret","components":[{"type":"bytes","name":"operatorPubKey"},{"type":"uint256","name":"index"},{"type":"bytes","name":"sharePubKey"},{"type":"bytes","name":"encryptedKey"}]}]}]`,
	OessSeparator:           "6f6573732d736570617261746f72",
	OperatorPublicKey:       "2a1b7a4e12a5554bf00d74d0a4df5ef7420599574ee3eca102aee47bc14d5669",
}
