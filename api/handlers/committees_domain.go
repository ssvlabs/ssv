package handlers

import (
	"github.com/ssvlabs/ssv/api"
	"github.com/ssvlabs/ssv/message/validation"
	"net/http"
)

type CommitteInOurDomain struct {
	OperatorIDs string `json:"operator_ids"`
}

type CommitteeDomainJSON struct {
	Data []*CommitteInOurDomain `json:"data"`
}

type CommitteDomainList struct {
}

func (c *CommitteDomainList) List(w http.ResponseWriter, r *http.Request) error {
	opt := &CommitteeDomainJSON{make([]*CommitteInOurDomain, 0)}
	validation.CommitteeInDomainMtx.Lock()
	defer validation.CommitteeInDomainMtx.Unlock()

	for k := range validation.CommitteeInDomain {
		opt.Data = append(opt.Data, &CommitteInOurDomain{OperatorIDs: k})
	}
	return api.Render(w, r, opt)
}
