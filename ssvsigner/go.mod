// ssvsigner intentionally has its own go.mod because it's designed to be moved to a separate repository in the future.
// To make local development easier, it's recommended to add the ssvsigner directory to the go.work file:
//
// use (
// 	.
// 	./ssvsigner
// )
//
// NOTE: Using go.work conflicts with `-modfile`, so `GOWORK=off` should be passed to `make tools` and `make lint`:
// - GOWORK=off make tools
// - GOWORK=off make lint
module github.com/ssvlabs/ssv/ssvsigner

go 1.26

require (
	github.com/alecthomas/kong v1.8.1
	github.com/attestantio/go-eth2-client v0.27.0
	github.com/carlmjohnson/requests v0.24.3
	github.com/docker/docker v28.3.3+incompatible
	github.com/docker/go-connections v0.5.0
	github.com/ethereum/go-ethereum v1.16.4
	github.com/fasthttp/router v1.5.4
	github.com/ferranbt/fastssz v0.1.4
	github.com/google/uuid v1.6.0
	github.com/herumi/bls-eth-go-binary v1.29.1
	github.com/holiman/uint256 v1.3.2
	github.com/lib/pq v1.10.9
	github.com/microsoft/go-crypto-openssl v0.2.9
	github.com/prysmaticlabs/go-bitfield v0.0.0-20260504143531-5cbb6d0f5f2e
	github.com/sourcegraph/conc v0.3.0
	github.com/ssvlabs/eth2-key-manager v1.5.6
	github.com/ssvlabs/ssv-spec v1.2.3-0.20260305184636-289c93aa4c12
	github.com/stretchr/testify v1.11.1
	github.com/testcontainers/testcontainers-go v0.37.0
	github.com/valyala/fasthttp v1.58.0
	github.com/wealdtech/go-eth2-wallet-encryptor-keystorev4 v1.1.3
	go.opentelemetry.io/otel v1.41.0
	go.opentelemetry.io/otel/metric v1.41.0
	go.uber.org/zap v1.27.0
	golang.org/x/crypto v0.45.0
)

require (
	dario.cat/mergo v1.0.1 // indirect
	github.com/Azure/go-ansiterm v0.0.0-20210617225240-d185dfc1b5a1 // indirect
	github.com/Microsoft/go-winio v0.6.2 // indirect
	github.com/andybalholm/brotli v1.1.1 // indirect
	github.com/btcsuite/btcd/btcec/v2 v2.3.4 // indirect
	github.com/cenkalti/backoff/v4 v4.3.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/containerd/errdefs v1.0.0 // indirect
	github.com/containerd/errdefs/pkg v0.3.0 // indirect
	github.com/containerd/log v0.1.0 // indirect
	github.com/containerd/platforms v0.2.1 // indirect
	github.com/cpuguy83/dockercfg v0.3.2 // indirect
	github.com/creack/pty v1.1.23 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.4.0 // indirect
	github.com/distribution/reference v0.6.0 // indirect
	github.com/docker/go-units v0.5.0 // indirect
	github.com/ebitengine/purego v0.8.2 // indirect
	github.com/emicklei/dot v1.6.4 // indirect
	github.com/fatih/color v1.18.0 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/go-playground/validator/v10 v10.13.0 // indirect
	github.com/goccy/go-yaml v1.12.0 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/klauspost/compress v1.18.0 // indirect
	github.com/klauspost/cpuid/v2 v2.3.0 // indirect
	github.com/lufia/plan9stats v0.0.0-20211012122336-39d0f177ccd0 // indirect
	github.com/magiconair/properties v1.8.10 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/minio/sha256-simd v1.0.1 // indirect
	github.com/mitchellh/mapstructure v1.5.0 // indirect
	github.com/moby/docker-image-spec v1.3.1 // indirect
	github.com/moby/go-archive v0.1.0 // indirect
	github.com/moby/patternmatcher v0.6.0 // indirect
	github.com/moby/sys/atomicwriter v0.1.0 // indirect
	github.com/moby/sys/sequential v0.6.0 // indirect
	github.com/moby/sys/user v0.4.0 // indirect
	github.com/moby/sys/userns v0.1.0 // indirect
	github.com/moby/term v0.5.0 // indirect
	github.com/morikuni/aec v1.0.0 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/image-spec v1.1.1 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/power-devops/perfstat v0.0.0-20210106213030-5aafc221ea8c // indirect
	github.com/savsgio/gotils v0.0.0-20240704082632-aef3928b8a38 // indirect
	github.com/shirou/gopsutil/v4 v4.25.1 // indirect
	github.com/sirupsen/logrus v1.9.3 // indirect
	github.com/stretchr/objx v0.5.2 // indirect
	github.com/tklauser/go-sysconf v0.3.14 // indirect
	github.com/tklauser/numcpus v0.8.0 // indirect
	github.com/tyler-smith/go-bip39 v1.1.0 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	github.com/wealdtech/go-bytesutil v1.2.1 // indirect
	github.com/wealdtech/go-eth2-types/v2 v2.8.1 // indirect
	github.com/wealdtech/go-eth2-util v1.8.1 // indirect
	github.com/yusufpapurcu/wmi v1.2.4 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.63.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp v1.38.0 // indirect
	go.opentelemetry.io/otel/trace v1.41.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/exp v0.0.0-20250911091902-df9299821621 // indirect
	golang.org/x/net v0.47.0 // indirect
	golang.org/x/sys v0.38.0 // indirect
	golang.org/x/text v0.31.0 // indirect
	golang.org/x/time v0.12.0 // indirect
	golang.org/x/xerrors v0.0.0-20240903120638-7835f813f4da // indirect
	google.golang.org/protobuf v1.36.9 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
