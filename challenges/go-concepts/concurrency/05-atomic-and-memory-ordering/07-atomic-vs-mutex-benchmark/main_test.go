package main

import (
	"sync"
	"testing"
)

// --- Correctness Tests ---

func TestAtomicCounterCorrectness(t *testing.T) {
	c := &AtomicCounter{}
	runCorrectnessTest(t, "AtomicCounter", c.Inc, c.Get)
}

func TestMutexCounterCorrectness(t *testing.T) {
	c := &MutexCounter{}
	runCorrectnessTest(t, "MutexCounter", c.Inc, c.Get)
}

func TestChannelCounterCorrectness(t *testing.T) {
	c := NewChannelCounter()
	runCorrectnessTest(t, "ChannelCounter", c.Inc, c.Get)
}

func runCorrectnessTest(t *testing.T, name string, inc func(), get func() int64) {
	t.Helper()
	const goroutines = 100
	const iterations = 1000
	expected := int64(goroutines * iterations)

	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				inc()
			}
		}()
	}
	wg.Wait()

	actual := get()
	if actual != expected {
		t.Errorf("%s: expected %d, got %d", name, expected, actual)
	}
}

// --- Step 2: Sequential Benchmarks ---
// Measure the base cost per operation without concurrency.

// TODO: Implement BenchmarkAtomicCounter_Sequential
// Loop b.N times, calling c.Inc() each iteration.
func BenchmarkAtomicCounter_Sequential(b *testing.B) {
	c := &AtomicCounter{}
	_ = c // remove once used
	// TODO: for i := 0; i < b.N; i++ { c.Inc() }
}

// TODO: Implement BenchmarkMutexCounter_Sequential
func BenchmarkMutexCounter_Sequential(b *testing.B) {
	c := &MutexCounter{}
	_ = c // remove once used
	// TODO: for i := 0; i < b.N; i++ { c.Inc() }
}

// TODO: Implement BenchmarkChannelCounter_Sequential
func BenchmarkChannelCounter_Sequential(b *testing.B) {
	c := NewChannelCounter()
	_ = c // remove once used
	// TODO: for i := 0; i < b.N; i++ { c.Inc() }
}

// --- Step 3: Parallel Benchmarks ---
// Use b.RunParallel to measure under concurrent contention.

// TODO: Implement BenchmarkAtomicCounter_Parallel
// Use b.RunParallel(func(pb *testing.PB) { for pb.Next() { c.Inc() } })
func BenchmarkAtomicCounter_Parallel(b *testing.B) {
	c := &AtomicCounter{}
	_ = c // remove once used
	// TODO: b.RunParallel(...)
}

// TODO: Implement BenchmarkMutexCounter_Parallel
func BenchmarkMutexCounter_Parallel(b *testing.B) {
	c := &MutexCounter{}
	_ = c // remove once used
	// TODO: b.RunParallel(...)
}

// TODO: Implement BenchmarkChannelCounter_Parallel
func BenchmarkChannelCounter_Parallel(b *testing.B) {
	c := NewChannelCounter()
	_ = c // remove once used
	// TODO: b.RunParallel(...)
}

// --- Step 4: Read-Heavy Benchmarks ---
// 90% reads, 10% writes. Atomics should excel here.

// TODO: Implement BenchmarkAtomicCounter_ReadHeavy
// Use b.RunParallel. Inside the loop: if i%10 == 0 { c.Inc() } else { c.Get() }
func BenchmarkAtomicCounter_ReadHeavy(b *testing.B) {
	c := &AtomicCounter{}
	_ = c // remove once used
	// TODO: b.RunParallel with 90/10 read/write split
}

// TODO: Implement BenchmarkMutexCounter_ReadHeavy
func BenchmarkMutexCounter_ReadHeavy(b *testing.B) {
	c := &MutexCounter{}
	_ = c // remove once used
	// TODO: b.RunParallel with 90/10 read/write split
}

// TODO: Implement BenchmarkChannelCounter_ReadHeavy
func BenchmarkChannelCounter_ReadHeavy(b *testing.B) {
	c := NewChannelCounter()
	_ = c // remove once used
	// TODO: b.RunParallel with 90/10 read/write split
}

// --- Verify: High Contention Benchmark ---
// Use b.SetParallelism(100) before b.RunParallel to simulate extreme contention.

// TODO: Implement BenchmarkAtomicCounter_HighContention
func BenchmarkAtomicCounter_HighContention(b *testing.B) {
	c := &AtomicCounter{}
	_ = c // remove once used
	// TODO: b.SetParallelism(100)
	// TODO: b.RunParallel(...)
}

// TODO: Implement BenchmarkMutexCounter_HighContention
func BenchmarkMutexCounter_HighContention(b *testing.B) {
	c := &MutexCounter{}
	_ = c // remove once used
	// TODO: b.SetParallelism(100)
	// TODO: b.RunParallel(...)
}

// TODO: Implement BenchmarkChannelCounter_HighContention
func BenchmarkChannelCounter_HighContention(b *testing.B) {
	c := NewChannelCounter()
	_ = c // remove once used
	// TODO: b.SetParallelism(100)
	// TODO: b.RunParallel(...)
}
