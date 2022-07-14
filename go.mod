module github.com/bloxapp/ssv

go 1.16

require (
	github.com/attestantio/go-eth2-client v0.11.3
	github.com/bloxapp/eth2-key-manager v1.1.3-0.20211102055147-c66d220973fd
	github.com/bloxapp/ssv-spec v0.0.0-20220630092415-9ac31859b4ad
	github.com/dgraph-io/badger/v3 v3.2103.2
	github.com/ethereum/go-ethereum v1.10.18
	github.com/ferranbt/fastssz v0.0.0-20220103083642-bc5fefefa28b
	github.com/gogo/protobuf v1.3.2
	github.com/golang/mock v1.6.0
	github.com/golang/protobuf v1.5.2
	github.com/google/uuid v1.3.0
	github.com/gorilla/websocket v1.4.2
	github.com/grpc-ecosystem/go-grpc-middleware v1.2.2
	github.com/grpc-ecosystem/go-grpc-prometheus v1.2.0
	github.com/herumi/bls-eth-go-binary v0.0.0-20220509081320-2d8ab06de53c
	github.com/ilyakaznacheev/cleanenv v1.2.5
	github.com/ipfs/go-log v1.0.5
	github.com/libp2p/go-libp2p v0.15.1
	github.com/libp2p/go-libp2p-connmgr v0.2.4
	github.com/libp2p/go-libp2p-core v0.9.0
	github.com/libp2p/go-libp2p-discovery v0.5.1
	github.com/libp2p/go-libp2p-kad-dht v0.15.0
	github.com/libp2p/go-libp2p-noise v0.2.2
	github.com/libp2p/go-libp2p-pubsub v0.5.6
	github.com/libp2p/go-tcp-transport v0.2.8
	github.com/multiformats/go-multiaddr v0.4.0
	github.com/patrickmn/go-cache v2.1.0+incompatible
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.12.1
	github.com/prysmaticlabs/eth2-types v0.0.0-20210303084904-c9735a06829d
	github.com/prysmaticlabs/ethereumapis v0.0.0-20210118163152-3569d231d255
	github.com/prysmaticlabs/go-bitfield v0.0.0-20210809151128-385d8c5e3fb7
	github.com/prysmaticlabs/go-ssz v0.0.0-20200612203617-6d5c9aa213ae
	github.com/prysmaticlabs/prysm v1.4.4
	github.com/rs/zerolog v1.26.1
	github.com/spf13/cobra v1.1.1
	github.com/stretchr/testify v1.7.1
	github.com/wealdtech/go-eth2-util v1.6.3
	go.opencensus.io v0.23.0
	go.uber.org/atomic v1.9.0
	go.uber.org/zap v1.19.0
	google.golang.org/grpc v1.40.0
)

replace github.com/prysmaticlabs/prysm => github.com/prysmaticlabs/prysm v1.4.2-0.20211101172615-63308239d94f

replace github.com/google/flatbuffers => github.com/google/flatbuffers v1.11.0

replace github.com/dgraph-io/ristretto => github.com/dgraph-io/ristretto v0.1.1-0.20211108053508-297c39e6640f

replace github.com/btcsuite/btcd => github.com/btcsuite/btcd v0.22.1
