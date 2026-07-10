package spectest

// adjustActualError and adjustExpectedErrorCode are identity in every build — they
// are tag-free by design. The only build-conditional error-code remapping lives in
// the *ForRunner / *ForRole pair in error_code_map_runtime{,_alan}.go, which remaps
// WrongBeaconRoleTypeErrorCode on the sync-committee-contribution path for the
// DEFAULT build only. These two remain as symmetric hook points.
func adjustActualError(err error) error {
	return err
}

func adjustExpectedErrorCode(code int) int {
	return code
}
