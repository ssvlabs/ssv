package msgqueue

// Option helps to configure the Options
type Option func(opts *Options) error

// Options is a set of message queue options.
type Options struct {
	Indexers []Indexer
}

// Apply applies the given options to this DiscoveryOpts
func (opts *Options) Apply(options ...Option) error {
	for _, o := range options {
		if err := o(opts); err != nil {
			return err
		}
	}
	return nil
}

// WithIndexers is an option that configures indexers.
// it can be called multiple times
func WithIndexers(indexers ...Indexer) Option {
	return func(opts *Options) error {
		opts.Indexers = append(opts.Indexers, indexers...)
		return nil
	}
}
