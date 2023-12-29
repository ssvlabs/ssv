package discovery

type Metrics interface {
	NodeRejected()
	NodeFound()
	ENRPing()
	ENRPong()
}
