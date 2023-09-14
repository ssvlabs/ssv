package dutystorage

import (
	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
)

type Storage struct {
	Attester      *Duties[eth2apiv1.AttesterDuty]
	Proposer      *Duties[eth2apiv1.ProposerDuty]
	SyncCommittee *SyncCommitteeDuties
}

func New() *Storage {
	return &Storage{
		Attester:      NewDuties[eth2apiv1.AttesterDuty](),
		Proposer:      NewDuties[eth2apiv1.ProposerDuty](),
		SyncCommittee: NewSyncCommittee(),
	}
}
