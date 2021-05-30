package eth1

// Eth1 represents the behavior of the eth1 node connector
type Eth1 interface {
	GetContractEvent() *ContractEvent
	// Sync triggers a request to loop over historical events
	Sync() error
}
