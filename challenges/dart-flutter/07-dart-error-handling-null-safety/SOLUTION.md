# Section 07 Solutions -- Dart Error Handling & Null Safety

## How to Use This Guide

Work through each exercise yourself before reading its solution. The learning happens in the struggle.

For each exercise: progressive hints first, then the full solution, followed by common mistakes and deeper insights. Read one hint, try again, then read the next.

---

## Exercise 1 -- Safe Division Service

### Progressive Hints

**Hint 1**: Each custom exception should carry the operands that caused the failure so the caller can build a meaningful message.

**Hint 2**: Check null first (return `Failure`), then check business rules (throw exceptions). Null input is data absence; division by zero is a constraint violation.

**Hint 3**: When a double overflows, Dart produces `infinity`, not an exception. Check `result.isInfinite` after the division.

### Full Solution

```dart
// exercise_01_solution.dart

class DivisionByZeroException implements Exception {
  final double numerator;
  DivisionByZeroException(this.numerator);

  @override
  String toString() => 'Cannot divide $numerator by zero';
}

class OverflowException implements Exception {
  final double numerator;
  final double denominator;
  final bool isPositive;
  OverflowException({required this.numerator, required this.denominator,
      required this.isPositive});
}

Result<double, String> safeDivide(double? numerator, double? denominator) {
  if (numerator == null) return const Failure('Numerator is required');
  if (denominator == null) return const Failure('Denominator is required');
  if (denominator == 0) throw DivisionByZeroException(numerator);

  final result = numerator / denominator;
  if (result.isInfinite) {
    throw OverflowException(
        numerator: numerator, denominator: denominator, isPositive: result > 0);
  }
  return Success(result);
}

String formatDivision(double? a, double? b) {
  try {
    return safeDivide(a, b).fold(
      onSuccess: (value) => '$a / $b = $value',
      onFailure: (error) => 'Error: $error',
    );
  } on DivisionByZeroException catch (e) {
    return 'Error: Cannot divide ${e.numerator} by zero';
  } on OverflowException {
    return 'Error: Result overflows double range';
  }
}
```

### Common Mistakes

**Catching Exception broadly.** Writing `catch (e)` in `formatDivision` silently swallows future exception types. Always catch specific types and let unknowns propagate.

**Forgetting `double.nan`.** Division `0.0 / 0.0` produces NaN, not infinity. A thorough implementation checks `result.isNaN` as well.

### Deep Dive: Why Mix Result and Exceptions?

This exercise deliberately uses both patterns. Null inputs are data quality issues -- `Failure` is appropriate. Constraint violations (zero, overflow) are shown as exceptions to demonstrate the mechanical difference. In production, pick one pattern per layer and stay consistent.

---

## Exercise 2 -- Null-Safe User Profile Builder

### Progressive Hints

**Hint 1**: `map['key']` returns `dynamic?`. Safely cast with `as String? ?? defaultValue` rather than `!`.

**Hint 2**: Use `??=` for caching computed values. Use `?.` for chained nullable access. Use type promotion (`if (x != null)`) to avoid `?.` when you want to do multiple operations.

### Full Solution

```dart
// exercise_02_solution.dart

class Address {
  final String? street, city, zipCode, country;
  const Address({this.street, this.city, this.zipCode, this.country});
  String format() {
    final parts = [street, city, zipCode, country].whereType<String>();
    return parts.isEmpty ? 'No address' : parts.join(', ');
  }
}

class UserProfile {
  final String name, email;
  final String? bio, avatarUrl;
  final Address? address;
  final Map<String, String>? preferences;
  const UserProfile({required this.name, required this.email,
      this.bio, this.avatarUrl, this.address, this.preferences});
}

class UserProfileBuilder {
  String? _cachedSummary;

  UserProfile fromMap(Map<String, dynamic> data) {
    final name = data['name'] as String? ?? 'Unknown User';
    final email = data['email'] as String? ?? 'no-email@example.com';
    final bio = (data['bio'] as String?)?.trim();                   // ?.
    final rawPrefs = data['preferences'] as Map<String, dynamic>?;
    final theme = rawPrefs?['theme'] as String?;                    // ?[]

    Map<String, String>? prefs;
    if (rawPrefs != null) {
      prefs = rawPrefs.map((k, v) => MapEntry(k, v?.toString() ?? ''));
    }
    prefs ??= {};                                                    // ??=
    prefs['theme'] ??= 'system';

    final addrData = data['address'] as Map<String, dynamic>?;
    final address = addrData != null                                 // promotion
        ? Address(street: addrData['street'] as String?,
                  city: addrData['city'] as String?)
        : null;

    final avatarUrl = data['avatarUrl'] as String?;
    if (avatarUrl != null) {
      print('Avatar has ${avatarUrl.length} chars');                 // promoted
    }

    return UserProfile(name: name, email: email, bio: bio,
        avatarUrl: avatarUrl, address: address, preferences: prefs);
  }

  String summarize(UserProfile? profile) {
    _cachedSummary ??= _buildSummary(profile);                       // ??=
    return _cachedSummary!;                                          // ! (safe: just assigned)
  }

  String _buildSummary(UserProfile? profile) {
    if (profile == null) return 'No profile available';
    return 'Name: ${profile.name}\n'
        'Email: ${profile.email}\n'
        'Bio: ${profile.bio ?? "No bio"}\n'                          // ??
        'Address: ${profile.address?.format() ?? "None"}';           // ?.
  }
}
```

### Common Mistakes

**Using `!` without proof.** `data['name']!` crashes if the key is missing. Always prefer `as String? ?? default`.

**Double space in name formatting.** Concatenating `'$first $middle $last'` with a null middle produces `'Jane  Doe'`. Use a list of non-null parts joined by space.

**Forgetting `?.` changes the return type.** `profile?.name` returns `String?` even though `name` is non-nullable on `UserProfile`.

---

## Exercise 3 -- Domain Error Hierarchy

### Progressive Hints

**Hint 1**: Make the base `OrderException` abstract. Concrete subtypes fill in the contract.

**Hint 2**: Each subtype's constructor takes only domain-specific parameters and computes generic fields internally.

**Hint 3**: Use `on SpecificType catch (e)` cascades, not a single `catch (e)` with `is` checks.

### Full Solution

```dart
// exercise_03_solution.dart

enum ErrorSeverity { warning, error, critical }

abstract class OrderException implements Exception {
  String get code;
  String get userMessage;
  String get debugMessage;
  ErrorSeverity get severity;
  Map<String, dynamic> get context;
}

class PaymentDeclinedException extends OrderException {
  final String paymentMethod, declineReason;
  final bool canRetry;
  PaymentDeclinedException({required this.paymentMethod,
      required this.declineReason, this.canRetry = true});

  @override String get code => 'PAYMENT_DECLINED';
  @override String get userMessage => canRetry
      ? 'Payment declined. Try another method.'
      : 'Payment permanently declined. Contact your bank.';
  @override String get debugMessage => '$paymentMethod declined: $declineReason';
  @override ErrorSeverity get severity =>
      canRetry ? ErrorSeverity.warning : ErrorSeverity.error;
  @override Map<String, dynamic> get context =>
      {'paymentMethod': paymentMethod, 'canRetry': canRetry};
}

class InsufficientInventoryException extends OrderException {
  final String productId;
  final int requested, available;
  InsufficientInventoryException({required this.productId,
      required this.requested, required this.available});

  @override String get code => 'INSUFFICIENT_INVENTORY';
  @override String get userMessage => 'Only $available units available.';
  @override String get debugMessage => '$productId: need $requested, have $available';
  @override ErrorSeverity get severity => ErrorSeverity.warning;
  @override Map<String, dynamic> get context =>
      {'productId': productId, 'requested': requested, 'available': available};
}

// FraudDetectedException and InvalidShippingAddressException follow the same pattern.

// The error handler: specific catches, never swallow unknowns
Map<String, dynamic> handleOrderError(Object error, StackTrace stack) {
  return switch (error) {
    PaymentDeclinedException e => {'code': e.code, 'message': e.userMessage,
        'retry': e.canRetry},
    InsufficientInventoryException e => {'code': e.code,
        'message': e.userMessage, 'retry': false},
    _ => throw error, // unknown errors propagate -- never swallow
  };
}
```

### Common Mistakes

**Making error context mutable.** The `context` map should be unmodifiable. If someone mutates it between throw and catch, debugging info is corrupted.

**Not rethrowing unknown errors.** The `default` branch must propagate, not return a generic response. Swallowing unknowns hides real bugs.

---

## Exercise 4 -- Migrating to Non-Nullable Types

### Progressive Hints

**Hint 1**: For every nullable field ask: "Is null meaningful or just undisciplined?" If it means "not yet configured," use `late` or a required constructor parameter. If it means "genuinely absent," keep `?`.

**Hint 2**: `isConnected` is never unknown -- use `bool` with a default of `false`.

**Hint 3**: `formatUserName` should require `first` and `last` as `String`, accept `middle` as `String?`, and always return `String`.

### Full Solution

```dart
// exercise_04_solution.dart

class UserService {
  late final String dbHost;     // required, set during init()
  late final int dbPort;        // required, set during init()
  late final String apiKey;     // required, set during init()
  bool isConnected = false;     // always has a value
  final List<Map<String, dynamic>> cachedUsers = []; // empty, not null

  void init({required String dbHost, required int dbPort,
      required String apiKey}) {
    this.dbHost = dbHost;
    this.dbPort = dbPort;
    this.apiKey = apiKey;
    isConnected = true;
  }

  // userId is required. Return type is nullable: null means "not found."
  Future<Map<String, dynamic>?> getUser(String userId) async {
    final match = cachedUsers.where((u) => u['id'] == userId);
    return match.isEmpty ? null : match.first;
  }

  // first/last required, middle genuinely optional. Always returns a String.
  String formatUserName(String first, String last, {String? middle}) {
    if (middle != null && middle.isNotEmpty) return '$first $middle $last';
    return '$first $last';
  }

  // Both params required. Returns bool, not bool?. Throws on bad state.
  Future<bool> updateUser(String userId, Map<String, dynamic> updates) async {
    if (!isConnected) throw StateError('Not connected. Call init() first.');
    return true;
  }
}
```

### Deep Dive: late vs nullable vs required

| Strategy | Compile-time safety | Runtime risk | When to use |
|----------|-------------------|--------------|-------------|
| `required` param | Full | None | Value known at construction |
| `late` | Partial | `LateInitializationError` | Value from a lifecycle method |
| Nullable `?` | Full | Null checks everywhere | Genuinely optional |

Prefer required > late > nullable. Each step trades compile-time certainty for flexibility.

---

## Exercise 5 -- Fault-Tolerant Service Layer

### Progressive Hints

**Hint 1**: `RetryPolicy` decides *whether* and *when* to retry. `ResilientClient` executes that decision.

**Hint 2**: Circuit breaker states (closed/open/halfOpen) should be an enum, not nullable booleans. Nullable booleans allow illegal states.

**Hint 3**: Backoff formula: `initialDelay * (multiplier ^ attempt)`. Always cap with a max delay.

### Full Solution (Key Components)

```dart
// exercise_05_solution.dart

class RetryPolicy {
  final int maxAttempts;
  final Duration initialDelay;
  final double backoffMultiplier;
  final Duration maxDelay;
  final bool Function(Object) isRetryable;

  const RetryPolicy({this.maxAttempts = 3,
      this.initialDelay = const Duration(milliseconds: 200),
      this.backoffMultiplier = 2.0,
      this.maxDelay = const Duration(seconds: 10),
      required this.isRetryable});

  Duration delayForAttempt(int attempt) {
    final ms = initialDelay.inMilliseconds *
        _pow(backoffMultiplier, attempt).toInt();
    return Duration(milliseconds: ms.clamp(0, maxDelay.inMilliseconds));
  }

  static double _pow(double base, int exp) =>
      List.filled(exp, base).fold(1.0, (a, b) => a * b);
}

enum CircuitState { closed, open, halfOpen }

class CircuitBreaker {
  final int failureThreshold;
  final Duration resetTimeout;
  CircuitState _state = CircuitState.closed;
  int _failureCount = 0;
  DateTime? _lastFailureTime;

  CircuitBreaker({this.failureThreshold = 5,
      this.resetTimeout = const Duration(seconds: 30)});

  CircuitState get state {
    if (_state == CircuitState.open && _shouldReset()) {
      _state = CircuitState.halfOpen;
    }
    return _state;
  }

  void recordSuccess() { _failureCount = 0; _state = CircuitState.closed; }
  void recordFailure() {
    _failureCount++;
    _lastFailureTime = DateTime.now();
    if (_failureCount >= failureThreshold) _state = CircuitState.open;
  }

  bool _shouldReset() {
    final last = _lastFailureTime;
    return last != null && DateTime.now().difference(last) >= resetTimeout;
  }
}

class ResilientClient {
  final RetryPolicy retryPolicy;
  final CircuitBreaker circuitBreaker;
  ResilientClient({required this.retryPolicy, required this.circuitBreaker});

  Future<T> execute<T>(Future<T> Function() operation) async {
    for (var attempt = 0; attempt < retryPolicy.maxAttempts; attempt++) {
      if (circuitBreaker.state == CircuitState.open) {
        throw StateError('Circuit breaker is open');
      }
      try {
        final result = await operation();
        circuitBreaker.recordSuccess();
        return result;
      } catch (e) {
        circuitBreaker.recordFailure();
        final isLast = attempt == retryPolicy.maxAttempts - 1;
        if (isLast || !retryPolicy.isRetryable(e)) rethrow;
        await Future.delayed(retryPolicy.delayForAttempt(attempt));
      }
    }
    throw StateError('Unreachable');
  }
}
```

### Common Mistakes

**Nullable booleans for state.** `bool? isOpen` and `bool? isHalfOpen` allows both true simultaneously. A single enum makes impossible states unrepresentable.

**No backoff cap.** Attempt 20 without a cap: `200ms * 2^20 = 209 seconds`. Always set a maximum.

**Retrying permanent errors.** The `isRetryable` predicate is essential -- retrying a 404 wastes time.

---

## Exercise 6 -- Null Safety Edge Cases with Generics

### Progressive Hints

**Hint 1**: `Cache<T extends Object>` prevents `T` from being nullable. Cache miss returns null; stored values never are.

**Hint 2**: For `NullableCache`, wrap results in a sealed `CacheEntry` to distinguish "not found" from "stored null."

### Full Solution (Key Components)

```dart
// exercise_06_solution.dart

class Cache<T extends Object> {
  final _store = <String, T>{};
  void put(String key, T value) => _store[key] = value;
  T? get(String key) => _store[key]; // null = not found
}

sealed class CacheEntry<T> { const CacheEntry(); }
class Hit<T> extends CacheEntry<T> { final T value; const Hit(this.value); }
class Miss<T> extends CacheEntry<T> { const Miss(); }

class NullableCache<T> {
  final _store = <String, T>{};
  final _keys = <String>{};
  void put(String key, T value) { _keys.add(key); _store[key] = value; }
  CacheEntry<T> get(String key) =>
      _keys.contains(key) ? Hit(_store[key] as T) : const Miss();
}

T firstNonNull<T extends Object>(List<T?> items) {
  for (final item in items) {
    if (item != null) return item; // promoted to T
  }
  throw StateError('All elements are null');
}

class Lazy<T extends Object> {
  final T Function() _factory;
  late final T value = _factory(); // computed once, cached by late final
}
```

### Deep Dive: Why `T extends Object` Matters

The default upper bound is `Object?`. Without the bound, `Cache<String?>` makes `T?` collapse to `String?` -- you cannot distinguish cache miss from stored null. `T extends Object` constrains to non-nullable types, giving null a single unambiguous meaning.

---

## Exercise 7 -- Complete Result Monad

### Progressive Hints

**Hint 1**: Get `map`, `flatMap`, and `fold` working first. Recovery and async come after.

**Hint 2**: Async variants work best as extensions on `Future<Result<T, E>>`.

**Hint 3**: `collect` iterates results, accumulating successes until the first failure.

### Full Solution (Key Components)

```dart
// exercise_07_solution.dart

sealed class Result<T, E> {
  const Result();

  Result<U, E> map<U>(U Function(T) f) => switch (this) {
    Success(value: final v) => Success(f(v)),
    Failure(error: final e) => Failure(e),
  };

  Result<T, F> mapError<F>(F Function(E) f) => switch (this) {
    Success(value: final v) => Success(v),
    Failure(error: final e) => Failure(f(e)),
  };

  Result<U, E> flatMap<U>(Result<U, E> Function(T) f) => switch (this) {
    Success(value: final v) => f(v),
    Failure(error: final e) => Failure(e),
  };

  R fold<R>({required R Function(T) onSuccess,
      required R Function(E) onFailure}) => switch (this) {
    Success(value: final v) => onSuccess(v),
    Failure(error: final e) => onFailure(e),
  };

  T getOrElse(T Function() fallback) => switch (this) {
    Success(value: final v) => v, Failure() => fallback() };

  Result<T, E> recover(Result<T, E> Function(E) f) => switch (this) {
    Success() => this, Failure(error: final e) => f(e) };

  static Result<List<T>, E> collect<T, E>(List<Result<T, E>> results) {
    final values = <T>[];
    for (final r in results) {
      switch (r) {
        case Success(value: final v): values.add(v);
        case Failure(error: final e): return Failure(e);
      }
    }
    return Success(values);
  }

  static (List<T>, List<E>) partition<T, E>(List<Result<T, E>> results) {
    final ok = <T>[], err = <E>[];
    for (final r in results) {
      switch (r) {
        case Success(value: final v): ok.add(v);
        case Failure(error: final e): err.add(e);
      }
    }
    return (ok, err);
  }
}

class Success<T, E> extends Result<T, E> {
  final T value; const Success(this.value);
}
class Failure<T, E> extends Result<T, E> {
  final E error; const Failure(this.error);
}

extension ResultFuture<T, E> on Future<Result<T, E>> {
  Future<Result<U, E>> mapAsync<U>(U Function(T) f) async => (await this).map(f);
  Future<Result<U, E>> flatMapAsync<U>(
      Future<Result<U, E>> Function(T) f) async => switch (await this) {
    Success(value: final v) => f(v),
    Failure(error: final e) => Failure(e),
  };
}

// Usage: chain four flatMaps -- failure at any step short-circuits
void main() {
  final result = validateEmail('alice@example.com')
      .flatMap(lookupUser)
      .flatMap(extractName)
      .flatMap(formatGreeting);
  print(result); // Success(Hello, Alice!)
}
```

### Common Mistakes

**Using `dynamic` for the error type.** Skipping the `E` parameter breaks `mapError` type safety and removes compiler help when chaining across layers.

**`throw e` vs `rethrow` in `getOrThrow`.** `throw e` creates a new stack trace. If you need the original, store the stack trace in `Failure` alongside the error.

---

## Exercise 8 -- Error Boundary System Across Isolates

### Progressive Hints

**Hint 1**: Isolates cannot share objects. `ErrorReport` must serialize to primitives: `toMap()` and `fromMap()` are not optional.

**Hint 2**: Workers send tagged messages: `{'type': 'error', 'report': report.toMap()}`. The boundary listener parses and routes.

**Hint 3**: Cascade detection: if isolate A and B both error within N seconds with the same category, flag a cascade.

**Hint 4**: Reuse the circuit breaker from Exercise 5, keyed by isolate name.

### Full Solution (Key Components)

```dart
// exercise_08_solution.dart
import 'dart:async';
import 'dart:isolate';

class ErrorReport {
  final String category, message, stackTrace, timestamp, sourceIsolate;
  final Map<String, String> metadata;
  ErrorReport({required this.category, required this.message,
      required this.stackTrace, required this.timestamp,
      required this.sourceIsolate, this.metadata = const {}});

  Map<String, dynamic> toMap() => {'category': category, 'message': message,
      'stackTrace': stackTrace, 'timestamp': timestamp,
      'sourceIsolate': sourceIsolate, 'metadata': metadata};

  factory ErrorReport.fromMap(Map<String, dynamic> m) => ErrorReport(
      category: m['category'], message: m['message'],
      stackTrace: m['stackTrace'], timestamp: m['timestamp'],
      sourceIsolate: m['sourceIsolate'],
      metadata: Map<String, String>.from(m['metadata']));
}

abstract class ErrorTelemetry {
  void onError(ErrorReport report);
  void onRecovery(String isolate, String action, bool ok);
  void onCascade(List<ErrorReport> correlated);
}

class CascadeDetector {
  final Duration window;
  final _recent = <ErrorReport>[];
  CascadeDetector({this.window = const Duration(seconds: 5)});

  List<ErrorReport>? check(ErrorReport report) {
    final now = DateTime.parse(report.timestamp);
    _recent.removeWhere(
        (r) => now.difference(DateTime.parse(r.timestamp)) > window);
    _recent.add(report);
    final correlated = _recent.where((r) =>
        r.sourceIsolate != report.sourceIsolate &&
        r.category == report.category).toList();
    return correlated.isNotEmpty ? [report, ...correlated] : null;
  }
}

class ErrorBoundary {
  final ErrorTelemetry telemetry;
  final CascadeDetector cascadeDetector;
  final _breakers = <String, _IsolateBreaker>{};
  final _isolates = <String, Isolate>{};

  ErrorBoundary({required this.telemetry, CascadeDetector? cascadeDetector})
      : cascadeDetector = cascadeDetector ?? CascadeDetector();

  Future<void> registerIsolate(String name,
      void Function(SendPort) entry) async {
    final port = ReceivePort();
    _breakers[name] = _IsolateBreaker();
    _isolates[name] = await Isolate.spawn(entry, port.sendPort,
        errorsAreFatal: false);
    port.listen((msg) {
      if (msg is Map && msg['type'] == 'error') {
        _handleError(name, ErrorReport.fromMap(msg['report']));
      }
    });
  }

  void _handleError(String name, ErrorReport report) {
    telemetry.onError(report);
    _breakers[name]?.recordFailure();
    final cascade = cascadeDetector.check(report);
    if (cascade != null) telemetry.onCascade(cascade);
  }

  bool isHealthy(String name) => _breakers[name]?.isHealthy ?? false;

  void shutdown() {
    for (final i in _isolates.values) i.kill();
    _isolates.clear();
  }
}

class _IsolateBreaker {
  int failures = 0;
  DateTime? lastFailure;
  bool get isHealthy => failures < 3;
  void recordFailure() { failures++; lastFailure = DateTime.now(); }
  void reset() { failures = 0; }
}

// Worker pattern: wrap all work in try/catch, send structured reports
void workerIsolate(SendPort port) {
  Timer.periodic(Duration(seconds: 2), (_) {
    try {
      if (DateTime.now().second % 3 == 0) throw FormatException('bad data');
      port.send({'type': 'result', 'data': 'ok'});
    } catch (e, stack) {
      port.send({'type': 'error', 'report': ErrorReport(
        category: 'dataCorruption', message: '$e',
        stackTrace: '$stack', timestamp: DateTime.now().toIso8601String(),
        sourceIsolate: 'worker',
      ).toMap()});
    }
  });
}
```

### Common Mistakes

**Sending class instances across isolate boundaries.** Isolates have separate heaps. You must serialize to maps of primitives. Forgetting `toMap()` causes silent message drops.

**Not cleaning up the cascade detector.** Without removing old errors from the window, the list grows unbounded and produces false cascade detections.

**Blocking the main isolate on recovery.** Recovery (especially respawning) should be async and non-blocking. Blocking on it freezes your UI.

### Debugging Tips

If error reports never arrive, verify the worker sends to the correct port and the message format matches the listener's expectations. The most common bug is sending the `ErrorReport` object rather than `toMap()`.

If the cascade detector never fires, check that test errors have timestamps within the correlation window. Simulated errors with stale timestamps fall outside.

### Alternative: Stream-Based Error Processing

For production, wrap `ReceivePort` in a typed stream and use transformers for windowing and throttling:

```dart
Stream<ErrorReport> errorStream(ReceivePort port) => port
    .where((m) => m is Map && m['type'] == 'error')
    .map((m) => ErrorReport.fromMap(m['report']));
```

This scales better when you need backpressure, sampling, or complex event processing.
