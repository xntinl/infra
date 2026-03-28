# Section 01: Solutions -- Dart Variables, Types & Operators

## How to Use This Guide

This file contains complete solutions for every exercise in Section 01. Before you look at any solution:

1. **Attempt the exercise independently** for at least the minimum estimated time
2. **Read the hints progressively** -- each level reveals a bit more without giving everything away
3. **Compare your working solution** against the one here -- there is no single correct approach, but understanding the differences will deepen your knowledge
4. **Read the "Common Mistakes" and "Deep Dive" sections** even if you solved it correctly -- they cover pitfalls you will encounter in real projects

The progressive hints are numbered. Try to stop at the lowest level that unblocks you.

---

## Exercise 1: Variable Declaration Fundamentals

### Progressive Hints

1. The program is almost complete as given. Focus on understanding WHY each error occurs, not just that it does.
2. `final` prevents reassignment of the variable binding. The object it points to can still be mutated (if mutable). `const` prevents both.
3. `const` requires the value to be determinable at compile time. `DateTime.now()` depends on when the program runs -- that is inherently a runtime value.
4. Try adding a `const` list and then calling `.add()` on it. Compare the error type (runtime) with the `final` reassignment error (compile-time).
5. Notice that `final startTime` works with `DateTime.now()` but `const` does not. This is the core distinction: `final` = assigned once at runtime; `const` = known at compile time.

### Complete Solution

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

  // ERROR 1: final can't be reassigned
  // playerName = 'Player Two';
  // Error: Can't assign to the final variable 'playerName'.

  // ERROR 2: const can't be reassigned
  // maxLevel = 99;
  // Error: Can't assign to the const variable 'maxLevel'.

  // ERROR 3: DateTime.now() is not a compile-time constant
  // const badConst = DateTime.now();
  // Error: Cannot invoke a non-'const' constructor where a const expression is expected.
}
```

### Detailed Explanation

The program demonstrates three levels of variable mutability:

- `var currentScore = 0` creates a mutable binding with inferred type `int`. The compiler tracks the type but allows reassignment to any `int` value. You cannot assign a `String` to `currentScore` because type inference locked it to `int` at declaration.

- `final playerName = 'Player One'` creates an immutable binding. The type is inferred as `String`. Once assigned, the binding cannot point to a different value. However, if the value were a mutable object (like a `List`), the object's contents could still change.

- `const maxLevel = 50` creates a compile-time constant. The value `50` is a literal that the compiler can evaluate without running the program. `const` values are canonicalized: every `const` expression that evaluates to the same value shares the same instance in memory.

- `final startTime = DateTime.now()` works because `final` allows runtime initialization. `const startTime = DateTime.now()` would fail because the compiler cannot know what time it will be when the program runs.

### Common Mistakes

**Mistake 1: Thinking `final` makes objects immutable.**

```dart
// mistake_final_mutability.dart
void main() {
  final items = [1, 2, 3];
  items.add(4);       // this works -- the list is mutable
  // items = [5, 6];  // this fails -- the binding is final
  print(items);       // [1, 2, 3, 4]
}
```

`final` freezes the variable (the reference), not the value (the object). If you want a truly immutable list, use `const` or `List.unmodifiable()`.

**Mistake 2: Using `const` for values that seem constant but depend on runtime.**

```dart
// mistake_const_runtime.dart
void main() {
  final envPort = '8080'; // pretend this came from Platform.environment
  // const port = int.parse(envPort); // ERROR: not a const expression
  final port = int.parse(envPort);    // correct: use final
  print(port);
}
```

**Mistake 3: Not understanding const canonicalization.**

```dart
// const_identity.dart
void main() {
  const a = [1, 2, 3];
  const b = [1, 2, 3];
  print(identical(a, b)); // true -- same instance in memory

  final c = [1, 2, 3];
  final d = [1, 2, 3];
  print(identical(c, d)); // false -- different instances
}
```

### Deep Dive: Compile-Time Constant Propagation

When you mark something `const`, the Dart compiler evaluates it during compilation and embeds the result directly in the compiled output. This has two implications:

1. **Performance**: no runtime allocation or computation for `const` values. They exist as literals in the binary.
2. **Canonicalization**: all `const` expressions that produce the same value share one instance. This saves memory and makes `identical()` checks fast.

This is why `const` constructors exist for classes -- they let you create canonical instances of your own types at compile time.

---

## Exercise 2: Type System Exploration

### Progressive Hints

1. Use `runtimeType` to inspect what Dart actually stores. The static type and runtime type can differ (e.g., `num flexible = 10` -- static type is `num`, runtime type is `int`).
2. `dynamic` accepts anything and lets you call any method. If the method does not exist, you get a `NoSuchMethodError` at runtime, not a compile error.
3. `Object` is the opposite of `dynamic` in strictness. It forces you to prove the type before accessing type-specific members.
4. Type promotion with `is` checks is how you go from `Object` to a specific type safely. Inside the `if (safe is String)` block, the compiler knows `safe` is a `String`.
5. Try assigning a `double` to an `int` variable. Dart does not do implicit numeric conversions -- you must explicitly call `.toInt()` or `.toDouble()`.

### Complete Solution

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

  flexible = 2.718;
  print('num reassigned: $flexible (${flexible.runtimeType})');

  dynamic wild = 'hello';
  print('dynamic String length: ${wild.length}');
  wild = 42;
  // print('dynamic int length: ${wild.length}'); // NoSuchMethodError at runtime

  Object safe = 'hello';
  // print(safe.length); // compile error: 'length' not defined for 'Object'

  if (safe is String) {
    print('Object promoted to String, length: ${safe.length}');
  }
}
```

### Detailed Explanation

Dart's type hierarchy is rooted at `Object?` (nullable) and `Object` (non-nullable). Every non-null value is an `Object`. The special types work as follows:

- **`num`**: abstract supertype of `int` and `double`. When you write `num flexible = 10`, the static type is `num` but the runtime type is `int` (because `10` is an integer literal). When you reassign `flexible = 2.718`, the runtime type changes to `double`. This is fine because both are subtypes of `num`.

- **`dynamic`**: a special type that disables static checking. The compiler treats every member access on a `dynamic` value as valid. If the member does not exist at runtime, you get a `NoSuchMethodError`. This is the equivalent of a dynamically typed language within Dart.

- **`Object`**: the top of the non-nullable type hierarchy. Unlike `dynamic`, `Object` only exposes members defined on `Object` itself (`hashCode`, `runtimeType`, `toString()`, `==`). To access type-specific members, you must use a type check (`is`) or explicit cast (`as`).

### Common Mistakes

**Mistake 1: Using `dynamic` when `Object` would suffice.**

```dart
// mistake_dynamic_overuse.dart
void processItems(dynamic items) {
  // No compile-time checking at all -- bugs hide here
  items.forEach((item) => print(item));
}

// Better:
void processItemsSafe(Iterable<Object> items) {
  for (var item in items) {
    print(item);
  }
}
```

**Mistake 2: Assuming numeric type coercion.**

```dart
// mistake_numeric_coercion.dart
void main() {
  int a = 5;
  // double b = a; // ERROR in Dart: int is not assignable to double
  double b = a.toDouble(); // correct
  print(b); // 5.0
}
```

**Mistake 3: Confusing `runtimeType` with static type for control flow.**

```dart
// mistake_runtime_type.dart
void main() {
  num x = 42;
  // Don't do this:
  if (x.runtimeType == int) {
    print('is int via runtimeType');
  }
  // Do this instead:
  if (x is int) {
    print('is int via type test -- and x is now promoted to int');
    print(x.isEven); // works because of promotion
  }
}
```

Using `is` triggers type promotion; checking `runtimeType` does not.

### Deep Dive: The Type Hierarchy

```
Object?
  |
  +-- Null (the type of null)
  +-- Object
        |
        +-- num
        |     +-- int
        |     +-- double
        +-- String
        +-- bool
        +-- List<T>
        +-- Map<K, V>
        +-- ...etc
```

`Never` sits at the bottom: it is a subtype of everything and has no values. A function that returns `Never` is guaranteed to never return normally (it always throws or loops forever). This is useful for exhaustiveness checking and assertion helpers.

`dynamic` is not actually in the hierarchy -- it is a special directive to the compiler meaning "skip all type checks for this."

---

## Exercise 3: Null-Safe Configuration Parser

### Progressive Hints

1. For `resolveConfig`: use `raw[key]` to look up the value. Remember that a missing key returns `null`, and the value itself might be `null`.
2. Chain the null-check with a whitespace check: `raw[key]?.trim()` gives you a trimmed string or `null`.
3. An empty string after trimming should be treated as missing. Use `.isEmpty` after trimming.
4. For `parsePort`: `int.tryParse` returns `null` for invalid strings. Chain: `rawPort?.trim()` then `int.tryParse`, then check for positive.
5. The full chain for `parsePort`: handle null input, trim, reject empty, parse, reject non-positive.

### Complete Solution

```dart
// exercise_03.dart

String resolveConfig(Map<String, String?> raw, String key, String defaultValue) {
  var value = raw[key]?.trim();
  if (value == null || value.isEmpty) {
    return defaultValue;
  }
  return value;
}

int? parsePort(String? rawPort) {
  var trimmed = rawPort?.trim();
  if (trimmed == null || trimmed.isEmpty) {
    return null;
  }
  var parsed = int.tryParse(trimmed);
  if (parsed == null || parsed <= 0) {
    return null;
  }
  return parsed;
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
  final missing = resolveConfig(rawConfig, 'missing', 'fallback');
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

### Detailed Explanation

The key insight in `resolveConfig` is the chain of null-aware operations:

1. `raw[key]` returns `String?` -- it is `null` if the key is missing OR if the value is explicitly `null`
2. `?.trim()` safely trims whitespace if the value is non-null, or propagates `null`
3. The `isEmpty` check catches both empty strings (`''`) and strings that were only whitespace (`'   '` becomes `''` after trim)

For `parsePort`, the chain is longer but follows the same pattern:

1. `rawPort?.trim()` handles null input and whitespace
2. `int.tryParse(trimmed)` returns `null` for non-numeric strings (unlike `int.parse` which throws)
3. The `parsed <= 0` check enforces the domain rule that ports must be positive

Notice that we never use `!` (the null assertion operator). Every null case is handled explicitly. This is a deliberate design choice: `!` converts a compile-time safety check into a runtime crash, which defeats the purpose of null safety.

### Common Mistakes

**Mistake 1: Using `!` instead of `??`.**

```dart
// mistake_null_assertion.dart
void main() {
  Map<String, String?> config = {'host': null};
  // var host = config['host']!; // crashes with Null check operator used on a null value
  var host = config['host'] ?? 'default'; // safe
  print(host);
}
```

**Mistake 2: Forgetting that `Map[key]` returns nullable even with non-nullable values.**

```dart
// mistake_map_lookup.dart
void main() {
  var scores = <String, int>{'alice': 100};
  // int score = scores['bob']; // ERROR: can't assign int? to int
  int score = scores['bob'] ?? 0; // correct
  print(score);
}
```

The map's value type is `int`, but the lookup returns `int?` because the key might not exist.

**Mistake 3: Not trimming before checking emptiness.**

```dart
// mistake_whitespace.dart
void main() {
  var input = '   ';
  print(input.isEmpty);       // false -- whitespace is not "empty"
  print(input.trim().isEmpty); // true -- now it is
}
```

### Deep Dive: The Null-Aware Operator Family

Dart has four null-aware operators, and understanding when to use each one avoids verbose null-checking boilerplate:

| Operator | Name | Usage | Equivalent Without Operator |
|----------|------|-------|----------------------------|
| `?.` | Null-aware access | `obj?.method()` | `obj == null ? null : obj.method()` |
| `??` | Null coalescing | `value ?? fallback` | `value != null ? value : fallback` |
| `??=` | Null-aware assignment | `variable ??= value` | `variable = variable ?? value` |
| `?[]` | Null-aware index | `list?[0]` | `list == null ? null : list[0]` |

These compose naturally: `config?['port']?.trim()` means "if config is null, return null; otherwise look up 'port'; if that is null, return null; otherwise trim it."

---

## Exercise 4: String Manipulation Toolkit

### Progressive Hints

1. For `formatTemplate`: use `String.replaceAllMapped` with a `RegExp` that matches `{{...}}`. Inside the callback, look up the captured key in the map.
2. The regex pattern for template placeholders: `r'\{\{(\w+)\}\}'`. Group 1 captures the key name.
3. For `buildSqlQuery`: start with the SELECT and FROM parts, then conditionally append WHERE and LIMIT using `if` checks on nullability.
4. For `isValidIdentifier`: the regex is `r'^[a-zA-Z_][a-zA-Z0-9_]*$'`. The `^` and `$` anchors ensure the entire string matches. Handle the empty string case explicitly or let the regex handle it (it will not match empty).
5. Use `RegExp(pattern).hasMatch(input)` for the identifier check. Make sure to handle the empty string -- `hasMatch('')` returns false for the pattern above, which is the correct behavior.

### Complete Solution

```dart
// exercise_04.dart

String formatTemplate(String template, Map<String, String> values) {
  return template.replaceAllMapped(
    RegExp(r'\{\{(\w+)\}\}'),
    (match) {
      var key = match.group(1)!;
      return values[key] ?? match.group(0)!;
    },
  );
}

String buildSqlQuery(
  String table,
  List<String> columns, {
  String? where,
  int? limit,
}) {
  var buffer = StringBuffer();
  buffer.writeln('SELECT ${columns.join(', ')}');
  buffer.write('FROM $table');
  if (where != null) {
    buffer.write('\nWHERE $where');
  }
  if (limit != null) {
    buffer.write('\nLIMIT $limit');
  }
  return buffer.toString();
}

bool isValidIdentifier(String input) {
  return RegExp(r'^[a-zA-Z_][a-zA-Z0-9_]*$').hasMatch(input);
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

### Detailed Explanation

**`formatTemplate`**: The `replaceAllMapped` method takes a regex and a callback. For each match, the callback receives a `Match` object. `match.group(1)` extracts the first capture group (the key name inside the braces). If the key exists in the map, we return the value; otherwise, we return the original placeholder text unchanged (`match.group(0)` is the full match including braces).

The `!` after `match.group(1)` is safe here because group 1 always exists when the regex matches -- it is a structural guarantee of the pattern, not a runtime gamble.

**`buildSqlQuery`**: Uses `StringBuffer` for efficient string building. The `writeln` adds a newline after the SELECT clause, while subsequent clauses use `write` with explicit `\n` prefixes so they are only added when the clause is present. This avoids trailing newlines or empty WHERE clauses.

**`isValidIdentifier`**: The regex `r'^[a-zA-Z_][a-zA-Z0-9_]*$'` anchored at both ends ensures:
- First character is a letter or underscore
- Remaining characters (zero or more) are letters, digits, or underscores
- The entire string must match (no partial matches)

The `r` prefix makes this a raw string, so backslashes are literal. Without `r`, you would need to double-escape: `'^[a-zA-Z_][a-zA-Z0-9_]*\$'`.

### Common Mistakes

**Mistake 1: Forgetting to escape braces in regex.**

```dart
// mistake_regex_braces.dart
void main() {
  // WRONG: { and } are regex quantifier syntax
  // RegExp(r'{{(\w+)}}'); // might work but is ambiguous

  // CORRECT: escape the braces
  RegExp(r'\{\{(\w+)\}\}');
}
```

In most regex engines, unescaped `{` is a quantifier. Dart's regex engine is lenient about this, but escaping makes intent clear and avoids surprises across tools.

**Mistake 2: Using string concatenation instead of StringBuffer for multi-part strings.**

```dart
// mistake_concatenation.dart
void main() {
  // Inefficient for many parts:
  var result = '';
  for (var i = 0; i < 1000; i++) {
    result += 'item $i, '; // creates a new String each iteration
  }

  // Efficient:
  var buffer = StringBuffer();
  for (var i = 0; i < 1000; i++) {
    buffer.write('item $i, ');
  }
  var resultFast = buffer.toString(); // single allocation
  print(resultFast.length);
}
```

For two or three concatenations, `+` is fine. For loops or many parts, `StringBuffer` avoids O(n^2) allocation.

**Mistake 3: Not anchoring regex patterns.**

```dart
// mistake_unanchored.dart
void main() {
  // Without anchors, this matches a substring
  print(RegExp(r'[a-zA-Z_][a-zA-Z0-9_]*').hasMatch('3abc')); // true! matches 'abc'

  // With anchors, it correctly rejects
  print(RegExp(r'^[a-zA-Z_][a-zA-Z0-9_]*$').hasMatch('3abc')); // false
}
```

### Deep Dive: String Interpolation Internals

When you write `'Hello, $name!'`, Dart calls `name.toString()` and concatenates the result. For `'${expression}'`, the expression is evaluated first, then `.toString()` is called on the result.

This means you can use any expression inside `${}`:

```dart
// interpolation_deep.dart
void main() {
  var items = [1, 2, 3];
  print('Sum: ${items.fold(0, (a, b) => a + b)}'); // Sum: 6
  print('Reversed: ${items.reversed.toList()}');     // Reversed: [3, 2, 1]

  // Nested interpolation (legal but avoid for readability)
  var greeting = 'Say ${"Hello, ${"World"}"}';
  print(greeting); // Say Hello, World
}
```

Adjacent string literals are concatenated at compile time, which means this:
```dart
var x = 'hello' ' ' 'world';
```
produces a single string `'hello world'` with zero runtime cost. This is useful for breaking long strings across source lines without any performance penalty.

---

## Exercise 5: Type-Safe Configuration System with Records

### Progressive Hints

1. Start by defining the record type for config entries. Each entry needs a key, value, priority, and source label.
2. To resolve configs, group entries by key, then select the one with the highest priority. Dart's `Iterable` methods (`where`, `fold`, `reduce`) work here.
3. For validation, use a switch expression on the key to determine what kind of validation to apply (port range, non-empty string, boolean parsing).
4. Handle the "same priority" conflict by processing entries in order and letting later entries overwrite earlier ones (last-write-wins within same priority).
5. For the formatted table output, use `String.padRight` to align columns.

### Complete Solution

```dart
// exercise_05.dart

typedef ConfigEntry = ({String key, String value, int priority, String source});
typedef ResolvedEntry = ({String value, String source});

Map<String, ResolvedEntry> resolveConfig(List<ConfigEntry> entries) {
  var resolved = <String, ConfigEntry>{};

  // Sort by priority ascending so higher priority overwrites lower.
  // Stable sort preserves insertion order within same priority (last wins).
  var sorted = List<ConfigEntry>.from(entries)
    ..sort((a, b) => a.priority.compareTo(b.priority));

  for (var entry in sorted) {
    resolved[entry.key] = entry;
  }

  return resolved.map(
    (key, entry) => MapEntry(key, (value: entry.value, source: entry.source)),
  );
}

String? validateEntry(String key, String value) {
  return switch (key) {
    'port' => switch (int.tryParse(value)) {
      null => 'Invalid port: "$value" is not a number',
      var p when p < 1 || p > 65535 => 'Invalid port: $p out of range 1-65535',
      _ => null,
    },
    'host' => value.trim().isEmpty ? 'Host cannot be empty' : null,
    'ssl' || 'debug' || 'verbose' => switch (value.toLowerCase()) {
      'true' || 'false' || '1' || '0' || 'yes' || 'no' => null,
      _ => 'Invalid boolean for $key: "$value"',
    },
    'timeout' || 'retries' => switch (int.tryParse(value)) {
      null => 'Invalid integer for $key: "$value"',
      var n when n < 0 => '$key cannot be negative: $n',
      _ => null,
    },
    _ => null, // unknown keys pass validation by default
  };
}

void printConfigTable(Map<String, ResolvedEntry> config) {
  print('${'Key'.padRight(15)} ${'Value'.padRight(20)} Source');
  print('${'-' * 15} ${'-' * 20} ${'-' * 15}');
  for (var entry in config.entries) {
    var validation = validateEntry(entry.key, entry.value.value);
    var status = validation == null ? '' : ' [ERROR: $validation]';
    print(
      '${entry.key.padRight(15)} '
      '${entry.value.value.padRight(20)} '
      '${entry.value.source}$status',
    );
  }
}

void main() {
  var defaults = <ConfigEntry>[
    (key: 'host', value: 'localhost', priority: 0, source: 'defaults'),
    (key: 'port', value: '3000', priority: 0, source: 'defaults'),
    (key: 'ssl', value: 'false', priority: 0, source: 'defaults'),
    (key: 'timeout', value: '30', priority: 0, source: 'defaults'),
    (key: 'retries', value: '3', priority: 0, source: 'defaults'),
  ];

  var fileConfig = <ConfigEntry>[
    (key: 'host', value: 'db.example.com', priority: 1, source: 'config.yaml'),
    (key: 'port', value: '5432', priority: 1, source: 'config.yaml'),
    (key: 'ssl', value: 'true', priority: 1, source: 'config.yaml'),
    (key: 'debug', value: 'true', priority: 1, source: 'config.yaml'),
  ];

  var envOverrides = <ConfigEntry>[
    (key: 'port', value: '99999', priority: 2, source: 'ENV'),
    (key: 'ssl', value: 'true', priority: 2, source: 'ENV'),
    (key: 'host', value: '', priority: 2, source: 'ENV'),
  ];

  var allEntries = [...defaults, ...fileConfig, ...envOverrides];
  var resolved = resolveConfig(allEntries);

  print('=== Resolved Configuration ===\n');
  printConfigTable(resolved);

  print('\n=== Validation Report ===\n');
  var errors = <String>[];
  for (var entry in resolved.entries) {
    var error = validateEntry(entry.key, entry.value.value);
    if (error != null) {
      errors.add('  $error');
    }
  }
  if (errors.isEmpty) {
    print('All entries valid.');
  } else {
    print('${errors.length} validation error(s):');
    for (var e in errors) {
      print(e);
    }
  }
}
```

### Detailed Explanation

This solution demonstrates several type system features working together:

**Type aliases**: `typedef ConfigEntry = (...)` creates a readable name for the record type. Without this, every function signature would include the full record type, hurting readability.

**Pattern matching in validation**: The nested `switch` on `key` first categorizes what kind of validation to apply, then the inner `switch` validates the value. The `var p when p < 1 || p > 65535` syntax combines destructuring (binding the parsed int to `p`) with a guard clause.

**Spread operator for merging**: `[...defaults, ...fileConfig, ...envOverrides]` concatenates three lists into one. This is more readable than `defaults + fileConfig + envOverrides` and makes the merge order explicit.

**Records for results**: `({String value, String source})` carries both the resolved value and its provenance. This is exactly the kind of "small aggregate without a full class" use case records were designed for.

### Common Mistakes

**Mistake 1: Sorting without stability awareness.**

Dart's `List.sort()` is guaranteed stable (equal elements keep their relative order). This is important here: entries with the same priority should resolve by insertion order. If the sort were unstable, the behavior would be unpredictable.

**Mistake 2: Validating before resolving.**

If you validate entries from all sources before merging, you waste effort validating values that will be overridden. Validate only the resolved (final) values.

**Mistake 3: Forgetting exhaustive cases in switch.**

When using pattern matching with string keys, the wildcard `_` case is mandatory since strings have infinite possible values. If you were switching on a sealed class hierarchy, the compiler would enforce exhaustiveness without needing `_`.

### Alternative Approach

Instead of sorting and overwriting, you could group entries by key and use `reduce` to pick the winner:

```dart
// alternative_resolve.dart
Map<String, ResolvedEntry> resolveConfigAlt(List<ConfigEntry> entries) {
  var grouped = <String, List<ConfigEntry>>{};
  for (var e in entries) {
    grouped.putIfAbsent(e.key, () => []).add(e);
  }
  return grouped.map((key, group) {
    var winner = group.reduce(
      (a, b) => a.priority >= b.priority ? (a.priority > b.priority ? a : b) : b,
    );
    return MapEntry(key, (value: winner.value, source: winner.source));
  });
}
```

This approach is more explicit about conflict resolution but more verbose.

---

## Exercise 6: Late Initialization and Lazy Computation Patterns

### Progressive Hints

1. Use `late final` with an initializer expression to get lazy computation: `late final x = _computeX();`. The initializer runs on first access.
2. To track initialization state, use a companion `bool` field: `bool _dbInitialized = false;` set to `true` inside the lazy initializer.
3. For disposal safety, add a `bool _disposed = false;` flag. Every property accessor checks this flag first.
4. Wrap access in a getter that checks `_disposed` before returning the `late` field. This gives you a clear custom error message instead of the cryptic `LateInitializationError`.
5. For timing, capture `Stopwatch` readings before and after the first access to each resource.

### Complete Solution

```dart
// exercise_06.dart

typedef DiagnosticEntry = ({String resource, bool initialized, Duration initTime});

class ResourceInitError implements Exception {
  final String resource;
  final String reason;
  ResourceInitError(this.resource, this.reason);

  @override
  String toString() => 'ResourceInitError: $resource -- $reason';
}

class ResourceManager {
  bool _disposed = false;

  bool _dbInitialized = false;
  bool _cacheInitialized = false;
  bool _regexInitialized = false;

  Duration _dbInitTime = Duration.zero;
  Duration _cacheInitTime = Duration.zero;
  Duration _regexInitTime = Duration.zero;

  late final String _connectionString = _initDatabase();
  late final int _cacheCapacity = _initCache();
  late final RegExp _emailPattern = _initRegex();

  String _initDatabase() {
    var sw = Stopwatch()..start();
    print('  [ResourceManager] Initializing database connection...');
    // Simulate expensive operation
    var result = 'postgres://localhost:5432/app_${DateTime.now().millisecondsSinceEpoch}';
    sw.stop();
    _dbInitTime = sw.elapsed;
    _dbInitialized = true;
    return result;
  }

  int _initCache() {
    var sw = Stopwatch()..start();
    print('  [ResourceManager] Computing cache capacity...');
    // Simulate capacity calculation
    var result = 1024 * 64; // 64K entries
    sw.stop();
    _cacheInitTime = sw.elapsed;
    _cacheInitialized = true;
    return result;
  }

  RegExp _initRegex() {
    var sw = Stopwatch()..start();
    print('  [ResourceManager] Compiling regex patterns...');
    var result = RegExp(r'^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$');
    sw.stop();
    _regexInitTime = sw.elapsed;
    _regexInitialized = true;
    return result;
  }

  void _checkDisposed(String resource) {
    if (_disposed) {
      throw ResourceInitError(resource, 'ResourceManager has been disposed');
    }
  }

  String get connectionString {
    _checkDisposed('connectionString');
    return _connectionString;
  }

  int get cacheCapacity {
    _checkDisposed('cacheCapacity');
    return _cacheCapacity;
  }

  RegExp get emailPattern {
    _checkDisposed('emailPattern');
    return _emailPattern;
  }

  List<DiagnosticEntry> get diagnostics => [
    (resource: 'database', initialized: _dbInitialized, initTime: _dbInitTime),
    (resource: 'cache', initialized: _cacheInitialized, initTime: _cacheInitTime),
    (resource: 'regex', initialized: _regexInitialized, initTime: _regexInitTime),
  ];

  void dispose() {
    print('  [ResourceManager] Disposing resources...');
    _disposed = true;
  }
}

void printDiagnostics(List<DiagnosticEntry> entries) {
  print('  ${'Resource'.padRight(15)} ${'Initialized'.padRight(15)} Init Time');
  print('  ${'-' * 15} ${'-' * 15} ${'-' * 15}');
  for (var (:resource, :initialized, :initTime) in entries) {
    print(
      '  ${resource.padRight(15)} '
      '${initialized.toString().padRight(15)} '
      '${initTime.inMicroseconds}us',
    );
  }
}

void main() {
  print('1. Creating ResourceManager (no initialization yet)');
  var rm = ResourceManager();

  print('\n2. Checking diagnostics before any access');
  printDiagnostics(rm.diagnostics);

  print('\n3. Accessing connectionString (triggers lazy init)');
  print('   Result: ${rm.connectionString}');

  print('\n4. Accessing connectionString again (cached, no re-init)');
  print('   Result: ${rm.connectionString}');

  print('\n5. Accessing cacheCapacity (triggers lazy init)');
  print('   Result: ${rm.cacheCapacity}');

  print('\n6. Diagnostics after partial initialization');
  printDiagnostics(rm.diagnostics);

  print('\n7. Disposing ResourceManager');
  rm.dispose();

  print('\n8. Attempting access after disposal');
  try {
    print(rm.connectionString);
  } on ResourceInitError catch (e) {
    print('   Caught: $e');
  }

  print('\n9. Final diagnostics');
  printDiagnostics(rm.diagnostics);
}
```

### Detailed Explanation

The `late final` pattern with an initializer expression is the core mechanism here. When you write `late final String _connectionString = _initDatabase();`, Dart does not call `_initDatabase()` at construction time. Instead, it defers the call to the first time `_connectionString` is read. After that first read, the value is cached and `_initDatabase()` is never called again.

The companion boolean flags (`_dbInitialized`, etc.) exist because there is no built-in way to ask a `late` variable "have you been initialized yet?" Accessing an uninitialized `late` variable without an initializer throws `LateInitializationError`, but `late final` with an initializer always succeeds on first access (it runs the initializer). The flags give us observability without side effects.

The disposal pattern wraps each `late` field in a getter that checks `_disposed` first. This is important because after disposal, the resources the `late` fields reference may no longer be valid. Without this check, the code would return stale data silently.

### Common Mistakes

**Mistake 1: Using `late` without understanding the initialization guarantee.**

```dart
// mistake_late_no_init.dart
void main() {
  late String name;
  // print(name); // LateInitializationError: Local 'name' has not been initialized.
  name = 'assigned';
  print(name); // works
}
```

`late` without an initializer is a promise: "I will assign this before reading it." Breaking that promise gives you a runtime error, not a compile error.

**Mistake 2: Expecting `late final` initializers to re-run.**

```dart
// mistake_late_rerun.dart
class Counter {
  late final int value = _compute();
  int _callCount = 0;

  int _compute() {
    _callCount++;
    return 42;
  }
}

void main() {
  var c = Counter();
  print(c.value); // triggers _compute, _callCount becomes 1
  print(c.value); // cached, _callCount stays 1
  print(c._callCount); // 1
}
```

**Mistake 3: Using `late` on a class field that could be accessed from multiple contexts.**

If a `late` field's initializer has side effects (like network calls), and the field is accessed from multiple asynchronous contexts, the initializer might be called while a previous call is still running. `late` does not provide synchronization. For thread-safe lazy initialization, you need a more sophisticated pattern (like a `Completer`).

### Deep Dive: late vs Nullable with Manual Check

An alternative to `late` is a nullable field with manual initialization:

```dart
// late_vs_nullable.dart
class WithLate {
  late final String value = 'computed: ${DateTime.now()}';
}

class WithNullable {
  String? _value;
  String get value => _value ??= 'computed: ${DateTime.now()}';
}
```

Both achieve lazy initialization. The differences:

- `late final` is enforced by the compiler: you cannot reassign it. The nullable pattern relies on `??=` which is convention, not enforcement.
- `late final` integrates with Dart's type system: the field's type is `String` (non-nullable). The nullable pattern exposes `String?` internally.
- The nullable pattern is more flexible: you can "reset" by setting `_value = null`. `late final` cannot be reset.

Choose `late final` when you want strict single initialization. Choose the nullable pattern when you need resetability.

---

## Exercise 7: Type-Safe Heterogeneous Container

### Progressive Hints

1. Start with a `TypedKey<T>` class that carries the type parameter. The key does not store the value -- it is a type-safe handle for retrieval.
2. Internally, store values in a `Map<TypedKey, dynamic>`. Yes, `dynamic` is used here -- but it is hidden behind the type-safe API surface. This is one of the legitimate uses of `dynamic`.
3. The `operator []` return type should be `T?` -- this requires a generic method on the container, not on the class. Use a method like `T? get<T>(TypedKey<T> key)` instead of `operator []` (since operators cannot be generic in Dart).
4. For listeners, store them as `Map<TypedKey, List<Function>>` and cast inside the notify method. The type safety comes from the registration API, not the storage.
5. For inheritance (`TypedKey<num>` accepting `int`), Dart's `is` check handles this naturally: `42 is num` is true. Just make sure your `set` method checks `value is T` at runtime as a safety net.

### Complete Solution

```dart
// exercise_07.dart

class TypedKey<T> {
  final String name;
  final T Function()? defaultFactory;

  const TypedKey(this.name, {this.defaultFactory});

  @override
  String toString() => 'TypedKey<$T>($name)';

  @override
  bool operator ==(Object other) =>
      other is TypedKey<T> && other.name == name;

  @override
  int get hashCode => Object.hash(name, T);
}

typedef ChangeListener<T> = void Function(T? oldValue, T newValue);

class TypedRegistry {
  final Map<TypedKey, dynamic> _store = {};
  final Map<TypedKey, List<Function>> _listeners = {};

  T? get<T>(TypedKey<T> key) {
    if (_store.containsKey(key)) {
      return _store[key] as T;
    }
    return key.defaultFactory?.call();
  }

  void set<T>(TypedKey<T> key, T value) {
    var oldValue = _store.containsKey(key) ? _store[key] as T? : null;
    _store[key] = value;
    _notifyListeners(key, oldValue, value);
  }

  bool containsKey<T>(TypedKey<T> key) => _store.containsKey(key);

  T remove<T>(TypedKey<T> key) {
    if (!_store.containsKey(key)) {
      throw StateError('Key $key not found in registry');
    }
    return _store.remove(key) as T;
  }

  void listen<T>(TypedKey<T> key, ChangeListener<T> listener) {
    _listeners.putIfAbsent(key, () => []).add(listener);
  }

  void _notifyListeners<T>(TypedKey<T> key, T? oldValue, T newValue) {
    var keyListeners = _listeners[key];
    if (keyListeners == null) return;
    for (var listener in keyListeners) {
      (listener as ChangeListener<T>)(oldValue, newValue);
    }
  }

  List<TypedKey> get keys => _store.keys.toList();

  int get length => _store.length;
}

// Keys defined as constants for type safety
const portKey = TypedKey<int>('port', defaultFactory: _defaultPort);
const hostKey = TypedKey<String>('host', defaultFactory: _defaultHost);
const debugKey = TypedKey<bool>('debug');
const tagsKey = TypedKey<List<String>>('tags');

int _defaultPort() => 8080;
String _defaultHost() => 'localhost';

void main() {
  var registry = TypedRegistry();

  // Type-safe registration
  registry.set(portKey, 3000);
  registry.set(hostKey, 'example.com');
  registry.set(debugKey, true);
  registry.set(tagsKey, ['api', 'v2']);

  // Type-safe retrieval -- no casts at call site
  int? port = registry.get(portKey);
  String? host = registry.get(hostKey);
  bool? debug = registry.get(debugKey);
  List<String>? tags = registry.get(tagsKey);

  print('Port: $port');
  print('Host: $host');
  print('Debug: $debug');
  print('Tags: $tags');

  // Default values
  var missingKey = TypedKey<int>('missing', defaultFactory: () => 42);
  print('Missing with default: ${registry.get(missingKey)}');

  // Compile-time type safety:
  // registry.set(portKey, 'not an int'); // COMPILE ERROR: String is not int
  // registry.set(hostKey, 123);           // COMPILE ERROR: int is not String

  // Listeners
  registry.listen<int>(portKey, (oldValue, newValue) {
    print('Port changed: $oldValue -> $newValue');
  });

  registry.set(portKey, 9090); // triggers listener

  // Registry info
  print('\nRegistry has ${registry.length} entries:');
  for (var key in registry.keys) {
    print('  $key = ${registry.get(key)}');
  }
}
```

### Detailed Explanation

The core trick is the `TypedKey<T>` class. The type parameter `T` is carried on the key object, and every method that accepts a `TypedKey<T>` uses that same `T` in its return type or parameter type. This creates a type-safe contract at the API boundary.

Internally, the container uses `dynamic` storage. This is acceptable because the API surface prevents type mismatches at compile time. The `as T` casts inside `get` and `remove` are guaranteed safe by the type-safe `set` method. This is a well-known pattern in typed container design: the implementation uses unsafe operations, but the public API makes misuse impossible.

The `defaultFactory` on `TypedKey` is a zero-argument function that returns `T`. This allows keys to define their default values without requiring the registry to know about them. The factory is `const`-compatible because it is a function reference (top-level functions are compile-time constants).

The listener mechanism stores listeners as `List<Function>` (untyped) because Dart's type system cannot express "a map from `TypedKey<T>` to `List<ChangeListener<T>>` for varying T." The cast `listener as ChangeListener<T>` inside `_notifyListeners` is safe because `listen<T>` ensures only correctly-typed listeners are registered for each key.

### Common Mistakes

**Mistake 1: Using `Map<String, dynamic>` and losing type safety entirely.**

The whole point is that keys carry type information. A string key has no type parameter, so retrieval requires casts at every call site.

**Mistake 2: Making `TypedKey` equality based only on `name`.**

Two keys with the same name but different type parameters (`TypedKey<int>('port')` vs `TypedKey<String>('port')`) must be different keys. The `hashCode` includes `T` for this reason.

**Mistake 3: Not handling the listener cast correctly.**

If you stored listeners as `List<ChangeListener<dynamic>>`, you would lose the type information and callers would receive `dynamic` parameters in their callbacks.

### Alternative Approaches

**Extension types (Dart 3.3+)**: You could wrap the key in an extension type for zero-cost abstraction, avoiding the class allocation for keys that are used frequently.

**Sealed class value wrapper**: Instead of `dynamic` storage, wrap values in a sealed class hierarchy that preserves type information in the wrapper. This trades runtime type checks for wrapper allocation.

---

## Exercise 8: Expression Evaluator with Operator Overloading

### Progressive Hints

1. Define the sealed class hierarchy first: `Expr` as the sealed base, with `Literal`, `Variable`, `BinaryOp`, `UnaryOp` as subclasses.
2. Overload `operator +`, `operator -`, `operator *`, `operator /` on `Expr` to return `BinaryOp` nodes. Dart handles precedence automatically because it uses the built-in operator precedence.
3. `Literal` can hold `num` (covering both `int` and `double`). Use `num` type promotion to handle int-vs-double propagation.
4. For `simplify`, use pattern matching on the structure: `BinaryOp(Literal(0), '+', right)` simplifies to `right`.
5. For `toString` with minimal parentheses: track the parent operator's precedence and only add parentheses when the child's precedence is lower.

### Complete Solution

```dart
// exercise_08.dart

sealed class Expr {
  const Expr();

  // Operator overloading for building expression trees
  Expr operator +(Expr other) => BinaryOp(this, '+', other);
  Expr operator -(Expr other) => BinaryOp(this, '-', other);
  Expr operator *(Expr other) => BinaryOp(this, '*', other);
  Expr operator /(Expr other) => BinaryOp(this, '/', other);
  Expr operator -() => UnaryOp('-', this);

  factory Expr.literal(num value) = Literal;
  factory Expr.variable(String name) = Variable;
}

class Literal extends Expr {
  final num value;
  const Literal(this.value);
}

class Variable extends Expr {
  final String name;
  const Variable(this.name);
}

class BinaryOp extends Expr {
  final Expr left;
  final String op;
  final Expr right;
  const BinaryOp(this.left, this.op, this.right);
}

class UnaryOp extends Expr {
  final String op;
  final Expr operand;
  const UnaryOp(this.op, this.operand);
}

// Evaluation result with metadata
typedef EvalResult = ({num value, String type, List<String> warnings});

int _precedence(String op) => switch (op) {
  '+' || '-' => 1,
  '*' || '/' => 2,
  _ => 0,
};

// Evaluate using pattern matching (functional style)
EvalResult evaluate(Expr expr, Map<String, num> vars) {
  var warnings = <String>[];

  num eval(Expr e) => switch (e) {
    Literal(:var value) => value,
    Variable(:var name) => vars[name] ??
        (throw ArgumentError('Undefined variable: $name')),
    UnaryOp(op: '-', :var operand) => -eval(operand),
    BinaryOp(:var left, op: '+', :var right) => eval(left) + eval(right),
    BinaryOp(:var left, op: '-', :var right) => eval(left) - eval(right),
    BinaryOp(:var left, op: '*', :var right) => eval(left) * eval(right),
    BinaryOp(:var left, op: '/', :var right) => () {
      var r = eval(right);
      if (r == 0) {
        warnings.add('Division by zero');
        return double.infinity;
      }
      var l = eval(left);
      // Preserve int when possible
      if (l is int && r is int && l % r == 0) return l ~/ r;
      return l / r;
    }(),
    UnaryOp() => throw ArgumentError('Unknown unary operator: ${e.op}'),
    BinaryOp() => throw ArgumentError('Unknown binary operator: ${e.op}'),
  };

  var result = eval(expr);
  var type = result is int ? 'int' : 'double';
  return (value: result, type: type, warnings: warnings);
}

// Simplify using pattern matching
Expr simplify(Expr expr) => switch (expr) {
  Literal() || Variable() => expr,

  // x + 0 = x, 0 + x = x
  BinaryOp(:var left, op: '+', right: Literal(value: 0)) => simplify(left),
  BinaryOp(left: Literal(value: 0), op: '+', :var right) => simplify(right),

  // x - 0 = x
  BinaryOp(:var left, op: '-', right: Literal(value: 0)) => simplify(left),

  // 0 - x = -x
  BinaryOp(left: Literal(value: 0), op: '-', :var right) =>
      UnaryOp('-', simplify(right)),

  // x * 1 = x, 1 * x = x
  BinaryOp(:var left, op: '*', right: Literal(value: 1)) => simplify(left),
  BinaryOp(left: Literal(value: 1), op: '*', :var right) => simplify(right),

  // x * 0 = 0, 0 * x = 0
  BinaryOp(op: '*', right: Literal(value: 0)) => Literal(0),
  BinaryOp(left: Literal(value: 0), op: '*') => Literal(0),

  // x / 1 = x
  BinaryOp(:var left, op: '/', right: Literal(value: 1)) => simplify(left),

  // Constant folding: both sides are literals
  BinaryOp(left: Literal(:var value), :var op, right: Literal(value: var rv)) =>
      Literal(switch (op) {
        '+' => value + rv,
        '-' => value - rv,
        '*' => value * rv,
        '/' => value == 0 ? double.infinity : value / rv,
        _ => throw ArgumentError('Unknown op: $op'),
      }),

  // Recurse into subexpressions
  BinaryOp(:var left, :var op, :var right) =>
      simplify(BinaryOp(simplify(left), op, simplify(right))),

  UnaryOp(op: '-', operand: Literal(:var value)) => Literal(-value),
  UnaryOp(op: '-', operand: UnaryOp(op: '-', :var operand)) =>
      simplify(operand),
  UnaryOp(:var op, :var operand) => UnaryOp(op, simplify(operand)),
};

// Pretty-print with minimal parentheses
String prettyPrint(Expr expr, {int parentPrecedence = 0}) => switch (expr) {
  Literal(:var value) => value is int ? '$value' : value.toStringAsFixed(2),
  Variable(:var name) => name,
  UnaryOp(op: '-', :var operand) => '-${prettyPrint(operand, parentPrecedence: 3)}',
  BinaryOp(:var left, :var op, :var right) => () {
    var prec = _precedence(op);
    var result = '${prettyPrint(left, parentPrecedence: prec)}'
        ' $op '
        '${prettyPrint(right, parentPrecedence: prec + 1)}';
    return prec < parentPrecedence ? '($result)' : result;
  }(),
  UnaryOp(:var op, :var operand) => '$op${prettyPrint(operand)}',
};

void main() {
  // Build expressions using operator overloading
  var x = Expr.variable('x');
  var y = Expr.variable('y');

  // 2 + 3 * x
  var expr1 = Expr.literal(2) + Expr.literal(3) * x;
  print('Expression: ${prettyPrint(expr1)}');
  var result1 = evaluate(expr1, {'x': 5});
  print('Evaluated (x=5): ${result1.value} (${result1.type})');

  // (x + y) * (x - y) -- difference of squares
  var expr2 = (x + y) * (x - y);
  print('\nExpression: ${prettyPrint(expr2)}');
  var result2 = evaluate(expr2, {'x': 10, 'y': 3});
  print('Evaluated (x=10, y=3): ${result2.value} (${result2.type})');

  // Simplification: x * 1 + 0 => x
  var expr3 = x * Expr.literal(1) + Expr.literal(0);
  print('\nBefore simplify: ${prettyPrint(expr3)}');
  var simplified = simplify(expr3);
  print('After simplify: ${prettyPrint(simplified)}');

  // Constant folding: 2 + 3 => 5
  var expr4 = Expr.literal(2) + Expr.literal(3);
  print('\nBefore fold: ${prettyPrint(expr4)}');
  print('After fold: ${prettyPrint(simplify(expr4))}');

  // Division by zero detection
  var expr5 = x / Expr.literal(0);
  print('\nExpression: ${prettyPrint(expr5)}');
  var result5 = evaluate(expr5, {'x': 10});
  print('Result: ${result5.value}');
  print('Warnings: ${result5.warnings}');

  // Undefined variable detection
  print('\nAttempting evaluation with missing variable:');
  try {
    evaluate(x + y, {'x': 5});
  } on ArgumentError catch (e) {
    print('Caught: $e');
  }

  // Double negation simplification: --x => x
  var expr6 = -(-x);
  print('\nBefore simplify: ${prettyPrint(expr6)}');
  print('After simplify: ${prettyPrint(simplify(expr6))}');

  // Type promotion: int operations stay int
  var intExpr = Expr.literal(10) + Expr.literal(20);
  var intResult = evaluate(intExpr, {});
  print('\nInt expression: ${intResult.value} (${intResult.type})');

  // Mixed int/double promotes to double
  var mixedExpr = Expr.literal(10) + Expr.literal(3.14);
  var mixedResult = evaluate(mixedExpr, {});
  print('Mixed expression: ${mixedResult.value} (${mixedResult.type})');
}
```

### Detailed Explanation

**Sealed class hierarchy**: `sealed class Expr` means the compiler knows all possible subtypes and enforces exhaustive switch coverage. If you add a new `Expr` subclass later, every `switch (expr)` in the codebase will get a compile error until you handle the new case.

**Operator overloading**: Dart lets you overload `+`, `-`, `*`, `/`, and several others. When you write `Expr.literal(2) + Expr.literal(3) * x`, Dart applies its built-in operator precedence: `*` binds tighter than `+`. So the AST is `BinaryOp(Literal(2), '+', BinaryOp(Literal(3), '*', Variable('x')))`. You get correct precedence for free.

**Pattern matching for evaluation**: The `switch` expression destructures each `Expr` subclass and matches on both the type and the operator string. The `Literal(:var value)` syntax extracts the `value` field. The `BinaryOp(:var left, op: '+', :var right)` syntax matches only when `op` equals `'+'`.

**Constant folding in simplify**: When both sides of a `BinaryOp` are `Literal`, the simplifier computes the result at "simplification time" (which could be compile time if you designed it that way). This is the same optimization real compilers perform.

**Minimal parentheses in prettyPrint**: The `parentPrecedence` parameter tracks what operator is "above" the current node. If the current node's operator has lower precedence than the parent, it needs parentheses. This produces `2 + 3 * x` instead of `(2) + ((3) * (x))`.

**Integer preservation**: In the division case, the evaluator checks if both operands are `int` and the division is exact (`l % r == 0`). If so, it uses integer division (`~/`). Otherwise, it uses double division (`/`). This matches how a developer would expect type propagation to work.

### Common Mistakes

**Mistake 1: Forgetting that Dart's `num` arithmetic returns `num`, not `int`.**

`int + int` returns `int` in Dart, but `num + num` returns `num`. When you store values as `num`, you need to track whether the result is actually an `int` to preserve precision information.

**Mistake 2: Not handling operator precedence in toString.**

Without precedence tracking, `(2 + 3) * 4` would print as `2 + 3 * 4`, which evaluates differently. The parentPrecedence parameter solves this.

**Mistake 3: Infinite recursion in simplify.**

If your simplification rules do not make progress (e.g., `simplify(BinaryOp(...))` returns the same `BinaryOp`), you can loop forever. The solution above avoids this by simplifying children first and only matching specific patterns.

### Alternative Approaches

**Visitor pattern (OOP style)**: Instead of external pattern-matching functions, define an `accept(ExprVisitor)` method on `Expr` and implement `EvalVisitor`, `SimplifyVisitor`, `PrintVisitor`. This is more extensible when you want to add new operations but more boilerplate upfront.

**Extension methods**: You could add `evaluate`, `simplify`, and `prettyPrint` as extension methods on `Expr`, keeping the sealed class hierarchy clean while still getting dot-call syntax.

**Compile-time expressions**: With `const` constructors on the `Expr` classes (which are already defined as `const`), you could build expression trees at compile time: `const expr = BinaryOp(Literal(2), '+', Literal(3));`. Evaluation would still be runtime, but tree construction is free.

---

## Debugging Tips for Working with Dart Types

### Inspecting Types at Runtime

When something behaves unexpectedly, add type inspection:

```dart
// debug_types.dart
void main() {
  var value = 42;
  print('Value: $value');
  print('Runtime type: ${value.runtimeType}');
  print('Is int: ${value is int}');
  print('Is num: ${value is num}');
  print('Is double: ${value is double}');
}
```

### Common Type Error Patterns

**"A value of type X can't be assigned to a variable of type Y"**

This means the types are incompatible. Check if you need an explicit conversion (`.toInt()`, `.toDouble()`, `.toString()`) or if you declared the variable with a type that is too narrow.

**"The argument type X can't be assigned to the parameter type Y"**

Same as above, but in a function call context. Check the function signature and your argument types.

**"The method M isn't defined for the type T"**

You are calling a method on a type that does not have it. This often happens with `Object` or `num` when you expected a more specific type. Use `is` checks to promote the type.

**"A nullable expression can't be used as a condition"**

You have a `bool?` where a `bool` is expected. Use `?? false` or an explicit null check.

### The `dart analyze` Command

Run `dart analyze` on your project directory to get all static analysis warnings and errors without running the code. This catches type issues, unused variables, missing returns, and more:

```
dart analyze .
```

### Using `assert` for Development-Time Checks

```dart
// debug_assert.dart
void processPort(int port) {
  assert(port > 0 && port <= 65535, 'Port out of range: $port');
  // In debug mode, this throws if the condition is false
  // In release mode (dart compile), asserts are stripped out
  print('Processing port $port');
}
```

---

## Additional Resources

- [Dart Language Tour](https://dart.dev/language) -- the official comprehensive guide to every language feature
- [Effective Dart](https://dart.dev/effective-dart) -- style, documentation, usage, and design guidelines
- [Dart Type System Deep Dive](https://dart.dev/language/type-system) -- understanding soundness, variance, and type inference
- [Dart Null Safety](https://dart.dev/null-safety/understanding-null-safety) -- Bob Nystrom's thorough explanation of Dart's null safety design
- [Dart Patterns and Records](https://dart.dev/language/patterns) -- full documentation on pattern matching features
- [DartPad](https://dartpad.dev) -- online Dart editor for quick experiments without local setup
- [Dart API Reference](https://api.dart.dev/stable/) -- complete API documentation for all core libraries
