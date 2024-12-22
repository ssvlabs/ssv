package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// Diff returns the output of git diff between two given sources, excluding the diff header.
func Diff(leftName, rightName string, left, right []byte, contextLines int) ([]byte, error) {
	expectChanges := !bytes.Equal(left, right)

	// Save the sources to temporary files.
	dir, err := os.MkdirTemp("", "differ-git-diff")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer func() {
		if err := os.RemoveAll(dir); err != nil {
			fmt.Printf("failed to remove temp dir: %v", err)
		}
	}()

	leftPath := filepath.Join(dir, "left")
	if err := os.WriteFile(leftPath, left, 0600); err != nil {
		return nil, fmt.Errorf("failed to write left file: %w", err)
	}
	rightPath := filepath.Join(dir, "right")
	if err := os.WriteFile(rightPath, right, 0600); err != nil {
		return nil, fmt.Errorf("failed to write right file: %w", err)
	}

	// Run git diff.
	hasChanges := false
	diff, err := exec.Command("git", "diff", "--no-index", fmt.Sprintf("-U%d", contextLines), leftPath, rightPath).CombinedOutput() // #nosec G204
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			hasChanges = exitErr.ExitCode() == 1
		} else {
			return nil, fmt.Errorf("failed to execute git diff (%w): %s", err, string(diff))
		}
	}
	if expectChanges {
		if !hasChanges {
			return nil, fmt.Errorf("git diff claims no changes, but sources are different")
		}
		if len(diff) == 0 {
			return nil, fmt.Errorf("git diff returned empty diff despite claiming changes")
		}
	} else {
		if hasChanges {
			return nil, fmt.Errorf("git diff claims changes, but sources are equal")
		}
		if len(diff) != 0 {
			return nil, fmt.Errorf("git diff returned non-empty diff despite claiming no changes")
		}
		return nil, nil
	}

	// Verify header.
	const header1 = "diff --git "
	const header2 = "index "
	const header3 = "--- "
	const header4 = "+++ "

	parts := bytes.SplitN(diff, []byte("\n"), 5)
	if len(parts) != 5 {
		return nil, fmt.Errorf("unexpected number of lines in git diff output: %d", len(parts))
	}
	if !bytes.HasPrefix(parts[0], []byte(header1)) {
		return nil, fmt.Errorf("unexpected header line 1 in git diff output: %q", parts[1])
	}
	if !bytes.HasPrefix(parts[1], []byte(header2)) {
		return nil, fmt.Errorf("unexpected header line 2 in git diff output: %q", parts[2])
	}
	if !bytes.HasPrefix(parts[2], []byte(header3)) {
		return nil, fmt.Errorf("unexpected header line 3 in git diff output: %q", parts[2])
	}
	if !bytes.HasPrefix(parts[3], []byte(header4)) {
		return nil, fmt.Errorf("unexpected header line 4 in git diff output: %q", parts[2])
	}

	// Reconstruct the diff without the header.
	b := bytes.NewBuffer(nil)
	fmt.Fprintf(b, "--- %s\n+++ %s\n", leftName, rightName)
	b.Write(parts[4])
	diff = b.Bytes()
	if len(diff) == 0 {
		return nil, fmt.Errorf("empty diff")
	}

	return diff, nil
}
