package discovery

import (
	"context"
	"net"
	"reflect"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/p2p/discover"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/logging"
	"github.com/bloxapp/ssv/network/peers"
	"github.com/bloxapp/ssv/utils"
)

func Test_newDiscV5Service(t *testing.T) {
	type args struct {
		pctx     context.Context
		logger   *zap.Logger
		discOpts *Options
	}
	logger := zap.New(zapcore.NewNopCore(), zap.WithFatalHook(zapcore.WriteThenPanic))
	privKey, err := utils.ECDSAPrivateKey(logger.Named(logging.NameBootNode), "")
	require.NoError(t, err)
	tests := []struct {
		name    string
		args    args
		want    Service
		wantErr bool
	}{
		{
			name: "ok",
			args: args{
				pctx:   context.Background(),
				logger: logger,
				discOpts: &Options{
					DiscV5Opts: &DiscV5Options{
						IP:         "0.0.0.0",
						BindIP:     "", // net.IPv4zero.String()
						Port:       3000,
						TCPPort:    5000,
						NetworkKey: privKey,
						Bootnodes:  []string{},
					},
				},
			},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := newDiscV5Service(tt.args.pctx, tt.args.logger, tt.args.discOpts)
			if (err != nil) != tt.wantErr {
				t.Errorf("newDiscV5Service() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			require.NotNil(t, got)
		})
	}
}

func TestDiscV5Service_Close(t *testing.T) {
	type fields struct {
		ctx          context.Context
		cancel       context.CancelFunc
		dv5Listener  *discover.UDPv5
		bootnodes    []*enode.Node
		conns        peers.ConnectionIndex
		subnetsIdx   peers.SubnetsIndex
		publishState int32
		conn         *net.UDPConn
		domainType   spectypes.DomainType
		subnets      []byte
	}
	// logger := zap.New(zapcore.NewNopCore(), zap.WithFatalHook(zapcore.WriteThenPanic))
	ctx, cancel := context.WithCancel(context.Background())
	tests := []struct {
		name    string
		fields  fields
		wantErr bool
	}{
		{
			name: "ok",
			fields: fields{
				ctx:    ctx,
				cancel: cancel,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dvs := &DiscV5Service{
				ctx:          tt.fields.ctx,
				cancel:       tt.fields.cancel,
				dv5Listener:  tt.fields.dv5Listener,
				bootnodes:    tt.fields.bootnodes,
				conns:        tt.fields.conns,
				subnetsIdx:   tt.fields.subnetsIdx,
				publishState: tt.fields.publishState,
				conn:         tt.fields.conn,
				domainType:   tt.fields.domainType,
				subnets:      tt.fields.subnets,
			}
			if err := dvs.Close(); (err != nil) != tt.wantErr {
				t.Errorf("DiscV5Service.Close() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDiscV5Service_Self(t *testing.T) {
	type fields struct {
		ctx          context.Context
		cancel       context.CancelFunc
		dv5Listener  *discover.UDPv5
		bootnodes    []*enode.Node
		conns        peers.ConnectionIndex
		subnetsIdx   peers.SubnetsIndex
		publishState int32
		conn         *net.UDPConn
		domainType   spectypes.DomainType
		subnets      []byte
	}
	tests := []struct {
		name   string
		fields fields
		want   *enode.LocalNode
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dvs := &DiscV5Service{
				ctx:          tt.fields.ctx,
				cancel:       tt.fields.cancel,
				dv5Listener:  tt.fields.dv5Listener,
				bootnodes:    tt.fields.bootnodes,
				conns:        tt.fields.conns,
				subnetsIdx:   tt.fields.subnetsIdx,
				publishState: tt.fields.publishState,
				conn:         tt.fields.conn,
				domainType:   tt.fields.domainType,
				subnets:      tt.fields.subnets,
			}
			if got := dvs.Self(); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("DiscV5Service.Self() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDiscV5Service_Node(t *testing.T) {
	type fields struct {
		ctx          context.Context
		cancel       context.CancelFunc
		dv5Listener  *discover.UDPv5
		bootnodes    []*enode.Node
		conns        peers.ConnectionIndex
		subnetsIdx   peers.SubnetsIndex
		publishState int32
		conn         *net.UDPConn
		domainType   spectypes.DomainType
		subnets      []byte
	}
	type args struct {
		logger *zap.Logger
		info   peer.AddrInfo
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    *enode.Node
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dvs := &DiscV5Service{
				ctx:          tt.fields.ctx,
				cancel:       tt.fields.cancel,
				dv5Listener:  tt.fields.dv5Listener,
				bootnodes:    tt.fields.bootnodes,
				conns:        tt.fields.conns,
				subnetsIdx:   tt.fields.subnetsIdx,
				publishState: tt.fields.publishState,
				conn:         tt.fields.conn,
				domainType:   tt.fields.domainType,
				subnets:      tt.fields.subnets,
			}
			got, err := dvs.Node(tt.args.logger, tt.args.info)
			if (err != nil) != tt.wantErr {
				t.Errorf("DiscV5Service.Node() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("DiscV5Service.Node() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDiscV5Service_Bootstrap(t *testing.T) {
	type fields struct {
		ctx          context.Context
		cancel       context.CancelFunc
		dv5Listener  *discover.UDPv5
		bootnodes    []*enode.Node
		conns        peers.ConnectionIndex
		subnetsIdx   peers.SubnetsIndex
		publishState int32
		conn         *net.UDPConn
		domainType   spectypes.DomainType
		subnets      []byte
	}
	type args struct {
		logger  *zap.Logger
		handler HandleNewPeer
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dvs := &DiscV5Service{
				ctx:          tt.fields.ctx,
				cancel:       tt.fields.cancel,
				dv5Listener:  tt.fields.dv5Listener,
				bootnodes:    tt.fields.bootnodes,
				conns:        tt.fields.conns,
				subnetsIdx:   tt.fields.subnetsIdx,
				publishState: tt.fields.publishState,
				conn:         tt.fields.conn,
				domainType:   tt.fields.domainType,
				subnets:      tt.fields.subnets,
			}
			if err := dvs.Bootstrap(tt.args.logger, tt.args.handler); (err != nil) != tt.wantErr {
				t.Errorf("DiscV5Service.Bootstrap() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDiscV5Service_checkPeer(t *testing.T) {
	type fields struct {
		ctx          context.Context
		cancel       context.CancelFunc
		dv5Listener  *discover.UDPv5
		bootnodes    []*enode.Node
		conns        peers.ConnectionIndex
		subnetsIdx   peers.SubnetsIndex
		publishState int32
		conn         *net.UDPConn
		domainType   spectypes.DomainType
		subnets      []byte
	}
	type args struct {
		logger *zap.Logger
		e      PeerEvent
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dvs := &DiscV5Service{
				ctx:          tt.fields.ctx,
				cancel:       tt.fields.cancel,
				dv5Listener:  tt.fields.dv5Listener,
				bootnodes:    tt.fields.bootnodes,
				conns:        tt.fields.conns,
				subnetsIdx:   tt.fields.subnetsIdx,
				publishState: tt.fields.publishState,
				conn:         tt.fields.conn,
				domainType:   tt.fields.domainType,
				subnets:      tt.fields.subnets,
			}
			if err := dvs.checkPeer(tt.args.logger, tt.args.e); (err != nil) != tt.wantErr {
				t.Errorf("DiscV5Service.checkPeer() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDiscV5Service_initDiscV5Listener(t *testing.T) {
	type fields struct {
		ctx          context.Context
		cancel       context.CancelFunc
		dv5Listener  *discover.UDPv5
		bootnodes    []*enode.Node
		conns        peers.ConnectionIndex
		subnetsIdx   peers.SubnetsIndex
		publishState int32
		conn         *net.UDPConn
		domainType   spectypes.DomainType
		subnets      []byte
	}
	type args struct {
		logger   *zap.Logger
		discOpts *Options
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dvs := &DiscV5Service{
				ctx:          tt.fields.ctx,
				cancel:       tt.fields.cancel,
				dv5Listener:  tt.fields.dv5Listener,
				bootnodes:    tt.fields.bootnodes,
				conns:        tt.fields.conns,
				subnetsIdx:   tt.fields.subnetsIdx,
				publishState: tt.fields.publishState,
				conn:         tt.fields.conn,
				domainType:   tt.fields.domainType,
				subnets:      tt.fields.subnets,
			}
			if err := dvs.initDiscV5Listener(tt.args.logger, tt.args.discOpts); (err != nil) != tt.wantErr {
				t.Errorf("DiscV5Service.initDiscV5Listener() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDiscV5Service_discover(t *testing.T) {
	type fields struct {
		ctx          context.Context
		cancel       context.CancelFunc
		dv5Listener  *discover.UDPv5
		bootnodes    []*enode.Node
		conns        peers.ConnectionIndex
		subnetsIdx   peers.SubnetsIndex
		publishState int32
		conn         *net.UDPConn
		domainType   spectypes.DomainType
		subnets      []byte
	}
	type args struct {
		ctx      context.Context
		handler  HandleNewPeer
		interval time.Duration
		filters  []NodeFilter
	}
	tests := []struct {
		name   string
		fields fields
		args   args
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dvs := &DiscV5Service{
				ctx:          tt.fields.ctx,
				cancel:       tt.fields.cancel,
				dv5Listener:  tt.fields.dv5Listener,
				bootnodes:    tt.fields.bootnodes,
				conns:        tt.fields.conns,
				subnetsIdx:   tt.fields.subnetsIdx,
				publishState: tt.fields.publishState,
				conn:         tt.fields.conn,
				domainType:   tt.fields.domainType,
				subnets:      tt.fields.subnets,
			}
			dvs.discover(tt.args.ctx, tt.args.handler, tt.args.interval, tt.args.filters...)
		})
	}
}

func TestDiscV5Service_RegisterSubnets(t *testing.T) {
	type fields struct {
		ctx          context.Context
		cancel       context.CancelFunc
		dv5Listener  *discover.UDPv5
		bootnodes    []*enode.Node
		conns        peers.ConnectionIndex
		subnetsIdx   peers.SubnetsIndex
		publishState int32
		conn         *net.UDPConn
		domainType   spectypes.DomainType
		subnets      []byte
	}
	type args struct {
		logger  *zap.Logger
		subnets []int
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dvs := &DiscV5Service{
				ctx:          tt.fields.ctx,
				cancel:       tt.fields.cancel,
				dv5Listener:  tt.fields.dv5Listener,
				bootnodes:    tt.fields.bootnodes,
				conns:        tt.fields.conns,
				subnetsIdx:   tt.fields.subnetsIdx,
				publishState: tt.fields.publishState,
				conn:         tt.fields.conn,
				domainType:   tt.fields.domainType,
				subnets:      tt.fields.subnets,
			}
			if err := dvs.RegisterSubnets(tt.args.logger, tt.args.subnets...); (err != nil) != tt.wantErr {
				t.Errorf("DiscV5Service.RegisterSubnets() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDiscV5Service_DeregisterSubnets(t *testing.T) {
	type fields struct {
		ctx          context.Context
		cancel       context.CancelFunc
		dv5Listener  *discover.UDPv5
		bootnodes    []*enode.Node
		conns        peers.ConnectionIndex
		subnetsIdx   peers.SubnetsIndex
		publishState int32
		conn         *net.UDPConn
		domainType   spectypes.DomainType
		subnets      []byte
	}
	type args struct {
		logger  *zap.Logger
		subnets []int
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dvs := &DiscV5Service{
				ctx:          tt.fields.ctx,
				cancel:       tt.fields.cancel,
				dv5Listener:  tt.fields.dv5Listener,
				bootnodes:    tt.fields.bootnodes,
				conns:        tt.fields.conns,
				subnetsIdx:   tt.fields.subnetsIdx,
				publishState: tt.fields.publishState,
				conn:         tt.fields.conn,
				domainType:   tt.fields.domainType,
				subnets:      tt.fields.subnets,
			}
			if err := dvs.DeregisterSubnets(tt.args.logger, tt.args.subnets...); (err != nil) != tt.wantErr {
				t.Errorf("DiscV5Service.DeregisterSubnets() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDiscV5Service_publishENR(t *testing.T) {
	type fields struct {
		ctx          context.Context
		cancel       context.CancelFunc
		dv5Listener  *discover.UDPv5
		bootnodes    []*enode.Node
		conns        peers.ConnectionIndex
		subnetsIdx   peers.SubnetsIndex
		publishState int32
		conn         *net.UDPConn
		domainType   spectypes.DomainType
		subnets      []byte
	}
	type args struct {
		logger *zap.Logger
	}
	tests := []struct {
		name   string
		fields fields
		args   args
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dvs := &DiscV5Service{
				ctx:          tt.fields.ctx,
				cancel:       tt.fields.cancel,
				dv5Listener:  tt.fields.dv5Listener,
				bootnodes:    tt.fields.bootnodes,
				conns:        tt.fields.conns,
				subnetsIdx:   tt.fields.subnetsIdx,
				publishState: tt.fields.publishState,
				conn:         tt.fields.conn,
				domainType:   tt.fields.domainType,
				subnets:      tt.fields.subnets,
			}
			dvs.publishENR(tt.args.logger)
		})
	}
}

func TestDiscV5Service_createLocalNode(t *testing.T) {
	type fields struct {
		ctx          context.Context
		cancel       context.CancelFunc
		dv5Listener  *discover.UDPv5
		bootnodes    []*enode.Node
		conns        peers.ConnectionIndex
		subnetsIdx   peers.SubnetsIndex
		publishState int32
		conn         *net.UDPConn
		domainType   spectypes.DomainType
		subnets      []byte
	}
	type args struct {
		logger   *zap.Logger
		discOpts *Options
		ipAddr   net.IP
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    *enode.LocalNode
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dvs := &DiscV5Service{
				ctx:          tt.fields.ctx,
				cancel:       tt.fields.cancel,
				dv5Listener:  tt.fields.dv5Listener,
				bootnodes:    tt.fields.bootnodes,
				conns:        tt.fields.conns,
				subnetsIdx:   tt.fields.subnetsIdx,
				publishState: tt.fields.publishState,
				conn:         tt.fields.conn,
				domainType:   tt.fields.domainType,
				subnets:      tt.fields.subnets,
			}
			got, err := dvs.createLocalNode(tt.args.logger, tt.args.discOpts, tt.args.ipAddr)
			if (err != nil) != tt.wantErr {
				t.Errorf("DiscV5Service.createLocalNode() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("DiscV5Service.createLocalNode() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_newUDPListener(t *testing.T) {
	type args struct {
		bindIP  net.IP
		port    int
		network string
	}
	tests := []struct {
		name    string
		args    args
		want    *net.UDPConn
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := newUDPListener(tt.args.bindIP, tt.args.port, tt.args.network)
			if (err != nil) != tt.wantErr {
				t.Errorf("newUDPListener() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("newUDPListener() = %v, want %v", got, tt.want)
			}
		})
	}
}
