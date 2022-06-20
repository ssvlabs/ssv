package main

import (
	"encoding/json"
	"fmt"
	"github.com/bloxapp/ssv-spec/qbft/spectest"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
)

func main() {
	all := map[string]spectest.SpecTest{}
	for _, t := range spectest.AllTests {
		n := reflect.TypeOf(t).String() + "_" + t.TestName()
		if all[n] != nil {
			panic(fmt.Sprintf("duplicate test: %s\n", n))
		}
		all[n] = t
	}

	byts, err := json.Marshal(all)
	if err != nil {
		panic(err.Error())
	}

	if len(all) != len(spectest.AllTests) {
		panic("did not generate all tests\n")
	}

	fmt.Printf("found %d tests\n", len(all))
	writeJson(byts)
}

func writeJson(data []byte) {
	basedir, _ := os.Getwd()
	path := filepath.Join(basedir, "qbft", "spectest", "generate")
	fileName := "tests.json"
	fullPath := path + "/" + fileName

	fmt.Printf("writing spec tests json to: %s\n", fullPath)
	if err := ioutil.WriteFile(fullPath, data, 0644); err != nil {
		panic(err.Error())
	}
}
