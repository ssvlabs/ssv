package utils

import (
	"github.com/pkg/errors"
	"io"
	"os"
)

// Copy copies the contents of the file at srcpath to a regular file at dstpath.
// If dstpath already exists and is not a directory, the function truncates it.
// The function does not copy file modes or file attributes.
func Copy(srcPath, dstPath string) (err error) {
	r, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer r.Close() // ok to ignore error: file was opened read-only.

	w, err := os.Create(dstPath)
	if err != nil {
		return err
	}

	defer func() {
		closeErr := w.Close()
		// Report the error from Close, if any.
		// But do so only if there isn't already
		// an outgoing error.
		if closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	if _, err := io.Copy(w, r); err != nil {
		return err
	}
	return nil
}

func Mkdir(path string, clean bool) (err error) {
	if clean == true {
		os.RemoveAll(path)
	}

	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		err := os.Mkdir(path, os.ModePerm)
		if err != nil {
			return err
		}
	}
	return nil
}
