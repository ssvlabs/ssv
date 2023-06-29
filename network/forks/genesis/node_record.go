package genesis

import (
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/network/forks"
	"github.com/bloxapp/ssv/network/records"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/pkg/errors"
)

func DecorateWithDomainType(domainType spectypes.DomainType) forks.NodeRecordDecoration {
	return func(node *enode.LocalNode) error {
		return records.SetDomainTypeEntry(node, domainType)
	}
}

func DecorateWithSubnets(subnets []byte) forks.NodeRecordDecoration {
	return func(node *enode.LocalNode) error {
		return records.SetSubnetsEntry(node, subnets)
	}
}

// DecorateNode will enrich the local node record with more entries, according to current fork
func (f *ForkGenesis) DecorateNode(node *enode.LocalNode, decorations ...forks.NodeRecordDecoration) error {
	if err := records.SetForkVersionEntry(node, forksprotocol.GenesisForkVersion.String()); err != nil {
		return err
	}
	for _, decoration := range decorations {
		if err := decoration(node); err != nil {
			return errors.Wrap(err, "failed to decorate node record")
		}
	}
	return nil
}
