package main

import (
	"sync"
	"testing"
)

// --- Correctness Tests ---
// These verify each counter produces the correct result under concurrency.

func TestAtomicCounterCorrectness(t *testing.T) {
	c := &AtomicCounter{}
	runCorrectnessTest(t, "AtomicCounter", c.Inc, c.Get)
}

func TestMutexCounterCorrectness(t *testing.T) {
	c := &MutexCounter{}
	runCorrectnessTest(t, "MutexCounter", c.Inc, c.Get)
}

func TestRWMutexCounterCorrectness(t *testing.T) {
	c := &RWMutexCounter{}
	runCorrectnessTest(t, "RWMutexCounter", c.Inc, c.Get)
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

// --- Sequential Benchmarks ---
// Measure the base cost per operation without concurrency.
// This isolates the overhead of each synchronization mechanism.

func BenchmarkAtomicCounter_Sequential(b *testing.B) {
	c := &AtomicCounter{}
	for i := 0; i < b.N; i++ {
		c.Inc()
	}
}

func BenchmarkMutexCounter_Sequential(b *testing.B) {
	c := &MutexCounter{}
	for i := 0; i < b.N; i++ {
		c.Inc()
	}
}

func BenchmarkRWMutexCounter_Sequential(b *testing.B) {
	c := &RWMutexCounter{}
	for i := 0; i < b.N; i++ {
		c.Inc()
	}
}

func BenchmarkChannelCounter_Sequential(b *testing.B) {
	c := NewChannelCounter()
	for i := 0; i < b.N; i++ {
		c.Inc()
	}
}

// --- Parallel Benchmarks ---
// Use b.RunParallel to measure under realistic concurrent contention.
// The framework spawns GOMAXPROCS goroutines and distributes b.N across them.

func BenchmarkAtomicCounter_Parallel(b *testing.B) {
	c := &AtomicCounter{}
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			c.Inc()
		}
	})
}

func BenchmarkMutexCounter_Parallel(b *testing.B) {
	c := &MutexCounter{}
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			c.Inc()
		}
	})
}

func BenchmarkRWMutexCounter_Parallel(b *testing.B) {
	c := &RWMutexCounter{}
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			c.Inc()
		}
	})
}

func BenchmarkChannelCounter_Parallel(b *testing.B) {
	c := NewChannelCounter()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			c.Inc()
		}
	})
}

// --- Read-Heavy Benchmarks ---
// 90% reads, 10% writes. Simulates real workloads where reads dominate.
// Atomics should excel because readers never block each other.
// RWMutex should outperform Mutex because concurrent reads are allowed.

func BenchmarkAtomicCounter_ReadHeavy(b *testing.B) {
	c := &AtomicCounter{}
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			if i%10 == 0 {
				c.Inc()
			} else {
				c.Get()
			}
			i++
		}
	})
}

func BenchmarkMutexCounter_ReadHeavy(b *testing.B) {
	c := &MutexCounter{}
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			if i%10 == 0 {
				c.Inc()
			} else {
				c.Get()
			}
			i++
		}
	})
}

func BenchmarkRWMutexCounter_ReadHeavy(b *testing.B) {
	c := &RWMutexCounter{}
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			if i%10 == 0 {
				c.Inc()
			} else {
				c.Get()
			}
			i++
		}
	})
}

func BenchmarkChannelCounter_ReadHeavy(b *testing.B) {
	c := NewChannelCounter()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			if i%10 == 0 {
				c.Inc()
			} else {
				c.Get()
			}
			i++
		}
	})
}

// --- High Contention Benchmarks ---
// b.SetParallelism(100) creates 100 * GOMAXPROCS goroutines.
// This simulates extreme contention to see how each approach degrades.

func BenchmarkAtomicCounter_HighContention(b *testing.B) {
	c := &AtomicCounter{}
	b.SetParallelism(100)
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			c.Inc()
		}
	})
}

func BenchmarkMutexCounter_HighContention(b *testing.B) {
	c := &MutexCounter{}
	b.SetParallelism(100)
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			c.Inc()
		}
	})
}

func BenchmarkRWMutexCounter_HighContention(b *testing.B) {
	c := &RWMutexCounter{}
	b.SetParallelism(100)
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			c.Inc()
		}
	})
}

func BenchmarkChannelCounter_HighContention(b *testing.B) {
	c := NewChannelCounter()
	b.SetParallelism(100)
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			c.Inc()
		}
	})
}
