package records

import (
	"github.com/ethereum/go-ethereum/p2p/enode"

	spectypes "github.com/ssvlabs/ssv-spec/types"
)

func UpdateENRDomainType(node *enode.LocalNode, domain spectypes.DomainType) ([]byte, error) {
	if err := SetDomainTypeEntry(node, domain); err != nil {
		return nil, err
	}
	return domain[:], nil
}