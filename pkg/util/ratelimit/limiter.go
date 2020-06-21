package ratelimit

import (
	"time"
)

type Limiter interface {
	Wait()
	Close()
}

type limiter chan struct{}

func New(rate uint16, duration time.Duration) Limiter {
	// have a max rate of 10/sec
	bucket := limiter(make(chan struct{}, rate))
	for i := 0; i < cap(bucket); i++ {
		bucket <- struct{}{}
	}

	// leaky bucket
	go func() {
		tickRate := time.Duration(int(duration) / int(rate))
		ticker := time.NewTicker(tickRate)
		defer ticker.Stop()
		for range ticker.C {
			_, ok := <-bucket
			// if this isn't going to run indefinitely, signal
			// this to return by closing the rate channel.
			if !ok {
				return
			}
		}
	}()

	return &bucket
}

func (l *limiter) Wait() {
	*l<- struct {}{}
}

func (l *limiter) Close() {
	close(*l)
}
