package mapping

type KeyValue struct {
	Key string
	Value string
}

type Map struct {
	m    map[string]string
	keys []string
}

func NewMap() *Map {
	return &Map{m: make(map[string]string)}
}

func (m *Map) Set(k string, v string) {
	if _, ok := m.m[k]; !ok {
		m.keys = append(m.keys, k)
	}
	m.m[k] = v
}

func (m *Map) Range() []KeyValue {
	var res []KeyValue
	for _, k := range m.keys {
		res = append(res, KeyValue{
			Key: k,
			Value: m.m[k],
		})
	}
	return res
}
