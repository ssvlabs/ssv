package main

import (
	"encoding/json"
	"fmt"
	"github.com/bloxapp/ssv-spec/ssv/spectest"
	"github.com/bloxapp/ssv-spec/ssv/spectest/tests"
	"io/ioutil"
	"os"
	"path/filepath"
)

func main() {
	all := map[string]*tests.SpecTest{}
	for _, t := range spectest.AllTests {
		all[t.Name] = t
	}

	byts, _ := json.Marshal(all)
	writeJson(byts)
}

func writeJson(data []byte) {
	basedir, _ := os.Getwd()
	path := filepath.Join(basedir, "ssv", "spectest", "generate")
	fileName := "tests.json"
	fullPath := path + "/" + fileName

	fmt.Printf("writing spec tests json to: %s\n", fullPath)
	if err := ioutil.WriteFile(fullPath, data, 0644); err != nil {
		panic(err.Error())
	}
}
