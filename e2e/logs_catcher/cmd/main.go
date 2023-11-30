package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/bloxapp/ssv/e2e/logs_catcher"
	"github.com/bloxapp/ssv/e2e/logs_catcher/docker"
	"strings"
)

type List []string

func (f *List) String() string {
	return strings.Join(*f, ",")
}

func (f *List) Set(value string) error {
	*f = append(*f, value)
	return nil
}

func (f *List) ParseToMaps() ([]map[string]string, error) {
	all := make([]map[string]string, 0)
	for _, el := range *f {
		m := make(map[string]string)
		err := json.Unmarshal([]byte(el), m)
		if err != nil {
			return nil, fmt.Errorf("failed parsing item %v. err:%v", el, err)
		}
		all = append(all, m)
	}
	return all, nil
}

var Ignored *List
var Fatalers *List
var Restarters *List

func main() {
	flag.Var(
		Ignored,
		"Ignored",
		"A list of containers to not read logs from e.g \"ssv-node-1, beacon-proxy\".. ",
	)
	flag.Var(
		Fatalers,
		"fataler",
		" Logs to fatal on, format as json fields { 'message': 'bad attestation', 'slot': 1 }",
	)
	flag.Var(
		Restarters,
		"restarter",
		" Logs to restart on, format as json fields { 'message': 'bad attestation', 'slot': 1 }",
	)
	parsedFatalers, err := Fatalers.ParseToMaps()
	if err != nil {
		panic(err)
	}
	parsedRestarters, err := Restarters.ParseToMaps()
	if err != nil {
		panic(err)
	}
	cfg := logs_catcher.Config{
		IgnoreContainers: []string(*Ignored),
		Fatalers:         parsedFatalers,
		Restarters:       parsedRestarters,
	}

	cli, err := docker.New()
	if err != nil {
		panic(fmt.Errorf("can't open docker client %v", err))
	}

	logs_catcher.SetupLogsListener(cfg, cli)
}
