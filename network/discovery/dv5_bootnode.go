package discovery

import (
	"context"

	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/utils"
)

// BootnodeOptions contains options to create the node
type BootnodeOptions struct {
	PrivateKey string `yaml:"PrivateKey" env:"BOOTNODE_NETWORK_KEY" env-description:"Private key for bootnode identity (generated if empty)"`
	ExternalIP string `yaml:"ExternalIP" env:"BOOTNODE_EXTERNAL_IP" env-description:"Override bootnode's external IP address"`
	Port       uint16 `yaml:"Port" env:"BOOTNODE_PORT" env-description:"Override bootnode's UDP port"`
}

// Bootnode represents a bootnode used for tests
type Bootnode struct {
	ctx    context.Context
	cancel context.CancelFunc
	disc   Service

	ENR string // Ethereum Node Records https://eips.ethereum.org/EIPS/eip-778
}

// NewBootnode creates a new bootnode
func NewBootnode(pctx context.Context, logger *zap.Logger, networkCfg networkconfig.NetworkConfig, opts *BootnodeOptions) (*Bootnode, error) {
	ctx, cancel := context.WithCancel(pctx)
	disc, err := createBootnodeDiscovery(ctx, logger, networkCfg, opts)
	if err != nil {
		cancel()
		return nil, err
	}
	np := disc.(NodeProvider)
	enr := np.Self().Node().String()
	logger.Info("bootnode is ready", fields.ENRStr(enr))
	return &Bootnode{
		ctx:    ctx,
		cancel: cancel,
		disc:   disc,
		ENR:    enr,
	}, nil
}

// Close implements io.Closer
func (b *Bootnode) Close() error {
	b.cancel()
	return nil
}

func createBootnodeDiscovery(ctx context.Context, logger *zap.Logger, networkCfg networkconfig.NetworkConfig, opts *BootnodeOptions) (Service, error) {
	privKey, err := utils.ECDSAPrivateKey(logger.Named(logging.NameBootNode), opts.PrivateKey)
	if err != nil {
		return nil, err
	}
	discOpts := &Options{
		NetworkConfig: networkCfg,
		DiscV5Opts: &DiscV5Options{
			IP:         opts.ExternalIP,
			BindIP:     "", // net.IPv4zero.String()
			Port:       opts.Port,
			TCPPort:    5000,
			NetworkKey: privKey,
			Bootnodes:  []string{},
		},
	}
	return newDiscV5Service(ctx, logger, discOpts)
}
