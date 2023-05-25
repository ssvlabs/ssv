package records

// serializable is a struct that can be encoded w/o worries of different encoding implementations,
// e.g. JSON where an unordered map can be different across environments.
// it uses a slice of entries to keep ordered values
// TODO: use SSZ
type serializable struct {
	Entries []string
}

func newSerializable(entries ...string) *serializable {
	return &serializable{
		Entries: entries,
	}
}
