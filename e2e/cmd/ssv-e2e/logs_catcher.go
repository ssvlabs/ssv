package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/bloxapp/ssv/e2e/logs_catcher"
	"github.com/bloxapp/ssv/e2e/logs_catcher/docker"
	"go.uber.org/zap"
)

type LogsCatcherCmd struct {
	Ignored   []string `env:"IGNORED" help:"A list of containers to not read logs from e.g 'ssv-node-1, beacon-proxy'.."`
	Fatalers  string   `env:"FATALERS" help:"Logs to fatal on, format as JSON fields { 'message': 'bad attestation', 'slot': 1 }"`
	Approvers string   `env:"APPROVERS" help:"Logs to collect for approval on, format as JSON fields { 'message': 'good attestation', 'slot': 1 }"`
}

func (cmd *LogsCatcherCmd) Run(logger *zap.Logger, globals Globals) error {
	// TODO: where do we stop?
	ctx := context.Background()

	parsedFatalers, err := parseToMaps(cmd.Fatalers)
	if err != nil {
		return fmt.Errorf("error parsing Fatalers: %w", err)
	}
	parsedApprovers, err := parseToMaps(cmd.Approvers)
	if err != nil {
		return fmt.Errorf("error parsing Approvers: %w", err)
	}

	cfg := logs_catcher.Config{
		IgnoreContainers: cmd.Ignored,
		Fatalers:         parsedFatalers,
		Approvers:        parsedApprovers,
	}

	cli, err := docker.New()
	if err != nil {
		return fmt.Errorf("failed to open docker client: %w", err)
	}

	cfg.FatalerFunc = logs_catcher.DefaultFataler
	//allDockers, err := docker.GetDockers(ctx, cli, func(container2 types.Container) bool {
	//	for _, nm := range container2.Names {
	//		for _, ign := range cfg.IgnoreContainers {
	//			if strings.Contains(nm, ign) {
	//				return false
	//			}
	//		}
	//	}
	//	return true
	//})
	if err != nil {
		return fmt.Errorf("failed to get dockers list %w", err)
	}

	cfg.ApproverFunc = logs_catcher.DefaultApprover(
		logger, 1,
	) // todo should probably make sure its one for each docker

	logs_catcher.Listen(ctx, logger, cfg, cli)
	return nil
}

func parseToMaps(input string) ([]map[string]any, error) {
	var list []map[string]any
	err := json.Unmarshal([]byte("["+input+"]"), &list)
	if err != nil {
		return nil, fmt.Errorf("failed to parse input (%s) to json: %w", input, err)
	}
	return list, nil
}
