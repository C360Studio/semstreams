package buffer

import (
	"fmt"
	"math/rand"
	"testing"
)

// BenchmarkBufferWrite benchmarks buffer Write operations across different configurations.
func BenchmarkBufferWrite(b *testing.B) {
	// Create buffers with error handling
	buf1, err := NewCircularBuffer[int](100, WithOverflowPolicy[int](DropOldest))
	if err != nil {
		b.Fatal(err)
	}
	buf2, err := NewCircularBuffer[int](100, WithOverflowPolicy[int](DropNewest))
	if err != nil {
		b.Fatal(err)
	}
	buf3, err := NewCircularBuffer[int](1000, WithOverflowPolicy[int](DropOldest))
	if err != nil {
		b.Fatal(err)
	}
	buf4, err := NewCircularBuffer[int](1000, WithOverflowPolicy[int](DropNewest))
	if err != nil {
		b.Fatal(err)
	}

	benchmarks := []struct {
		name   string
		buffer Buffer[int]
	}{
		{"Circular_100_DropOldest", buf1},
		{"Circular_100_DropNewest", buf2},
		{"Circular_1000_DropOldest", buf3},
		{"Circular_1000_DropNewest", buf4},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			buffer := bm.buffer
			defer buffer.Close()

			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				i := 0
				for pb.Next() {
					buffer.Write(i)
					i++
				}
			})
		})
	}
}

// BenchmarkBufferRead benchmarks buffer Read operations.
func BenchmarkBufferRead(b *testing.B) {
	// Create buffers with error handling
	buf1, err := NewCircularBuffer[int](100)
	if err != nil {
		b.Fatal(err)
	}
	buf2, err := NewCircularBuffer[int](1000)
	if err != nil {
		b.Fatal(err)
	}
	buf3, err := NewCircularBuffer[int](10000)
	if err != nil {
		b.Fatal(err)
	}

	benchmarks := []struct {
		name   string
		buffer Buffer[int]
	}{
		{"Circular_100", buf1},
		{"Circular_1000", buf2},
		{"Circular_10000", buf3},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			buffer := bm.buffer
			defer buffer.Close()

			// Pre-populate buffer
			capacity := buffer.Capacity()
			for i := 0; i < capacity; i++ {
				buffer.Write(i)
			}

			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					buffer.Read()
				}
			})
		})
	}
}

// BenchmarkBufferReadBatch benchmarks batch read operations.
func BenchmarkBufferReadBatch(b *testing.B) {
	batchSizes := []int{1, 5, 10, 50, 100}

	for _, batchSize := range batchSizes {
		b.Run(fmt.Sprintf("BatchSize_%d", batchSize), func(b *testing.B) {
			buffer, err := NewCircularBuffer[int](1000)
			if err != nil {
				b.Fatal(err)
			}
			defer buffer.Close()

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				// Fill buffer
				for j := 0; j < 1000; j++ {
					buffer.Write(j)
				}

				// Read in batches
				for !buffer.IsEmpty() {
					buffer.ReadBatch(batchSize)
				}
			}
		})
	}
}

// BenchmarkBufferPeek benchmarks buffer Peek operations.
func BenchmarkBufferPeek(b *testing.B) {
	buffer, err := NewCircularBuffer[int](1000)
	if err != nil {
		b.Fatal(err)
	}
	defer buffer.Close()

	// Pre-populate buffer
	for i := 0; i < 1000; i++ {
		_ = buffer.Write(i)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			buffer.Peek()
		}
	})
}

// BenchmarkBufferMixed benchmarks mixed buffer operations (Write/Read/Peek).
func BenchmarkBufferMixed(b *testing.B) {
	// Create buffer with error handling
	buf1, err := NewCircularBuffer[int](100, WithOverflowPolicy[int](DropOldest))
	if err != nil {
		b.Fatal(err)
	}
	buf2, err := NewCircularBuffer[int](1000, WithOverflowPolicy[int](DropOldest))
	if err != nil {
		b.Fatal(err)
	}

	benchmarks := []struct {
		name   string
		buffer Buffer[int]
	}{
		{"Circular_100", buf1},
		{"Circular_1000", buf2},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			buffer := bm.buffer
			defer buffer.Close()

			// Pre-populate buffer
			capacity := buffer.Capacity()
			for i := 0; i < capacity/2; i++ {
				buffer.Write(i)
			}

			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				i := capacity / 2
				for pb.Next() {
					switch rand.Intn(5) {
					case 0, 1: // 40% writes
						buffer.Write(i)
						i++
					case 2, 3: // 40% reads
						buffer.Read()
					case 4: // 20% peeks
						buffer.Peek()
					}
				}
			})
		})
	}
}

// BenchmarkBufferOverflow benchmarks buffer overflow performance.
func BenchmarkBufferOverflow(b *testing.B) {
	policies := []struct {
		name   string
		policy OverflowPolicy
	}{
		{"DropOldest", DropOldest},
		{"DropNewest", DropNewest},
	}

	for _, pol := range policies {
		b.Run(pol.name, func(b *testing.B) {
			buffer, err := NewCircularBuffer[int](100, WithOverflowPolicy[int](pol.policy))
			if err != nil {
				b.Fatal(err)
			}
			defer buffer.Close()

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				buffer.Write(i)
			}
		})
	}
}

// BenchmarkBufferWithStatistics benchmarks buffer performance with statistics enabled.
func BenchmarkBufferWithStatistics(b *testing.B) {
	configs := []struct {
		name      string
		withStats bool
	}{
		{"WithoutStats", false},
		{"WithStats", true},
	}

	for _, config := range configs {
		b.Run(config.name, func(b *testing.B) {
			// Stats are ALWAYS enabled now - this benchmark tests same performance
			buffer, err := NewCircularBuffer[int](1000)
			if err != nil {
				b.Fatal(err)
			}
			defer buffer.Close()

			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				i := 0
				for pb.Next() {
					if i%2 == 0 {
						buffer.Write(i)
					} else {
						buffer.Read()
					}
					i++
				}
			})
		})
	}
}

// BenchmarkBufferMemoryUsage measures relative memory usage of different buffer configurations.
func BenchmarkBufferMemoryUsage(b *testing.B) {
	const numItems = 10000

	benchmarks := []struct {
		name     string
		capacity int
	}{
		{"Cap_100", 100},
		{"Cap_1000", 1000},
		{"Cap_10000", 10000},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				buffer, err := NewCircularBuffer[string](bm.capacity)
				if err != nil {
					b.Fatal(err)
				}
				// Fill buffer
				itemsToWrite := numItems
				if itemsToWrite > bm.capacity {
					itemsToWrite = bm.capacity
				}

				for j := 0; j < itemsToWrite; j++ {
					buffer.Write(fmt.Sprintf("value%d_%d", i, j))
				}

				buffer.Close()
			}
		})
	}
}

// BenchmarkBufferConcurrentAccess benchmarks concurrent access patterns.
func BenchmarkBufferConcurrentAccess(b *testing.B) {
	buffer, err := NewCircularBuffer[int](1000, WithOverflowPolicy[int](DropOldest))
	if err != nil {
		b.Fatal(err)
	}
	defer buffer.Close()

	// Pre-populate
	for i := 0; i < 1000; i++ {
		_ = buffer.Write(i)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			// Mix of operations that would typically happen concurrently
			go func() {
				buffer.Read()
			}()

			go func() {
				_ = buffer.Write(rand.Int())
			}()

			// Occasionally check size or peek (read operations)
			if rand.Intn(100) == 0 {
				buffer.Size()
			}
			if rand.Intn(50) == 0 {
				buffer.Peek()
			}
		}
	})
}

// BenchmarkBufferDropCallback benchmarks performance with drop callbacks.
func BenchmarkBufferDropCallback(b *testing.B) {
	configs := []struct {
		name         string
		withCallback bool
	}{
		{"WithoutCallback", false},
		{"WithCallback", true},
	}

	for _, config := range configs {
		b.Run(config.name, func(b *testing.B) {
			var bufferOpts []Option[int]
			bufferOpts = append(bufferOpts, WithOverflowPolicy[int](DropOldest))

			if config.withCallback {
				bufferOpts = append(bufferOpts, WithDropCallback(func(item int) {
					// Minimal callback - just assignment
					_ = item
				}))
			}

			buffer, err := NewCircularBuffer[int](100, bufferOpts...)
			if err != nil {
				b.Fatal(err)
			}
			defer buffer.Close()

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				buffer.Write(i)
			}
		})
	}
}

// BenchmarkBufferGenericTypes benchmarks performance with different generic types.
func BenchmarkBufferGenericTypes(b *testing.B) {
	b.Run("Int", func(b *testing.B) {
		buffer, err := NewCircularBuffer[int](1000)
		if err != nil {
			b.Fatal(err)
		}
		defer buffer.Close()

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			buffer.Write(i)
		}
	})

	b.Run("String", func(b *testing.B) {
		buffer, err := NewCircularBuffer[string](1000)
		if err != nil {
			b.Fatal(err)
		}
		defer buffer.Close()

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			buffer.Write(fmt.Sprintf("item%d", i))
		}
	})

	b.Run("Struct", func(b *testing.B) {
		type TestStruct struct {
			ID   int
			Name string
			Data []byte
		}

		buffer, err := NewCircularBuffer[TestStruct](1000)
		if err != nil {
			b.Fatal(err)
		}
		defer buffer.Close()

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			buffer.Write(TestStruct{
				ID:   i,
				Name: fmt.Sprintf("item%d", i),
				Data: make([]byte, 64),
			})
		}
	})
}

// Example benchmarks to demonstrate usage patterns.

// BenchmarkExample_HighThroughputWrite simulates a high-throughput write scenario.
func BenchmarkExample_HighThroughputWrite(b *testing.B) {
	buffer, err := NewCircularBuffer[int](10000, WithOverflowPolicy[int](DropOldest)) // Stats always enabled
	if err != nil {
		b.Fatal(err)
	}
	defer buffer.Close()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			_ = buffer.Write(i)
			i++
		}
	})
}

// BenchmarkExample_BatchProcessing simulates batch processing workload.
func BenchmarkExample_BatchProcessing(b *testing.B) {
	buffer, err := NewCircularBuffer[int](5000)
	if err != nil {
		b.Fatal(err)
	}
	defer buffer.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Fill buffer
		for j := 0; j < 5000; j++ {
			_ = buffer.Write(j)
		}

		// Process in batches
		for !buffer.IsEmpty() {
			batch := buffer.ReadBatch(100)
			// Simulate processing
			_ = batch
		}
	}
}

// BenchmarkExample_ProducerConsumer simulates producer-consumer pattern.
func BenchmarkExample_ProducerConsumer(b *testing.B) {
	buffer, err := NewCircularBuffer[int](1000, WithOverflowPolicy[int](DropOldest))
	if err != nil {
		b.Fatal(err)
	}
	defer buffer.Close()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if rand.Intn(2) == 0 { // 50% producer
				_ = buffer.Write(rand.Int())
			} else { // 50% consumer
				buffer.Read()
			}
		}
	})
}

// BenchmarkExample_BufferCapacityScaling benchmarks how performance scales with buffer capacity.
func BenchmarkExample_BufferCapacityScaling(b *testing.B) {
	capacities := []int{10, 100, 1000, 10000, 100000}

	for _, capacity := range capacities {
		b.Run(fmt.Sprintf("Cap_%d", capacity), func(b *testing.B) {
			buffer, err := NewCircularBuffer[int](capacity, WithOverflowPolicy[int](DropOldest))
			if err != nil {
				b.Fatal(err)
			}
			defer buffer.Close()

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				buffer.Write(i)
				if i%10 == 0 {
					buffer.Read()
				}
			}
		})
	}
}
