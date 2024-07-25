package types

import (
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	typesv2 "github.com/ssvlabs/ssv/protocol/v2/types"
)

// TODO: (Alan) write tests with all fields and equality checks
func ConvertToGenesisShare(share *spectypes.Share, operator *spectypes.CommitteeMember) *genesisspectypes.Share {
	q, pc := ComputeQuorumAndPartialQuorum(len(share.Committee))

	key := make([]byte, len(share.ValidatorPubKey))
	copy(key, share.ValidatorPubKey[:])

	genesisShare := &genesisspectypes.Share{
		OperatorID:          operator.OperatorID,
		SharePubKey:         share.SharePubKey,
		ValidatorPubKey:     key,
		Committee:           make([]*genesisspectypes.Operator, 0, len(share.Committee)),
		Quorum:              q,
		PartialQuorum:       pc,
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

// TODO: (Alan) write tests with all fields and equality checks
func ConvertToAlanShare(alanShare *typesv2.SSVShare, operator *spectypes.CommitteeMember) *SSVShare {
	q, pc := ComputeQuorumAndPartialQuorum(len(alanShare.Committee))

	key := make([]byte, len(alanShare.ValidatorPubKey))
	copy(key, alanShare.ValidatorPubKey[:])

	share := &SSVShare{
		Share: genesisspectypes.Share{
			OperatorID:          operator.OperatorID,
			ValidatorPubKey:     key, // Ensure this is necessary; remove if ValidatorPubKey is already a slice.
			SharePubKey:         alanShare.SharePubKey,
			Committee:           make([]*genesisspectypes.Operator, 0, len(alanShare.Committee)),
			Quorum:              q,
			PartialQuorum:       pc,
			DomainType:          genesisspectypes.DomainType(alanShare.DomainType),
			FeeRecipientAddress: alanShare.FeeRecipientAddress,
			Graffiti:            alanShare.Graffiti,
		},
	}

	for _, c := range alanShare.Committee {
		share.Committee = append(share.Committee, &genesisspectypes.Operator{
			OperatorID: c.Signer,
			PubKey:     c.SharePubKey,
		})
	}

	return share
}
