module github.com/bloxapp/ssv

go 1.15

require (
	github.com/Nvveen/Gotty v0.0.0-20120604004816-cd527374f1e5 // indirect
	github.com/bloxapp/eth2-key-manager v1.0.4
	github.com/containerd/fifo v0.0.0-20201026212402-0724c46b320c // indirect
	github.com/docker/go-connections v0.4.0 // indirect
	github.com/docker/go-metrics v0.0.1 // indirect
	github.com/docker/go-units v0.4.0 // indirect
	github.com/ethereum/go-ethereum v1.9.25
	github.com/gogo/protobuf v1.3.2
	github.com/golang/protobuf v1.4.3
	github.com/grpc-ecosystem/go-grpc-middleware v1.2.2
	github.com/grpc-ecosystem/go-grpc-prometheus v1.2.0
	github.com/herumi/bls-eth-go-binary v0.0.0-20210102080045-a126987eca2b
	github.com/libp2p/go-libp2p v0.12.1-0.20201208224947-3155ff3089c0
	github.com/libp2p/go-libp2p-core v0.7.0
	github.com/libp2p/go-libp2p-pubsub v0.4.0
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/image-spec v1.0.1 // indirect
	github.com/patrickmn/go-cache v2.1.0+incompatible
	github.com/pborman/uuid v1.2.1
	github.com/pkg/errors v0.9.1
	github.com/prysmaticlabs/ethereumapis v0.0.0-20210118163152-3569d231d255
	github.com/prysmaticlabs/go-bitfield v0.0.0-20210107162333-9e9cf77d4921
	github.com/prysmaticlabs/prysm v1.1.0
	github.com/sirupsen/logrus v1.7.0 // indirect
	github.com/spf13/cobra v1.1.1
	github.com/stretchr/testify v1.6.1
	go.opencensus.io v0.22.5
	go.uber.org/zap v1.16.0
	google.golang.org/grpc v1.33.1
)

replace github.com/ethereum/go-ethereum => github.com/prysmaticlabs/bazel-go-ethereum v0.0.0-20201113091623-013fd65b3791
