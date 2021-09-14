package tasks

// Retry executes a function x times or until successful
func Retry(fn Fn, retries int) error {
	var err error
	for retries > 0 {
		if err = fn(); err == nil {
			return nil
		}
		retries--
	}
	return err
}
