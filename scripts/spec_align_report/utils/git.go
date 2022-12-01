package utils

import (
	"bytes"
	"fmt"
	"github.com/pkg/errors"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"
)

const ssvSpecRepo = "git@github.com:bloxapp/ssv-spec.git"
const ssvSpecPath = "./scripts/spec_align_report/ssv-spec"

func CloneSpec(tag string, commit string) error {
	if err := CleanSpecPath(); err != nil {
		return err
	}

	cmd := exec.Command("git", "clone", "--depth", "100", "--branch", tag, ssvSpecRepo, ssvSpecPath)
	if _, err := cmd.Output(); err != nil {
		return err
	}

	if len(commit) > 0 {
		cmd = exec.Command("git", "checkout", commit)
		cmd.Dir = ssvSpecPath
		var errb bytes.Buffer
		cmd.Stderr = &errb
		if _, err := cmd.Output(); err != nil {
			fmt.Println(Error(errb.String()))
			return err
		}
	}

	fmt.Println("successfully cloned ssv spec repo")
	return nil
}

func CleanSpecPath() error {
	if err := os.RemoveAll(ssvSpecPath); err != nil {
		return errors.Wrap(err, "couldn't clean spec path:"+ssvSpecPath)
	}
	return nil
}
func GitDiff(name string, ssv string, spec string) error {
	cmd := exec.Command("git", "diff", "--color", "-w", "--word-diff", "--no-index", "--ignore-blank-lines",
		ssv, spec)

	if output, err := cmd.Output(); err != nil {
		diffPath := fmt.Sprintf("%s/%s.diff", DataPath, name)
		if err := ioutil.WriteFile(diffPath, output, 0644); err != nil {
			return err
		}
		fmt.Println(Error(fmt.Sprintf("%s is not aligned to spec: %s", name, diffPath)))

		temp := fmt.Sprintf("%v", cmd.Args)
		fmt.Println(Info(strings.Join(strings.Split(temp[1:len(temp)-1], " "), " ")))

		return err
	}
	fmt.Println(Success(fmt.Sprintf("%s is aligned to spec", name)))
	return nil
}
