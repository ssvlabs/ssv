package crawler

import (
	"fmt"
	"github.com/libp2p/go-libp2p/core/peer"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"slices"
	"strings"
	"sync"
)

// Start PeerID To version code

var PeerIDtoSignerMtx sync.Mutex

type OperatorInfo struct {
	OpID    spectypes.OperatorID
	Version string
	Subnets string
	IP      string
}

var PeerIDtoSigner map[peer.ID]*OperatorInfo = make(map[peer.ID]*OperatorInfo)

// End PeerID To version code

// Start CommitteeInDomain Code

var CommitteeInDomainMtx sync.Mutex
var CommitteeInDomain = make(map[string]struct{})

func OperatorIDsToString(operatorIDs []spectypes.OperatorID) string {
	if len(operatorIDs) == 0 {
		return ""
	}

	slices.Sort(operatorIDs) // to make sure no duplicates

	// Convert each uint64 to string and join them
	stringIDs := make([]string, len(operatorIDs))
	for i, id := range operatorIDs {
		stringIDs[i] = fmt.Sprintf("%d", id)
	}

	return strings.Join(stringIDs, ",")
}
