package operator

type nodeMetrics interface {
	SSVNodeHealthy()
	SSVNodeNotHealthy()
}

type nopMetrics struct{}

func (n nopMetrics) SSVNodeHealthy()    {}
func (n nopMetrics) SSVNodeNotHealthy() {}
