package validation

//go:generate protoc -I $GOPATH/src/github.com/grpc-ecosystem/grpc-gateway/third_party/googleapis -I $GOPATH/src/github.com/prysmaticlabs/ethereumapis --proto_path=$GOPATH/src:. --go_out=$GOPATH/src $GOPATH/src/github.com/bloxapp/ssv/ibft/valueImpl/validation/msgs.proto
