package dutystore

import (
	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"

	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
)

type Store struct {
	Attester      *Duties[eth2apiv1.AttesterDuty]
	Proposer      *Duties[eth2apiv1.ProposerDuty]
	PTC           *Duties[gloas.PTCDuty]
	SyncCommittee *SyncCommitteeDuties
	VoluntaryExit *VoluntaryExitDuties
}

func New() *Store {
	return &Store{
		Attester:      NewDuties[eth2apiv1.AttesterDuty](),
		Proposer:      NewDuties[eth2apiv1.ProposerDuty](),
		PTC:           NewDuties[gloas.PTCDuty](),
		SyncCommittee: NewSyncCommitteeDuties(),
		VoluntaryExit: NewVoluntaryExit(),
	}
}
