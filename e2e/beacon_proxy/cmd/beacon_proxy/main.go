package main

import (
	"context"
	"encoding/json"
	"flag"
	beaconproxy "github.com/bloxapp/ssv/e2e/beacon_proxy"
	"github.com/bloxapp/ssv/e2e/beacon_proxy/intercept"
	"github.com/bloxapp/ssv/logging"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"net/http"
	"os"
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

var Nodes = new(List)

func parseNodesToGateways() []beaconproxy.Gateway {
	inrcpt := intercept.NewHappyInterceptor(nil) // todo pass validators form validators.json
	nds := *Nodes
	gt := make([]beaconproxy.Gateway, len(nds))
	for i, n := range nds {
		gt[i] = beaconproxy.Gateway{
			Name:        n, // should be ssv-node
			Port:        6631 + i,
			Interceptor: inrcpt,
		}
	}
	return gt
}

func main() {
	if err := logging.SetGlobalLogger("DEBUG", "capital", "json", &logging.LogFileOptions{"debug.log", 512, 3}); err != nil {
		panic("failed initializing logging")
	}

	logger := zap.L()

	validatorsFilePath := flag.String("validators", "validators.json", "validators json file to load validators and respective tests")
	beaconNodeUrl := flag.String("beacon_url", "http://bn-h-2.stage.bloxinfra.com:5056", "beaocn node to proxy and intercept")
	flag.Var(Nodes, "nodes", "nodes to intercept")

	flag.Parse()

	_, err := os.Stat(*validatorsFilePath)
	if errors.Is(err, os.ErrNotExist) {
		logger.Fatal("Can't find validators file", zap.String("file", *validatorsFilePath))
	}

	contents, err := os.ReadFile(*validatorsFilePath)
	if err != nil {
		logger.Fatal("Can't read file contents", zap.String("file", *validatorsFilePath), zap.Error(err))
	}

	validators := make(map[string]string)
	err = json.Unmarshal(contents, &validators)
	if err != nil {
		logger.Fatal("err parsing json file", zap.String("file", *validatorsFilePath), zap.Error(err))
	}

	if *beaconNodeUrl == "" {
		logger.Fatal("must provide a valid beacon node url to proceed")
	}

	if Nodes == nil {
		logger.Fatal("no nodes to intercept")
	}

	if len(*Nodes) == 0 {
		logger.Fatal("no nodes to intercept")
	}

	gts := parseNodesToGateways()

	ctx, c := context.WithCancel(context.Background())
	defer c()

	bp, err := beaconproxy.New(context.Background(), logger, *beaconNodeUrl, gts, validators)

	if err != nil {
		logger.Fatal("beacon proxy creation error", zap.Error(err))
	}

	if err := bp.Run(ctx); !errors.Is(err, http.ErrServerClosed) {
		logger.Fatal("beacon proxy stopped", zap.Error(err))
	}
}
