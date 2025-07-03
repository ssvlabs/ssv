package pebble

type Obj struct {
	key   []byte
	value []byte
}

func (o Obj) Key() []byte {
	return o.key
}
func (o Obj) Value() []byte {
	return o.value
}
