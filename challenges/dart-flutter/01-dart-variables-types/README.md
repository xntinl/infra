# Section 01: Dart Variables, Types & Operators

## Introduction

Every program you write manipulates data. Before you can transform, transmit, or persist anything, you need to store it somewhere and know what kind of thing it is. Dart's type system is your first line of defense against an entire category of bugs that would otherwise surface only at runtime -- or worse, in production. Understanding how Dart handles variables, types, and operators is not just syntax memorization; it is the foundation that every subsequent section in this curriculum builds on.

Dart is a statically typed language with sound null safety and type inference. This means the compiler can catch type errors before your code ever runs, but it also means you need to understand the rules the compiler enforces. This section takes you from basic variable declarations through advanced type system features like records, pattern matching, and type promotion.

## Prerequisites

- Dart SDK 3.0+ installed and available on your PATH (`dart --version` should print 3.x or higher)
- A code editor with Dart support (VS Code with Dart extension, IntelliJ with Dart plugin, or any editor you prefer)
- Basic programming experience: you know what a variable is, what a function does, and how to run a program from the terminal
- Comfort reading error messages -- you will see many intentional ones in this section

## Learning Objectives

By the end of this section, you will be able to:

1. **Identify** the differences between `var`, `final`, `const`, and `late` declarations and when each is appropriate
2. **Explain** how Dart's type inference works and articulate the trade-offs between inferred and explicit types
3. **Implement** programs using Dart's built-in types, string manipulation features, and null-aware operators
4. **Analyze** type promotion behavior and predict when smart casts succeed or fail
5. **Evaluate** design decisions around compile-time constants vs runtime finals
6. **Design** type-safe data structures using records, pattern matching, and sealed classes

---

## Core Concepts

### 1. Variable Declarations: var, final, const

Dart gives you three primary keywords for declaring variables, and each communicates a different contract to both the compiler and other developers reading your code.

```dart
// var_final_const.dart
void main() {
  // var: mutable binding, type inferred from initializer
  var count = 0;
  count = 42; // fine -- var allows reassignment

  // final: single-assignment binding, value set at runtime
  final timestamp = DateTime.now();
  // timestamp = DateTime.now(); // ERROR: can't reassign a final variable

  // const: compile-time constant, value must be determinable at compile time
  const maxRetries = 3;
  // const now = DateTime.now(); // ERROR: DateTime.now() is not a compile-time constant

  print('count: $count');
  print('timestamp: $timestamp');
  print('maxRetries: $maxRetries');
}
```

The distinction between `final` and `const` trips up many developers. Think of it this way: `final` means "assigned once, possibly at runtime." `const` means "known at compile time, baked into the binary." A `final` variable holding a `List` still allows mutations to the list contents. A `const` list is deeply immutable.

```dart
// final_vs_const_depth.dart
void main() {
  final mutableList = [1, 2, 3];
  mutableList.add(4); // fine -- the list itself is mutable
  print(mutableList); // [1, 2, 3, 4]

  const immutableList = [10, 20, 30];
  // immutableList.add(40); // RUNTIME ERROR: Cannot add to an unmodifiable list

  // const propagates: elements must also be const-compatible
  const nested = [1, [2, 3]]; // inner list is also deeply immutable
  print(nested);
}
```

### 2. Built-in Types

Dart's type hierarchy has a few types that deserve attention beyond the obvious `int`, `double`, `String`, and `bool`.

```dart
// builtin_types.dart
void main() {
  // num is the supertype of both int and double
  num flexibleNumber = 42;
  flexibleNumber = 3.14; // fine -- num accepts both

  // dynamic disables static type checking entirely
  dynamic anything = 'hello';
  anything = 42;
  anything = true;
  // The compiler will NOT catch method calls on dynamic that don't exist
  // anything.nonExistentMethod(); // compiles, crashes at runtime

  // Object is the root of the non-nullable type hierarchy
  Object safe = 'hello';
  // safe.length; // ERROR: Object doesn't expose String's .length
  // You must check and cast explicitly

  // Never: a type with no values, used for functions that never return
  // Never noReturn() => throw StateError('unreachable');

  print('flexibleNumber: $flexibleNumber');
  print('anything: $anything');
  print('safe type: ${safe.runtimeType}');
}
```

The key insight: prefer `Object` over `dynamic` when you need a generic container. `Object` forces you to type-check before using type-specific methods, which is exactly what you want. `dynamic` silently skips all checks -- it is an escape hatch, not a strategy.

### 3. Type Inference vs Explicit Typing

Dart's type inference is powerful enough that explicit type annotations are often redundant. But "can omit" and "should omit" are different questions.

```dart
// type_inference.dart
void main() {
  // Inferred: the compiler knows this is int
  var count = 0;

  // Explicit: useful for public APIs, non-obvious types, or when you want a supertype
  int explicitCount = 0;
  num flexibleCount = 0; // without explicit type, this would be inferred as int

  // Where inference fails or misleads:
  // var items = []; // inferred as List<dynamic> -- probably not what you want
  var items = <String>[]; // explicit type argument fixes this

  // Late declarations REQUIRE explicit types or initializers
  late String deferredValue;
  deferredValue = 'computed later';

  print('count: $count, explicitCount: $explicitCount');
  print('flexibleCount type: ${flexibleCount.runtimeType}');
  print('items: $items');
  print('deferredValue: $deferredValue');
}
```

Rule of thumb: use `var` for local variables where the type is obvious from the right-hand side. Use explicit types for public API surfaces (function parameters, return types, class fields) and wherever the inferred type would be `dynamic` or overly narrow.

### 4. Strings: Interpolation, Multiline, Raw

Strings in Dart go well beyond simple concatenation.

```dart
// strings.dart
void main() {
  var name = 'Dart';
  var version = 3;

  // Interpolation: $ for simple expressions, ${} for complex ones
  var greeting = 'Hello, $name $version!';
  var computed = 'Next version: ${version + 1}';
  print(greeting);   // Hello, Dart 3!
  print(computed);    // Next version: 4

  // Multiline strings preserve line breaks
  var query = '''
SELECT *
FROM users
WHERE active = true
ORDER BY created_at DESC
''';
  print(query);

  // Raw strings: no interpolation, no escape processing
  var regex = r'\d+\.\d+\.\d+';
  print('Regex pattern: $regex'); // prints literal \d+\.\d+\.\d+

  // Adjacent string literals are concatenated at compile time
  var longMessage = 'This is a very long message '
      'that spans multiple lines in source '
      'but is a single string at runtime.';
  print(longMessage);
}
```

### 5. Operators

Dart includes the standard arithmetic and comparison operators, but also has several that are less common across languages.

```dart
// operators.dart
void main() {
  // Arithmetic: +, -, *, /, ~/ (integer division), %
  print(7 / 2);   // 3.5  (double division)
  print(7 ~/ 2);  // 3    (integer division, truncates toward zero)
  print(7 % 2);   // 1    (modulo)

  // Type test operators
  Object value = 'hello';
  print(value is String);   // true
  print(value is! int);     // true

  // Type cast
  var length = (value as String).length;
  print('length: $length');

  // Cascade operator: chain method calls on the same object
  var buffer = StringBuffer()
    ..write('Hello')
    ..write(', ')
    ..write('World');
  print(buffer.toString()); // Hello, World

  // Spread operator (in collection literals)
  var baseConfig = ['--verbose', '--color'];
  var fullConfig = ['dart', 'run', ...baseConfig, 'main.dart'];
  print(fullConfig); // [dart, run, --verbose, --color, main.dart]
}
```

### 6. Null-Aware Operators

Null safety is a core feature of Dart, and the null-aware operators are how you work with nullable types ergonomically.

```dart
// null_aware.dart
void main() {
  String? maybeName; // nullable -- starts as null

  // ?. -- null-aware access: returns null instead of crashing
  print(maybeName?.length); // null (not a crash)

  // ?? -- null coalescing: provide a default
  var displayName = maybeName ?? 'Anonymous';
  print(displayName); // Anonymous

  // ??= -- null-aware assignment: assign only if currently null
  maybeName ??= 'Default User';
  print(maybeName); // Default User
  maybeName ??= 'Will Not Overwrite';
  print(maybeName); // Default User (unchanged, because it was no longer null)

  // ?[] -- null-aware index
  List<int>? maybeList;
  print(maybeList?[0]); // null (not a crash)

  // Chaining null-aware operators
  Map<String, List<int>>? config;
  var firstValue = config?['retries']?.first;
  print(firstValue); // null
}
```

### 7. Late Variables and Lazy Initialization

`late` tells the compiler: "I guarantee this will be initialized before it is read." This is useful for expensive computations that should be deferred, or for fields that cannot be initialized in the constructor.

```dart
// late_variables.dart
class DatabaseConnection {
  // late + initializer = lazy: computed on first access, then cached
  late final String connectionString = _buildConnectionString();

  String _buildConnectionString() {
    print('Building connection string...'); // only printed once
    return 'postgres://localhost:5432/mydb';
  }
}

void main() {
  var db = DatabaseConnection();
  print('Instance created, connection string NOT yet computed.');
  print(db.connectionString); // triggers computation
  print(db.connectionString); // uses cached value, no recomputation

  // late without initializer: you promise to assign before reading
  late int result;
  // print(result); // RUNTIME ERROR: LateInitializationError
  result = 42;
  print(result); // 42
}
```

### 8. Type Promotion and Smart Casts

Dart's flow analysis tracks type checks and narrows types automatically within the scope where the check holds. This eliminates most explicit casts.

```dart
// type_promotion.dart
void processValue(Object value) {
  if (value is String) {
    // Inside this block, value is promoted to String
    print('String of length ${value.length}');
  } else if (value is int) {
    // Here, value is promoted to int
    print('Integer doubled: ${value * 2}');
  } else {
    print('Unknown type: ${value.runtimeType}');
  }
}

void demonstratePromotionLimits() {
  Object? value = 'hello';

  // Promotion works with local variables
  if (value != null) {
    print(value.hashCode); // promoted to Object (non-nullable)
  }

  // Promotion does NOT work with class fields or top-level variables
  // because another thread or setter could change them between the check and use
}

void main() {
  processValue('Dart');
  processValue(42);
  processValue(3.14);
  demonstratePromotionLimits();
}
```

### 9. Records (Dart 3+)

Records are anonymous, immutable, aggregate types. They let you return multiple values from a function without defining a class, and they support both positional and named fields.

```dart
// records.dart

// Function returning a positional record
(int, String) getUserInfo() {
  return (1, 'Alice');
}

// Function returning a named-field record
({int id, String name, bool active}) getUserDetails() {
  return (id: 42, name: 'Bob', active: true);
}

void main() {
  // Positional record access
  var info = getUserInfo();
  print('ID: ${info.$1}, Name: ${info.$2}');

  // Named record access
  var details = getUserDetails();
  print('User: ${details.name}, Active: ${details.active}');

  // Destructuring
  var (id, name) = getUserInfo();
  print('Destructured -- ID: $id, Name: $name');

  // Named destructuring
  var (:id as userId, :name as userName, :active) = getUserDetails();
  // Note: the above renames id -> userId and name -> userName
  // Actually, Dart uses a simpler syntax:
  var details2 = getUserDetails();
  var (:id as uid, :name as uname, active: isActive) = (id: details2.id, name: details2.name, active: details2.active);

  // Records are value types: equality is structural
  var r1 = (1, 'hello');
  var r2 = (1, 'hello');
  print('Records equal: ${r1 == r2}'); // true

  // Records as map keys (structural equality)
  var cache = <(String, int), String>{};
  cache[('users', 1)] = 'Alice';
  print(cache[('users', 1)]); // Alice
}
```

### 10. Pattern Matching Basics with Types

Dart 3 introduced pattern matching, which builds on type checks to provide more expressive destructuring and control flow.

```dart
// pattern_matching.dart
sealed class Shape {}
class Circle extends Shape {
  final double radius;
  Circle(this.radius);
}
class Rectangle extends Shape {
  final double width, height;
  Rectangle(this.width, this.height);
}

double area(Shape shape) => switch (shape) {
  Circle(radius: var r) => 3.14159 * r * r,
  Rectangle(width: var w, height: var h) => w * h,
};

void main() {
  var shapes = [Circle(5.0), Rectangle(3.0, 4.0), Circle(1.0)];

  for (var shape in shapes) {
    print('Area: ${area(shape).toStringAsFixed(2)}');
  }

  // Pattern matching with records
  var point = (x: 3.0, y: 4.0);
  var (x: px, y: py) = point;
  print('Point: ($px, $py), distance from origin: ${(px * px + py * py)}');

  // Guard clauses in switch
  var value = 42;
  var label = switch (value) {
    < 0 => 'negative',
    == 0 => 'zero',
    > 0 && < 100 => 'small positive',
    _ => 'large positive',
  };
  print('$value is $label');
}
```

---

## Exercises

### Exercise 1 (Basic): Variable Declaration Fundamentals

**Estimated time: 15 minutes**

You will write a program that declares variables using `var`, `final`, and `const`, and then observe how the compiler responds when you attempt to violate each keyword's contract.

**Instructions:**

1. Create a file called `exercise_01.dart`
2. In `main()`, declare the following:
   - A `var` called `currentScore` initialized to `0`, then reassign it to `100`
   - A `final` called `playerName` initialized to `'Player One'`
   - A `const` called `maxLevel` initialized to `50`
   - A `final` called `startTime` initialized to `DateTime.now()`
3. Print all four values with descriptive labels
4. Uncomment the three error lines (provided in the scaffold) one at a time, observe the error, then re-comment them

```dart
// exercise_01.dart
void main() {
  var currentScore = 0;
  currentScore = 100;

  final playerName = 'Player One';
  const maxLevel = 50;
  final startTime = DateTime.now();

  print('Score: $currentScore');
  print('Player: $playerName');
  print('Max Level: $maxLevel');
  print('Start Time: $startTime');

  // Uncomment each line one at a time to see the error:
  // playerName = 'Player Two';     // ERROR 1: final can't be reassigned
  // maxLevel = 99;                  // ERROR 2: const can't be reassigned
  // const badConst = DateTime.now(); // ERROR 3: not a compile-time constant
}
```

**Verification:**

```
dart run exercise_01.dart
```

Expected output (timestamp will vary):
```
Score: 100
Player: Player One
Max Level: 50
Start Time: 2026-03-28 10:30:00.123456
```

When you uncomment ERROR 1, you should see:
```
Error: Can't assign to the final variable 'playerName'.
```

When you uncomment ERROR 3, you should see:
```
Error: Cannot invoke a non-'const' constructor where a const expression is expected.
```

---

### Exercise 2 (Basic): Type System Exploration

**Estimated time: 20 minutes**

You will build a small program that demonstrates each of Dart's built-in types and inspects their runtime type information. The goal is to internalize which types exist and how they relate to each other.

**Instructions:**

1. Create `exercise_02.dart`
2. Declare one variable of each type: `int`, `double`, `String`, `bool`, `num`, `dynamic`, `Object`
3. For each variable, print its value and its `runtimeType`
4. Demonstrate that `num` accepts both `int` and `double` by reassigning
5. Show that `dynamic` skips type checking by calling `.length` on an `int` (observe the runtime crash)
6. Show that `Object` enforces type checking by attempting to access `.length` (observe the compile error)

```dart
// exercise_02.dart
void main() {
  int wholeNumber = 42;
  double fractional = 3.14;
  String text = 'Dart';
  bool flag = true;
  num flexible = 10;

  print('--- Type Inspection ---');
  print('int: $wholeNumber (${wholeNumber.runtimeType})');
  print('double: $fractional (${fractional.runtimeType})');
  print('String: $text (${text.runtimeType})');
  print('bool: $flag (${flag.runtimeType})');
  print('num: $flexible (${flexible.runtimeType})');

  // num accepts both subtypes
  flexible = 2.718;
  print('num reassigned: $flexible (${flexible.runtimeType})');

  // dynamic: no compile-time checks
  dynamic wild = 'hello';
  print('dynamic String length: ${wild.length}');
  wild = 42;
  // Uncomment to see runtime error:
  // print('dynamic int length: ${wild.length}'); // NoSuchMethodError

  // Object: compile-time checks enforced
  Object safe = 'hello';
  // Uncomment to see compile error:
  // print(safe.length); // Error: 'length' isn't defined for 'Object'

  // Safe access via type check
  if (safe is String) {
    print('Object promoted to String, length: ${safe.length}');
  }
}
```

**Verification:**

```
dart run exercise_02.dart
```

Expected output:
```
--- Type Inspection ---
int: 42 (int)
double: 3.14 (double)
String: Dart (String)
bool: true (bool)
num: 10 (int)
num reassigned: 2.718 (double)
dynamic String length: 5
Object promoted to String, length: 5
```

---

### Exercise 3 (Intermediate): Null-Safe Configuration Parser

**Estimated time: 30 minutes**

Build a configuration parser that reads from a `Map<String, String?>` and produces validated, non-nullable configuration values using null-aware operators. Some keys might be missing, some values might be null, and some might have invalid formats.

**Instructions:**

1. Create `exercise_03.dart`
2. Define a `Map<String, String?>` representing raw config (some keys present with values, some with null, some missing entirely)
3. Write a function `String resolveConfig(Map<String, String?> raw, String key, String defaultValue)` that uses `??` to provide defaults
4. Write a function `int? parsePort(String? rawPort)` that uses `?.` and `int.tryParse`
5. Build and print a final config summary showing resolved values
6. Handle edge cases: empty strings, whitespace-only values, negative ports

Complete the functions where `// TODO` markers are placed:

```dart
// exercise_03.dart

String resolveConfig(Map<String, String?> raw, String key, String defaultValue) {
  // TODO: Look up the key. If missing or null, return defaultValue.
  // Also treat empty or whitespace-only strings as missing.
  throw UnimplementedError();
}

int? parsePort(String? rawPort) {
  // TODO: Return null if rawPort is null, empty, or not a valid positive integer.
  // Use ?. and int.tryParse. Reject negative values.
  throw UnimplementedError();
}

void main() {
  final rawConfig = <String, String?>{
    'host': 'localhost',
    'port': '8080',
    'database': null,
    'timeout': '',
    'retries': '   ',
    'logLevel': 'debug',
  };

  final host = resolveConfig(rawConfig, 'host', '0.0.0.0');
  final database = resolveConfig(rawConfig, 'database', 'app_db');
  final timeout = resolveConfig(rawConfig, 'timeout', '30');
  final retries = resolveConfig(rawConfig, 'retries', '3');
  final missing = resolveConfig(rawConfig, 'nonexistent', 'fallback');
  final port = parsePort(rawConfig['port']);
  final badPort = parsePort(rawConfig['database']);
  final negativePort = parsePort('-1');

  print('host: $host');
  print('database: $database');
  print('timeout: $timeout');
  print('retries: $retries');
  print('missing key: $missing');
  print('port: $port');
  print('badPort: $badPort');
  print('negativePort: $negativePort');
}
```

**Expected output after completing the TODOs:**

```
host: localhost
database: app_db
timeout: 30
retries: 3
missing key: fallback
port: 8080
badPort: null
negativePort: null
```

---

### Exercise 4 (Intermediate): String Manipulation Toolkit

**Estimated time: 45 minutes**

Build a set of string utility functions that exercise interpolation, multiline strings, raw strings, and type conversions. You will process a template string with placeholders and produce formatted output.

**Instructions:**

1. Create `exercise_04.dart`
2. Implement `String formatTemplate(String template, Map<String, String> values)` that replaces `{{key}}` placeholders with values from the map
3. Implement `String buildSqlQuery(String table, List<String> columns, {String? where, int? limit})` that builds a multiline SQL string using interpolation
4. Implement `bool isValidIdentifier(String input)` that checks if a string matches Dart identifier rules (use a raw string regex)
5. Write `main()` that tests all three functions with multiple inputs including edge cases

Complete the scaffolding:

```dart
// exercise_04.dart

String formatTemplate(String template, Map<String, String> values) {
  // TODO: Replace all occurrences of {{key}} with corresponding values.
  // If a key is not in the map, leave the placeholder unchanged.
  throw UnimplementedError();
}

String buildSqlQuery(
  String table,
  List<String> columns, {
  String? where,
  int? limit,
}) {
  // TODO: Build a SQL-like string. Use multiline strings for readability.
  // Include WHERE and LIMIT clauses only if provided (not null).
  throw UnimplementedError();
}

bool isValidIdentifier(String input) {
  // TODO: Return true if input matches: starts with letter or underscore,
  // followed by letters, digits, or underscores.
  // Use a raw string (r'...') for the regex pattern.
  throw UnimplementedError();
}

void main() {
  // Test formatTemplate
  var template = 'Hello, {{name}}! Welcome to {{place}}.';
  print(formatTemplate(template, {'name': 'Alice', 'place': 'Dart Land'}));
  print(formatTemplate(template, {'name': 'Bob'}));

  // Test buildSqlQuery
  print(buildSqlQuery('users', ['id', 'name', 'email']));
  print(buildSqlQuery('orders', ['*'], where: 'status = "active"', limit: 10));

  // Test isValidIdentifier
  var testCases = ['myVar', '_private', '3invalid', 'has space', 'camelCase123', ''];
  for (var tc in testCases) {
    print("'$tc' valid: ${isValidIdentifier(tc)}");
  }
}
```

**Expected output after completing the TODOs:**

```
Hello, Alice! Welcome to Dart Land.
Hello, Bob! Welcome to {{place}}.
SELECT id, name, email
FROM users
SELECT *
FROM orders
WHERE status = "active"
LIMIT 10
'myVar' valid: true
'_private' valid: true
'3invalid' valid: false
'has space' valid: false
'camelCase123' valid: true
'' valid: false
```

---

### Exercise 5 (Advanced): Type-Safe Configuration System with Records

**Estimated time: 60 minutes**

Design a configuration system that uses records and pattern matching to parse, validate, and merge configuration from multiple sources (defaults, file, environment overrides). Each source has a priority, and the final config should reflect the highest-priority non-null value for each key.

**Hints:**

- Define a record type for each config entry: `({String key, String value, int priority, String source})`
- Write a function that takes a list of config entries and resolves them by selecting the highest-priority value per key
- Use pattern matching in a switch expression to categorize config values (port numbers, hostnames, boolean flags, durations)
- Handle conflicts: when two sources have the same priority for the same key, the last one wins (stable sort)
- Validate resolved values: ports must be 1-65535, hostnames must be non-empty, booleans must parse correctly

**What to produce:**

1. A `resolveConfig` function that merges entries and returns a `Map<String, ({String value, String source})>`
2. A `validateEntry` function using pattern matching that returns either a validated value or an error message
3. A `main()` that demonstrates merging three sources and printing the resolved configuration
4. At least three test cases that exercise conflict resolution, validation errors, and missing keys

**Verification approach:** Print the resolved config as a formatted table. Include at least one intentionally invalid entry (like port 99999) and show that validation catches it.

---

### Exercise 6 (Advanced): Late Initialization and Lazy Computation Patterns

**Estimated time: 90 minutes**

Build a resource manager that uses `late` variables to implement lazy initialization with proper error handling, lifecycle management, and diagnostic reporting.

**Hints:**

- Create a `ResourceManager` class with `late final` fields for expensive resources (database connection string, cache capacity computed from available memory, compiled regex patterns)
- Each lazy field should log when it is first accessed (to prove laziness)
- Add a `bool get isInitialized` check for each resource using a companion boolean flag
- Implement a `dispose()` method that cleans up resources and a `reinitialize()` that resets state
- Handle the case where a `late` variable is accessed after `dispose()` -- this should throw a clear custom error, not a `LateInitializationError`
- Write a `DiagnosticReport` that uses records to capture `({String resource, bool initialized, Duration initTime})`

**What to produce:**

1. `ResourceManager` class with at least 3 lazy-initialized resources
2. `DiagnosticReport` using records
3. `main()` that demonstrates: creation without initialization, first access triggering lazy init, second access using cached value, disposal, and the error from post-disposal access
4. Timing information showing that lazy init only happens once

**Verification approach:** Run the program and confirm that "Initializing X..." messages appear only on first access, not on construction or repeated access.

---

### Exercise 7 (Insane): Type-Safe Heterogeneous Container

**Estimated time: 2-4 hours**

Build a type-safe heterogeneous container -- a single data structure that can hold values of different types while preserving full type safety at compile time. This is a problem that languages with reified generics handle differently than those with type erasure. Dart's type system, combined with records and sealed classes, makes an interesting middle ground.

**Problem statement:**

Create a `TypedRegistry` that:

- Allows registering values of any type, keyed by a `TypedKey<T>` that carries the type information
- Retrieves values with the correct static type (no casts at the call site)
- Supports default values via a factory function on the key
- Supports listeners that are notified when a value for a specific key changes
- Is fully type-safe: it should be impossible to register an `int` for a key typed as `String` without a compile error
- Handles key absence gracefully (returns null or default, never throws)
- Implements `operator []` and `operator []=` with type safety

Consider:

- How do you store heterogeneous values internally without losing type information?
- How do you implement the listener mechanism while keeping listeners typed?
- What happens with inheritance -- if you have `TypedKey<num>`, can you store an `int`?
- Can you make the container const-constructible for compile-time registries?
- What are the performance implications of your type-checking approach?

There is no single correct answer. The design space includes sealed classes for value wrappers, extension types for zero-cost key abstractions, and records for change notifications. Research Dart's extension types (Dart 3.3+) as a potential tool.

---

### Exercise 8 (Insane): Expression Evaluator with Operator Overloading

**Estimated time: 3-4 hours**

Build a mini expression evaluator that uses Dart's operator overloading and type system to represent, validate, and evaluate mathematical expressions at both compile time and runtime.

**Problem statement:**

Design an expression tree system where:

- Expressions are represented as an algebraic data type using sealed classes: `Literal`, `Variable`, `BinaryOp`, `UnaryOp`
- `BinaryOp` and `UnaryOp` use operator overloading so you can write `Expr(2) + Expr(3) * Expr.variable('x')` and get the correct AST
- Operator precedence is handled by Dart's built-in precedence (since you are overloading real operators)
- Implement an `evaluate(Map<String, num> variables)` method on the expression tree
- Implement a `simplify()` method that applies algebraic simplifications: `x + 0 = x`, `x * 1 = x`, `x * 0 = 0`, `0 - x = -x`
- Implement `toString()` that produces a human-readable infix representation with minimal parentheses
- Use pattern matching to implement `evaluate`, `simplify`, and `toString` as external functions (not methods), showing both OOP and functional styles
- Handle type promotions: expressions containing only integers should produce `int` results; any `double` involvement produces `double`
- Detect and report errors: division by zero, undefined variables, overflow

Consider:

- Sealed classes give you exhaustive pattern matching -- the compiler will tell you if you forget a case
- Records can represent intermediate evaluation results with metadata: `({num value, String type, List<String> warnings})`
- How do you handle operator precedence in `toString()` to produce minimal parentheses?
- Can you implement compile-time constant expressions using `const` constructors?
- What are the limits of operator overloading in Dart? (Which operators can you overload? Which can't you?)

There is no single correct answer. Start with a basic working evaluator and layer on features incrementally.

---

## Summary

In this section you covered:

- **Variable declarations**: `var` for mutable locals, `final` for single-assignment runtime values, `const` for compile-time constants, and `late` for deferred initialization
- **Type system fundamentals**: Dart's built-in types, the difference between `dynamic` and `Object`, and when explicit typing adds value over inference
- **String handling**: interpolation with `$` and `${}`, multiline strings with triple quotes, raw strings with `r`, and adjacent literal concatenation
- **Operators**: arithmetic (including `~/`), type test (`is`, `as`), cascade (`..`), spread (`...`), and the full null-aware family (`?.`, `??`, `??=`, `?[]`)
- **Type promotion**: how Dart's flow analysis narrows types after `is` checks, and why this only works on local variables
- **Records**: anonymous immutable aggregate types with positional and named fields, structural equality, and destructuring
- **Pattern matching**: switch expressions with sealed classes, guard clauses, and record patterns

Important notes:

- Prefer `final` over `var` when you don't need reassignment. It communicates intent and prevents accidental mutation.
- Prefer `Object` over `dynamic`. Reserve `dynamic` for genuine interop scenarios (JSON parsing, FFI).
- `const` is not just about immutability -- it enables compile-time evaluation and canonical instance sharing, which can improve performance.
- Records replace many uses of tuples, pairs, and simple DTOs. Use them freely for local computations; consider classes when you need methods or identity.
- Pattern matching with sealed classes gives you exhaustive checking. If you add a new subclass, the compiler will flag every switch that needs updating.

## What's Next

In **Section 02: Functions & Closures**, you will learn how to define functions with positional and named parameters, default values, and return types. You will explore first-class functions, closures and variable capture, tear-offs, and how Dart handles function types in its type system. The type knowledge from this section is a direct prerequisite -- every function parameter and return type builds on what you learned here.

## References

- [Dart Language Tour: Variables](https://dart.dev/language/variables)
- [Dart Language Tour: Built-in Types](https://dart.dev/language/built-in-types)
- [Dart Language Tour: Operators](https://dart.dev/language/operators)
- [Dart Language Tour: Records](https://dart.dev/language/records)
- [Dart Language Tour: Patterns](https://dart.dev/language/patterns)
- [Dart Language Tour: Branches (Pattern Matching)](https://dart.dev/language/branches)
- [Effective Dart: Design](https://dart.dev/effective-dart/design)
- [Dart Type System](https://dart.dev/language/type-system)
- [Dart Null Safety](https://dart.dev/null-safety)
- [Dart API Reference: dart:core](https://api.dart.dev/stable/dart-core/dart-core-library.html)
