//go:build alan_spec
// +build alan_spec

package spectest

import (
	"errors"

	spectypes "github.com/ssvlabs/ssv-spec/types"
)

const (
	alanUnknownDutyRoleDataErrorCode = 12
)

func adjustActualError(err error) error {
	if err == nil {
		return nil
	}

	var specErr *spectypes.Error
	if errors.As(err, &specErr) && specErr.Code == spectypes.PostConsensusQuorumWithInvalidSignatures {
		return spectypes.WrapError(spectypes.ReconstructSignatureErrorCode, err)
	}

	return err
}

func adjustExpectedErrorCode(code int) int {
	if code >= alanUnknownDutyRoleDataErrorCode {
		return code - 2
	}
	return code
}
