package commons

import (
	"fmt"

	spectypes "github.com/ssvlabs/ssv-spec/types"
)

// CommitteeTopicID returns the Alan-side committee topic ID. Only referenced by tests: production code
// derives topics from the cached committee subnets (SubnetTopicID/BooleTopic); tests keep
// this as an independent derivation of the expected Alan topic from the committee ID.
func CommitteeTopicID(cid spectypes.CommitteeID) []string {
	return []string{fmt.Sprintf("%d", AlanCommitteeSubnet(cid))}
}
