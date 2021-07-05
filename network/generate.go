package network

//go:generate protoc -I ../ibft/proto -I $GOPATH/src/github.com/prysmaticlabs/ethereumapis --proto_path=$GOPATH/src:. --go_out=../ ./network_msgs.proto
