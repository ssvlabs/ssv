
_TLDR; ~linear growth in memory/cpu demand_

```sh
Running tool: /Users/anatolie/sdk/go1.24.0/bin/go test -benchmem -run=^$ -bench ^BenchmarkTracer$ github.com/ssvlabs/ssv/operator/dutytracer -v -cover

goos: darwin
goarch: arm64
pkg: github.com/ssvlabs/ssv/operator/dutytracer
cpu: Apple M3 Pro
BenchmarkTracer
BenchmarkTracer/Messages_10
BenchmarkTracer/Messages_10-12         	  110802	     10350 ns/op	   10092 B/op	     172 allocs/op
BenchmarkTracer/Messages_20
BenchmarkTracer/Messages_20-12         	   72912	     16078 ns/op	   19556 B/op	     336 allocs/op
BenchmarkTracer/Messages_50
BenchmarkTracer/Messages_50-12         	   43262	     29362 ns/op	   46045 B/op	     799 allocs/op
BenchmarkTracer/Messages_100
BenchmarkTracer/Messages_100-12        	   27374	     44014 ns/op	   90021 B/op	    1577 allocs/op
BenchmarkTracer/Messages_200
BenchmarkTracer/Messages_200-12        	   15274	     78534 ns/op	  171142 B/op	    3016 allocs/op
BenchmarkTracer/Messages_500
BenchmarkTracer/Messages_500-12        	    6464	    244562 ns/op	  369697 B/op	    6586 allocs/op
BenchmarkTracer/Messages_1000
BenchmarkTracer/Messages_1000-12       	    1302	    858549 ns/op	  618787 B/op	   11174 allocs/op
BenchmarkTracer/Messages_2000
BenchmarkTracer/Messages_2000-12       	    1713	    675100 ns/op	 1070798 B/op	   19584 allocs/op
BenchmarkTracer/Messages_4000
BenchmarkTracer/Messages_4000-12       	     841	   1456188 ns/op	 2337454 B/op	   42937 allocs/op
BenchmarkTracer/Messages_8000
BenchmarkTracer/Messages_8000-12       	     409	   2994378 ns/op	 5002500 B/op	   91862 allocs/op
PASS
coverage: 31.6% of statements
ok  	github.com/ssvlabs/ssv/operator/dutytracer	13.197s
```

---

I'll analyze the benchmark results for the BenchmarkTracer test, focusing on the patterns in performance metrics across different message counts.
Let's break down the key patterns:
- Time per Operation (ns/op):
    - The time increases roughly linearly with message count, but with some interesting variations
    - From 10 to 1000 messages: Steady increase from ~10μs to ~858μs
    - At 2000 messages: Unexpected drop to ~675μs (this could be due to CPU caching or other system factors)
    - At 4000 and 8000 messages: Continues to increase to ~1.4ms and ~3ms respectively
- Memory Allocation (B/op):
    - Memory allocation grows linearly with message count
    - Starting at ~10KB for 10 messages
    - Ending at ~5MB for 8000 messages
    - The growth rate is consistent, approximately doubling with each doubling of message count
- Allocations per Operation:
    - Number of allocations also grows linearly
    - Starting at 172 allocations for 10 messages
    - Ending at 91,862 allocations for 8000 messages
    - The ratio of allocations to messages is roughly constant (about 11-12 allocations per message)
- Operations per Second:
    - Decreases exponentially as message count increases
    - From ~110,802 ops/sec for 10 messages
    - Down to ~409 ops/sec for 8000 messages
- Key Observations:
    - The system shows good scalability up to 1000 messages, with predictable linear growth
    - There's an interesting performance anomaly at 2000 messages where performance actually improves
    - The memory allocation pattern is very consistent and predictable
    - The number of allocations per message remains relatively constant, suggesting the code is maintaining a consistent pattern of object creation
- Potential Areas for Optimization:
    - The high number of allocations per message (11-12) might be worth investigating
    - The memory growth is linear but significant - might be worth looking into memory pooling for high message counts
    - The performance anomaly at 2000 messages might indicate an opportunity for optimization at that scale