package network

//go:generate protoc -I $GOPATH/src/github.com/gogo/protobuf/gogoproto --proto_path=.:../ibft/proto --go_out=../ ./network_msgs.proto
