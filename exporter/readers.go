package exporter

import (
	"github.com/bloxapp/ssv/exporter/api"
	"github.com/bloxapp/ssv/exporter/storage"
	"github.com/pkg/errors"
	"sort"
)

// operatorIndexSorter sorts operators by Index
type operatorIndexSorter []storage.OperatorInformation

func (a operatorIndexSorter) Len() int           { return len(a) }
func (a operatorIndexSorter) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a operatorIndexSorter) Less(i, j int) bool { return a[i].Index < a[j].Index }

// getOperators returns a list operators according to the given filter
func getOperators(s storage.OperatorsCollection, filter api.MessageFilter) ([]storage.OperatorInformation, error) {
	var operators []storage.OperatorInformation
	if len(filter.PublicKey) > 0 {
		operator, err := s.GetOperatorInformation(filter.PublicKey)
		if err != nil {
			return nil, errors.Wrap(err, "could not read operator")
		}
		operators = append(operators, *operator)
	} else {
		var err error
		operators, err = s.ListOperators(filter.From, filter.To)
		if err != nil {
			return nil, errors.Wrap(err, "could not read operators")
		}
	}
	sort.Sort(operatorIndexSorter(operators))
	return operators, nil
}

// validatorIndexSorter sorts validators by Index
type validatorIndexSorter []storage.ValidatorInformation

func (a validatorIndexSorter) Len() int           { return len(a) }
func (a validatorIndexSorter) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a validatorIndexSorter) Less(i, j int) bool { return a[i].Index < a[j].Index }

// getValidators returns a list validators according to the given filter
func getValidators(s storage.ValidatorsCollection, filter api.MessageFilter) ([]storage.ValidatorInformation, error) {
	var validators []storage.ValidatorInformation
	if len(filter.PublicKey) > 0 {
		validator, err := s.GetValidatorInformation(filter.PublicKey)
		if err != nil {
			return nil, errors.Wrap(err, "could not read validator")
		}
		validators = append(validators, *validator)
	} else {
		var err error
		validators, err = s.ListValidators(filter.From, filter.To)
		if err != nil {
			return nil, errors.Wrap(err, "could not read validators")
		}
	}
	sort.Sort(validatorIndexSorter(validators))
	return validators, nil
}
