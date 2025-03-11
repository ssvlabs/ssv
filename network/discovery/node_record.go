package discovery

import (
	"fmt"

	"github.com/ethereum/go-ethereum/p2p/enode"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/network/records"
)

type NodeRecordDecoration func(*enode.LocalNode) error

func DecorateWithDomainType(key records.ENRKey, domainType spectypes.DomainType) NodeRecordDecoration {
	return func(node *enode.LocalNode) error {
		return records.SetDomainTypeEntry(node, key, domainType)
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
			return fmt.Errorf("failed to decorate node record: %w", err)
		}
	}
	return nil
}
