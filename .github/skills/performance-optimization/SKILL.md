---
name: performance-optimization
type: skill
description: "Performance optimization patterns. Use when reviewing code for performance issues, optimizing queries, reducing memory usage, or improving throughput."
---

## Purpose

Identify and fix performance bottlenecks without premature optimization. Focus on measurable improvements in critical paths: database queries, hot loops, memory allocation, and I/O operations.

---

## Rules

### Optimization Principles

1. **Measure before optimizing** — profile first, identify actual bottlenecks, then fix
2. **Optimize hot paths** — focus on code that runs frequently (loops, request handlers, queries)
3. **Algorithmic improvement first** — O(n²) → O(n log n) beats micro-optimization every time
4. **No premature optimization** — clarity over cleverness unless profiling proves otherwise
5. **Benchmark changes** — verify optimization actually improves performance

### Database Performance

1. **Index frequently queried columns** — especially WHERE, JOIN, ORDER BY columns
2. **Limit result sets** — always use LIMIT, never `SELECT *` without bounds
3. **Batch operations** — insert/update in batches, not one-by-one
4. **N+1 query detection** — if you query in a loop, restructure to batch query
5. **Connection pooling** — reuse connections, don't create per-request

### Memory Management

1. **Stream large datasets** — don't load entire result sets into memory
2. **Release resources promptly** — close files, connections, cursors after use
3. **Avoid unnecessary copies** — use references/slices where appropriate
4. **Limit collection sizes** — cap in-memory collections, use pagination

### I/O Optimization

1. **Buffer I/O operations** — batch writes, use buffered readers
2. **Async where beneficial** — non-blocking I/O for independent operations
3. **Cache expensive computations** — cache results of deterministic expensive functions
4. **Compress large payloads** — gzip for network, compression for storage

### Concurrency

1. **Parallel independent operations** — when tasks don't share state, run concurrently
2. **Minimize lock contention** — keep critical sections small
3. **Prefer immutable data** — no locks needed for read-only data
4. **Bounded concurrency** — limit parallel goroutines/threads/tasks

---

## Performance Checklist

```
[ ] Database queries indexed on filter/sort columns
[ ] No N+1 query patterns (queries inside loops)
[ ] Result sets bounded (LIMIT/pagination)
[ ] Large datasets streamed, not loaded entirely into memory
[ ] Resources (connections, files) closed promptly
[ ] Hot paths profiled and optimized
[ ] Batch operations used for bulk inserts/updates
[ ] No unnecessary data copies
[ ] Connection pooling configured
[ ] Caching applied to expensive deterministic computations
```

---

## Red Flags

| Pattern                            | Impact                     | Fix                                |
| ---------------------------------- | -------------------------- | ---------------------------------- |
| `SELECT * FROM large_table`        | Memory explosion           | Add LIMIT, select specific columns |
| Query inside a for loop            | N+1 queries, O(n) DB calls | Batch query with IN clause         |
| Loading entire file into string    | Memory spike               | Stream/buffer line-by-line         |
| Creating DB connection per request | Connection exhaustion      | Use connection pool                |
| Unbounded cache                    | Memory leak                | LRU cache with max size            |
| Synchronous I/O in hot path        | Blocking, poor throughput  | Async or buffered I/O              |
