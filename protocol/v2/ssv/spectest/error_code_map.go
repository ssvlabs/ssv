//go:build !alan_spec

package spectest

func adjustActualError(err error) error {
	return err
}

func adjustExpectedErrorCode(code int) int {
	return code
}
