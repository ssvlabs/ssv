package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/bloxapp/ssv/e2e/logs_catcher"
	"github.com/bloxapp/ssv/e2e/logs_catcher/docker"
	"github.com/docker/docker/api/types"
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
var Approvers *List

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
		Approvers,
		"approver",
		" Logs to collect for approval on, format as json fields { 'message': 'good attestation', 'slot': 1 }",
	)
	parsedFatalers, err := Fatalers.ParseToMaps()
	if err != nil {
		panic(err)
	}
	parsedApprovers, err := Approvers.ParseToMaps()
	if err != nil {
		panic(err)
	}
	cfg := logs_catcher.Config{
		IgnoreContainers: []string(*Ignored),
		Fatalers:         parsedFatalers,
		Approvers:        parsedApprovers,
	}

	cli, err := docker.New()
	if err != nil {
		panic(fmt.Errorf("can't open docker client %v", err))
	}

	ctx := context.Background()
	cfg.FatalerFunc = logs_catcher.DefaultFataler
	allDockers, err := docker.GetDockers(ctx, cli, func(container2 types.Container) bool {
		for _, nm := range container2.Names {
			for _, ign := range cfg.IgnoreContainers {
				if nm == ign {
					return false
				}
			}
		}
		return true
	})

	if err != nil {
		panic(fmt.Errorf("Can't get dockers list %v", err))
	}
	cfg.ApproverFunc = logs_catcher.DefaultApprover(
		len(allDockers),
	) // todo should probably make sure its one for each docker

	logs_catcher.Listen(cfg, cli)
}
