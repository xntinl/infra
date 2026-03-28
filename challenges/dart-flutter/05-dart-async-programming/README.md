# Section 05 -- Dart Asynchronous Programming

## Introduction

Every meaningful application waits -- for network responses, file I/O, user input. The question is how it handles those waits. Dart was designed from the ground up for async: its event loop, Futures, Streams, and Isolates are first-class language features, not bolted-on afterthoughts. Flutter depends on this -- your `build` method and network calls share one thread, yet the UI stays fluid at 60fps.

## Prerequisites

Sections 01-04 (variables, functions, control flow, OOP). Dart SDK 3.0+.

## Learning Objectives

1. **Explain** how Dart's event loop processes microtasks and event queues.
2. **Construct** Futures with async/await, then/catchError, and Completers.
3. **Design** Stream pipelines with transformations (map, where, asyncMap, expand).
4. **Implement** async* generator functions that yield values into streams.
5. **Manage** StreamSubscription lifecycles (pause, resume, cancel).
6. **Apply** Isolates for CPU-bound work via message passing.
7. **Evaluate** concurrency patterns: parallel execution, error isolation, backpressure.

---

## Core Concepts

### The Event Loop

Dart runs on a single thread with two queues: **microtask** (high priority) and **event** (normal). Synchronous code always finishes before any async callback runs.

```dart
// file: event_loop_basics.dart
import 'dart:async';

void main() {
  print('1 - Sync start');
  Future(() => print('4 - Event queue'));
  scheduleMicrotask(() => print('3 - Microtask'));
  Future.value('x').then((_) => print('3.5 - Future.value microtask'));
  print('2 - Sync end');
  // Order: 1, 2, 3, 3.5, 4
}
```

### Future and async/await

A Future is a value that will exist later. It completes with success or error.

```dart
// file: future_fundamentals.dart
import 'dart:async';

Future<String> fetchUser(int id) async {
  await Future.delayed(Duration(seconds: 1));
  if (id <= 0) throw ArgumentError('ID must be positive');
  return 'User_$id';
}

Future<void> main() async {
  // async/await with error handling
  try {
    print(await fetchUser(1));
  } on ArgumentError catch (e) {
    print('Bad arg: ${e.message}');
  }

  // Parallel execution: ~1s total, not ~3s
  final results = await Future.wait([fetchUser(1), fetchUser(2), fetchUser(3)]);
  print(results);

  // Race: first to finish wins
  final fastest = await Future.any([
    Future.delayed(Duration(milliseconds: 300), () => 'A'),
    Future.delayed(Duration(milliseconds: 100), () => 'B'),
  ]);
  print('Winner: $fastest');
}
```

### Completer

For wrapping callback-based APIs into Future-based APIs.

```dart
// file: completer_pattern.dart
import 'dart:async';

Future<String> wrapCallbackApi() {
  final completer = Completer<String>();
  simulateCallback(
    onSuccess: (data) { if (!completer.isCompleted) completer.complete(data); },
    onError: (err) { if (!completer.isCompleted) completer.completeError(err); },
  );
  return completer.future;
}
```

### Streams and async* Generators

A Stream delivers a sequence of values over time. Single-subscription streams buffer; broadcast streams multicast.

```dart
// file: stream_fundamentals.dart
import 'dart:async';

Stream<int> countDown(int from) async* {
  for (var i = from; i >= 0; i--) {
    await Future.delayed(Duration(milliseconds: 200));
    yield i;
  }
}

void main() async {
  // Consume with await-for
  await for (final v in countDown(5)) { print(v); }

  // Transformations: filter, transform, limit
  final pipeline = countDown(10).where((n) => n.isEven).map((n) => n * n).take(3);
  await for (final v in pipeline) { print(v); }

  // Broadcast stream: multiple listeners
  final controller = StreamController<String>.broadcast();
  final s1 = controller.stream.listen((m) => print('A: $m'));
  final s2 = controller.stream.listen((m) => print('B: $m'));
  controller.add('hello');
  await Future.delayed(Duration(milliseconds: 50));
  await s1.cancel(); await s2.cancel(); await controller.close();
}
```

### Stream Transformations and asyncMap

```dart
// file: stream_transformations.dart
import 'dart:async';

Future<Map<String, dynamic>> fetchDetails(int id) async {
  await Future.delayed(Duration(milliseconds: 100));
  return {'id': id, 'name': 'User_$id', 'score': id * 17 % 100};
}

Stream<int> idStream() async* {
  for (var i = 1; i <= 10; i++) { yield i; }
}

void main() async {
  final results = idStream()
      .where((id) => id.isOdd)
      .asyncMap(fetchDetails)                    // async transform per element
      .where((u) => (u['score'] as int) > 30)
      .map((u) => '${u['name']} (${u['score']})');
  await for (final r in results) { print(r); }
}
```

### StreamSubscription Lifecycle

Subscriptions hold resources. Unmanaged subscriptions cause memory leaks.

```dart
// file: subscription_lifecycle.dart
import 'dart:async';

Stream<int> ticks() async* { var i = 0; while (true) { await Future.delayed(Duration(milliseconds: 200)); yield i++; } }

void main() async {
  final sub = ticks().listen((v) => print('Tick $v'));
  await Future.delayed(Duration(seconds: 1));
  sub.pause(); await Future.delayed(Duration(milliseconds: 500));
  sub.resume(); await Future.delayed(Duration(seconds: 1));
  await sub.cancel();
}
```

### Isolates

For CPU-bound work. Independent memory, communication via message passing only.

```dart
// file: isolate_basics.dart
import 'dart:isolate';

void main() async {
  // Simplest API: Isolate.run (Dart 2.19+)
  final result = await Isolate.run(() {
    var sum = 0;
    for (var i = 0; i < 1000000000; i++) { sum += i % 7; }
    return sum;
  });
  print('Result: $result');
}
```

### Zones

Scoped error handling and context values.

```dart
// file: zone_basics.dart
import 'dart:async';

void main() {
  runZonedGuarded(() {
    Future.delayed(Duration(milliseconds: 100), () => throw StateError('boom'));
  }, (error, stack) {
    print('Caught: $error');
  });
}
```

### Common Pitfalls

```dart
// file: common_pitfalls.dart
// 1. Forgetting await: fetchSomething(); // fires but errors go unhandled
// 2. Sequential instead of parallel:
//    BAD:  final a = await f1(); final b = await f2(); // ~2s
//    GOOD: final [a, b] = await Future.wait([f1(), f2()]); // ~1s
// 3. Stream leak: someStream().listen((d) => print(d)); // who cancels this?
// 4. Broadcast drops: controller.add(1); controller.stream.listen(...); // 1 is lost
```

---

## Exercises

### Exercise 1 -- Fetch and Handle (Basic)

Use async/await with error handling. Call three APIs concurrently; use fallbacks for weather and stock failures, but propagate news errors.

```dart
// file: exercise_01_starter.dart
import 'dart:async';
import 'dart:math';

final _rng = Random(42);

Future<String> fetchWeather(String city) async {
  await Future.delayed(Duration(milliseconds: 300));
  if (city.isEmpty) throw ArgumentError('City empty');
  if (_rng.nextDouble() < 0.3) throw Exception('Timeout');
  return '$city: ${20 + _rng.nextInt(15)}C';
}

Future<double> fetchStockPrice(String symbol) async {
  await Future.delayed(Duration(milliseconds: 200));
  if (symbol.length > 5) throw FormatException('Invalid: $symbol');
  return 100 + _rng.nextDouble() * 50;
}

Future<List<String>> fetchNews(String topic) async {
  await Future.delayed(Duration(milliseconds: 400));
  return List.generate(3, (i) => '$topic headline #${i + 1}');
}

// TODO: Run all three concurrently. Weather fallback: 'Weather unavailable'.
// Stock fallback: -1.0. News: propagate errors. Return Map with keys: weather, stock, news.
Future<Map<String, dynamic>> aggregateDashboard(String city, String symbol, String topic) async {
  throw UnimplementedError();
}

void main() async {
  final d = await aggregateDashboard('London', 'AAPL', 'tech');
  print(d);
  final p = await aggregateDashboard('Tokyo', 'TOOLONG', 'sports');
  assert(p['stock'] == -1.0);
  final n = await aggregateDashboard('', 'GOOG', 'science');
  assert(n['weather'] == 'Weather unavailable');
  print('Exercise 01 complete.');
}
```

**Verification**: `dart run exercise_01.dart` -- all asserts pass, no unhandled exceptions.

---

### Exercise 2 -- Sensor Stream Pipeline (Basic)

Create an async* generator emitting temperature readings. Build a pipeline that filters comfortable range (18-32C), adds fahrenheit, and takes 10 readings.

```dart
// file: exercise_02_starter.dart
import 'dart:async';
import 'dart:math';

// TODO: async* generator yielding {'timestamp': DateTime.now(), 'celsius': double, 'sensorId': sensorId}
// Random celsius between 15.0-35.0, every 200ms, infinite.
Stream<Map<String, dynamic>> temperatureSensor(String sensorId) async* {
  throw UnimplementedError();
}

// TODO: Filter 18-32C, add 'fahrenheit' key, take first 10.
Stream<Map<String, dynamic>> processedReadings(String sensorId) {
  throw UnimplementedError();
}

void main() async {
  var count = 0;
  await for (final r in processedReadings('sensor-A')) {
    count++;
    print('${(r['celsius'] as double).toStringAsFixed(1)}C / ${(r['fahrenheit'] as double).toStringAsFixed(1)}F');
  }
  assert(count == 10);
  print('Exercise 02 complete.');
}
```

**Verification**: Exactly 10 readings, all celsius in 18-32, correct fahrenheit.

---

### Exercise 3 -- Debounce with StreamController (Intermediate)

Implement a generic debounce: on each value, reset a timer; emit when the timer fires. Flush pending value on stream close.

```dart
// file: exercise_03_starter.dart
import 'dart:async';

// TODO: Implement debounce using StreamController and Timer.
Stream<T> debounce<T>(Stream<T> source, Duration duration) {
  throw UnimplementedError();
}

Stream<String> simulateTyping() async* {
  for (final (delay, text) in [
    (Duration(milliseconds: 50), 'h'), (Duration(milliseconds: 80), 'he'),
    (Duration(milliseconds: 60), 'hel'), (Duration(milliseconds: 90), 'hell'),
    (Duration(milliseconds: 70), 'hello'),
    (Duration(milliseconds: 500), 'hello '),  // pause here
    (Duration(milliseconds: 60), 'hello w'), (Duration(milliseconds: 80), 'hello wo'),
    (Duration(milliseconds: 70), 'hello wor'), (Duration(milliseconds: 60), 'hello worl'),
    (Duration(milliseconds: 50), 'hello world'),
  ]) { await Future.delayed(delay); yield text; }
}

void main() async {
  final results = debounce(simulateTyping(), Duration(milliseconds: 300));
  await for (final q in results) { print('Search: $q'); }
  // Should emit ~2 values: "hello" and "hello world"
  print('Exercise 03 complete.');
}
```

**Verification**: ~2 emitted values, not 11.

---

### Exercise 4 -- Settle All Futures (Intermediate)

Build `settleAll` that runs all Futures concurrently, wrapping each result as Success or Failure. No short-circuit on error.

```dart
// file: exercise_04_starter.dart
import 'dart:async';

sealed class Result<T> {}
class Success<T> extends Result<T> { final T value; Success(this.value); @override String toString() => 'Success($value)'; }
class Failure<T> extends Result<T> { final Object error; Failure(this.error); @override String toString() => 'Failure($error)'; }

// TODO: Run all concurrently, wrap each individually. Preserve order.
Future<List<Result<T>>> settleAll<T>(List<Future<T>> futures) async { throw UnimplementedError(); }
List<T> successes<T>(List<Result<T>> results) { throw UnimplementedError(); }
List<Object> failures<T>(List<Result<T>> results) { throw UnimplementedError(); }

Future<String> fetchService(String name, {bool fail = false}) async {
  await Future.delayed(Duration(milliseconds: 100));
  if (fail) throw Exception('$name is down');
  return '$name: OK';
}

void main() async {
  final r = await settleAll([
    fetchService('auth'), fetchService('payments', fail: true),
    fetchService('inventory'), fetchService('notifications', fail: true),
    fetchService('search'),
  ]);
  assert(successes(r).length == 3);
  assert(failures(r).length == 2);
  print('Exercise 04 complete.');
}
```

**Verification**: 3 successes, 2 failures, asserts pass.

---

### Exercise 5 -- Isolate Worker Pool (Advanced)

Build a pool of N isolates distributing tasks round-robin. Each worker runs a long-lived loop. Tasks are top-level functions sent via SendPort/ReceivePort.

```dart
// file: exercise_05_starter.dart
import 'dart:async';
import 'dart:isolate';

typedef TaskFunction<T> = T Function(dynamic argument);

// TODO: Implement WorkerPool with start(), execute(), pendingTasks, shutdown().
// - start() spawns N isolates with long-lived event loops.
// - execute() sends (function, arg) to next worker round-robin, returns Future<T>.
// - Workers catch errors per-task (failing task must not kill the worker).
class WorkerPool {
  final int size;
  WorkerPool({required this.size});
  Future<void> start() async { throw UnimplementedError(); }
  Future<T> execute<T>(TaskFunction<T> fn, dynamic arg) { throw UnimplementedError(); }
  Map<int, int> get pendingTasks => throw UnimplementedError();
  Future<void> shutdown() async { throw UnimplementedError(); }
}

int fibonacci(dynamic n) { if (n is! int) throw ArgumentError('Expected int'); if (n <= 1) return n; return fibonacci(n - 1) + fibonacci(n - 2); }
bool isPrime(dynamic n) { if (n is! int) throw ArgumentError('Expected int'); if (n < 2) return false; for (var i = 2; i * i <= n; i++) { if (n % i == 0) return false; } return true; }

void main() async {
  final pool = WorkerPool(size: 4);
  await pool.start();
  final fibs = await Future.wait([for (var i = 30; i < 40; i++) pool.execute(fibonacci, i)]);
  print('Fibonacci: $fibs');
  final primes = await Future.wait([for (var n in [104729, 104723]) pool.execute(isPrime, n)]);
  print('Primes: $primes');
  await pool.shutdown();
  print('Exercise 05 complete.');
}
```

**Verification**: Correct fibonacci values, pool distributes work, clean shutdown.

---

### Exercise 6 -- Pub/Sub Event System (Advanced)

Typed event bus with named channels. Lazy channel creation, automatic cleanup when last subscriber cancels, dispose closes everything.

```dart
// file: exercise_06_starter.dart
import 'dart:async';

// TODO: Implement EventBus with publish<T>(), subscribe<T>(), activeChannels,
// subscriberCount(), dispose(). Channels auto-cleanup when last subscriber cancels.
class EventBus {
  Future<void> publish<T>(String channel, T event) async { throw UnimplementedError(); }
  Stream<T> subscribe<T>(String channel) { throw UnimplementedError(); }
  List<String> get activeChannels => throw UnimplementedError();
  int subscriberCount(String channel) { throw UnimplementedError(); }
  Future<void> dispose() async { throw UnimplementedError(); }
}

class UserEvent { final String action, userId; UserEvent(this.action, this.userId); @override String toString() => 'UserEvent($action, $userId)'; }
class OrderEvent { final String orderId; final double amount; OrderEvent(this.orderId, this.amount); @override String toString() => 'OrderEvent($orderId, \$$amount)'; }

void main() async {
  final bus = EventBus();
  final s1 = bus.subscribe<UserEvent>('users').listen((e) => print('Analytics: $e'));
  final s2 = bus.subscribe<UserEvent>('users').listen((e) => print('Audit: $e'));
  final s3 = bus.subscribe<OrderEvent>('orders').listen((e) => print('Billing: $e'));
  print('Channels: ${bus.activeChannels}, User subs: ${bus.subscriberCount("users")}');

  await bus.publish('users', UserEvent('login', 'u-123'));
  await bus.publish('orders', OrderEvent('ord-456', 99.99));
  await Future.delayed(Duration(milliseconds: 100));

  await s1.cancel(); await s2.cancel();
  await Future.delayed(Duration(milliseconds: 50));
  print('After cancel all user subs, channels: ${bus.activeChannels}');
  await s3.cancel(); await bus.dispose();
  print('Exercise 06 complete.');
}
```

**Verification**: Events reach correct listeners. "users" channel disappears after both subs cancel.

---

### Exercise 7 -- Task Scheduler with Circuit Breaker (Insane)

Build four components: CancellationToken, RetryPolicy (exponential backoff + jitter), CircuitBreaker (closed/open/half-open states), and TaskScheduler (priority queue, concurrency limit, integrates retry and circuit breaker).

```dart
// file: exercise_07_starter.dart
import 'dart:async';
import 'dart:math';

// TODO: CancellationToken with isCancelled, cancel(), onCancel(cb), throwIfCancelled()
class CancellationToken { /* implement */ }

// TODO: RetryPolicy -- execute(fn, token) with exponential backoff: baseDelay * 2^attempt + jitter
class RetryPolicy {
  final int maxRetries;
  final Duration baseDelay;
  final void Function(int attempt, Object error)? onRetry;
  RetryPolicy({this.maxRetries = 3, this.baseDelay = const Duration(milliseconds: 100), this.onRetry});
  Future<T> execute<T>(Future<T> Function() fn, CancellationToken? token) async { throw UnimplementedError(); }
}

// TODO: CircuitBreaker -- tracks consecutive failures, opens after threshold, half-open after cooldown
enum CircuitState { closed, open, halfOpen }
class CircuitBreakerOpenException implements Exception { final Duration retryAfter; CircuitBreakerOpenException(this.retryAfter); }
class CircuitBreaker {
  final int failureThreshold; final Duration cooldown;
  CircuitBreaker({this.failureThreshold = 3, this.cooldown = const Duration(seconds: 5)});
  CircuitState get state => throw UnimplementedError();
  Future<T> execute<T>(Future<T> Function() fn) async { throw UnimplementedError(); }
}

// TODO: TaskScheduler -- priority queue, maxConcurrency, integrates token + retry + breaker
enum TaskPriority { low, normal, high, critical }
class TaskScheduler {
  final int maxConcurrency; final CircuitBreaker? circuitBreaker;
  TaskScheduler({this.maxConcurrency = 3, this.circuitBreaker});
  Future<T> schedule<T>(Future<T> Function() task, {TaskPriority priority = TaskPriority.normal, CancellationToken? token, RetryPolicy? retryPolicy}) { throw UnimplementedError(); }
  int get runningCount => throw UnimplementedError();
  int get queuedCount => throw UnimplementedError();
  Future<void> shutdown() async { throw UnimplementedError(); }
}

void main() async {
  // Test CancellationToken
  final token = CancellationToken(); var fired = false;
  token.onCancel(() => fired = true); token.cancel();
  assert(token.isCancelled && fired);
  print('Token: OK');

  // Test RetryPolicy
  var attempts = 0;
  await RetryPolicy(maxRetries: 3, baseDelay: Duration(milliseconds: 50)).execute(() async {
    if (++attempts < 3) throw Exception('fail'); return 'ok';
  }, null);
  print('Retry: OK after $attempts attempts');

  // Test CircuitBreaker
  final breaker = CircuitBreaker(failureThreshold: 2, cooldown: Duration(seconds: 1));
  for (var i = 0; i < 3; i++) { try { await breaker.execute(() async { throw Exception('down'); }); } catch (_) {} }
  print('Breaker state: ${breaker.state}');

  // Test Scheduler
  final sched = TaskScheduler(maxConcurrency: 2);
  final rng = Random(42);
  final futures = [for (var i = 0; i < 6; i++) sched.schedule(
    () async { await Future.delayed(Duration(milliseconds: 50)); if (rng.nextDouble() < 0.3) throw Exception('fail'); return 'Task-$i ok'; },
    priority: i < 2 ? TaskPriority.critical : TaskPriority.normal,
    retryPolicy: RetryPolicy(maxRetries: 2, baseDelay: Duration(milliseconds: 30)),
  )];
  final results = await Future.wait(futures.map((f) => f.catchError((e) => 'FAILED: $e')));
  results.forEach(print);
  await sched.shutdown();
  print('Exercise 07 complete.');
}
```

**Verification**: Token callbacks fire, retry succeeds after failures, breaker opens, scheduler respects concurrency and priority.

---

### Exercise 8 -- Stream Processing Engine with Backpressure (Insane)

Build BackpressureController (buffer/drop/latest/error strategies), StreamProcessor (typed input/output, per-item error handling), and ProcessingPipeline (chains processors with backpressure between stages).

```dart
// file: exercise_08_starter.dart
import 'dart:async';
import 'dart:collection';
import 'dart:convert';

enum BackpressureStrategy { buffer, drop, latest, error }

// TODO: BackpressureController<T> -- wraps source stream with bounded buffer and strategy.
class BackpressureController<T> {
  final Stream<T> source; final int bufferSize; final BackpressureStrategy strategy;
  BackpressureController({required this.source, this.bufferSize = 100, this.strategy = BackpressureStrategy.buffer});
  Stream<T> get stream => throw UnimplementedError();
  int get currentBufferSize => throw UnimplementedError();
  Future<void> dispose() async { throw UnimplementedError(); }
}

// TODO: StreamProcessor<In, Out> -- bind(source) returns transformed stream, tracks processed/error/dropped counts.
abstract class StreamProcessor<In, Out> {
  String get name;
  Future<Out> process(In input);
  Future<Out?> onError(Object error, In input) async => null;
  Stream<Out> bind(Stream<In> source);
  int get processedCount; int get errorCount; int get droppedCount;
}

// TODO: ProcessingPipeline -- chains processors, backpressure between stages, metrics per stage.
class ProcessingPipeline {
  final List<StreamProcessor> processors; final BackpressureStrategy strategy; final int bufferSize;
  ProcessingPipeline({required this.processors, this.strategy = BackpressureStrategy.buffer, this.bufferSize = 50});
  Stream<dynamic> connect(Stream<dynamic> source) { throw UnimplementedError(); }
  Map<String, Map<String, int>> get metrics => throw UnimplementedError();
  Future<void> dispose() async { throw UnimplementedError(); }
}

// TODO: Implement ParseProcessor (jsonDecode), EnrichProcessor (add computed fields), FilterProcessor (predicate).

Stream<String> generateEvents(int count) async* {
  for (var i = 0; i < count; i++) {
    await Future.delayed(Duration(milliseconds: 10));
    yield (i > 0 && i % 7 == 0) ? 'MALFORMED' : '{"id":$i,"value":${i * 1.5},"cat":"${i % 3 == 0 ? "A" : "B"}"}';
  }
}

void main() async {
  // Test backpressure
  final fast = Stream.periodic(Duration(milliseconds: 1), (i) => i).take(100);
  final bp = BackpressureController<int>(source: fast, bufferSize: 10);
  var count = 0;
  await for (final _ in bp.stream) { count++; if (count % 20 == 0) await Future.delayed(Duration(milliseconds: 50)); }
  print('Received: $count');
  await bp.dispose();

  // Test pipeline: parse -> enrich -> filter(cat==A)
  // Implement ParseProcessor, EnrichProcessor, FilterProcessor, then:
  // final pipeline = ProcessingPipeline(processors: [ParseProcessor(), EnrichProcessor(), FilterProcessor((d) => d['cat'] == 'A')]);
  // final output = pipeline.connect(generateEvents(50));
  // await for (final e in output) { ... }
  // print(pipeline.metrics);
  // await pipeline.dispose();
  print('Exercise 08 complete.');
}
```

**Verification**: Backpressure prevents memory blowup. Pipeline parses, enriches, filters. Malformed events are handled. Metrics show per-stage counts.

---

## Summary

- **Event loop**: single-threaded, microtask queue before event queue
- **Future**: one-shot async value; compose with async/await or then/catchError; parallelize with Future.wait
- **Completer**: manual Future completion for wrapping callback APIs
- **Stream**: async sequence; single-subscription (buffers) vs broadcast (multicasts); transform with map/where/asyncMap/expand
- **async* generators**: declarative stream creation with yield
- **StreamSubscription**: pause/resume/cancel; always cancel to prevent leaks
- **Isolates**: true parallelism via separate memory and message passing
- **Zones**: scoped error handling and zone-local values
- **Patterns**: error isolation, debounce, circuit breaker, backpressure, worker pools

The core principle: async is not about speed, it is about responsiveness. Keep the event loop free, handle errors at every level, clean up resources when done.

## What's Next

Section 06: **Dart Generics and the Type System** -- type-safe abstractions that make these async patterns (Result types, typed processors, worker pools) compile-time safe.

## References

- [Dart Async Programming](https://dart.dev/codelabs/async-await)
- [Dart Streams](https://dart.dev/tutorials/language/streams)
- [Dart Concurrency / Isolates](https://dart.dev/language/concurrency)
- [Zone API Reference](https://api.dart.dev/stable/dart-async/Zone-class.html)
- [Effective Dart: Asynchrony](https://dart.dev/effective-dart/usage#asynchrony)
