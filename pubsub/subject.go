package pubsub

type subject interface {
	Register(Observer Observer)
	Deregister(Observer Observer)
	notifyAll()
}

type BaseSubject struct {
	ObserverList []Observer
	Name         string
}

func (b *BaseSubject) Register(o Observer) {
	b.ObserverList = append(b.ObserverList, o)
}

func (b *BaseSubject) Deregister(o Observer) {
	b.ObserverList = removeFromSlice(b.ObserverList, o)
}

func (b *BaseSubject) notifyAll() {
	for _, observer := range b.ObserverList {
		observer.Update(b.Name)
	}
}

func removeFromSlice(observerList []Observer, observerToRemove Observer) []Observer {
	observerListLength := len(observerList)
	for i, observer := range observerList {
		if observerToRemove.getID() == observer.getID() {
			observerList[observerListLength-1], observerList[i] = observerList[i], observerList[observerListLength-1]
			return observerList[:observerListLength-1]
		}
	}
	return observerList
}