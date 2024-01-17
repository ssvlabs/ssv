package types

type Metrics interface {
	SignatureVerified()
}

type nopMetrics struct{}

func (n nopMetrics) SignatureVerified() {}
