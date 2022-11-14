package utils

import (
	"bytes"
	"fmt"
	"github.com/pkg/errors"
	"io/ioutil"
)

type Compare struct {
	Replace map[string]string
	SpecReplace map[string]string
	SSVPath string
	SpecPath string
}

func (c *Compare) ReplaceMap() error{
	output, err := ioutil.ReadFile(c.SSVPath)
	if err != nil {
		return err
	}

	for k, v := range c.Replace {
		if cnt:=bytes.Count(output,[]byte(k)); cnt ==0 {
			return errors.New(fmt.Sprintf("%s - no occurrences found to replace in ssv", k))
		}

		output = bytes.Replace(output, []byte(k), []byte(v), -1)
	}
	if err = ioutil.WriteFile(c.SSVPath, output, 0666); err != nil {
		return err
	}


	output, err = ioutil.ReadFile(c.SpecPath)
	if err != nil {
		return err
	}

	for k, v := range c.SpecReplace {
		if cnt:=bytes.Count(output,[]byte(k)); cnt ==0 {
			return errors.New(fmt.Sprintf("%s - no occurrences found to replace in spec", k))
		}

		output = bytes.Replace(output, []byte(k), []byte(v), -1)
	}
	if err = ioutil.WriteFile(c.SpecPath, output, 0666); err != nil {
		return err
	}
	return nil
}
