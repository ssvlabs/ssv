package proto

//go:generate protoc -I $GOPATH/src/github.com/prysmaticlabs/ethereumapis --proto_path=$GOPATH/src:. --go_out=$GOPATH/src $GOPATH/src/github.com/bloxapp/ssv/ibft/proto/msgs.proto
//go:generate protoc -I $GOPATH/src/github.com/prysmaticlabs/ethereumapis --proto_path=$GOPATH/src:. --go_out=$GOPATH/src $GOPATH/src/github.com/bloxapp/ssv/ibft/proto/params.proto
//go:generate protoc -I $GOPATH/src/github.com/prysmaticlabs/ethereumapis --proto_path=$GOPATH/src:. --go_out=$GOPATH/src $GOPATH/src/github.com/bloxapp/ssv/ibft/proto/state.proto
//go:generate protoc -I $GOPATH/src/github.com/prysmaticlabs/ethereumapis --proto_path=$GOPATH/src:. --go_out=$GOPATH/src $GOPATH/src/github.com/bloxapp/ssv/ibft/proto/beacon.proto
