package network

//go:generate protoc -I ../ibft/proto -I $GOPATH/src/github.com/prysmaticlabs/ethereumapis --proto_path=$GOPATH/src:. --go_out=$GOPATH/src $GOPATH/src/github.com/bloxapp/ssv/network/network_msgs.proto
