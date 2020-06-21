package framework

import (
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/resource"
)

// BenchmarkContext is similar to TestContext but without the ability
// to create subtests.
type BenchmarkContext interface {
	context
	Warm()
}

var _ BenchmarkContext = &benchmarkContext{}
var _ test.Failer = &benchmarkContext{}

type benchmarkContext struct {
	tc   *testContext
	warm chan struct{}
}

func (c *benchmarkContext) Warm() {
	c.warm <- struct{}{}
}

func (c *benchmarkContext) WhenDone(fn func() error) {
	c.tc.WhenDone(fn)
}

func newBenchmarkContext(b *Benchmark) *benchmarkContext {
	// benchmarks cannot be nested with SubTest, so we always use a fresh root contexts
	return &benchmarkContext{
		tc:   newRootContext(b.t, b.t.goTest, b.t.labels...),
		warm: make(chan struct{}),
	}
}

/* TODO(landow)
	Is there cleaner way to hide a method from an embedded struct?
	Really I just want to disable NewSubTest type things.
*/

func (c *benchmarkContext) ApplyConfig(ns string, yamlText ...string) error {
	return c.tc.ApplyConfig(ns, yamlText...)
}

func (c *benchmarkContext) ApplyConfigOrFail(t test.Failer, ns string, yamlText ...string) {
	c.tc.ApplyConfigOrFail(t, ns, yamlText...)
}

func (c *benchmarkContext) DeleteConfig(ns string, yamlText ...string) error {
	return c.tc.DeleteConfig(ns, yamlText...)
}

func (c *benchmarkContext) DeleteConfigOrFail(t test.Failer, ns string, yamlText ...string) {
	c.tc.DeleteConfigOrFail(t, ns, yamlText...)
}

func (c *benchmarkContext) ApplyConfigDir(ns string, configDir string) error {
	return c.tc.ApplyConfigDir(ns, configDir)
}

func (c *benchmarkContext) DeleteConfigDir(ns string, configDir string) error {
	return c.tc.DeleteConfigDir(ns, configDir)
}

func (c *benchmarkContext) WorkDir() string {
	return c.tc.WorkDir()
}

func (c *benchmarkContext) CreateDirectoryOrFail(name string) string {
	return c.tc.CreateDirectoryOrFail(name)
}

func (c *benchmarkContext) CreateTmpDirectoryOrFail(prefix string) string {
	return c.tc.CreateTmpDirectoryOrFail(prefix)
}

func (c *benchmarkContext) Error(args ...interface{}) {
	c.tc.Error(args...)
}

func (c *benchmarkContext) TrackResource(r resource.Resource) resource.ID {
	return c.tc.TrackResource(r)
}

func (c *benchmarkContext) GetResource(ref interface{}) error {
	return c.tc.GetResource(ref)
}

// The Environment in which the tests run
func (c *benchmarkContext) Environment() resource.Environment {
	return c.tc.Environment()
}

// Settings returns common settings
func (c *benchmarkContext) Settings() *resource.Settings {
	return c.tc.Settings()
}

// CreateDirectory creates a new subdirectory within this context.
func (c *benchmarkContext) CreateDirectory(name string) (string, error) {
	return c.tc.CreateDirectory(name)
}

// CreateTmpDirectory creates a new temporary directory within this context.
func (c *benchmarkContext) CreateTmpDirectory(prefix string) (string, error) {
	return c.tc.CreateTmpDirectory(prefix)
}

func (c *benchmarkContext) Errorf(format string, args ...interface{}) {
	c.tc.Errorf(format, args...)
}

func (c *benchmarkContext) Failed() bool {
	return c.tc.Failed()
}

func (c *benchmarkContext) Log(args ...interface{}) {
	c.tc.Log(args...)
}

func (c *benchmarkContext) Logf(format string, args ...interface{}) {
	c.tc.Logf(format, args...)
}

func (c *benchmarkContext) Name() string {
	return c.tc.Name()
}

func (c *benchmarkContext) Skip(args ...interface{}) {
	c.tc.Skip(args...)
}

func (c *benchmarkContext) SkipNow() {
	c.tc.SkipNow()
}

func (c *benchmarkContext) Skipf(format string, args ...interface{}) {
	c.tc.Skipf(format, args...)
}

func (c *benchmarkContext) Skipped() bool {
	return c.tc.Skipped()
}

func (c *benchmarkContext) Fail() {
	c.tc.Fail()
}
func (c *benchmarkContext) FailNow() {
	c.tc.FailNow()
}
func (c *benchmarkContext) Fatal(args ...interface{}) {
	c.tc.Fatal(args...)
}
func (c *benchmarkContext) Fatalf(format string, args ...interface{}) {
	c.tc.Fatalf(format, args)
}
func (c *benchmarkContext) Helper() {
	c.tc.Helper()
}
