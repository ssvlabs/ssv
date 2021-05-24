module github.com/bloxapp/ssv

go 1.15

require (
	github.com/bloxapp/eth2-key-manager v1.0.4
	github.com/dgraph-io/badger v1.6.1
	github.com/dgraph-io/badger/v3 v3.2011.1
	github.com/ethereum/go-ethereum v1.9.25
	github.com/gogo/protobuf v1.3.2
	github.com/golang/protobuf v1.4.3
	github.com/grpc-ecosystem/go-grpc-middleware v1.2.2
	github.com/grpc-ecosystem/go-grpc-prometheus v1.2.0
	github.com/herumi/bls-eth-go-binary v0.0.0-20210102080045-a126987eca2b
	github.com/ipfs/go-ipfs-addr v0.0.1
	github.com/libp2p/go-libp2p v0.12.1-0.20201208224947-3155ff3089c0
	github.com/libp2p/go-libp2p-core v0.7.0
	github.com/libp2p/go-libp2p-noise v0.1.2
	github.com/libp2p/go-libp2p-pubsub v0.4.0
	github.com/libp2p/go-tcp-transport v0.2.1
	github.com/multiformats/go-multiaddr v0.3.1
	github.com/patrickmn/go-cache v2.1.0+incompatible
	github.com/pborman/uuid v1.2.1
	github.com/pkg/errors v0.9.1
	github.com/prysmaticlabs/ethereumapis v0.0.0-20210118163152-3569d231d255
	github.com/prysmaticlabs/go-bitfield v0.0.0-20210107162333-9e9cf77d4921
	github.com/prysmaticlabs/prysm v1.1.0
	github.com/sirupsen/logrus v1.7.0 // indirect
	github.com/spf13/cobra v1.1.1
	github.com/stretchr/testify v1.6.1
	github.com/wealdtech/go-eth2-util v1.6.2
	go.opencensus.io v0.22.5
	go.uber.org/zap v1.16.0
	golang.org/x/sys v0.0.0-20210124154548-22da62e12c0c // indirect
	google.golang.org/grpc v1.33.1
)

replace github.com/ethereum/go-ethereum => github.com/prysmaticlabs/bazel-go-ethereum v0.0.0-20201113091623-013fd65b3791

replace github.com/google/flatbuffers => github.com/google/flatbuffers v1.11.0
