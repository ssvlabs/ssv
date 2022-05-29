package v0

import (
	"github.com/bloxapp/ssv/network/records"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	"github.com/ethereum/go-ethereum/p2p/enode"
)

// DecorateNode decorates the given node's record with version specific fields
func (f *ForkV0) DecorateNode(node *enode.LocalNode, args map[string]interface{}) error {
	if err := records.SetForkVersionEntry(node, forksprotocol.V0ForkVersion.String()); err != nil {
		return err
	}
	oid, ok := args["operatorID"]
	if !ok {
		return records.SetNodeTypeEntry(node, records.Exporter)
	}
	if err := records.SetNodeTypeEntry(node, records.Operator); err != nil {
		return err
	}
	return records.SetOperatorIDEntry(node, oid.(string))
}
