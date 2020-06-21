package framework

import (
	"fmt"
	"github.com/hashicorp/go-multierror"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/util/ratelimit"
	"os/exec"
	"path"
	"sync"
	"testing"
	"time"
)

const (
	// the repeat function should try to run 100 times per second
	defaultRate      = 100
	defaultFrequency = time.Second
)

type Benchmark struct {
	t               *Test
	pprofPort       uint16
	repeatFn        func(ctx BenchmarkContext, i int) error
	repeatRate      uint16
	repeatFrequency time.Duration
	warmupTimeout   time.Duration
}

func NewBenchmark(t *testing.T) *Benchmark {
	return &Benchmark{
		t:               NewTest(t),
		repeatRate:      defaultRate,
		repeatFrequency: defaultFrequency,
	}
}

// Repeat will trigger the given function at the configured rate.
// The index given to repeat is guaranteed to be unique, but not sequential.
//
// Resources created within Repeat should be tracked for cleanup by the test framework or
// be cleaned up  by calling ctx.WhenDone.
//
// TODO implement the following behavior
// Returning an error is preferred if the benchmark can continue running. The benchmark will be cancelled if:
// - the test is terminated via Fatal or FailNow
// - the error rate is too high
func (b *Benchmark) Repeat(fn func(ctx BenchmarkContext, i int) error) *Benchmark {
	b.repeatFn = fn
	return b
}

func (b *Benchmark) buildRepeater(ctx *benchmarkContext) (func(ctx TestContext), chan error) {
	stop := make(chan error)
	errMu := sync.Mutex{}
	var errs error

	// the TestContext always comes from the parent benchmarkContext
	return func(_ TestContext) {
		defer close(stop)
		limiter := ratelimit.New(b.repeatRate, b.repeatFrequency)
		wg := sync.WaitGroup{}
		i := 0
		start := time.Now()

		defer func() {
			// ensure async tasks finish before exiting test
			wg.Wait()
			limiter.Close()
			scopes.Framework.Infof("ran Repeat fn %d times over %v", i-1, time.Since(start))
		}()


		for {
			select {
			case err := <-stop:
				if err != nil {
					ctx.Error(err)
				}
				return
			default:
				// TODO check "fail now to cancel"
				limiter.Wait()
				wg.Add(1)
				go func() {
					if err := b.repeatFn(ctx, i); err != nil {
						// TODO do someting with errors like failing if error rate is too high
						errMu.Lock()
						errs = multierror.Append(errs, err)
						errMu.Unlock()
					}
					wg.Done()
				}()
				i++
			}
		}
	}, stop
}

func (b *Benchmark) Run() {
	ctx := newBenchmarkContext(b)
	// TODO individual benchmarks having their own setup functions?

	// TODO cancel profiling if repeater fails
	repeater, stopRepeater := b.buildRepeater(ctx)
	go b.t.doRun(ctx.tc, repeater, false)
	// TODO any cleanup failure should fail the entire run since it invalidates performance measurements

	var err error
	select {
	// TODO is Warm call really necessary or would "WarmupTime" be better?
	case <-ctx.warm:
		if path, err := profile(ctx); err != nil {
			err = fmt.Errorf("error generating profile: %v", err)
		} else {
			scopes.Framework.Infof("Wrote profile to %s", path)
		}
	case <-b.timeout(ctx):
		err = fmt.Errorf("timed out waiting for load test to call Warm")
	}

	stopRepeater <- err
}

const defaultWarmup = 30*time.Second

func (b *Benchmark) timeout(ctx *benchmarkContext) chan struct{} {
	if b.warmupTimeout == 0 {
		go func() {
			time.Sleep(defaultWarmup)
			ctx.Warm()
		}()
		return nil
	}
	timeout := make(chan struct{}, 1)
	go func() {
		time.Sleep(b.warmupTimeout)
		timeout <- struct{}{}
	}()
	return timeout
}

func profile(ctx BenchmarkContext) (string, error) {
	cluster := ctx.Environment().Clusters()[0].(kube.Cluster)
	pods, err := cluster.GetPods("istio-system", "app=istiod")
	if err != nil {
		return "", err
	}
	pf, err := cluster.NewPortForwarder(pods[0], 8888, 8080)
	if err != nil {
		return "", err
	}
	if err := pf.Start(); err != nil {
		return "", err
	}
	defer pf.Close()

	dir, err := ctx.CreateDirectory("profiles")
	if err != nil {
		return "", err
	}
	profilePath := path.Join(dir, fmt.Sprintf("%s.pprof", pods[0].Name))
	profileURL := fmt.Sprintf("http://%s/debug/pprof/profile", pf.Address())
	// TODO is there a way to call pprof.Command without exec?
	return profilePath, exec.Command("go", "tool", "pprof", "-seconds", "30", "-raw", "-output", profilePath, profileURL).Run()
}
