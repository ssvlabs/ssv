package types

import (
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

func ConvertToGenesisShare(share *spectypes.Share, operator *spectypes.CommitteeMember) *genesisspectypes.Share {
	genesisShare := &genesisspectypes.Share{}

	genesisShare.OperatorID = operator.OperatorID
	genesisShare.ValidatorPubKey = share.ValidatorPubKey[:]
	genesisShare.SharePubKey = share.SharePubKey
	genesisShare.Committee = make([]*genesisspectypes.Operator, 0, len(share.Committee))
	for _, c := range share.Committee {
		genesisShare.Committee = append(genesisShare.Committee, &genesisspectypes.Operator{
			OperatorID: c.Signer,
			PubKey:     c.SharePubKey,
		})
	}
	genesisShare.Quorum = operator.GetQuorum()
	genesisShare.DomainType = genesisspectypes.DomainType(share.DomainType)
	genesisShare.FeeRecipientAddress = share.FeeRecipientAddress
	genesisShare.Graffiti = share.Graffiti
	return genesisShare
}
