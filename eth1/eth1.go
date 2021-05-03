package eth1

// Eth1 represents the behavior of the eth1 node connector
type Eth1 interface {
	// StreamSmartContractEvents returns channel with the smart contract events
	StreamSmartContractEvents(contractAddr string) error
	GetEvent() *Event
}

