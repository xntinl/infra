# Section 08 -- Solutions: Metaprogramming, Code Generation, FFI and Zones

## How to Use This Guide

Work through the exercises first. When stuck, use this in layers: hints first, then common mistakes, then the full solution. The deep dives explain the why behind implementation choices.

---

## Exercise 1 -- Custom Annotations and Validation Registry

### Hints

1. Annotations are just classes with `const` constructors. Define `Required`, `Range`, `Pattern` as plain Dart classes.
2. Without `dart:mirrors`, build a manual registry: `Map<String, Map<String, List<Object>>>` keyed by class name, then field name.
3. Use Dart 3 pattern matching in the `validate` function to dispatch on annotation types cleanly.

### Solution

```dart
// file: exercise_01_annotations.dart
class Required { const Required(); }

class Range {
  final num min, max;
  const Range(this.min, this.max);
}

class Pattern {
  final String regex;
  const Pattern(this.regex);
}

class ValidationError {
  final String field, message;
  const ValidationError(this.field, this.message);
  @override String toString() => '$field: $message';
}

class ValidationResult {
  final List<ValidationError> errors;
  const ValidationResult(this.errors);
  bool get isValid => errors.isEmpty;
  @override String toString() => isValid ? 'VALID' : errors.join('; ');
}

class FieldRegistry {
  static final Map<String, Map<String, List<Object>>> _data = {};
  static void register(String cls, String field, List<Object> annotations) {
    _data.putIfAbsent(cls, () => {})[field] = annotations;
  }
  static Map<String, List<Object>>? getFields(String cls) => _data[cls];
}

ValidationResult validate(String className, Map<String, dynamic> values) {
  final fields = FieldRegistry.getFields(className);
  if (fields == null) return const ValidationResult([]);
  final errors = <ValidationError>[];

  for (final entry in fields.entries) {
    final name = entry.key;
    final value = values[name];
    for (final ann in entry.value) {
      switch (ann) {
        case Required():
          if (value == null || (value is String && value.isEmpty))
            errors.add(ValidationError(name, 'is required'));
        case Range(:final min, :final max):
          if (value is num && (value < min || value > max))
            errors.add(ValidationError(name, 'must be between $min and $max (got $value)'));
        case Pattern(:final regex):
          if (value is String && !RegExp(regex).hasMatch(value))
            errors.add(ValidationError(name, 'must match $regex'));
      }
    }
  }
  return ValidationResult(errors);
}

void main() {
  FieldRegistry.register('Form', 'username', [const Required(), const Pattern(r'^[a-zA-Z0-9_]{3,20}$')]);
  FieldRegistry.register('Form', 'age', [const Required(), const Range(13, 120)]);
  FieldRegistry.register('Form', 'email', [const Required(), const Pattern(r'^[\w.+-]+@[\w-]+\.[\w.]+$')]);

  print(validate('Form', {'username': 'alice_99', 'age': 25, 'email': 'a@b.com'}));
  print(validate('Form', {'username': 'ab', 'age': 5, 'email': 'bad'}));
  print(validate('Form', {'username': '', 'age': 13, 'email': ''}));
}
```

### Common Mistakes

- **Missing `const` on annotation constructors.** Works here, fails in real code generation where the analyzer requires constant metadata.
- **Checking only the first error.** Always collect all errors in one pass. Users hate fix-one-resubmit cycles.
- **Using `dart:mirrors`.** Disabled in AOT. The manual registry simulates what generators do at build time.

---

## Exercise 2 -- json_serializable Project Setup

### Hints

1. Version mismatches between `json_annotation` and `json_serializable` cause confusing errors. Pin compatible versions.
2. Each model needs `part 'filename.g.dart';` matching the filename exactly.
3. For the generic `ApiResponse<T>`, use `@JsonSerializable(genericArgumentFactories: true)`.
4. Use `explicitToJson: true` on classes with nested objects, otherwise you get `Instance of 'Address'`.

### Solution (Key Files)

```dart
// file: lib/models/user.dart
import 'package:json_annotation/json_annotation.dart';
import 'address.dart';
part 'user.g.dart';

@JsonSerializable(explicitToJson: true)
class User {
  final String id, name;
  @JsonKey(name: 'email_address') final String email;
  final Address address;
  @JsonKey(name: 'created_at') final DateTime createdAt;
  @JsonKey(defaultValue: <String>[]) final List<String> roles;
  @JsonKey(includeIfNull: false) final String? avatarUrl;

  User({required this.id, required this.name, required this.email,
        required this.address, required this.createdAt,
        this.roles = const [], this.avatarUrl});

  factory User.fromJson(Map<String, dynamic> json) => _$UserFromJson(json);
  Map<String, dynamic> toJson() => _$UserToJson(this);
}
```

```dart
// file: lib/models/api_response.dart
import 'package:json_annotation/json_annotation.dart';
part 'api_response.g.dart';

@JsonSerializable(genericArgumentFactories: true)
class ApiResponse<T> {
  final T data;
  final String message;
  @JsonKey(name: 'status_code') final int statusCode;

  ApiResponse({required this.data, required this.message, required this.statusCode});

  factory ApiResponse.fromJson(Map<String, dynamic> json, T Function(Object?) fromJsonT) =>
      _$ApiResponseFromJson(json, fromJsonT);
  Map<String, dynamic> toJson(Object? Function(T) toJsonT) => _$ApiResponseToJson(this, toJsonT);
}
```

### Common Mistakes

- **Missing `explicitToJson: true`.** Without it, nested objects serialize as `Instance of 'Address'`.
- **Mismatched `part` directive.** Must be exactly `<your_file>.g.dart`. Case sensitive.
- **Not running `--delete-conflicting-outputs`.** Stale `.g.dart` files from previous runs cause conflicts.

---

## Exercise 3 -- Dart Compilation Targets

### Solution

```dart
// file: exercise_03_compile.dart
import 'dart:io';

void main(List<String> args) {
  final sw = Stopwatch()..start();
  if (args.isEmpty) { stderr.writeln('Usage: wordcount <file>'); exit(1); }
  final file = File(args[0]);
  if (!file.existsSync()) { stderr.writeln('Not found: ${args[0]}'); exit(1); }

  final content = file.readAsStringSync();
  final lines = content.split('\n').length;
  final words = content.split(RegExp(r'\s+')).where((w) => w.isNotEmpty).length;
  sw.stop();

  print('Lines: $lines | Words: $words | Chars: ${content.length} | Time: ${sw.elapsedMicroseconds}us');
}
```

**Expected results:** AOT has fastest cold startup. JIT warms up for repeated runs. Kernel is intermediate. JS fails on `dart:io`. AOT binary is 5-10MB, self-contained.

---

## Exercise 4 -- Custom DTO Mapper Generator

### Hints

1. The annotations package has zero dependencies. The generator depends on `analyzer`, `build`, `source_gen`, and annotations.
2. Use `TypeChecker.fromRuntime(MapField)` to detect `@MapField` on fields.
3. For field renaming, read `ConstantReader(annotation).read('name')`, checking `isNull` first.
4. Register the builder with `SharedPartBuilder` and configure `build.yaml` with `applies_builders: ["source_gen|combining_builder"]`.

### Solution (Generator Core)

```dart
// file: dto_mapper_generator/lib/src/generator.dart
class DtoMapperGenerator extends GeneratorForAnnotation<DtoMapper> {
  @override
  String generateForAnnotatedElement(Element element, ConstantReader annotation, BuildStep buildStep) {
    if (element is! ClassElement) throw InvalidGenerationSourceError('Classes only', element: element);

    final className = element.name;
    final targetType = annotation.read('target').typeValue.getDisplayString(withNullability: false);
    final fields = element.fields.where((f) => !f.isStatic && !f.isSynthetic);

    final toDto = <String>[];
    final fromDto = <String>[];
    for (final field in fields) {
      final mapField = _readMapField(field);
      final dtoName = mapField?.name ?? field.name;
      toDto.add('${dtoName}: ${mapField?.toTransform ?? 'entity.${field.name}'}');
      fromDto.add('${field.name}: ${mapField?.fromTransform ?? 'dto.$dtoName'}');
    }

    return '''
extension ${className}Mapper on $className {
  $targetType toDto() { final entity = this; return $targetType(${toDto.join(', ')}); }
  static $className fromDto($targetType dto) => $className(${fromDto.join(', ')});
}''';
  }
}
```

### Common Mistakes

- **Wrong `build.yaml` config.** Typos in builder names cause silent failures. Run `dart run build_runner build --verbose`.
- **Not checking `isNull` on optional annotation fields.** `@MapField()` without arguments causes `.stringValue` to throw.

---

## Exercise 5 -- FFI: Calling C Functions

### Solution

```c
// file: mathlib.c
#include <stdlib.h>

int factorial(int n) {
    if (n < 0) return -1;
    int result = 1;
    for (int i = 2; i <= n; i++) result *= i;
    return result;
}

double* moving_average(double* data, int length, int window) {
    if (!data || length <= 0 || window <= 0 || window > length) return NULL;
    int rlen = length - window + 1;
    double* result = (double*)malloc(rlen * sizeof(double));
    for (int i = 0; i < rlen; i++) {
        double sum = 0;
        for (int j = 0; j < window; j++) sum += data[i + j];
        result[i] = sum / window;
    }
    return result;
}

void free_array(double* arr) { free(arr); }
```

```dart
// file: exercise_05_ffi.dart
import 'dart:ffi';
import 'package:ffi/ffi.dart';

typedef FactorialN = Int32 Function(Int32);
typedef FactorialD = int Function(int);
typedef MovAvgN = Pointer<Double> Function(Pointer<Double>, Int32, Int32);
typedef MovAvgD = Pointer<Double> Function(Pointer<Double>, int, int);
typedef FreeN = Void Function(Pointer<Double>);
typedef FreeD = void Function(Pointer<Double>);

void main() {
  final lib = DynamicLibrary.open('libmathlib.dylib');
  final factorial = lib.lookupFunction<FactorialN, FactorialD>('factorial');
  final movAvg = lib.lookupFunction<MovAvgN, MovAvgD>('moving_average');
  final freeArr = lib.lookupFunction<FreeN, FreeD>('free_array');

  for (final n in [0, 1, 5, 10, -1]) {
    final r = factorial(n);
    print('factorial($n) = ${r == -1 ? "ERROR" : r}');
  }

  final data = [1.0, 2.0, 3.0, 4.0, 5.0, 6.0, 7.0, 8.0, 9.0, 10.0];
  const window = 3;
  final nativeData = calloc<Double>(data.length);
  for (var i = 0; i < data.length; i++) nativeData[i] = data[i];

  final resultPtr = movAvg(nativeData, data.length, window);
  if (resultPtr != nullptr) {
    final results = [for (var i = 0; i < data.length - window + 1; i++) resultPtr[i]];
    print('Moving avg: $results');
    freeArr(resultPtr);
  }
  calloc.free(nativeData);
}
```

### Common Mistakes

- **Mixing up allocators.** Dart allocates with `calloc` and frees with `calloc.free()`. C allocates with `malloc` and frees with `free_array()`. Crossing them corrupts memory.
- **Reading past allocated bounds.** `Pointer<Double>` indexing has no bounds checking. Track lengths explicitly.
- **Library path issues.** macOS: `.dylib`, Linux: `.so`, Windows: `.dll`. Use absolute paths if the library is not in the working directory.

---

## Exercise 6 -- Zones: Request-Scoped Context

### Solution

```dart
// file: exercise_06_zones.dart
import 'dart:async';

Future<void> runWithContext({
  required String requestId, required String userId,
  required Duration timeout, required Future<String> Function() handler,
}) async {
  final completer = Completer<String>();
  runZonedGuarded(() {
    final timer = Timer(timeout, () {
      if (!completer.isCompleted) completer.completeError(TimeoutException('Timeout: $requestId'));
    });
    handler().then((r) { timer.cancel(); if (!completer.isCompleted) completer.complete(r); })
      .catchError((Object e) { timer.cancel(); if (!completer.isCompleted) completer.completeError(e); });
  }, (error, stack) {
    Zone.root.print('[$requestId] UNHANDLED: $error');
    if (!completer.isCompleted) completer.completeError(error);
  }, zoneValues: {#requestId: requestId, #userId: userId},
     zoneSpecification: ZoneSpecification(
       print: (self, parent, zone, line) => parent.print(zone, '[${zone[#requestId]}] $line'),
     ));

  try {
    final result = await completer.future;
    Zone.root.print('[$requestId] Completed: $result');
  } on TimeoutException catch (e) { Zone.root.print('[$requestId] $e'); }
    catch (e) { Zone.root.print('[$requestId] FAILED: $e'); }
}

void main() async {
  await Future.wait([
    runWithContext(requestId: 'req-001', userId: 'u-42', timeout: Duration(milliseconds: 300),
      handler: () async { print('Working...'); await Future.delayed(Duration(milliseconds: 100)); return 'OK'; }),
    runWithContext(requestId: 'req-002', userId: 'u-17', timeout: Duration(milliseconds: 300),
      handler: () async { print('About to fail'); throw StateError('DB down'); }),
    runWithContext(requestId: 'req-003', userId: 'u-99', timeout: Duration(milliseconds: 200),
      handler: () async { print('Slow task'); await Future.delayed(Duration(seconds: 1)); return 'Late'; }),
  ]);
}
```

### Common Mistakes

- **Recursive print.** If your zone's `print` override calls `print()`, it recurses infinitely. Use `parent.print(zone, line)`.
- **`runZonedGuarded` returns null.** The guarded zone swallows errors. Use a `Completer` to bridge results out.
- **Zone values are immutable.** Set at creation, cannot be changed. Store mutable objects if you need mutation.

---

## Exercise 7 -- Validation Code Generation Pipeline

### Hints

1. Use `TypeChecker.fromRuntime(NotEmpty)` to detect annotations on fields.
2. For nested validation, check if a field's type has `@Validatable` on its class. If so, generate a call to that type's validator.
3. Escape regex characters in generated string literals -- backslashes and dollar signs need double-escaping.

### Solution Approach

The generator iterates each field, checks for annotation types, and emits validation code per annotation:

```dart
// Generator core pattern for each field:
if (_notEmptyChecker.hasAnnotationOfExact(field)) {
  buffer.writeln('if (instance.${field.name}.isEmpty) '
      'errors.add(ValidationError("${field.name}", "${field.name} must not be empty"));');
}
// Nested: check if field type has @Validatable
final fieldClass = field.type.element;
if (fieldClass is ClassElement && _validatableChecker.hasAnnotationOfExact(fieldClass)) {
  buffer.writeln('errors.addAll(${fieldClass.name}Validator()'
      '.validate(instance.${field.name}).errors.map('
      '(e) => ValidationError("${field.name}.\${e.field}", e.message)));');
}
```

The output is a `class ${ClassName}Validator` with a `ValidationResult validate($ClassName instance)` method.

### Common Mistakes

- **Not handling `isNull` on optional annotation params.** `@NotEmpty()` without a message argument -- reading `.stringValue` directly throws.
- **Off-by-one on `@Min`/`@Max`.** `@Min(0)` should accept exactly 0. Use `<` not `<=` for the check.

---

## Exercise 8 -- FFI with Callbacks and Safe Wrappers

### Hints

1. `Pointer.fromFunction` requires a static or top-level function. Use a static variable as a trampoline for closures.
2. The `SafeEventQueue` tracks a `_isDisposed` flag. Every method checks it first.
3. Strings in the `Event` struct are `Pointer<Utf8>`. Read with `.toDartString()`, but never free them from Dart -- the C `destroy_queue` function handles it.

### Solution (Safe Wrapper Pattern)

```dart
// file: exercise_08_ffi_advanced.dart (key pattern)
class SafeEventQueue {
  Pointer<EventQueue>? _ptr;
  bool _isDisposed = false;
  final void Function(Pointer<EventQueue>) _destroy;

  void _checkDisposed() {
    if (_isDisposed) throw StateError('Use-after-free: EventQueue already disposed');
  }

  void pushEvent(String name) {
    _checkDisposed();
    final namePtr = name.toNativeUtf8();
    _nativePush(_ptr!, namePtr);
    calloc.free(namePtr);  // Dart allocated, Dart frees.
  }

  void processEvents(void Function(int id, String name, double ts) callback) {
    _checkDisposed();
    _activeCallback = callback;
    _nativeProcess(_ptr!, Pointer.fromFunction<EventCallbackNative>(_trampoline));
    _activeCallback = null;
  }

  void dispose() {
    if (_isDisposed) return;
    _isDisposed = true;
    _destroy(_ptr!);
    _ptr = null;
  }

  static void Function(int, String, double)? _activeCallback;
  static void _trampoline(Pointer<Event> e) {
    _activeCallback?.call(e.ref.id, e.ref.name.toDartString(), e.ref.timestamp);
  }
}
```

### Common Mistakes

- **Using a closure with `Pointer.fromFunction`.** It requires a static function. The trampoline pattern with a static variable is the standard workaround.
- **Freeing C-allocated strings from Dart.** The C `destroy_queue` frees event names. If Dart also frees them, you get a double-free crash.
- **Use-after-free is silent in C.** Without the `SafeEventQueue` wrapper, calling functions on freed pointers produces undefined behavior. The wrapper converts this into a clear Dart exception.

---

## Exercise 9 -- OpenAPI Client Code Generator

### Architecture

The generator is a compiler: it reads a specification language (OpenAPI YAML), builds an intermediate representation, and emits Dart code. Structure it in three phases:

1. **Parse**: `yaml` package reads the spec. Resolve `$ref` by splitting on `/` and walking the YAML map. Build `ApiSchema` and `ApiEndpoint` model objects.
2. **Transform**: Convert OpenAPI types to Dart types (`string` to `String`, `integer` to `int`, `array` to `List<T>`). Handle `required` vs nullable. Resolve enum schemas.
3. **Emit**: Generate model classes with `@JsonSerializable`, generate client methods returning `Future<ApiResult<T>>`, generate dartdoc from descriptions.

The hardest part is `$ref` resolution and handling the full OpenAPI type system. Start with a subset (no `oneOf`, `allOf`, `discriminator`) and expand.

### Debugging Tips

- **Parse errors in YAML**: lint the spec with an OpenAPI validator before feeding it to your generator.
- **Generated code does not compile**: print the generated output, paste it into a file, and fix issues manually before fixing the generator.

---

## Exercise 10 -- Hot-Reload via Isolates

### Architecture

1. **Watch**: `Directory(path).watch()` returns `FileSystemEvent` stream. Filter for `.dart` modify events. Debounce with a `Timer` -- cancel and restart on each event.
2. **Compile**: `Process.run('dart', ['compile', 'kernel', file, '-o', snapshot])`. If it fails, log the error and keep the current isolate.
3. **Transfer**: Send `{'command': 'serialize', 'replyTo': port}` to old isolate. It responds with JSON state. Timeout after 5 seconds.
4. **Swap**: `isolate.kill(priority: Isolate.immediate)`. Then `Isolate.spawnUri(Uri.file(snapshot), [serializedState], parentPort)`.
5. **Restore**: New isolate reads state from args, deserializes with defaults for missing fields, continues operation.

### Common Mistakes

- **Not debouncing.** File watchers fire multiple events per save. Without debouncing, you reload 3-4 times.
- **Forgetting `Uri.file()`.** `Isolate.spawnUri` takes a URI, not a file path. Especially matters on Windows.
- **State schema drift.** When you add fields in the reloaded code, `fromJson` must handle missing keys with defaults. Every field needs a fallback.
- **Not killing the old isolate.** Both run simultaneously if you forget. Always kill before spawning.

### Alternatives

For simpler needs: `package:hotreloader` (process restart, no state), or Flutter's built-in hot reload (JIT VM feature, not available in CLI). The isolate approach is the most educational because it forces understanding of process lifecycle, IPC, and state serialization.

---

## General Debugging Tips

**Code generation not running:** Check `build.yaml` config. Run `dart run build_runner build --verbose`. Delete `.dart_tool/build` and rebuild.

**FFI crashes with no message:** These are segfaults. Run under `lldb dart run file.dart`. Check pointer validity, struct field order matches C exactly, and library architecture matches (arm64 vs x86_64).

**Zone errors disappearing:** `runZonedGuarded` catches errors. If the handler does not print, errors vanish. Use `Zone.root.print()` to bypass zone overrides.

**Builder conflicts:** When multiple generators target one file, both must use `SharedPartBuilder`. Run with `--delete-conflicting-outputs` to clear stale files.
