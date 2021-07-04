package eventqueue

import "sync"

type Queue struct {
	stop  bool
	queue []func()
	lock  sync.Mutex
}

func New() *Queue {
	return &Queue{
		queue: make([]func(), 0),
		lock:  sync.Mutex{},
	}
}

func (q *Queue) Add(f func()) {
	q.lock.Lock()
	defer q.lock.Unlock()

	if q.stop {
		return
	}

	q.queue = append(q.queue, f)
}

func (q *Queue) Pop() func() {
	q.lock.Lock()
	defer q.lock.Unlock()

	if q.stop {
		return nil
	}

	if len(q.queue) > 0 {
		ret := q.queue[0]
		q.queue = q.queue[1:len(q.queue)]
		return ret
	}
	return nil
}

func (q *Queue) ClearAndStop() {
	q.lock.Lock()
	defer q.lock.Unlock()

	q.stop = true
	q.queue = make([]func(), 0)
}
