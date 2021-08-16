package proto

//go:generate protoc -I $GOPATH/src/github.com/gogo/protobuf/gogoproto --proto_path=$GOPATH/src:. --go_out=./ ./msgs.proto --go_opt=paths=source_relative
//go:generate protoc --proto_path=$GOPATH/src:. --go_out=./ ./params.proto --go_opt=paths=source_relative
//go:generate protoc -I $GOPATH/src/github.com/gogo/protobuf/gogoproto --proto_path=$GOPATH/src:. --go_out=./ ./state.proto --go_opt=paths=source_relative
//go:generate protoc -I $GOPATH/src/github.com/prysmaticlabs/ethereumapis --proto_path=$GOPATH/src:. --go_out=./ ./beacon.proto --go_opt=paths=source_relative
