package utils

import (
	"fmt"
	"github.com/pkg/errors"
	"io/ioutil"
	"os"
	"os/exec"
)

const ssvSpecRepo = "git@github.com:bloxapp/ssv-spec.git"
const ssvSpecPath = "./scripts/spec_align_report/ssv-spec"
func CloneSpec(tag string) error {
	if err:= CleanSpecPath(); err != nil {
		return err
	}

	cmd := exec.Command("git", "clone","--depth", "1", "--branch", tag,ssvSpecRepo , "./scripts/spec_align_report/ssv-spec")
	_, err := cmd.Output()

	if err != nil {
		return err
	}
	fmt.Println("successfully cloned ssv spec repo")
	return nil
}
func CleanSpecPath() error{
	if err:= os.RemoveAll(ssvSpecPath); err != nil {
		return errors.Wrap(err, "couldn't clean spec path:" + ssvSpecPath)
	}
	return nil
}
func GitDiff(ssv string, spec string, outputPath string) (error){
	cmd := exec.Command("git", "diff", "--color", "-w", "--word-diff","--no-index", "--ignore-blank-lines",
		ssv, spec)
	fmt.Println(cmd.Args)
	if output, err := cmd.Output(); err != nil {
		ioutil.WriteFile(outputPath, output, 0644)
		return err
	}
	return nil
}