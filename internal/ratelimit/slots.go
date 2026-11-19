package ratelimit

type Slots chan struct{}

func NewSlots(n int) Slots {
	return make(Slots, n)
}

func (s Slots) TryAcquire() bool {
	select {
	case s <- struct{}{}:
		return true
	default:
		return false
	}
}

func (s Slots) Release() {
	<-s
}
