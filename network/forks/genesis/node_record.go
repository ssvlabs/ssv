package genesis

import (
	"github.com/bloxapp/ssv/network/records"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	"github.com/ethereum/go-ethereum/p2p/enode"
)

// DecorateNode will enrich the local node record with more entries, according to current fork
func (f *ForkGenesis) DecorateNode(node *enode.LocalNode, args map[string]interface{}) error {
	if err := records.SetForkVersionEntry(node, forksprotocol.GenesisForkVersion.String()); err != nil {
		return err
	}
	var subnets []byte
	raw, ok := args["subnets"]
	if !ok {
		subnets = make([]byte, subnetsCount)
	} else {
		subnets = raw.([]byte)
	}
	return records.SetSubnetsEntry(node, subnets)
}
