package types

import (
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv/protocol/v2/types"
)

func ConvertToGenesisShare(share *spectypes.Share, operator *spectypes.CommitteeMember) *genesisspectypes.Share {
	q, pc := types.ComputeQuorumAndPartialQuorum(len(share.Committee))
	genesisShare := &genesisspectypes.Share{
		OperatorID:          operator.OperatorID,
		SharePubKey:         share.SharePubKey,
		Committee:           make([]*genesisspectypes.Operator, 0, len(share.Committee)),
		Quorum:              q,
		PartialQuorum:       pc,
		DomainType:          genesisspectypes.DomainType(share.DomainType),
		FeeRecipientAddress: share.FeeRecipientAddress,
		Graffiti:            share.Graffiti,
	}
	copy(genesisShare.ValidatorPubKey[:], share.ValidatorPubKey[:])

	for _, c := range share.Committee {
		genesisShare.Committee = append(genesisShare.Committee, &genesisspectypes.Operator{
			OperatorID: c.Signer,
			PubKey:     c.SharePubKey,
		})
	}

	return genesisShare
}
