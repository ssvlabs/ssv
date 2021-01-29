package proto

//go:generate protoc -I $GOPATH/src/github.com/prysmaticlabs/ethereumapis --proto_path=$GOPATH/src:. --go_out=$GOPATH/src $GOPATH/src/github.com/bloxapp/ssv/ibft/proto/msgs.proto
//go:generate protoc -I $GOPATH/src/github.com/prysmaticlabs/ethereumapis --proto_path=$GOPATH/src:. --go_out=$GOPATH/src $GOPATH/src/github.com/bloxapp/ssv/ibft/proto/params.proto
