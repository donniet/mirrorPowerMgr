package main

import "time"

type sleeper struct {
	Timeout time.Duration
	signal  chan bool
	state   bool
	getter  chan chan bool
	C       chan bool
}

func NewSleeper(timeout time.Duration) *sleeper {
	ret := &sleeper{
		Timeout: timeout,
		signal:  make(chan bool),
		getter:  make(chan chan bool),
		state:   false,
		C:       make(chan bool),
	}
	go ret.loop()
	return ret
}

func (p *sleeper) loop() {
	powerOffTimer := time.NewTimer(p.Timeout)
	defer powerOffTimer.Stop()

	for {
		select {
		case sig, ok := <-p.signal:
			if !ok {
				return
			}

			if sig {
				powerOffTimer.Reset(p.Timeout)
				if !p.state {
					p.state = true
					p.C <- true
				}
			} else {
				powerOffTimer.Stop()
				if p.state {
					p.state = false
					p.C <- false
				}
			}
		case <-powerOffTimer.C:
			if p.state {
				p.state = false
				p.C <- false
			}
		case g := <-p.getter:
			g <- p.state
		}
	}
}

func (p *sleeper) Close() {
	close(p.signal)
	close(p.getter)
	close(p.C)
}

func (p *sleeper) On() {
	p.signal <- true
}

func (p *sleeper) Sleep() {
	p.signal <- false
}

func (p *sleeper) Status() bool {
	s := make(chan bool)

	p.getter <- s
	return <-s
}
