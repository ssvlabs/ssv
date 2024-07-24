package types

import (
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

func ConvertToGenesisShare(share *spectypes.Share, operator *spectypes.CommitteeMember) *genesisspectypes.Share {
	genesisShare := &genesisspectypes.Share{
		OperatorID:          operator.OperatorID,
		ValidatorPubKey:     share.ValidatorPubKey[:], // Ensure this is necessary; remove if ValidatorPubKey is already a slice.
		SharePubKey:         share.SharePubKey,
		Committee:           make([]*genesisspectypes.Operator, 0, len(share.Committee)),
		Quorum:              operator.GetQuorum(),
		DomainType:          genesisspectypes.DomainType(share.DomainType),
		FeeRecipientAddress: share.FeeRecipientAddress,
		Graffiti:            share.Graffiti,
	}

	for _, c := range share.Committee {
		genesisShare.Committee = append(genesisShare.Committee, &genesisspectypes.Operator{
			OperatorID: c.Signer,
			PubKey:     c.SharePubKey,
		})
	}

	return genesisShare
}
