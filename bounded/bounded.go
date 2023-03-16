package bounded

const goroutines = 4

type job struct {
	f   func() error
	out chan<- error
}

var in = make(chan job, goroutines)

func init() {
	for i := 0; i < goroutines; i++ {
		go func() {
			for j := range in {
				j.out <- j.f()
			}
		}()
	}
}

// Run runs the function f in a goroutine.
func Run(f func() error) error {
	out := make(chan error)
	in <- job{f, out}
	return <-out
}
