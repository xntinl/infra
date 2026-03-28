# Section 07 -- Dart Error Handling & Null Safety

## Introduction

Every program encounters the unexpected. A network call times out, a user submits garbage input, a JSON field you trusted turns out to be missing. The question is never *if* things go wrong but *how your code behaves when they do*.

Dart gives you two powerful, interlocking systems for writing defensive code. The first is a structured exception handling model that distinguishes between recoverable problems (exceptions) and programmer errors (errors). The second is sound null safety -- arguably Dart's single most important language feature -- which eliminates an entire category of runtime crashes at compile time by making the type system track whether a value can be absent.

Together, these systems let you express intent precisely: this function might fail, that variable is never null, this return type carries either a result or an error. When you use them well, your code becomes self-documenting about its failure modes, and the compiler catches mistakes that would otherwise surface as crashes in production.

This section teaches you to think defensively in Dart. You will move from basic try/catch mechanics through null safety's type-level guarantees, and ultimately to functional error handling patterns that make failure a first-class part of your API design.

## Prerequisites

You should be comfortable with:

- **Section 01-03**: Variables, types, functions, closures, control flow, collections
- **Section 04**: Classes, inheritance, abstract classes, mixins, interfaces
- **Section 05**: Futures, async/await, Streams, Isolates
- **Section 06**: Generics, type parameters, bounds, variance concepts

## Learning Objectives

By the end of this section, you will be able to:

1. **Distinguish** between Exception and Error in Dart and **select** the appropriate one for a given failure scenario
2. **Implement** structured try/catch/finally blocks that catch specific exception types without swallowing unexpected errors
3. **Design** custom exception hierarchies that carry context information for debugging
4. **Apply** Dart's null safety system to eliminate null reference errors at compile time
5. **Evaluate** when to use each null-aware operator and **justify** the choice
6. **Analyze** how flow analysis promotes nullable types through null checks
7. **Construct** a typed Result monad that encodes success and failure in the type system
8. **Architect** a layered error handling strategy that spans synchronous code, async operations, and isolate boundaries

---

## Core Concepts

### 1. Exception vs Error: Know the Difference

Dart's hierarchy has two branches under `Object` that represent problems: `Exception` and `Error`. They exist for fundamentally different reasons, and confusing them leads to code that either crashes too easily or hides real bugs.

**Exceptions** represent anticipated failures -- conditions that a well-written program should handle gracefully. A file not found, a network timeout, a malformed response. These are problems in the environment, not in your logic.

**Errors** represent programmer mistakes -- bugs. An index out of bounds, a null dereference, a type cast that should never fail. You generally should not catch these; you should fix the code that causes them.

```dart
// exception_vs_error.dart

// Exceptions: anticipated, recoverable conditions
class InsufficientFundsException implements Exception {
  final double requested;
  final double available;

  InsufficientFundsException({required this.requested, required this.available});

  @override
  String toString() =>
      'InsufficientFundsException: requested \$$requested '
      'but only \$$available available';
}

// Errors: programmer mistakes, should not be caught in normal flow
void demonstrateError() {
  final list = [1, 2, 3];
  // This is a RangeError -- a bug in your code, not a runtime condition
  // print(list[10]); // RangeError (index): Invalid value: Not in range 0..2
}

// The critical distinction in practice
double withdraw(double amount, double balance) {
  // This is a bug guard -- use Error (or assert)
  if (amount < 0) {
    throw ArgumentError.value(amount, 'amount', 'must be non-negative');
  }

  // This is a business rule -- use Exception
  if (amount > balance) {
    throw InsufficientFundsException(requested: amount, available: balance);
  }

  return balance - amount;
}
```

The rule of thumb: if the caller can reasonably be expected to handle it, throw an Exception. If it indicates a bug that should be fixed in code, throw an Error.

### 2. try/catch/finally: Structured Recovery

Dart's try/catch supports catching specific types with the `on` keyword, which prevents the dangerous habit of catching everything. The `finally` block runs regardless of whether an exception occurred -- essential for cleanup.

```dart
// try_catch_finally.dart
import 'dart:io';

class ApiException implements Exception {
  final int statusCode;
  final String message;
  ApiException(this.statusCode, this.message);

  @override
  String toString() => 'ApiException($statusCode): $message';
}

class TimeoutException implements Exception {
  final Duration duration;
  TimeoutException(this.duration);

  @override
  String toString() => 'TimeoutException: exceeded ${duration.inSeconds}s';
}

Future<String> fetchData(String url) async {
  HttpClient? client;
  try {
    client = HttpClient();
    client.connectionTimeout = const Duration(seconds: 5);
    final request = await client.getUrl(Uri.parse(url));
    final response = await request.close();

    if (response.statusCode != 200) {
      throw ApiException(response.statusCode, 'Failed to fetch $url');
    }

    return await response.transform(const SystemEncoding().decoder).join();
  } on SocketException catch (e) {
    // Network-level failure: no connectivity, DNS resolution failed
    throw TimeoutException(const Duration(seconds: 5));
  } on ApiException {
    // Re-throw without modification -- caller needs to handle this
    rethrow;
  } on FormatException catch (e, stackTrace) {
    // Catch with stack trace for debugging malformed URLs
    print('Malformed URL: $e');
    print('Stack trace: $stackTrace');
    throw ApiException(400, 'Invalid URL format');
  } finally {
    // Always runs: clean up the client whether we succeeded or not
    client?.close();
  }
}
```

Three important details here. First, `on TypeName catch (e, stackTrace)` gives you both the exception and the stack trace -- always capture the stack trace when logging. Second, `rethrow` preserves the original stack trace, while `throw e` resets it -- prefer `rethrow` when you are not transforming the exception. Third, `finally` runs even if you rethrow, which makes it the right place for resource cleanup.

### 3. Custom Exceptions with Context

A bare string exception tells you *what* went wrong. A well-designed custom exception tells you *what*, *where*, *why*, and *what to do about it*.

```dart
// custom_exceptions.dart

enum ErrorSeverity { warning, error, critical }

abstract class AppException implements Exception {
  String get code;
  String get userMessage;
  String get debugMessage;
  ErrorSeverity get severity;
  Map<String, dynamic> get context;
  DateTime get timestamp;

  @override
  String toString() => '[$code] $debugMessage | context: $context';
}

class ValidationException extends AppException {
  @override
  final String code;
  @override
  final String userMessage;
  @override
  final String debugMessage;
  @override
  final ErrorSeverity severity;
  @override
  final Map<String, dynamic> context;
  @override
  final DateTime timestamp;

  final Map<String, List<String>> fieldErrors;

  ValidationException({
    required this.fieldErrors,
    this.code = 'VALIDATION_ERROR',
    this.userMessage = 'Please correct the highlighted fields.',
    String? debugMessage,
    this.severity = ErrorSeverity.warning,
    Map<String, dynamic>? context,
  })  : debugMessage = debugMessage ?? 'Validation failed: $fieldErrors',
        context = context ?? {},
        timestamp = DateTime.now();

  bool hasErrorFor(String field) => fieldErrors.containsKey(field);
  List<String> errorsFor(String field) => fieldErrors[field] ?? [];
}

class NetworkException extends AppException {
  @override
  final String code;
  @override
  final String userMessage;
  @override
  final String debugMessage;
  @override
  final ErrorSeverity severity;
  @override
  final Map<String, dynamic> context;
  @override
  final DateTime timestamp;

  final int? statusCode;
  final String? endpoint;

  NetworkException({
    required this.endpoint,
    this.statusCode,
    this.code = 'NETWORK_ERROR',
    this.userMessage = 'Connection problem. Please try again.',
    String? debugMessage,
    this.severity = ErrorSeverity.error,
  })  : debugMessage = debugMessage ?? 'HTTP $statusCode from $endpoint',
        context = {'endpoint': endpoint, 'statusCode': statusCode},
        timestamp = DateTime.now();

  bool get isServerError => (statusCode ?? 0) >= 500;
  bool get isClientError =>
      (statusCode ?? 0) >= 400 && (statusCode ?? 0) < 500;
}
```

### 4. Sound Null Safety: The Type System as Guardian

Before null safety, any variable in Dart could be null at any time. This meant every field access was a potential crash site, and the only defense was manual null checks scattered throughout your code.

Sound null safety flips the default: types are non-nullable unless you explicitly opt in. The compiler then enforces this everywhere -- you cannot assign null to a `String`, and you cannot call methods on a `String?` without first proving it is not null.

```dart
// null_safety_basics.dart

// Non-nullable: the compiler guarantees these are never null
String greeting = 'hello';
int count = 0;
List<String> names = [];

// Nullable: the ? suffix explicitly marks "this might be absent"
String? middleName;
int? cachedResult;
List<String>? previousResults;

// The compiler prevents mistakes at the call site
void demonstrateCompilerProtection() {
  String? name = fetchName();

  // COMPILE ERROR: property 'length' can't be accessed on 'String?'
  // print(name.length);

  // Option 1: null check with promotion
  if (name != null) {
    // Inside this block, name is promoted to String (non-nullable)
    print(name.length); // safe
  }

  // Option 2: null-aware access
  print(name?.length); // returns int? -- null if name is null

  // Option 3: default value
  print(name?.length ?? 0); // returns int -- 0 if name is null

  // Option 4: assert non-null (use ONLY when you have proof)
  // print(name!.length); // throws if null -- avoid unless certain
}

String? fetchName() => null; // simulates an absent value
```

### 5. Null-Aware Operators: The Complete Toolkit

Dart provides a family of operators that make null handling concise without sacrificing clarity. Each one serves a specific purpose.

```dart
// null_aware_operators.dart

class UserProfile {
  final String? displayName;
  final Address? address;
  final List<String>? tags;
  final Map<String, String>? metadata;

  UserProfile({this.displayName, this.address, this.tags, this.metadata});
}

class Address {
  final String? city;
  final String? zipCode;
  Address({this.city, this.zipCode});
}

void demonstrateOperators(UserProfile? user) {
  // ?. -- null-aware member access: "call this method IF not null"
  final nameLength = user?.displayName?.length;
  // type: int? -- null if user is null OR displayName is null

  // ?[] -- null-aware subscript: "index into this IF not null"
  final firstTag = user?.tags?[0];
  // type: String? -- null if user, tags, or index is out of range

  // ?? -- null coalescing: "use this value, OR fall back to that"
  final displayName = user?.displayName ?? 'Anonymous';
  // type: String -- guaranteed non-null

  // ??= -- null-aware assignment: "assign only if currently null"
  String? cachedCity;
  cachedCity ??= user?.address?.city;
  // assigns only on the first call when cachedCity is null

  // ! -- null assertion: "I guarantee this is not null"
  // USE WITH EXTREME CAUTION -- this is a runtime crash waiting to happen
  // Only use when you have PROOF: a preceding null check, a known state
  if (user?.displayName != null) {
    final confirmedName = user!.displayName!; // safe here, proven above
  }

  // Chaining for deep access
  final metaValue = user?.metadata?['theme'] ?? 'light';
}
```

### 6. Late Variables and Type Promotion

The `late` keyword tells the compiler "I will initialize this before it is used, but not right now." It is useful for dependency injection and expensive initialization, but it trades compile-time safety for a runtime check.

```dart
// late_and_promotion.dart

class DatabaseService {
  // late: initialized after construction, before first use
  late final String connectionString;
  late final int poolSize;

  // If you access these before init(), you get LateInitializationError
  void init(Map<String, dynamic> config) {
    connectionString = config['connectionString'] as String;
    poolSize = config['poolSize'] as int? ?? 5;
  }

  // late final: can only be assigned once -- immutable after initialization
  // Attempting a second assignment throws a LateInitializationError
}

// Flow analysis and type promotion
void processValue(Object? input) {
  // The compiler tracks null checks and type tests through your control flow

  if (input == null) {
    print('No input provided');
    return; // early return means input is non-null below
  }

  // input is promoted to Object (non-nullable) here
  print('Got value: $input');

  if (input is String) {
    // input is promoted to String here
    print('String of length ${input.length}');
  } else if (input is List) {
    // input is promoted to List here
    print('List with ${input.length} elements');
  }

  // Promotion does NOT work with instance fields (they could change between
  // the check and the use via another thread or setter). Use a local variable:
  // final localCopy = someObject.nullableField;
  // if (localCopy != null) { /* localCopy is promoted */ }
}
```

### 7. The Result Pattern: Errors as Values

Exceptions are invisible in a function's type signature. A function that returns `User` might also throw three different exceptions, but the type system does not tell you that. The Result pattern makes failure explicit by encoding it in the return type.

```dart
// result_pattern.dart

sealed class Result<T, E> {
  const Result();

  bool get isSuccess => this is Success<T, E>;
  bool get isFailure => this is Failure<T, E>;

  T? get valueOrNull => switch (this) {
    Success(value: final v) => v,
    Failure() => null,
  };

  R fold<R>({
    required R Function(T value) onSuccess,
    required R Function(E error) onFailure,
  }) =>
      switch (this) {
        Success(value: final v) => onSuccess(v),
        Failure(error: final e) => onFailure(e),
      };

  Result<U, E> map<U>(U Function(T value) transform) => switch (this) {
    Success(value: final v) => Success(transform(v)),
    Failure(error: final e) => Failure(e),
  };

  Result<U, E> flatMap<U>(Result<U, E> Function(T value) transform) =>
      switch (this) {
        Success(value: final v) => transform(v),
        Failure(error: final e) => Failure(e),
      };
}

class Success<T, E> extends Result<T, E> {
  final T value;
  const Success(this.value);
}

class Failure<T, E> extends Result<T, E> {
  final E error;
  const Failure(this.error);
}

// Usage: the return type tells the whole story
Result<User, AuthError> authenticate(String email, String password) {
  if (email.isEmpty) {
    return Failure(AuthError.invalidEmail);
  }
  if (password.length < 8) {
    return Failure(AuthError.weakPassword);
  }
  // ... actual auth logic
  return Success(User(email: email, name: 'Test'));
}

enum AuthError { invalidEmail, weakPassword, invalidCredentials, accountLocked }

class User {
  final String email;
  final String name;
  User({required this.email, required this.name});
}
```

### 8. Zone Error Handling

Zones let you catch errors that escape normal try/catch -- uncaught async errors that would otherwise crash your application. They are Dart's mechanism for establishing error boundaries around entire regions of code.

```dart
// zone_error_handling.dart
import 'dart:async';

void runWithErrorBoundary() {
  runZonedGuarded(
    () {
      // All code in this zone has a safety net
      Future.delayed(Duration(seconds: 1), () {
        throw StateError('Uncaught async error');
      });

      // Synchronous code is also covered
      Timer.run(() {
        throw FormatException('Bad data in timer callback');
      });
    },
    (error, stackTrace) {
      // This handler catches ANY uncaught error in the zone
      print('Zone caught: $error');
      print('Stack: $stackTrace');
      // Log to telemetry, show a generic error UI, etc.
    },
  );
}
```

---

## Exercises

### Exercise 1 -- Safe Division Service (Basic)

Build a division service that handles every edge case through exceptions and null safety.

**Why this matters**: This exercise trains the fundamental mechanics of throwing, catching, and typing nullability. Every real application performs operations that can fail for multiple distinct reasons, and you need to communicate each reason clearly.

**Instructions**:

1. Create custom exceptions `DivisionByZeroException` and `OverflowException` that implement `Exception` and carry context (the operands that caused the failure)
2. Write a function `Result<double, String> safeDivide(double? numerator, double? denominator)` that:
   - Returns `Failure` with a descriptive message if either argument is null
   - Throws `DivisionByZeroException` when the denominator is zero
   - Throws `OverflowException` when the result exceeds `1e308`
   - Returns `Success` with the result otherwise
3. Write a wrapper function `String formatDivision(double? a, double? b)` that calls `safeDivide`, catches all exceptions, and returns a human-readable string for every case

**Starter code**:

```dart
// exercise_01_safe_division.dart

class DivisionByZeroException implements Exception {
  // TODO: add fields for numerator, include toString()
}

class OverflowException implements Exception {
  // TODO: add fields for both operands and the direction (positive/negative)
}

Result<double, String> safeDivide(double? numerator, double? denominator) {
  // TODO: implement with null checks, business rule checks, and proper throws
}

String formatDivision(double? a, double? b) {
  // TODO: call safeDivide, handle both Result failures and thrown exceptions
}

void main() {
  print(formatDivision(10, 3));      // "10.0 / 3.0 = 3.3333..."
  print(formatDivision(10, 0));      // "Error: Cannot divide 10.0 by zero"
  print(formatDivision(null, 5));    // "Error: Numerator is required"
  print(formatDivision(1e308, 0.1)); // "Error: Result overflows double range"
}
```

**Verification**: Run `dart run exercise_01_safe_division.dart`. All four main() calls should produce the expected output strings. No unhandled exceptions should escape to the console.

---

### Exercise 2 -- Null-Safe User Profile Builder (Basic)

Build a user profile system that processes partially-complete data using every null-aware operator.

**Why this matters**: Real-world data is messy. API responses have missing fields, user forms are half-filled, cached data may be stale or absent. Null safety forces you to handle every absence explicitly, and the null-aware operators keep the code readable.

**Instructions**:

1. Define a `UserProfile` class with a mix of required and optional fields: `name` (required), `email` (required), `bio` (nullable), `avatarUrl` (nullable), `address` (nullable `Address`), `preferences` (nullable `Map<String, String>`)
2. Write a `UserProfileBuilder` that constructs profiles from a `Map<String, dynamic>` (simulating JSON). Use every null-aware operator at least once: `?.`, `?[]`, `??`, `??=`, and `!` (with a preceding safety check)
3. Write a `String summarize(UserProfile? profile)` function that produces a formatted summary, gracefully handling null at every level
4. Include at least one example where type promotion through `if (x != null)` replaces the need for `?.`

**Verification**: Create profiles from complete data, partially missing data, and entirely null input. Confirm no runtime null errors occur and all summaries read naturally.

---

### Exercise 3 -- Domain Error Hierarchy for E-Commerce (Intermediate)

Design a complete exception hierarchy for an e-commerce order processing system.

**Why this matters**: In a real domain, errors are not flat. A payment failure is different from an inventory shortage, which is different from a shipping address validation error. A well-designed hierarchy lets different layers of the application handle different categories of errors without knowing about types they do not care about.

**Instructions**:

1. Design a base `OrderException` with: error code, user-facing message, debug message, severity level, contextual data map, timestamp
2. Create at least four concrete subtypes: `PaymentDeclinedException`, `InsufficientInventoryException`, `InvalidShippingAddressException`, `FraudDetectedException`
3. Each subtype carries domain-specific context (e.g., `PaymentDeclinedException` carries the payment method, decline reason, and whether retry is possible)
4. Write an `OrderProcessor` class with a `processOrder` method that can throw any of these exceptions depending on the input
5. Write an `OrderErrorHandler` that catches each type, logs appropriately, and returns a structured error response

**Verification**: Process orders that trigger each exception type. Verify that the error handler produces distinct, informative responses for each. Verify that unexpected exceptions (those not in your hierarchy) propagate rather than being silently swallowed.

---

### Exercise 4 -- Migrating to Non-Nullable Types (Intermediate)

You are given a pre-null-safety codebase (simulated with nullable types everywhere). Migrate it to idiomatic null safety.

**Why this matters**: Most professional Dart work involves existing codebases. Understanding how to tighten nullability -- deciding what should be nullable vs non-nullable, where to add assertions vs defaults -- is a daily skill.

**Instructions**:

1. Start with the provided "legacy" code where every field and parameter is nullable
2. Analyze each field and parameter: determine whether null is a meaningful value or just a lack of discipline
3. Convert to non-nullable where appropriate, using: required parameters, default values, late initialization, and assertion
4. Document every change with a comment explaining why you chose nullable vs non-nullable
5. Ensure the migrated code compiles with no warnings

**Starter code**:

```dart
// exercise_04_migration_before.dart

class LegacyUserService {
  String? dbHost;
  int? dbPort;
  String? apiKey;
  bool? isConnected;

  List<Map<String, dynamic>>? cachedUsers;

  Future<Map<String, dynamic>?> getUser(String? userId) async {
    if (userId == null) return null;
    // simulated lookup
    return cachedUsers?.firstWhere(
      (u) => u['id'] == userId,
      orElse: () => <String, dynamic>{},
    );
  }

  String? formatUserName(String? first, String? last, String? middle) {
    if (first == null && last == null) return null;
    return '${first ?? ""} ${middle ?? ""} ${last ?? ""}'.trim();
  }

  Future<bool?> updateUser(String? userId, Map<String, dynamic>? updates) async {
    if (userId == null || updates == null) return null;
    return true;
  }
}
```

**Verification**: The migrated code should compile cleanly. Calling `getUser` with a valid ID should return a non-nullable type (or a `Result`). Calling `formatUserName('Jane', 'Doe', null)` should return `'Jane Doe'`, not `'Jane  Doe'` (no double space).

---

### Exercise 5 -- Fault-Tolerant Service Layer (Advanced)

Build a service layer with retry logic, circuit breakers, and structured error reporting.

**Why this matters**: In distributed systems, failures are not exceptional -- they are expected. A single HTTP call might fail because the network blipped, the server is overloaded, or the service is down entirely. Your code needs strategies: retry transient failures, back off exponentially, stop trying when a service is clearly down (circuit breaker), and report what happened in a structured way.

**Instructions**:

1. Implement a `RetryPolicy` class configurable with: max attempts, initial delay, backoff multiplier, and a predicate for which exceptions are retryable
2. Implement a `CircuitBreaker` with three states: Closed (normal), Open (failing fast), Half-Open (testing recovery). Track failure counts and timestamps
3. Implement a `ResilientClient` that combines both: attempts the operation with retries, trips the circuit breaker on repeated failures, and produces a structured `ServiceCallReport` with timing, attempts, and outcome
4. All error types must use your own exception hierarchy (not bare strings)
5. The circuit breaker must be non-nullable in its state tracking -- use an enum and sealed classes, not nullable fields

**Verification**: Simulate a service that fails intermittently. Verify that: retries succeed on transient failures, the circuit breaker opens after threshold failures, the circuit breaker transitions to half-open after the timeout, and all reports contain accurate timing data.

---

### Exercise 6 -- Null Safety Edge Cases with Generics (Advanced)

Explore the tricky intersections of null safety and the generic type system.

**Why this matters**: Generics with null safety introduce subtle questions. Is `T` nullable? What if `T` itself is `String?` -- then `T?` is `String?` too, but `T` is already nullable. Understanding these edges prevents confusing bugs in library code.

**Instructions**:

1. Implement a `Cache<T extends Object>` where keys are strings and values are guaranteed non-null. Add a `T? get(String key)` method that returns null for cache misses (not the same as storing null)
2. Implement a `NullableCache<T>` where `T` can be nullable, and you must distinguish between "key not found" and "key maps to null." Return a `CacheEntry<T>` sealed class with `Hit<T>` and `Miss` subtypes
3. Write a generic function `T firstNonNull<T extends Object>(List<T?> items)` that returns the first non-null element or throws
4. Write a `Lazy<T extends Object>` class using `late` that computes a value on first access and caches it, with proper typing so the computed value is guaranteed non-null
5. Demonstrate how type promotion works (and does not work) with generic types in at least two scenarios

**Verification**: Write tests that exercise: cache hit, cache miss, storing null in `NullableCache`, `firstNonNull` with all nulls (should throw), `Lazy` accessing the value twice (should compute only once), and a generic type promotion scenario.

---

### Exercise 7 -- Complete Result Monad (Insane)

Implement a production-grade `Result<T, E>` monad with the full functional toolkit and async support.

**Why this matters**: The Result pattern from the Core Concepts section is a sketch. A production version needs composability: chaining operations that each might fail, transforming errors between layers, running async operations that return Results, and recovering from specific error types. This is how libraries like `dartz` and `fpdart` work under the hood.

**Instructions**:

1. Implement `Result<T, E>` as a sealed class with `Success<T, E>` and `Failure<T, E>` subtypes
2. Core operations:
   - `map<U>(U Function(T) f)` -- transform the success value
   - `mapError<F>(F Function(E) f)` -- transform the error value
   - `flatMap<U>(Result<U, E> Function(T) f)` -- chain dependent operations
   - `fold<R>(R Function(T), R Function(E))` -- eliminate the Result into a single type
   - `getOrElse(T Function() fallback)` -- unwrap with a default
   - `getOrThrow()` -- unwrap or throw the error
3. Recovery operations:
   - `recover(Result<T, E> Function(E) f)` -- attempt to recover from failure
   - `recoverWith(T Function(E) f)` -- recover with a direct value
4. Async support:
   - `Future<Result<T, E>> mapAsync<T, E>(Future<T> Function(T) f)` as an extension on `Future<Result<T, E>>`
   - `Future<Result<U, E>> flatMapAsync<U>(Future<Result<U, E>> Function(T) f)`
   - A static `Result.fromFuture<T>(Future<T> future)` that catches exceptions and returns `Failure`
5. Collector:
   - `Result<List<T>, E> collect(List<Result<T, E>> results)` -- succeeds only if all succeed, fails with the first error
   - `(List<T>, List<E>) partition(List<Result<T, E>> results)` -- separates successes and failures
6. All operations must preserve type safety. No `dynamic`, no casts.

**Verification**: Write a chain of at least four `flatMap` calls representing a multi-step process (validate input, fetch data, transform, persist). Verify that failure at any step short-circuits the chain. Test `collect` with mixed results. Test async variants with delayed futures. Demonstrate `recover` turning a failure into a success.

---

### Exercise 8 -- Error Boundary System Across Isolates (Insane)

Design and implement an error handling framework that catches, categorizes, reports, and recovers from errors spanning multiple isolates.

**Why this matters**: In a Flutter application, you might have isolates for image processing, data parsing, and network calls. When an isolate fails, the main isolate needs to know what happened, decide whether to retry on a new isolate, degrade gracefully, or alert the user. This is the hardest error handling problem in Dart because isolate communication is message-based and errors do not automatically propagate.

**Instructions**:

1. Define an `ErrorCategory` enum: `transient`, `permanent`, `resourceExhaustion`, `securityViolation`, `dataCorruption`
2. Define a serializable `ErrorReport` class that can cross isolate boundaries (only primitive types and simple collections): category, message, stack trace as string, timestamp, source isolate name, metadata map, severity, suggested recovery action
3. Implement an `ErrorBoundary` class for the main isolate that:
   - Maintains a registry of active isolates
   - Receives error reports via `ReceivePort`
   - Categorizes errors using configurable rules
   - Tracks error rates per isolate (for circuit-breaking)
   - Supports pluggable `RecoveryStrategy` implementations: retry on a new isolate, degrade (disable the feature), escalate (notify the user), ignore
4. Implement at least two worker isolate functions that perform work and send structured error reports back when they fail (not just crashing silently)
5. Implement `CascadeDetector` that identifies when errors in one isolate correlate with errors in another (e.g., database isolate failing causes data processing isolate to fail)
6. Implement telemetry hooks: an `ErrorTelemetry` abstract class with `onError`, `onRecovery`, `onCascade` callbacks that an application can implement to send data to a monitoring service

**Verification**: Spawn at least three isolates. Inject failures into two of them. Verify that: error reports arrive at the boundary with correct metadata, the circuit breaker trips after repeated failures in one isolate, the cascade detector identifies correlated failures, recovery strategies execute (e.g., a new isolate is spawned to replace a crashed one), and the telemetry hooks fire for each event.

---

## Summary

This section covered two fundamental pillars of robust Dart code:

**Error handling** gives you the tools to anticipate and respond to failure. You learned the critical distinction between Exception (recoverable conditions) and Error (programmer bugs), how to structure try/catch/finally for precise recovery, and how to design custom exception hierarchies that carry the context needed for debugging. The Result pattern takes this further by making failure visible in the type system rather than hiding it behind thrown exceptions.

**Null safety** eliminates the billion-dollar mistake at compile time. By making non-nullable the default, Dart forces you to think about absence explicitly. The null-aware operators (`?.`, `??`, `??=`, `!`) provide concise syntax for common patterns, and flow analysis with type promotion means the compiler gets smarter as your null checks get more precise.

The intersection of these systems is where professional Dart code lives: using `Result<T, E>` with non-nullable generics, catching specific exception types rather than broad `catch(e)` blocks, and designing APIs where the types tell the complete story about what can go wrong.

## What's Next

**Section 08 -- Dart Advanced** builds on these foundations with more advanced language features: extensions, extension types, metaprogramming with annotations, advanced pattern matching beyond what we used here, and records. The error handling and null safety patterns from this section will appear throughout as we explore more expressive Dart code.

## References

- [Dart Language Tour: Exceptions](https://dart.dev/language/error-handling)
- [Sound Null Safety](https://dart.dev/null-safety)
- [Understanding Null Safety (Bob Nystrom)](https://dart.dev/null-safety/understanding-null-safety)
- [Effective Dart: Usage -- Exceptions](https://dart.dev/effective-dart/usage#exceptions)
- [Dart API: Exception class](https://api.dart.dev/stable/dart-core/Exception-class.html)
- [Dart API: Error class](https://api.dart.dev/stable/dart-core/Error-class.html)
- [Zones and Error Handling](https://dart.dev/articles/archive/zones)
- [fpdart package](https://pub.dev/packages/fpdart) -- functional programming in Dart, including Either/Result types
