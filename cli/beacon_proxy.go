package cli

import (
	beaconproxy "github.com/bloxapp/ssv/e2e/beacon_proxy"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

type ArgumentDoc struct {
	FieldPath   []string
	YAMLPath    []string
	EnvName     string
	Default     string
	Description string
}

var BeaconProxyCmd = &cobra.Command{
	Use:   "beacon-proxy",
	Short: "Start a Beacon node proxy for E2E testing",
	Run: func(cmd *cobra.Command, args []string) {
		ctx := cmd.Context()
		logger := zap.L().Named("beacon-proxy")
		remoteAddr := cmd.Flag("remote-addr").Value.String()
		proxy, err := beaconproxy.New(ctx, logger, remoteAddr, []beaconproxy.Gateway{
			{
				Port: 6631,
				Name: "ssv-node-1",
			},
			{
				Port: 6632,
				Name: "ssv-node-2",
			},
			{
				Port: 6633,
				Name: "ssv-node-3",
			},
			{
				Port: 6634,
				Name: "ssv-node-4",
			},
		})
		if err != nil {
			logger.Fatal("failed to create beacon proxy", zap.Error(err))
		}
		if err := proxy.Run(ctx); err != nil {
			logger.Fatal("failed to start beacon proxy", zap.Error(err))
		}
	},
}

func init() {
	BeaconProxyCmd.Flags().StringP("remote-addr", "r", "", "Remote address of Beacon node to passthrough")
	RootCmd.AddCommand(BeaconProxyCmd)
}
