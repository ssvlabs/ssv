package tests

type EncodingSpecTestType uint64

const (
	ConsensusDataType EncodingSpecTestType = iota
	ShareDataType
	SSVMsgDataType
)

type EncodingSpecTest struct {
	Name     string
	DataType EncodingSpecTestType
	Data     []byte
}
