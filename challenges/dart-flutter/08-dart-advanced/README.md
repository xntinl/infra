# Section 08 -- Dart Advanced: Metaprogramming, Code Generation, FFI and Zones

## Introduction

You have spent seven sections mastering Dart itself. Now it is time to push the language to its limits -- into the machinery that powers production frameworks like Flutter, Riverpod, and Freezed. Annotations let you attach intent to code. Code generation reads that intent and writes implementation. FFI reaches outside the Dart runtime into C libraries. Zones give you ambient control over async execution. Together, these tools make Dart write Dart for you.

## Prerequisites

- Sections 01 through 07 completed
- Dart SDK 3.0+ with `dart` and `dart pub` on your PATH
- A C compiler (gcc, clang, or MSVC) for FFI exercises
- VS Code with Dart extension recommended

## Learning Objectives

1. **Analyze** how annotations drive code generation pipelines in production Dart packages
2. **Create** custom code generators using `build_runner` and `source_gen`
3. **Evaluate** when to use code generation versus runtime reflection versus manual implementation
4. **Create** FFI bindings that call C functions with proper memory management
5. **Analyze** how Zones control asynchronous execution, error handling, and context propagation
6. **Evaluate** compilation targets (JIT, AOT, JS) and choose the right one for deployment
7. **Create** publishable packages with custom lint rules and semantic versioning

## Core Concepts

### 1. Annotations and Metadata

Annotations are plain constant values attached to declarations. In AOT-compiled Dart, mirrors are gone -- but build-time tools can read them. This is the foundation of every code generation system.

```dart
// file: annotations_basics.dart
class ApiEndpoint {
  final String path;
  final String method;
  const ApiEndpoint(this.path, {this.method = 'GET'});
}

class Validate {
  final int minLength;
  final int maxLength;
  const Validate({this.minLength = 0, this.maxLength = 255});
}

@ApiEndpoint('/users', method: 'POST')
class CreateUserRequest {
  @Validate(minLength: 3, maxLength: 50)
  final String name;

  @Validate(minLength: 5, maxLength: 100)
  final String email;

  const CreateUserRequest({required this.name, required this.email});
}

void main() {
  final req = CreateUserRequest(name: 'Alice', email: 'alice@example.com');
  print('Created request for: ${req.name}');
  // Annotations are inert at runtime. Code generators read them at build time.
}
```

### 2. Code Generation with build_runner

The `build_runner` package watches source files, finds annotations, and runs generators that produce `.g.dart` files. Understanding how it works from the inside separates framework users from framework authors.

```dart
// file: json_serializable_example.dart
import 'package:json_annotation/json_annotation.dart';
part 'json_serializable_example.g.dart';

@JsonSerializable(explicitToJson: true)
class User {
  final String id;
  final String name;
  @JsonKey(name: 'email_address')
  final String email;
  @JsonKey(defaultValue: false)
  final bool isActive;

  User({required this.id, required this.name, required this.email, this.isActive = false});

  factory User.fromJson(Map<String, dynamic> json) => _$UserFromJson(json);
  Map<String, dynamic> toJson() => _$UserToJson(this);
}
// Run: dart run build_runner build --delete-conflicting-outputs
```

```dart
// file: freezed_example.dart
import 'package:freezed_annotation/freezed_annotation.dart';
part 'freezed_example.freezed.dart';

@freezed
class AuthState with _$AuthState {
  const factory AuthState.initial() = _Initial;
  const factory AuthState.loading() = _Loading;
  const factory AuthState.authenticated(String userName) = _Authenticated;
  const factory AuthState.error(String message) = _Error;
}

void handleAuth(AuthState state) {
  final msg = state.when(
    initial: () => 'Welcome',
    loading: () => 'Signing in...',
    authenticated: (name) => 'Hello, $name',
    error: (msg) => 'Error: $msg',
  );
  print(msg);
}
```

### 3. Writing a Custom Generator

```dart
// file: custom_generator.dart
import 'package:analyzer/dart/element/element.dart';
import 'package:build/build.dart';
import 'package:source_gen/source_gen.dart';

class AutoToString { const AutoToString(); }

class AutoToStringGenerator extends GeneratorForAnnotation<AutoToString> {
  @override
  String generateForAnnotatedElement(Element element, ConstantReader annotation, BuildStep buildStep) {
    if (element is! ClassElement) {
      throw InvalidGenerationSourceError('@AutoToString can only be applied to classes.', element: element);
    }
    final className = element.name;
    final fields = element.fields.where((f) => !f.isStatic && !f.isPrivate);
    final fieldStrings = fields.map((f) => '${f.name}: \${${f.name}}').join(', ');

    return '''
extension ${className}ToString on $className {
  String toDebugString() => '$className($fieldStrings)';
}
''';
  }
}

Builder autoToStringBuilder(BuilderOptions options) =>
    SharedPartBuilder([AutoToStringGenerator()], 'auto_to_string');
```

### 4. dart:ffi -- Calling C from Dart

FFI lets Dart call C libraries directly. This is how packages like `sqlite3` and platform integrations work. The critical challenge is memory management -- C has no garbage collector.

```dart
// file: ffi_basics.dart
import 'dart:ffi';
import 'package:ffi/ffi.dart';

typedef AddNative = Int32 Function(Int32 a, Int32 b);
typedef AddDart = int Function(int a, int b);

typedef GreetNative = Pointer<Utf8> Function(Pointer<Utf8> name);
typedef GreetDart = Pointer<Utf8> Function(Pointer<Utf8> name);

void main() {
  final dylib = DynamicLibrary.open('libmath.dylib');
  final add = dylib.lookupFunction<AddNative, AddDart>('add');

  print('3 + 7 = ${add(3, 7)}');

  final greet = dylib.lookupFunction<GreetNative, GreetDart>('greet');
  final namePtr = 'World'.toNativeUtf8();
  final resultPtr = greet(namePtr);
  print(resultPtr.toDartString());

  calloc.free(namePtr);  // Free what Dart allocated.
  // C-allocated memory must be freed by C's free function.
}
```

```dart
// file: ffi_structs.dart
import 'dart:ffi';
import 'package:ffi/ffi.dart';

final class Point extends Struct {
  @Double() external double x;
  @Double() external double y;
}

typedef DistanceNative = Double Function(Pointer<Point> a, Pointer<Point> b);
typedef DistanceDart = double Function(Pointer<Point> a, Pointer<Point> b);

void main() {
  final dylib = DynamicLibrary.open('libgeometry.dylib');
  final distance = dylib.lookupFunction<DistanceNative, DistanceDart>('distance');

  final a = calloc<Point>()..ref.x = 0.0..ref.y = 0.0;
  final b = calloc<Point>()..ref.x = 3.0..ref.y = 4.0;

  print('Distance: ${distance(a, b)}');  // 5.0
  calloc.free(a);
  calloc.free(b);
}
```

### 5. Zones

Zones create execution contexts that wrap synchronous and asynchronous code. They catch uncaught async errors, carry ambient data (like request IDs) without parameter threading, and intercept operations like `print` and `Timer`.

```dart
// file: zones_basics.dart
import 'dart:async';

void main() {
  // Zone-local values: visible to all code in the zone without passing parameters.
  runZoned(() {
    print('Request ID: ${Zone.current[#requestId]}');
    Future.delayed(Duration(milliseconds: 100), () {
      print('Async still has it: ${Zone.current[#requestId]}');
    });
  }, zoneValues: {#requestId: 'req-abc-123'});

  // Error zones catch uncaught async errors.
  runZonedGuarded(() {
    Future(() => throw StateError('Async failure'));
  }, (error, stack) {
    print('Zone caught: $error');
  });
}
```

```dart
// file: zones_practical.dart
import 'dart:async';

class ZoneLogger {
  static void log(String message) {
    final reqId = Zone.current[#requestId] ?? 'no-request';
    final userId = Zone.current[#userId] ?? 'anonymous';
    print('[${DateTime.now().toIso8601String()}] [$reqId] [$userId] $message');
  }
}

Future<void> handleRequest(String requestId, String userId) {
  return runZonedGuarded(() async {
    ZoneLogger.log('Request started');
    await processOrder();
    ZoneLogger.log('Request completed');
  }, (error, stack) {
    ZoneLogger.log('Request failed: $error');
  }, zoneValues: {#requestId: requestId, #userId: userId})!;
}

Future<void> processOrder() async {
  ZoneLogger.log('Processing order');
  await Future.delayed(Duration(milliseconds: 50));
  ZoneLogger.log('Order processed');
}

void main() async {
  await handleRequest('req-001', 'user-42');
}
```

### 6. Compilation Targets

Dart compiles to multiple targets, and each serves a different deployment scenario. Choosing the wrong one causes real production problems -- oversized Docker images, slow cold starts, or missing APIs at runtime.

| Target | Command | Use Case | Tradeoff |
|--------|---------|----------|----------|
| JIT | `dart run app.dart` | Development, hot reload | Full debug info, slower startup for large apps |
| AOT | `dart compile exe app.dart -o app` | Production servers, CLIs | Fast startup, tree-shaken, no mirrors |
| Kernel | `dart compile kernel app.dart -o app.dill` | CI/CD, tooling | Intermediate format, requires Dart VM to run |
| JavaScript | `dart compile js app.dart -o app.js -O2` | Browsers | No dart:io, significant size optimization |

```dart
// file: compile_targets.dart
import 'dart:io';

void main(List<String> args) {
  // AOT binary: self-contained, no Dart SDK on target, ~5-10MB
  // JIT: fast iteration, supports dart:mirrors (rarely used)
  // JS: dart:io unavailable, but dart:html works
  print('Arguments: $args');
  print('PID: ${pid}');  // Only works in native targets

  // Practical tip: always profile against AOT builds.
  // JIT performance is misleading for production workloads.
}
```

### 7. Package Development

Every non-trivial Dart project depends on packages. Creating well-structured packages makes you a contributor to the ecosystem, not just a consumer.

```dart
// file: package_structure_example.dart

// A proper package layout:
// my_package/
//   lib/
//     my_package.dart        <-- barrel file (public API)
//     src/
//       core.dart            <-- implementation (private by convention)
//   test/
//     core_test.dart
//   pubspec.yaml
//   CHANGELOG.md
//   analysis_options.yaml

// The barrel file controls what consumers see:
// export 'src/core.dart' show MyPublicClass;
// Never export implementation details from lib/src/.

// Semantic versioning:
// 1.0.0 -> 1.0.1  patch: bug fix, no API change
// 1.0.0 -> 1.1.0  minor: new feature, backward compatible
// 1.0.0 -> 2.0.0  major: breaking change
```

Custom lint rules via `custom_lint` encode team conventions into the analyzer. Instead of documenting "never use print in production" in a wiki that nobody reads, the analyzer flags it as a warning. You implement `DartLintRule` subclasses, register them in a plugin, and every developer gets instant feedback.

### 8. Dart Macros (Experimental)

Dart macros are an upcoming language feature that brings compile-time metaprogramming directly into the language -- no external build tools needed. Unlike `build_runner` which runs as a separate process, macros will execute during compilation, with full access to the type system. They can augment classes, add methods, and generate code as part of the compilation itself. While still experimental, understanding their direction is important because they will eventually replace many code generation use cases.

### 9. Performance Profiling

Writing fast code requires measurement, not guessing. Dart DevTools provides CPU profiling, memory analysis, and timeline views for both Flutter and CLI applications.

```dart
// file: profiling_example.dart
void main() {
  final sw = Stopwatch()..start();
  final result = fibonacci(40);
  sw.stop();
  print('fibonacci(40) = $result in ${sw.elapsedMilliseconds}ms');

  // Profiling workflow:
  // 1. dart run --observe profiling_example.dart
  // 2. Open the DevTools URL printed to console
  // 3. CPU Profiler: which functions consume the most time
  // 4. Memory: track allocations, find leaks
  // 5. Timeline: visualize async gaps and idle periods
  //
  // Key rules:
  // - Profile AOT builds for production perf (JIT numbers are misleading)
  // - Run multiple iterations to warm up JIT
  // - Measure wall-clock AND CPU time (they differ with async)
}

int fibonacci(int n) => n <= 1 ? n : fibonacci(n - 1) + fibonacci(n - 2);
```

---

## Exercises

### Exercise 1 -- Custom Annotations and Validation Registry (Basic)

**Objective:** Create custom annotations (`@Required`, `@Range`, `@Pattern`) and a manual `FieldRegistry` that maps class/field names to metadata, simulating what code generators do at build time.

**Instructions:**
1. Create `exercise_01_annotations.dart`
2. Define three annotation classes with `const` constructors
3. Build a `FieldRegistry` as `Map<String, Map<String, List<Object>>>` (class name to field name to annotations)
4. Implement a `validate(className, Map<String, dynamic> values)` function that checks values against registered metadata
5. Test with a `RegistrationForm` with fields: username (required, pattern), age (range 13-120), email (required, pattern)
6. Show validation passing for valid data and clear error messages for each failure type

**Verification:** `dart run exercise_01_annotations.dart` -- outputs validation results for valid, invalid, and edge-case inputs.

---

### Exercise 2 -- json_serializable Project Setup (Basic)

**Objective:** Set up a complete `build_runner` + `json_serializable` project with three model classes and verify round-trip JSON serialization.

**Instructions:**
1. `dart create exercise_02_json_models` and add `json_annotation`, `json_serializable`, `build_runner`
2. Create models: `Address` (5 fields), `User` (nested Address, DateTime, List<String> roles), `ApiResponse<T>` (generic wrapper)
3. Use `@JsonKey` for custom names, defaults, and `includeIfNull: false`
4. Use `@JsonSerializable(genericArgumentFactories: true)` for the generic model
5. Run `dart run build_runner build --delete-conflicting-outputs`
6. Write main.dart that tests round-trip serialization including edge cases: null optionals, empty lists, nested objects

**Verification:** `dart run bin/main.dart` -- JSON encode/decode for all models, nested objects and DateTime handled correctly.

---

### Exercise 3 -- Dart Compilation Targets (Basic)

**Objective:** Compile a CLI word-counter app to all targets and compare binary sizes, startup times, and API availability.

**Instructions:**
1. Create `exercise_03_compile.dart`: reads a file path from args, counts words/lines/characters
2. Compile four ways: JIT (`dart run`), AOT (`dart compile exe`), kernel (`dart compile kernel`), JS (`dart compile js -O2`)
3. Create a 1000+ word `sample.txt`, time each execution
4. Compare binary sizes and startup times
5. Document which `dart:io` APIs fail under JS compilation

**Verification:** All native versions produce identical output. Document the JS limitations.

---

### Exercise 4 -- Custom DTO Mapper Generator (Intermediate)

**Objective:** Build a `source_gen` generator that reads `@DtoMapper(target: Type)` and generates `toDto()` / `fromDto()` extension methods with field renaming and type conversion support.

**Instructions:**
1. Create three packages: `dto_mapper_annotations/`, `dto_mapper_generator/`, `dto_mapper_example/`
2. Define `@DtoMapper(target: Type)` and `@MapField(name, fromTransform, toTransform)` annotations
3. Write a `GeneratorForAnnotation<DtoMapper>` that inspects fields, handles `@MapField` renaming and type transforms (DateTime to String, enum to String)
4. Register the builder in `build.yaml` with `SharedPartBuilder`
5. In the example, map a `User` domain class to `UserDto` with renamed fields and DateTime/enum conversions
6. Verify generated code compiles and maps correctly

**Verification:** `dart run build_runner build && dart run bin/main.dart` in the example package.

---

### Exercise 5 -- FFI: Calling C Functions (Intermediate)

**Objective:** Write a C library, compile it to a shared library, and call its functions from Dart with proper memory management.

**Instructions:**
1. Write `mathlib.c` with: `int factorial(int n)`, `double* moving_average(double* data, int length, int window)`, `void free_array(double* arr)`
2. Compile: `gcc -shared -o libmathlib.dylib mathlib.c`
3. Create `exercise_05_ffi.dart` that loads the library, defines type signatures, calls `factorial` with several values including edge cases (negative, zero)
4. For `moving_average`: allocate native memory, copy a Dart `List<double>` in, call the function, read results back, free everything
5. Handle errors: null pointer returns, negative inputs

**Verification:** `gcc -shared -o libmathlib.dylib mathlib.c && dart run exercise_05_ffi.dart`

---

### Exercise 6 -- Zones: Request-Scoped Context and Error Handling (Intermediate)

**Objective:** Build a simulated HTTP request handler using Zones for automatic context propagation, print interception, error catching, and timeout management.

**Instructions:**
1. Create `exercise_06_zones.dart`
2. Implement `runWithContext` that creates a zone with: zone-local values (requestId, userId), a `ZoneSpecification` overriding `print` to prefix with `[requestId]`, error handling via `runZonedGuarded`, timeout via `Timer`
3. Simulate three concurrent requests: one succeeds, one throws, one times out
4. All log output must include request context automatically without parameter threading
5. Use `Zone.root.print()` in error handlers to avoid recursive zone print calls

**Verification:** `dart run exercise_06_zones.dart` -- three interleaved request logs, each prefixed with its request ID.

---

### Exercise 7 -- Validation Code Generation Pipeline (Advanced)

**Objective:** Create a full code generation system: `@Validatable` model annotations produce generated `Validator<T>` classes that collect all errors, handle nested objects, and use strong types.

**Instructions:**
1. Three packages: annotations (`@Validatable`, `@NotEmpty`, `@Min`, `@Max`, `@Email`), generator, example
2. Generator inspects field types and annotations, generates a `Validator<T>` class per model
3. Generated validators collect all errors (not just the first), handle nested `@Validatable` fields by calling their validators, produce meaningful error messages with field names
4. No `dynamic`, no string-based field references in generated code
5. Write tests: valid objects pass, each constraint catches violations, multiple errors collected, nested validation works, edge cases (empty strings, boundary values, null optionals)

**Verification:** `dart run build_runner build && dart test` in the example package.

---

### Exercise 8 -- FFI with Callbacks, Structs, and Safe Wrappers (Advanced)

**Objective:** Implement FFI bindings for a C event queue library with callbacks, nested structs, and a safe Dart wrapper that prevents use-after-free.

**Instructions:**
1. Write `eventlib.c` with: `Event` struct (id, name, timestamp), `EventQueue` struct (events array, count, capacity), functions for create/push/process/destroy, and a callback typedef `void (*EventCallback)(Event*)`
2. Create Dart FFI bindings mapping both structs, using `Pointer.fromFunction` for the callback
3. Build `SafeEventQueue` wrapper that: tracks disposal state, throws on use-after-free, uses `Finalizer` for emergency cleanup
4. Properly allocate and free all memory -- Dart allocations freed by Dart, C allocations freed by C

**Verification:** `gcc -shared -o libeventlib.dylib eventlib.c && dart run exercise_08_ffi_advanced.dart`

---

### Exercise 9 -- OpenAPI Client Code Generator (Insane)

**Objective:** Build a code generator that reads an OpenAPI 3.0 YAML spec and generates a fully type-safe Dart HTTP client with models, serialization, error handling, and authentication.

**Instructions:**
1. Three packages: annotations, builder, example with an OpenAPI spec
2. Spec must define 4+ endpoints (CRUD), path/query params, nested schemas, enums, error responses
3. Generator: parse YAML at build time, generate model classes with `@JsonSerializable`, generate a client class with typed methods returning `Future<ApiResult<T>>`, generate dartdoc from spec descriptions
4. Generated code: zero warnings, strong types everywhere, nullable vs required from spec, custom base URLs and interceptors
5. Tests verify generated code compiles and methods have correct signatures

**Verification:** `dart run build_runner build && dart analyze --fatal-infos && dart test`

---

### Exercise 10 -- Hot-Reload for Dart CLI via Isolates (Insane)

**Objective:** Implement a hot-reload system for CLI apps that watches files, recompiles kernel snapshots, swaps running Isolates, and preserves application state across reloads.

**Instructions:**
1. `hot_reload_runner.dart`: watches directory with `FileSystemEntity.watch`, debounces changes, compiles to kernel snapshot via `Process.run`, spawns new Isolate with `Isolate.spawnUri`, transfers serialized state from old to new Isolate via `SendPort`/`ReceivePort`, kills old Isolate
2. `hot_reloadable_app.dart`: maintains in-memory state (counter, event list), exposes `serialize()` as JSON, accepts serialized state on startup, prints state periodically
3. Safety: debounce rapid changes, handle compilation errors (keep old Isolate), timeout on state transfer, backward-compatible deserialization when fields are added
4. Demonstration: modify the app programmatically and show the reload happening with state preserved

**Verification:** Run the runner, edit the app in another terminal, observe compile-swap-restore cycle.

---

## Summary

- **Annotations** are compile-time constants that communicate intent to build tools
- **Code generation** with `build_runner` and `source_gen` eliminates boilerplate by reading annotations and writing code
- **json_serializable** and **Freezed** are gold-standard examples of code generation done right
- **dart:ffi** bridges Dart and C with manual memory management
- **Zones** provide ambient execution contexts for error handling, logging, and async control
- **Compilation targets** (JIT, AOT, JS, kernel) serve different deployment needs
- **Package development** follows barrel files, semantic versioning, and proper structure
- **Custom lint rules** encode conventions into the analyzer
- **Profiling** with DevTools replaces guessing with measurement

## What's Next

Section 09 begins Flutter. You will take everything from these eight Dart sections and apply it to building user interfaces with widgets, composition, and reactive rendering.

## References

- [Dart Annotations and Metadata](https://dart.dev/language/metadata)
- [build_runner Documentation](https://pub.dev/packages/build_runner)
- [source_gen Package](https://pub.dev/packages/source_gen)
- [json_serializable](https://pub.dev/packages/json_serializable) | [Freezed](https://pub.dev/packages/freezed)
- [dart:ffi Documentation](https://dart.dev/interop/c-interop)
- [Zones in Dart](https://dart.dev/articles/zones)
- [Dart Compilation](https://dart.dev/tools/dart-compile)
- [pub.dev Publishing Guide](https://dart.dev/tools/pub/publishing)
- [custom_lint Package](https://pub.dev/packages/custom_lint)
- [Dart DevTools](https://dart.dev/tools/dart-devtools)
- [Dart Macros (Experimental)](https://github.com/dart-lang/language/blob/main/working/macros/feature-specification.md)
