package eventdatahandler

type eventDB interface {
	BeginTx()
	EndTx()
}
