package types

import (
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	typesv2 "github.com/ssvlabs/ssv/protocol/v2/types"
)

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

func ConvertFromGenesisShare(genesisShare *genesisspectypes.Share) *spectypes.Share {
	var key spectypes.ValidatorPK
	copy(key[:], genesisShare.ValidatorPubKey[:])

	share := &spectypes.Share{
		ValidatorPubKey:     key,
		SharePubKey:         genesisShare.SharePubKey,
		Committee:           make([]*spectypes.ShareMember, 0, len(genesisShare.Committee)),
		DomainType:          spectypes.DomainType(genesisShare.DomainType),
		FeeRecipientAddress: genesisShare.FeeRecipientAddress,
		Graffiti:            genesisShare.Graffiti,
	}

	for _, c := range genesisShare.Committee {
		share.Committee = append(share.Committee, &spectypes.ShareMember{
			SharePubKey: c.PubKey,
			Signer:      c.OperatorID,
		})
	}

	return share
}

// ConvertToGenesisSSVShare converts an Alan share to a genesis SSV share.
func ConvertToGenesisSSVShare(alanSSVShare *typesv2.SSVShare, operator *spectypes.CommitteeMember) *SSVShare {
	genesisShare := ConvertToGenesisShare(&alanSSVShare.Share, operator)

	convertedMetadata := Metadata{
		BeaconMetadata: alanSSVShare.Metadata.BeaconMetadata,
		OwnerAddress:   alanSSVShare.Metadata.OwnerAddress,
		Liquidated:     alanSSVShare.Metadata.Liquidated,
		// lastUpdated field is not converted because it's unexported
	}

	return &SSVShare{
		Share:    *genesisShare,
		Metadata: convertedMetadata,
	}
}
