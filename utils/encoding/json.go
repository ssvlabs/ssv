package encoding

import (
	"github.com/pquerna/ffjson/ffjson"
	"io"
)

type PoolBuffer = func()

var emptyPoolFunc = func() {}

func WriteJSON(item interface{}, out io.Writer) (int, error) {
	buf, err := ffjson.Marshal(&item)
	if err != nil {
		return 0, err
	}
	defer ffjson.Pool(buf)
	return out.Write(buf)
}

func EncodeJSON(item interface{}) (PoolBuffer, []byte, error) {
	buf, err := ffjson.Marshal(&item)
	if err != nil {
		return emptyPoolFunc, nil, err
	}
	return func() {
		ffjson.Pool(buf)
	}, buf, err
}

func DecodeJSON(data []byte, item interface{}) error {
	return ffjson.Unmarshal(data, &item)
}
