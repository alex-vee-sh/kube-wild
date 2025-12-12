# Performance Benchmarks

This document tracks performance benchmarks for critical code paths.

## Running Benchmarks

```bash
# Run all benchmarks
go test -bench=. -benchmem ./...

# Run specific benchmark
go test -bench=BenchmarkLabelsAllowed -benchmem ./...

# Compare benchmarks (requires benchstat)
go test -bench=. -benchmem -count=5 ./... > old.txt
# ... make changes ...
go test -bench=. -benchmem -count=5 ./... > new.txt
benchstat old.txt new.txt
```

## Key Benchmarks

### Label Filtering

- `BenchmarkLabelsAllowed_NoFilters`: ~9.8ns/op, 0 allocs/op
- `BenchmarkLabelsAllowed_WithFilters`: ~29.5ns/op, 0 allocs/op (fast path)
- `BenchmarkLabelsAllowed_WithDuplicates`: ~59.6ns/op, 0 allocs/op (slow path with pre-computed grouping)
- `BenchmarkLabelsAllowed_NilMap`: ~9.8ns/op, 0 allocs/op (nil map handling)
- `BenchmarkLabelKeyRegex_SinglePass`: ~206ns/op, 0 allocs/op (optimized single-pass regex matching)

### Namespace Filtering

- `BenchmarkNamespaceAllowed_ExactMap`: ~14.3ns/op, 0 allocs/op (O(1) map lookup for 4+ namespaces)
- `BenchmarkNamespaceAllowed_ExactIteration`: ~10.6ns/op, 0 allocs/op (O(n) iteration for <4 namespaces)

### String Operations

- `BenchmarkFuzzyContains_WithBuilder`: ~520ns/op, 360B/op, 8 allocs/op (uses strings.Builder)
- `BenchmarkFuzzyContains_EmptyTarget`: ~1.5ns/op, 0 allocs/op (early exit optimization)
- `BenchmarkMatches_IgnoreCase`: ~84ns/op, 8B/op, 1 alloc/op (ToLower allocation)
- `BenchmarkMatches_RegexPrecompiled`: ~43.8ns/op, 0 allocs/op (pre-compiled regex)

### Large Scale Filtering

- `BenchmarkFiltering_LargeList`: ~646μs/op for 10,000 resources, 0 allocs/op (zero-allocation hot path)

### String Concatenation

- `BenchmarkStringConcat_Plus`: ~20.4ns/op, 0 allocs/op (simple concatenation)
- `BenchmarkStringConcat_Builder`: ~19.8ns/op, 16B/op, 1 alloc/op (pooled Builder - better for reuse)

## Performance Goals

- Zero allocations in hot paths (label/annotation filtering, namespace matching)
- Pre-compiled regexes (no runtime compilation)
- Pre-computed filter grouping (no per-resource map allocations)
- Single-pass regex matching (check all regexes in one iteration)
- O(1) namespace/node lookup for 4+ exact matches
- Early exits for empty strings and nil maps

## Notes

- Benchmarks run on Apple M3 Pro (arm64)
- All hot-path operations achieve zero allocations
- Pre-computation optimizations show significant performance gains
- Pooled strings.Builder provides better performance for repeated operations

