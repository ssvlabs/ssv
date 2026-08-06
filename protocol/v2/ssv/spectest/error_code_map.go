//go:build !alan_spec

package spectest

// adjustActualError and adjustExpectedErrorCode are identity in the default build;
// the alan_spec build remaps v1.2.2 fixture error codes in error_code_map_alan.go.
// The *ForRunner / *ForRole pair in error_code_map_runtime{,_alan}.go additionally
// remaps WrongBeaconRoleTypeErrorCode on the sync-committee-contribution path for
// the DEFAULT build only.
func adjustActualError(err error) error {
	return err
}

func adjustExpectedErrorCode(code int) int {
	return code
}
