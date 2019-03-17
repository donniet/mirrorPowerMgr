package main

import (
	"testing"
	"time"
)

func TestSleeper(t *testing.T) {
	s := NewSleeper(50 * time.Millisecond)
	s.On()

	if p, ok := <-s.C; !ok {
		t.Errorf("channel closed")
	} else if !p {
		t.Errorf("expected to receive on signal")
	}

	time.Sleep(50 * time.Millisecond)

	if p, ok := <-s.C; !ok {
		t.Errorf("channel closed")
	} else if p {
		t.Errorf("expected standby signal")
	}

	s.On()
	if p, ok := <-s.C; !ok {
		t.Errorf("channel closed")
	} else if !p {
		t.Errorf("expected to receive on signal")
	}

	s.Sleep()

	if p, ok := <-s.C; !ok {
		t.Errorf("channel closed")
	} else if p {
		t.Errorf("expected standby signal")
	}

	s.Close()

	select {
	case _, ok := <-s.C:
		if ok {
			t.Errorf("expected closed channel")
		}
	default:
		t.Errorf("channel should be closed, default case shouldn't hit")
	}
}
