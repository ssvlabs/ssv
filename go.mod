module github.com/bloxapp/ssv

go 1.15

require (
	github.com/Nvveen/Gotty v0.0.0-20120604004816-cd527374f1e5 // indirect
	github.com/attestantio/go-eth2-client v0.6.30
	github.com/bazelbuild/buildtools v0.0.0-20200528175155-f4e8394f069d // indirect
	github.com/bloxapp/eth2-key-manager v1.1.0-rc.5
	github.com/confluentinc/confluent-kafka-go v1.4.2 // indirect
	github.com/dgraph-io/badger/v3 v3.2011.1
	github.com/docker/go-connections v0.4.0 // indirect
	github.com/docker/go-metrics v0.0.1 // indirect
	github.com/docker/go-units v0.4.0 // indirect
	github.com/ethereum/go-ethereum v1.9.25
	github.com/ferranbt/fastssz v0.0.0-20210719200358-90640294cb9c
	github.com/gogo/protobuf v1.3.2
	github.com/golang/protobuf v1.5.2
	github.com/google/shlex v0.0.0-20191202100458-e7afc7fbc510 // indirect
	github.com/google/uuid v1.2.0
	github.com/gorilla/websocket v1.4.2
	github.com/graph-gophers/graphql-go v0.0.0-20200309224638-dae41bde9ef9 // indirect
	github.com/grpc-ecosystem/go-grpc-middleware v1.2.2
	github.com/grpc-ecosystem/go-grpc-prometheus v1.2.0
	github.com/grpc-ecosystem/grpc-gateway v1.14.6 // indirect
	github.com/herumi/bls-eth-go-binary v0.0.0-20210917013441-d37c07cfda4e
	github.com/ilyakaznacheev/cleanenv v1.2.5
	github.com/influxdata/influxdb v1.8.0 // indirect
	github.com/ipfs/go-ipfs-addr v0.0.1
	github.com/libp2p/go-libp2p v0.14.4
	github.com/libp2p/go-libp2p-core v0.8.6
	github.com/libp2p/go-libp2p-noise v0.2.0
	github.com/libp2p/go-libp2p-pubsub v0.5.0
	github.com/libp2p/go-tcp-transport v0.2.4
	github.com/multiformats/go-multiaddr v0.3.3
	github.com/olekukonko/tablewriter v0.0.4 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/image-spec v1.0.1 // indirect
	github.com/patrickmn/go-cache v2.1.0+incompatible
	github.com/pborman/uuid v1.2.1
	github.com/pkg/errors v0.9.1
	github.com/prestonvanloon/go-recaptcha v0.0.0-20190217191114-0834cef6e8bd // indirect
	github.com/prometheus/client_golang v1.11.0
	github.com/prysmaticlabs/ethereumapis v0.0.0-20210118163152-3569d231d255
	github.com/prysmaticlabs/go-bitfield v0.0.0-20210809151128-385d8c5e3fb7
	github.com/prysmaticlabs/go-ssz v0.0.0-20200612203617-6d5c9aa213ae
	github.com/prysmaticlabs/prysm v1.4.2-0.20210827024218-7757b49f067e
	github.com/rs/zerolog v1.23.0
	github.com/sirupsen/logrus v1.7.0 // indirect
	github.com/spf13/cobra v1.1.1
	github.com/stretchr/testify v1.7.0
	github.com/wealdtech/go-eth2-util v1.6.3
	go.opencensus.io v0.23.0
	go.uber.org/zap v1.18.1
	golang.org/x/sync v0.0.0-20210220032951-036812b2e83c
	golang.org/x/time v0.0.0-20200630173020-3af7569d3a1e // indirect
	google.golang.org/grpc v1.37.0
	gopkg.in/confluentinc/confluent-kafka-go.v1 v1.4.2 // indirect
	olympos.io/encoding/edn v0.0.0-20201019073823-d3554ca0b0a3 // indirect
)

replace github.com/ethereum/go-ethereum => github.com/prysmaticlabs/bazel-go-ethereum v0.0.0-20201113091623-013fd65b3791

replace github.com/google/flatbuffers => github.com/google/flatbuffers v1.11.0

replace github.com/attestantio/go-eth2-client v0.6.30 => github.com/bloxapp/go-eth2-client v0.6.31-0.20210706133239-eb1bd7a3cb25
