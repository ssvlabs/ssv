package discovery

import (
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/network/records"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/pkg/errors"
)

type NodeRecordDecoration func(*enode.LocalNode) error

func DecorateWithDomainType(domainType spectypes.DomainType) NodeRecordDecoration {
	return func(node *enode.LocalNode) error {
		return records.SetDomainTypeEntry(node, domainType)
	}
}

func DecorateWithSubnets(subnets []byte) NodeRecordDecoration {
	return func(node *enode.LocalNode) error {
		return records.SetSubnetsEntry(node, subnets)
	}
}

// DecorateNode will enrich the local node record with more entries, according to current fork
func DecorateNode(node *enode.LocalNode, decorations ...NodeRecordDecoration) error {
	for _, decoration := range decorations {
		if err := decoration(node); err != nil {
			return errors.Wrap(err, "failed to decorate node record")
		}
	}
	return nil
}
