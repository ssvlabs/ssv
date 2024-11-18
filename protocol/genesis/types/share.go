package types

import (
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	beaconprotocol "github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	typesv2 "github.com/ssvlabs/ssv/protocol/v2/types"
)

func ConvertToGenesisShare(share *spectypes.Share, operator *spectypes.CommitteeMember) *genesisspectypes.Share {
	q, pc := ComputeQuorumAndPartialQuorum(uint64(len((share.Committee))))

	key := make([]byte, len(share.ValidatorPubKey))
	copy(key, share.ValidatorPubKey[:])

	genesisShare := &genesisspectypes.Share{
		OperatorID:          operator.OperatorID,
		SharePubKey:         make([]byte, len(share.SharePubKey)),
		ValidatorPubKey:     key,
		Committee:           make([]*genesisspectypes.Operator, 0, len(share.Committee)),
		Quorum:              q,
		PartialQuorum:       pc,
		DomainType:          genesisspectypes.DomainType(share.DomainType),
		FeeRecipientAddress: share.FeeRecipientAddress,
		Graffiti:            make([]byte, len(share.Graffiti)),
	}

	copy(genesisShare.SharePubKey, share.SharePubKey)
	copy(genesisShare.Graffiti, share.Graffiti)

	for _, c := range share.Committee {
		newMember := &genesisspectypes.Operator{
			OperatorID: c.Signer,
			PubKey:     make([]byte, len(c.SharePubKey)),
		}
		copy(newMember.PubKey, c.SharePubKey)
		genesisShare.Committee = append(genesisShare.Committee, newMember)
	}

	return genesisShare
}

// ConvertToGenesisSSVShare converts an Alan share to a genesis SSV share.
func ConvertToGenesisSSVShare(alanSSVShare *typesv2.SSVShare, operator *spectypes.CommitteeMember) *SSVShare {
	genesisShare := ConvertToGenesisShare(&alanSSVShare.Share, operator)

	convertedMetadata := Metadata{
		BeaconMetadata: &beaconprotocol.ValidatorMetadata{
			Balance:         alanSSVShare.Balance,
			Status:          alanSSVShare.Status,
			Index:           alanSSVShare.ValidatorIndex,
			ActivationEpoch: alanSSVShare.ActivationEpoch,
		},
		OwnerAddress: alanSSVShare.OwnerAddress,
		Liquidated:   alanSSVShare.Liquidated,
		// lastUpdated field is not converted because it's unexported
	}

	return &SSVShare{
		Share:    *genesisShare,
		Metadata: convertedMetadata,
	}
}
