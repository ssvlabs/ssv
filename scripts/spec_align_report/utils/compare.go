package utils

import (
	"bytes"
	"fmt"
	"github.com/pkg/errors"
	"io/ioutil"
)

type Compare struct {
	Name        string
	Replace     []KeyValue
	SpecReplace []KeyValue
	SSVPath     string
	SpecPath    string
}

func (c *Compare) ReplaceMap() error {
	output, err := ioutil.ReadFile(c.SSVPath)
	if err != nil {
		return err
	}

	for _, kv := range c.Replace {
		if cnt := bytes.Count(output, []byte(kv.Key)); cnt == 0 {
			return errors.New(fmt.Sprintf("%s - no occurrences found to replace in ssv", kv.Key))
		}

		output = bytes.Replace(output, []byte(kv.Key), []byte(kv.Value), -1)
	}
	if err = ioutil.WriteFile(c.SSVPath, output, 0666); err != nil {
		return err
	}

	output, err = ioutil.ReadFile(c.SpecPath)
	if err != nil {
		return err
	}

	for _, kv := range c.SpecReplace {
		if cnt := bytes.Count(output, []byte(kv.Key)); cnt == 0 {
			return errors.New(fmt.Sprintf("%s - no occurrences found to replace in spec", kv.Value))
		}

		output = bytes.Replace(output, []byte(kv.Key), []byte(kv.Value), -1)
	}
	if err = ioutil.WriteFile(c.SpecPath, output, 0666); err != nil {
		return err
	}
	return nil
}
func (c *Compare) Run() error {
	return GitDiff(c.Name, c.SSVPath, c.SpecPath)
}
