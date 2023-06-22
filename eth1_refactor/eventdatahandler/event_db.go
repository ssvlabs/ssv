package eventdatahandler

type eventDB interface {
	BeginTx()
	EndTx()
	Rollback()
}
