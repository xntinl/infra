# Section 05 -- Solutions: Dart Asynchronous Programming

## How to Use This Guide

Read one hint at a time, try again after each. Only look at the full solution when genuinely stuck. The struggle with async bugs teaches more than any correct answer.

---

## Exercise 1 -- Fetch and Handle

### Hints

1. Start all three Futures before awaiting any of them -- that gives you concurrency.
2. Wrap weather and stock in `.catchError()` individually so each resolves to either real data or fallback.
3. The Futures are already running when you assign them; `await` just waits for completion.

### Solution

```dart
// file: exercise_01_solution.dart
Future<Map<String, dynamic>> aggregateDashboard(String city, String symbol, String topic) async {
  // Start all three concurrently -- the Future begins when you call the function, not when you await.
  final weatherFuture = fetchWeather(city).catchError((_) => 'Weather unavailable');
  final stockFuture = fetchStockPrice(symbol).then<double>((v) => v).catchError((_) => -1.0);
  final newsFuture = fetchNews(topic); // no fallback -- propagates errors

  return {
    'weather': await weatherFuture,
    'stock': await stockFuture,
    'news': await newsFuture,
  };
}
```

### Common Mistakes

**Sequential execution**: Writing `final weather = await fetchWeather(...)` before calling the others means they run one after another (~900ms instead of ~400ms). Fix: call all functions first, then await.

**Using Future.wait without individual wrapping**: `Future.wait([a, b, c])` fails fast -- one error kills the whole group and you lose successful results.

**Catching too broadly on news**: The requirement says to propagate news errors. Wrapping it in catchError silently hides failures.

---

## Exercise 2 -- Sensor Stream Pipeline

### Hints

1. `async*` + `while(true)` + `yield` + `await Future.delayed` = infinite timed stream.
2. Chain `.where()`, `.map()`, `.take(10)` on the stream. Transformations are lazy.
3. `.take(10)` auto-cancels the infinite source after 10 matching values pass through.

### Solution

```dart
// file: exercise_02_solution.dart
Stream<Map<String, dynamic>> temperatureSensor(String sensorId) async* {
  final random = Random();
  while (true) {
    await Future.delayed(Duration(milliseconds: 200));
    yield {'timestamp': DateTime.now(), 'celsius': 15.0 + random.nextDouble() * 20.0, 'sensorId': sensorId};
  }
}

Stream<Map<String, dynamic>> processedReadings(String sensorId) {
  return temperatureSensor(sensorId)
      .where((r) { final c = r['celsius'] as double; return c >= 18.0 && c <= 32.0; })
      .map((r) => {...r, 'fahrenheit': (r['celsius'] as double) * 9.0 / 5.0 + 32.0})
      .take(10);
}
```

### Common Mistakes

**Forgetting await in generator**: Without `await Future.delayed(...)`, the loop runs synchronously forever, blocking the event loop completely.

**Mutating the input map**: `reading['fahrenheit'] = ...` mutates the original. Use spread `{...reading, 'key': value}` for safety.

**Using listen instead of await-for**: `listen` does not block, so `main` exits before the stream finishes. `await for` waits for completion.

---

## Exercise 3 -- Debounce with StreamController

### Hints

1. Create a `StreamController<T>`. In its `onListen`, subscribe to the source stream.
2. On each value: store it, cancel existing Timer, start a new Timer for the duration.
3. On Timer fire: add the stored value to the controller.
4. On source done: cancel timer, flush pending value, close controller.
5. On cancel: cancel timer and source subscription.

### Solution

```dart
// file: exercise_03_solution.dart
Stream<T> debounce<T>(Stream<T> source, Duration duration) {
  late StreamController<T> controller;
  StreamSubscription<T>? sub;
  Timer? timer;
  T? latest;
  bool hasValue = false;

  controller = StreamController<T>(
    onListen: () {
      sub = source.listen(
        (value) {
          latest = value; hasValue = true;
          timer?.cancel();
          timer = Timer(duration, () {
            if (hasValue) { controller.add(latest as T); hasValue = false; }
          });
        },
        onError: controller.addError,   // forward errors immediately
        onDone: () {
          timer?.cancel();
          if (hasValue) controller.add(latest as T);  // flush pending
          controller.close();
        },
      );
    },
    onCancel: () { timer?.cancel(); return sub?.cancel(); },
  );
  return controller.stream;
}
```

### Common Mistakes

**Not cancelling timer on new values**: Without `timer?.cancel()`, every keystroke creates a timer and all of them fire. Zero debouncing.

**Not flushing on done**: If the stream ends with a pending value, it is lost. The `onDone` handler must emit it.

**Using broadcast controller**: Broadcast controllers do not buffer. A debounce has exactly one consumer -- use a regular controller.

### Debugging Tips

Add `print('Timer started for: $value')` inside the listen callback and `print('Timer fired, emitting: $latest')` inside the Timer callback to trace timing.

---

## Exercise 4 -- Settle All Futures

### Hints

1. Map each `Future<T>` to `Future<Result<T>>` that always succeeds.
2. `.then((v) => Success<T>(v)).catchError((e) => Failure<T>(e))` wraps each one.
3. Use `Future.wait` on the wrapped list -- it cannot fail since every element resolves.

### Solution

```dart
// file: exercise_04_solution.dart
Future<List<Result<T>>> settleAll<T>(List<Future<T>> futures) {
  return Future.wait(
    futures.map((f) => f.then<Result<T>>(Success.new).catchError((Object e) => Failure<T>(e))).toList(),
  );
}

List<T> successes<T>(List<Result<T>> results) =>
    [for (final r in results) if (r is Success<T>) r.value];

List<Object> failures<T>(List<Result<T>> results) =>
    [for (final r in results) if (r is Failure<T>) r.error];
```

### Common Mistakes

**Catching Exception instead of Object**: Dart can throw anything, not just Exception. Use `Object` in catchError.

**Losing order**: `Future.wait` preserves input order. Manual approaches with `forEach` may not.

### Deep Dive

This is Dart's equivalent of JavaScript's `Promise.allSettled`. The trick: make every Future infallible by converting errors into values. `Future.wait` then sees only successes. The sealed class gives you exhaustive pattern matching at compile time.

---

## Exercise 5 -- Isolate Worker Pool

### Hints

1. Each worker isolate runs a long-lived `ReceivePort.listen` loop. It receives `[taskId, function, argument]`, executes, and sends back `[taskId, result]` or `[taskId, null, error]`.
2. You cannot send closures across isolates. Only top-level/static functions work.
3. Use a Completer per task, keyed by taskId. When the result arrives, complete it.
4. Round-robin: `workers[counter++ % size]`.

### Solution

```dart
// file: exercise_05_solution.dart
class WorkerPool {
  final int size;
  final List<_WorkerHandle> _workers = [];
  final Map<int, Completer<dynamic>> _pending = {};
  int _taskId = 0, _nextWorker = 0;
  late ReceivePort _resultPort;

  WorkerPool({required this.size});

  Future<void> start() async {
    _resultPort = ReceivePort();
    _resultPort.listen((msg) {
      if (msg is List && msg.length >= 2) {
        final id = msg[0] as int;
        final completer = _pending.remove(id);
        if (completer == null) return;
        if (msg.length == 3) { completer.completeError(msg[2]); }
        else { completer.complete(msg[1]); }
      }
    });

    for (var i = 0; i < size; i++) {
      final initPort = ReceivePort();
      final isolate = await Isolate.spawn(_workerEntry, initPort.sendPort);
      final workerSendPort = await initPort.first as SendPort;
      workerSendPort.send(_resultPort.sendPort);
      _workers.add(_WorkerHandle(isolate, workerSendPort));
    }
  }

  static void _workerEntry(SendPort initSendPort) {
    final port = ReceivePort();
    initSendPort.send(port.sendPort);
    SendPort? resultPort;
    port.listen((msg) {
      if (msg is SendPort) { resultPort = msg; return; }
      if (msg is List && msg.length == 3) {
        final taskId = msg[0] as int;
        try { resultPort?.send([taskId, Function.apply(msg[1] as Function, [msg[2]])]); }
        catch (e) { resultPort?.send([taskId, null, e.toString()]); }
      }
    });
  }

  Future<T> execute<T>(TaskFunction<T> fn, dynamic arg) {
    final id = _taskId++;
    final completer = Completer<T>();
    _pending[id] = completer;
    _workers[_nextWorker++ % size].sendPort.send([id, fn, arg]);
    return completer.future;
  }

  Map<int, int> get pendingTasks => {for (var i = 0; i < size; i++) i: 0}; // simplified

  Future<void> shutdown() async {
    for (final w in _workers) { w.isolate.kill(priority: Isolate.immediate); }
    _resultPort.close();
    _workers.clear(); _pending.clear();
  }
}

class _WorkerHandle {
  final Isolate isolate;
  final SendPort sendPort;
  _WorkerHandle(this.isolate, this.sendPort);
}
```

### Common Mistakes

**Sending closures**: Closures capture their enclosing scope, which lives in main isolate memory. Only top-level or static functions cross the isolate boundary.

**Not awaiting the handshake**: The first message from worker is its SendPort. Sending tasks before receiving it means messages are lost.

**Unclosed ReceivePorts**: They keep the event loop alive. The program never exits. Always close them in shutdown.

**Unhandled worker errors**: If a task throws inside the worker and you do not catch it there, the Completer never completes and hangs forever.

---

## Exercise 6 -- Pub/Sub Event System

### Hints

1. Use `StreamController.broadcast()` per channel. Track subscriber count manually.
2. Wrap each subscriber's connection in its own `StreamController` so you detect individual cancellations.
3. In the per-subscriber `onCancel`, decrement count. If zero, close and remove the channel.

### Solution

```dart
// file: exercise_06_solution.dart
class EventBus {
  final Map<String, _Channel> _channels = {};

  _Channel _ensure(String name) => _channels.putIfAbsent(name, () => _Channel());

  Future<void> publish<T>(String channel, T event) async {
    _channels[channel]?.controller.add(event);
  }

  Stream<T> subscribe<T>(String channel) {
    final ch = _ensure(channel);
    ch.subCount++;
    late StreamController<T> subCtrl;
    late StreamSubscription sub;

    subCtrl = StreamController<T>(
      onListen: () {
        sub = ch.controller.stream.listen(
          (e) { if (e is T && !subCtrl.isClosed) subCtrl.add(e); },
          onError: subCtrl.addError,
          onDone: () { if (!subCtrl.isClosed) subCtrl.close(); },
        );
      },
      onCancel: () {
        sub.cancel();
        ch.subCount--;
        if (ch.subCount <= 0) { ch.controller.close(); _channels.remove(channel); }
      },
    );
    return subCtrl.stream;
  }

  List<String> get activeChannels => _channels.keys.toList();
  int subscriberCount(String channel) => _channels[channel]?.subCount ?? 0;

  Future<void> dispose() async {
    for (final ch in _channels.values) { if (!ch.controller.isClosed) await ch.controller.close(); }
    _channels.clear();
  }
}

class _Channel {
  final controller = StreamController.broadcast();
  int subCount = 0;
}
```

### Common Mistakes

**Single-subscription controller for channel**: Channels need multiple listeners. Must be broadcast.

**Relying on broadcast onCancel**: Broadcast controller's `onCancel` fires when the last listener leaves, but manual counting is more explicit and predictable.

**Closing twice**: Check `isClosed` before closing in `dispose`.

---

## Exercise 7 -- Task Scheduler with Circuit Breaker

### Hints

**CancellationToken**: Boolean flag + callback list. `cancel()` sets flag and fires all callbacks. `onCancel` on an already-cancelled token fires immediately.

**RetryPolicy**: Loop 0..maxRetries. On catch: check token, compute `baseDelay * 2^attempt + random jitter`, wait, retry. After maxRetries, rethrow last error.

**CircuitBreaker**: Track `_consecutiveFailures` and `_openedAt` timestamp. State machine: closed (normal), open (reject all for cooldown), half-open (allow one probe -- success resets to closed, failure reopens).

**TaskScheduler**: List sorted by priority. `schedule` adds to queue, returns Completer's future. `_drain` loop: while running < maxConcurrency and queue not empty, pop highest priority, execute it. In finally block after execution, decrement running and call `_drain` again.

### Solution (Key Components)

```dart
// file: exercise_07_solution.dart

class CancellationToken {
  bool _cancelled = false;
  final _callbacks = <void Function()>[];
  bool get isCancelled => _cancelled;
  void cancel() { if (_cancelled) return; _cancelled = true; for (final cb in _callbacks) cb(); _callbacks.clear(); }
  void onCancel(void Function() cb) { if (_cancelled) { cb(); return; } _callbacks.add(cb); }
  void throwIfCancelled() { if (_cancelled) throw CancelledException(); }
}

class RetryPolicy {
  final int maxRetries; final Duration baseDelay;
  final void Function(int, Object)? onRetry;
  final _rng = Random();
  RetryPolicy({this.maxRetries = 3, this.baseDelay = const Duration(milliseconds: 100), this.onRetry});

  Future<T> execute<T>(Future<T> Function() fn, CancellationToken? token) async {
    Object lastErr = Exception('no attempts');
    for (var attempt = 0; attempt <= maxRetries; attempt++) {
      token?.throwIfCancelled();
      try { return await fn(); }
      catch (e) {
        lastErr = e;
        if (attempt == maxRetries) break;
        onRetry?.call(attempt + 1, e);
        final ms = baseDelay.inMilliseconds * (1 << attempt) + _rng.nextInt(baseDelay.inMilliseconds ~/ 2 + 1);
        await Future.delayed(Duration(milliseconds: ms));
      }
    }
    throw lastErr;
  }
}

class CircuitBreaker {
  final int failureThreshold; final Duration cooldown;
  int _failures = 0; DateTime? _openedAt; CircuitState _state = CircuitState.closed;
  CircuitBreaker({this.failureThreshold = 3, this.cooldown = const Duration(seconds: 5)});

  CircuitState get state { _checkTransition(); return _state; }

  void _checkTransition() {
    if (_state == CircuitState.open && _openedAt != null && DateTime.now().difference(_openedAt!) >= cooldown) {
      _state = CircuitState.halfOpen;
    }
  }

  Future<T> execute<T>(Future<T> Function() fn) async {
    _checkTransition();
    if (_state == CircuitState.open) throw CircuitBreakerOpenException(cooldown - DateTime.now().difference(_openedAt!));
    try { final r = await fn(); _failures = 0; _state = CircuitState.closed; return r; }
    catch (e) { _failures++; if (_failures >= failureThreshold) { _state = CircuitState.open; _openedAt = DateTime.now(); } rethrow; }
  }
}

class TaskScheduler {
  final int maxConcurrency; final CircuitBreaker? circuitBreaker;
  final List<_Task> _queue = []; int _running = 0; bool _shutdown = false;
  TaskScheduler({this.maxConcurrency = 3, this.circuitBreaker});

  Future<T> schedule<T>(Future<T> Function() task, {TaskPriority priority = TaskPriority.normal, CancellationToken? token, RetryPolicy? retryPolicy}) {
    if (_shutdown) throw StateError('Shut down');
    final c = Completer<T>();
    if (token?.isCancelled == true) { c.completeError(CancelledException()); return c.future; }
    _queue.add(_Task(task, priority, token, retryPolicy, c));
    _queue.sort((a, b) => b.priority.index.compareTo(a.priority.index));
    _drain();
    return c.future;
  }

  void _drain() {
    while (_running < maxConcurrency && _queue.isNotEmpty && !_shutdown) {
      _runTask(_queue.removeAt(0));
    }
  }

  Future<void> _runTask(_Task t) async {
    if (t.token?.isCancelled == true) { t.completer.completeError(CancelledException()); return; }
    _running++;
    try {
      Future<dynamic> wrapped() async { t.token?.throwIfCancelled(); return circuitBreaker != null ? await circuitBreaker!.execute(t.task) : await t.task(); }
      final result = t.retryPolicy != null ? await t.retryPolicy!.execute(wrapped, t.token) : await wrapped();
      if (!t.completer.isCompleted) t.completer.complete(result);
    } catch (e) { if (!t.completer.isCompleted) t.completer.completeError(e); }
    finally { _running--; _drain(); }
  }

  int get runningCount => _running;
  int get queuedCount => _queue.length;
  Future<void> shutdown() async { _shutdown = true; for (final t in _queue) t.completer.completeError(StateError('Shut down')); _queue.clear(); }
}

class _Task { final Function task; final TaskPriority priority; final CancellationToken? token; final RetryPolicy? retryPolicy; final Completer completer; _Task(this.task, this.priority, this.token, this.retryPolicy, this.completer); }
```

### Common Mistakes

**Completing a Completer twice**: After retry succeeds, a race with cancellation could double-complete. Always check `completer.isCompleted`.

**Not draining after completion**: Without `_drain()` in `finally`, queued tasks wait forever when a slot opens.

**Circuit breaker state checked at schedule time, not execution time**: A task may be queued while open, but by execution time the cooldown passed. Always check state at execution.

**Priority starvation**: Low-priority tasks never run if high-priority ones keep arriving. Production systems add aging to prevent this.

---

## Exercise 8 -- Stream Processing Engine with Backpressure

### Hints

1. **BackpressureController**: Listen to source, buffer events in a Queue. On buffer full, apply strategy (drop oldest / drop new / keep latest / throw). Flush buffer whenever the output controller is not paused.
2. **StreamProcessor.bind**: Return a stream from a new StreamController. In onListen, subscribe to source. For each input, call `process()` in try/catch. On error, call `onError()` -- null means skip the item.
3. **Pipeline.connect**: Fold over processors: `processors.fold(source, (stream, proc) => proc.bind(bpController(stream)))`.
4. **FilterProcessor trick**: Throw from `process()` when predicate fails, return null from `onError()` to skip. Keeps the processor interface uniform.

### Solution (Key Components)

```dart
// file: exercise_08_solution.dart

class BackpressureController<T> {
  final Stream<T> source; final int bufferSize; final BackpressureStrategy strategy;
  final Queue<T> _buf = Queue(); StreamSubscription<T>? _sub; StreamController<T>? _out;

  BackpressureController({required this.source, this.bufferSize = 100, this.strategy = BackpressureStrategy.buffer});

  Stream<T> get stream { _out ??= _build(); return _out!.stream; }
  int get currentBufferSize => _buf.length;

  StreamController<T> _build() {
    late StreamController<T> c;
    c = StreamController<T>(
      onListen: () { _sub = source.listen(
        (e) {
          if (_buf.length >= bufferSize) {
            switch (strategy) {
              case BackpressureStrategy.buffer: _buf.removeFirst(); _buf.addLast(e);
              case BackpressureStrategy.drop: return;
              case BackpressureStrategy.latest: _buf.clear(); _buf.addLast(e);
              case BackpressureStrategy.error: c.addError(Exception('Buffer overflow')); return;
            }
          } else { _buf.addLast(e); }
          _flush(c);
        },
        onError: c.addError,
        onDone: () { _flush(c); c.close(); },
      ); },
      onPause: () => _sub?.pause(), onResume: () { _sub?.resume(); _flush(c); },
      onCancel: () => _sub?.cancel(),
    );
    return c;
  }

  void _flush(StreamController<T> c) { while (_buf.isNotEmpty && !c.isPaused && !c.isClosed) c.add(_buf.removeFirst()); }
  Future<void> dispose() async { await _sub?.cancel(); if (_out != null && !_out!.isClosed) await _out!.close(); _buf.clear(); }
}

abstract class StreamProcessor<In, Out> {
  String get name;
  int _proc = 0, _err = 0, _drop = 0;
  Future<Out> process(In input);
  Future<Out?> onError(Object error, In input) async => null;

  Stream<Out> bind(Stream<In> source) {
    late StreamController<Out> c; StreamSubscription<In>? sub;
    c = StreamController<Out>(
      onListen: () { sub = source.listen((input) async {
        try { final r = await process(input); _proc++; if (!c.isClosed) c.add(r); }
        catch (e) { _err++; try { final r = await onError(e, input); if (r != null && !c.isClosed) c.add(r); else _drop++; } catch (_) { _drop++; } }
      }, onError: (e) { if (!c.isClosed) c.addError(e); }, onDone: () { if (!c.isClosed) c.close(); }); },
      onCancel: () => sub?.cancel(),
    );
    return c.stream;
  }

  int get processedCount => _proc; int get errorCount => _err; int get droppedCount => _drop;
}

class ProcessingPipeline {
  final List<StreamProcessor> processors; final BackpressureStrategy strategy; final int bufferSize;
  final List<BackpressureController> _bps = [];
  ProcessingPipeline({required this.processors, this.strategy = BackpressureStrategy.buffer, this.bufferSize = 50});

  Stream<dynamic> connect(Stream<dynamic> source) {
    var current = source;
    for (final p in processors) {
      final bp = BackpressureController(source: current, bufferSize: bufferSize, strategy: strategy);
      _bps.add(bp); current = p.bind(bp.stream);
    }
    return current;
  }

  Map<String, Map<String, int>> get metrics => { for (final p in processors) p.name: {'processed': p.processedCount, 'errors': p.errorCount, 'dropped': p.droppedCount} };
  Future<void> dispose() async { for (final bp in _bps) await bp.dispose(); _bps.clear(); }
}

// Concrete processors
class ParseProcessor extends StreamProcessor<String, Map<String, dynamic>> {
  @override String get name => 'parser';
  @override Future<Map<String, dynamic>> process(String input) async => jsonDecode(input) as Map<String, dynamic>;
  @override Future<Map<String, dynamic>?> onError(Object e, String input) async => null; // skip malformed
}

class EnrichProcessor extends StreamProcessor<Map<String, dynamic>, Map<String, dynamic>> {
  @override String get name => 'enricher';
  @override Future<Map<String, dynamic>> process(Map<String, dynamic> input) async {
    final v = (input['value'] as num?)?.toDouble() ?? 0;
    return {...input, 'enriched': true, 'tier': v > 10 ? 'high' : 'low'};
  }
}

class FilterProcessor extends StreamProcessor<Map<String, dynamic>, Map<String, dynamic>> {
  final bool Function(Map<String, dynamic>) predicate;
  FilterProcessor(this.predicate);
  @override String get name => 'filter';
  @override Future<Map<String, dynamic>> process(Map<String, dynamic> input) async { if (!predicate(input)) throw StateError('filtered'); return input; }
  @override Future<Map<String, dynamic>?> onError(Object e, Map<String, dynamic> input) async => (e is StateError) ? null : throw e;
}
```

### Common Mistakes

**Flushing into paused/closed controller**: Always check `!c.isPaused && !c.isClosed` before adding. Otherwise you get StateError.

**Async ordering in bind**: Each `onData` is async, so events may process out of order under load. For strict ordering, queue events internally.

**Forgetting to dispose BackpressureControllers**: Each pipeline stage creates one. Leak them and source subscriptions stay active forever.

### Deep Dive: Backpressure Strategies

The four strategies represent real tradeoffs:
- **buffer (drop oldest)**: Good for time-series. Recent data matters more. Bounded memory.
- **drop**: "Best effort" telemetry. System stays stable, accepts data gaps.
- **latest**: Perfect for UI state (mouse position). Only the newest value matters.
- **error**: When data loss is unacceptable. Fail loudly so the consumer can react (scale up, slow down).

Dart's native backpressure mechanism is pause/resume on StreamSubscription. Our controller bridges the gap by managing a bounded buffer on top of that.

---

## General Debugging Tips

**Unawaited futures**: Enable the `unawaited_futures` lint. If you see "Unhandled exception" with no obvious source, search for Future-returning calls missing `await`.

**Stream leaks**: If your program does not exit, you have an uncancelled StreamSubscription or unclosed ReceivePort. Track every `.listen()` and ensure a matching `.cancel()`.

**Ordering surprises**: `Future.value(x).then(f)` schedules `f` as a microtask. `Future(() => x).then(f)` uses the event queue. This distinction matters when code depends on execution order.

**Isolate serialization**: Not all objects cross the isolate boundary. Closures, native resources, and objects with finalizers cannot be sent. If `Isolate.spawn` throws a confusing error, inspect what you are sending.

**Zone error swallowing**: If errors vanish, check for `runZonedGuarded` that catches but does not rethrow. The runtime considers the error handled, but your app may be corrupted.
