# 2. Operators: Map, Filter, FlatMap

<!--
difficulty: insane
concepts: [stream-operators, map, filter, flatmap, operator-chaining, type-safe-pipelines, backpressure-propagation, operator-fusion]
tools: [go]
estimated_time: 2h
bloom_level: create
prerequisites: [43-capstone-stream-processing-engine/01-source-connectors, 13-goroutines-and-channels, 10-generics]
-->

## Prerequisites

- Go 1.22+ installed
- Completed exercise 01 (source connectors) or equivalent experience with the `Record` and `Source` abstractions
- Completed Sections 10 (generics) and 13-14 (concurrency) or equivalent experience

## Learning Objectives

- **Design** a composable operator framework where stateless transformations are chained into processing pipelines via a uniform interface
- **Create** Map, Filter, and FlatMap operators that transform record streams with correct backpressure propagation and error handling
- **Evaluate** the performance implications of per-operator goroutines versus operator fusion for reducing channel overhead

## The Challenge

Stream processing engines transform data through chains of operators. Every record from a source flows through a pipeline of transformations: mapping (transforming a record into a new record), filtering (discarding records that do not match a predicate), and flat-mapping (transforming a single record into zero or more output records). Engines like Apache Flink and Kafka Streams build complex topologies from these primitives.

You will design an `Operator` interface that consumes records from an input channel and produces records on an output channel, then implement the three foundational stateless operators. The challenge is not just making them work -- it is designing the interface so operators compose cleanly, propagate backpressure correctly (a slow downstream operator slows upstream operators rather than dropping records), handle errors without crashing the pipeline, and shut down gracefully when the source is exhausted or the context is cancelled.

You will also implement operator chaining -- a `Pipeline` builder that connects operators into a linear topology with automatic channel wiring. As an optimization, you will implement operator fusion: detecting adjacent Map operators and merging their functions into a single operator to eliminate an intermediate channel and goroutine.

## Requirements

1. Define an `Operator` interface with `Process(ctx context.Context, in <-chan Record) (<-chan Record, <-chan error)` that reads from an input channel, applies its transformation, and writes results to an output channel
2. Implement `MapOperator` that applies a `func(Record) (Record, error)` to each input record, emitting the transformed record or sending the error to the error channel
3. Implement `FilterOperator` that applies a `func(Record) (bool, error)` predicate to each input record, emitting only records for which the predicate returns true
4. Implement `FlatMapOperator` that applies a `func(Record) ([]Record, error)` to each input record, emitting zero or more output records per input record
5. All operators must propagate backpressure: if the output channel is full, the operator blocks on send rather than dropping records
6. All operators must respect context cancellation: when the context is cancelled, operators drain in-flight work and close their output channels
7. Implement a `Pipeline` builder that chains operators: `NewPipeline().Map(fn).Filter(fn).FlatMap(fn).Build()` returning a composite operator
8. Implement operator fusion: when two adjacent `MapOperator` instances are chained, the pipeline builder fuses their functions into a single `MapOperator` with the composed function `f(g(record))`
9. Implement error routing: operators can be configured with an error handler that decides whether to skip the record, retry, or abort the pipeline
10. Track per-operator metrics: records in, records out, records dropped, errors, and processing latency

## Hints

- Each operator should spawn a goroutine that reads from `in`, applies the transformation, and sends to a created output channel -- close the output channel when `in` is closed and all work is drained
- For backpressure, use blocking sends on the output channel with a select on `ctx.Done()` to avoid deadlocks during shutdown
- For operator fusion, check the concrete type of adjacent operators during `Build()` -- if both are `*MapOperator`, create a new `MapOperator` with `func(r Record) (Record, error) { return f(g(r)) }` where the inner function is applied first
- Use a buffered output channel (small buffer, e.g., 16) per operator to smooth out micro-bursts without defeating backpressure
- For `FlatMapOperator`, iterate over the returned slice and send each record individually, checking context cancellation between each send
- For error routing, define an `ErrorHandler` type `func(error, Record) ErrorAction` where `ErrorAction` is an enum of `Skip`, `Retry`, `Abort`

## Success Criteria

1. `MapOperator` correctly transforms every input record and emits the transformed record
2. `FilterOperator` correctly discards records that do not match the predicate and passes matching records through unmodified
3. `FlatMapOperator` correctly emits zero, one, or many records per input record
4. Backpressure propagates correctly: a slow consumer causes the producer to block rather than drop records
5. Pipeline builder correctly chains three or more operators with automatic channel wiring
6. Operator fusion reduces the goroutine count when adjacent Map operators are detected
7. Context cancellation causes all operators in the pipeline to shut down within 1 second
8. All tests pass with the `-race` flag enabled

## Research Resources

- [Apache Flink DataStream API](https://nightlies.apache.org/flink/flink-docs-stable/docs/dev/datastream/operators/overview/) -- reference design for stream operator composition
- [Go concurrency patterns: pipelines](https://go.dev/blog/pipelines) -- channel-based pipeline patterns in Go
- [Operator fusion in stream processing](https://flink.apache.org/2015/03/13/peeking-into-apache-flinks-engine-room/) -- how Flink fuses operators to reduce overhead
- [Go generics](https://go.dev/doc/tutorial/generics) -- type-safe operator design using generics
- [Reactive Streams specification](https://www.reactive-streams.org/) -- backpressure semantics in stream processing

## What's Next

Continue to [Windowing](../03-windowing/03-windowing.md) where you will implement time-based and count-based windowing to aggregate records over bounded intervals.

## Summary

- Map, Filter, and FlatMap are the foundational stateless operators for stream processing
- A uniform `Operator` interface enables composition of arbitrary operator chains via a pipeline builder
- Backpressure propagation through blocking channel sends ensures no records are dropped under load
- Operator fusion merges adjacent compatible operators to eliminate intermediate channels and goroutines
- Error routing gives pipeline authors control over how transformation failures are handled per operator
- Per-operator metrics provide visibility into where records are transformed, filtered, or lost
