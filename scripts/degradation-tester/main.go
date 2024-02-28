package main

import (
	"fmt"
	"github.com/alecthomas/kong"
	"github.com/bloxapp/ssv/scripts/degradation-tester/config"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v3"
	"log"
	"os"
	"os/exec"
)

var cli struct {
	Config string `default:"config.yaml" help:"Path to the config file." type:"existingfile"`
	Output string `default:"output.diff" help:"Path to the output diff file." type:"path"`
}

func getNewBenchmark(path string) {

	//Directly calling your benchmark function
	//cmd := exec.Command("go", "test", "-bench=.", path)
	cmd := exec.Command("/usr/bin/go", "test", "-bench=.", "-benchmem", "-count 10", path)
	cmd.Dir = path
	//fmt.Println(cmd.Args)
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Println("Error running benchmark:", err)
		//	return
	}
	fmt.Println(string(output))

}

func run() (changes int, err error) {
	// Parse the config.yaml file.
	var config config.Config
	yamlFile, err := os.ReadFile(cli.Config)
	if err != nil {
		return 0, errors.Wrap(err, "failed to read config file")
	}
	err = yaml.Unmarshal(yamlFile, &config)
	if err != nil {
		return 0, errors.Wrap(err, "failed to unmarshal YAML")
	}

	for _, test := range config.Tests {
		getNewBenchmark(test.TestPath)
	}

	return 0, nil
}

func main() {
	kong.Parse(&cli)

	changed, err := run()
	if err != nil {
		log.Fatalf("an error occurred: %v", err)
	}

	if changed > 0 {
		fmt.Printf("\n✏️  %d differences found. See %s for details.\n\n", changed, cli.Output)
		os.Exit(1)
	}

	fmt.Printf("\n✅ Degradation tests have passed.\n")
}
