package utils

import (
	"bytes"
	"fmt"
	"github.com/pkg/errors"
	"os"
)

type Compare struct {
	Name        string
	Replace     []KeyValue
	SpecReplace []KeyValue
	SSVPath     string
	SpecPath    string
}

func (c *Compare) ReplaceMap() error {
	output, err := os.ReadFile(c.SSVPath)
	if err != nil {
		return err
	}

	for _, kv := range c.Replace {
		if cnt := bytes.Count(output, []byte(kv.Key)); cnt == 0 {
			return errors.New(fmt.Sprintf("%s - no occurrences found to replace in ssv", kv.Key))
		}

		output = bytes.Replace(output, []byte(kv.Key), []byte(kv.Value), -1)
	}
	if err = os.WriteFile(c.SSVPath, output, 0666); err != nil {
		return err
	}

	output, err = os.ReadFile(c.SpecPath)
	if err != nil {
		return err
	}

	for _, kv := range c.SpecReplace {
		if cnt := bytes.Count(output, []byte(kv.Key)); cnt == 0 {
			return errors.New(fmt.Sprintf("%s - no occurrences found to replace in spec", kv.Key))
		}

		output = bytes.Replace(output, []byte(kv.Key), []byte(kv.Value), -1)
	}
	if err = os.WriteFile(c.SpecPath, output, 0666); err != nil {
		return err
	}
	return nil
}
func (c *Compare) Run() error {
	return GitDiff(c.Name, c.SSVPath, c.SpecPath)
}
