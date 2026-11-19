package ratelimit

import "testing"

func TestSlotsBoundConcurrency(t *testing.T) {
	s := NewSlots(1)
	if !s.TryAcquire() {
		t.Fatal("first acquire failed")
	}
	if s.TryAcquire() {
		t.Fatal("acquired beyond capacity")
	}
	s.Release()
	if !s.TryAcquire() {
		t.Fatal("slot not returned after release")
	}
}
