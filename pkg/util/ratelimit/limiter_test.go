package ratelimit

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestLimiter(t *testing.T) {
	tests := []struct {
		rate             uint16
		duration         time.Duration
		iterations       int
		expectedDuration time.Duration
	}{
		{
			rate:             10,
			duration:         time.Second,
			iterations:       30,
			expectedDuration: 3 * time.Second,
		},
		{
			rate:             1000,
			duration:         time.Minute,
			iterations:       50,
			expectedDuration: 3 * time.Second,
		},
	}
	const tolerance = 50*time.Millisecond
	for _, tt := range tests {
		name := fmt.Sprintf("%d_per_%v_for_%d_takes_%v",
			tt.rate,
			tt.duration,
			tt.iterations,
			tt.expectedDuration)

		t.Run(name, func(t *testing.T) {
			limiter := New(tt.rate, tt.duration)
			start := time.Now()
			wg := sync.WaitGroup{}
			for i := 0; i < tt.iterations; i++ {
				limiter.Wait()
				wg.Add(1)
				go func() {
					defer wg.Done()
				}()
			}
			wg.Wait() // ensure all jobs completed
			limiter.Close()
			elapsed := time.Since(start)
			if absDiff(elapsed, tt.expectedDuration) > tolerance {
				t.Errorf("expected to take %v; took %v", tt.expectedDuration, elapsed)
			} else {
				t.Logf("took %v", elapsed)
			}
		})

	}
}

func absDiff(a, b time.Duration) time.Duration {
	if a >= b {
		return a - b
	}
	return b - a
}
