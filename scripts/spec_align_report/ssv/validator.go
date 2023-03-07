package ssv

import "github.com/bloxapp/ssv/scripts/spec_align_report/utils"

func ProcessValidator() {
	if err := utils.Mkdir(utils.DataPath+"/validator", true); err != nil {
		panic(err)
	}

	validatorCompareStruct := initValidatorCompareStruct()
	if err := validatorCompareStruct.ReplaceMap(); err != nil {
		panic(err)
	}
	_ = validatorCompareStruct.Run()

}

func initValidatorCompareStruct() *utils.Compare {
	c := &utils.Compare{
		Name:        "validator",
		Replace:     validatorSet(),
		SpecReplace: specValidatorSet(),
		SSVPath:     utils.DataPath + "/validator/validator.go",
		SpecPath:    utils.DataPath + "/validator/validator_spec.go",
	}
	if err := utils.Copy("./protocol/ssv/validator/validator.go", c.SSVPath); err != nil {
		panic(err)
	}
	if err := utils.Copy("./scripts/spec_align_report/ssv-spec/ssv/validator.go", c.SpecPath); err != nil {
		panic(err)
	}
	return c
}
