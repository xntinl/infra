# Section 03 -- Dart Control Flow & Collections

## Introduction

Every useful program makes decisions and works with groups of data. Control flow determines *which* code runs and *how many times*; collections give you structured containers for data. Dart 3 introduced pattern matching and switch expressions that let the compiler verify you handled every case -- bugs caught at compile time, not production.

## Prerequisites

- **Section 01**: Variables, types, type inference, null safety
- **Section 02**: Functions, closures, higher-order functions
- **Dart SDK 3.0+** installed and on your PATH

## Learning Objectives

1. **Apply** conditional logic using if/else, ternary, and switch expressions
2. **Analyze** data with Dart 3 pattern matching: destructuring, guards, sealed exhaustiveness
3. **Construct** and manipulate Lists, Sets, and Maps
4. **Design** transformation pipelines with Iterable methods (map, where, fold, reduce, expand)
5. **Evaluate** when to use spread, collection-if, and collection-for
6. **Create** custom sorting with Comparable and comparator functions
7. **Synthesize** pattern matching with collection operations for query systems

---

## Core Concepts

### 1. Conditional Expressions

**if/else** for statements with side effects. **Ternary** for simple binary expressions. **Switch expressions** (Dart 3+) for multi-case value mapping with exhaustiveness.

```dart
// file: conditionals_basics.dart
void main() {
  final score = 85;

  // if/else: branches with complex logic
  String grade;
  if (score >= 90) { grade = 'A'; }
  else if (score >= 80) { grade = 'B'; }
  else { grade = 'F'; }
  print('Grade: $grade'); // B

  // Ternary: simple binary choice
  final passed = score >= 60 ? 'Yes' : 'No';

  // Switch expression: multi-case mapping
  final label = switch (score) {
    >= 90 => 'Excellent',
    >= 80 => 'Good',
    _ => 'Needs Improvement',
  };
  print('Label: $label'); // Good
}
```

### 2. Pattern Matching and Switch Expressions

Pattern matching lets the compiler *destructure* values and *verify* exhaustive handling. When you add a variant to a sealed class, the compiler flags every switch that needs updating.

```dart
// file: pattern_matching.dart
sealed class Shape {}
class Circle extends Shape { final double radius; Circle(this.radius); }
class Rectangle extends Shape { final double w, h; Rectangle(this.w, this.h); }

double area(Shape s) => switch (s) {
  Circle(radius: var r) => 3.14159 * r * r,
  Rectangle(w: var w, h: var h) => w * h,
  // No default needed -- sealed guarantees exhaustiveness
};

// Guards with `when` add conditions without nesting if/else:
String classify(double temp, {bool humid = false}) => switch (temp) {
  < 0 => 'Freezing',
  >= 15 && < 25 when humid => 'Warm and muggy',
  >= 15 && < 25 => 'Pleasant',
  >= 25 when humid => 'Hot and oppressive',
  _ => 'Other',
};
```

### 3. Loops

Use **for-in** when you need elements, **for** when you need the index, **while** when iteration count is unknown, **do-while** for at-least-once execution.

```dart
// file: loops_basics.dart
void main() {
  final fruits = ['apple', 'banana', 'cherry'];

  for (final f in fruits) { print(f); }           // for-in
  for (var i = 0; i < fruits.length; i++) {}       // classic for

  // break, continue, and labels
  outer:
  for (var i = 0; i < 3; i++) {
    for (var j = 0; j < 3; j++) {
      if (i == 1 && j == 1) continue outer; // skip to next outer iteration
      print('($i, $j)');
    }
  }
}
```

### 4. Lists, Sets, and Maps

```dart
// file: collections_core.dart
void main() {
  // List: ordered, indexed, growable by default
  final nums = [10, 20, 30];
  nums.add(40);
  nums.sort();
  final sub = nums.sublist(1, 3);
  print(nums.asMap()); // {0: 10, 1: 20, 2: 30, 3: 40}

  // Set: unique values, O(1) lookup
  final backend = {'dart', 'go', 'rust'};
  final frontend = {'dart', 'typescript'};
  print(backend.intersection(frontend)); // {dart}
  print(backend.difference(frontend));   // {go, rust}

  // Map: key-value pairs
  final inv = {'apples': 50, 'bananas': 30};
  inv.putIfAbsent('dates', () => 25);         // add only if missing
  inv.update('apples', (c) => c + 10);        // transform existing
  for (final e in inv.entries) { print('${e.key}: ${e.value}'); }
}
```

### 5. Collection Operators: Spread, Collection-if, Collection-for

Build collections *declaratively* instead of creating empty lists and appending conditionally.

```dart
// file: collection_operators.dart
void main() {
  final base = [1, 2, 3];
  List<int>? maybeNull;

  final combined = [...base, ...?maybeNull, 4, 5]; // spread + null-aware spread

  final isAdmin = true;
  final menu = ['Home', if (isAdmin) 'Admin', 'Settings']; // collection-if

  final squares = [for (var i = 1; i <= 5; i++) i * i];    // collection-for
  print(squares); // [1, 4, 9, 16, 25]
}
```

### 6. Iterable Methods: Functional Pipelines

These transform collections without mutation. `map` and `where` return lazy Iterables; `toList()` forces evaluation.

```dart
// file: iterable_pipelines.dart
void main() {
  final sales = [
    {'product': 'Widget', 'amount': 25.0, 'region': 'North'},
    {'product': 'Gadget', 'amount': 50.0, 'region': 'South'},
    {'product': 'Widget', 'amount': 30.0, 'region': 'North'},
  ];

  // Chained pipeline: North Widget revenue
  final revenue = sales
      .where((s) => s['region'] == 'North')
      .where((s) => s['product'] == 'Widget')
      .map((s) => s['amount'] as double)
      .fold<double>(0, (sum, a) => sum + a);
  print(revenue); // 55.0

  // expand (flatMap), take, skip, every, any
  final nested = [[1, 2], [3, 4]];
  print(nested.expand((l) => l).toList()); // [1, 2, 3, 4]
}
```

### 7. Immutable Collections and Custom Sorting

```dart
// file: immutable_and_sorting.dart
class Employee implements Comparable<Employee> {
  final String name;
  final double salary;
  Employee(this.name, this.salary);

  @override
  int compareTo(Employee other) => other.salary.compareTo(salary); // desc

  @override
  String toString() => '$name(\$$salary)';
}

void main() {
  const colors = ['red', 'green', 'blue']; // compile-time immutable
  final frozen = List<int>.unmodifiable([1, 2, 3]); // runtime immutable

  final team = [Employee('Alice', 95000), Employee('Bob', 80000)];
  team.sort(); // uses Comparable (salary desc)
  team.sort((a, b) => a.name.compareTo(b.name)); // custom comparator
}
```

### 8. Pattern Matching with Collections

```dart
// file: collection_patterns.dart
void main() {
  final coords = [10, 20, 30];
  if (coords case [var x, var y, var z]) { print('3D: ($x,$y,$z)'); }

  final scores = [95, 88, 76, 92];
  if (scores case [var first, ...var rest]) { print('First: $first'); }

  final user = {'name': 'Alice', 'role': 'admin'};
  if (user case {'name': String name, 'role': 'admin'}) {
    print('Admin: $name');
  }

  final resp = {'status': 200, 'data': {'items': [1, 2, 3]}};
  final msg = switch (resp) {
    {'status': 200, 'data': {'items': [_, _, ...]}} => 'Success with 2+ items',
    {'status': int code} when code >= 400 => 'Error: $code',
    _ => 'Unknown',
  };
}
```

---

## Exercises

### Exercise 1 (Basic): Loop Gymnastics

Write a program that processes a list of integers using all loop types.

```dart
// file: ex01_loop_gymnastics.dart
void main() {
  final numbers = [12, 7, 25, 3, 18, 42, 9, 31, 15, 6];

  // Task 1: for-in loop, print only numbers > 10
  // Task 2: classic for with index, skip index 4 (continue), stop at index 8 (break)
  // Task 3: while loop to find first number divisible by 7
  // Task 4: nested loops with label -- for each number, find FIRST divisor in [2,3,5,7]
  //         If none found (prime like 31), print it is divisible by itself
  // Task 5: do-while to sum numbers until total > 50
}
```

**Verification**: `dart run ex01_loop_gymnastics.dart`
```
--- Task 1 ---
12, 25, 18, 42, 31, 15
--- Task 2 ---
[0] 12, [1] 7, [2] 25, [3] 3, [5] 42, [6] 9, [7] 31
--- Task 3 ---
Found: 7 at index 1
--- Task 4 ---
12/2, 7/7, 25/5, 3/3, 18/2, 42/2, 9/3, 31/31, 15/3, 6/2
--- Task 5 ---
Sum: 65, Count: 5
```

---

### Exercise 2 (Basic): Collection Toolkit

```dart
// file: ex02_collection_toolkit.dart
void main() {
  // Task 1: Create 10 city names, sort alphabetically, print first 3 and last 3 via sublist
  // Task 2: Two Sets of team scores -- print union, intersection, difference each way
  // Task 3: Map<String, List<int>> of students->scores. Use putIfAbsent for new student,
  //         update to append score. Print each student with average.
  // Task 4: Single list literal using collection-for + collection-if for FizzBuzz 1-20
}
```

**Verification**: `dart run ex02_collection_toolkit.dart`

Task 4 expected:
```
[1, 2, Fizz, 4, Buzz, Fizz, 7, 8, Fizz, Buzz, 11, Fizz, 13, 14, FizzBuzz, 16, 17, Fizz, 19, Buzz]
```

---

### Exercise 3 (Intermediate): Pipeline Processor

Chain Iterable methods to build data transformation pipelines over transaction records.

```dart
// file: ex03_pipeline_processor.dart
class Transaction {
  final String category;
  final double amount;
  final DateTime date;
  final bool refunded;
  Transaction(this.category, this.amount, this.date, {this.refunded = false});
}

void main() {
  final transactions = [
    Transaction('food', 23.50, DateTime(2024, 1, 5)),
    Transaction('transport', 45.00, DateTime(2024, 1, 8)),
    Transaction('food', 12.75, DateTime(2024, 1, 12)),
    Transaction('entertainment', 60.00, DateTime(2024, 1, 15), refunded: true),
    Transaction('food', 34.20, DateTime(2024, 1, 18)),
    Transaction('utilities', 120.00, DateTime(2024, 1, 20)),
    Transaction('transport', 15.00, DateTime(2024, 1, 22)),
    Transaction('entertainment', 25.00, DateTime(2024, 1, 25)),
    Transaction('food', 8.90, DateTime(2024, 1, 28)),
    Transaction('utilities', 85.50, DateTime(2024, 2, 1)),
    Transaction('transport', 30.00, DateTime(2024, 2, 5), refunded: true),
  ];

  // Task 1: Total spent excluding refunded (where + map + fold). Expected: 369.85
  // Task 2: Top 3 highest non-refunded (sort desc, take 3)
  // Task 3: Group by category excluding refunded (fold into Map<String, List<Transaction>>)
  // Task 4: Monthly totals excluding refunded (fold into Map<String, double>)
  // Task 5: Categories where ALL transactions < $50 (use grouped map + every)
}
```

**Verification**: Total = 369.85. Monthly: 2024-01: 284.35, 2024-02: 85.50. Under-$50 categories: food, transport.

---

### Exercise 4 (Intermediate): Switch Expression Mastery

```dart
// file: ex04_switch_mastery.dart
sealed class ApiResponse {}
class Success extends ApiResponse { final int statusCode; final Map<String, dynamic> body; Success(this.statusCode, this.body); }
class Failure extends ApiResponse { final int statusCode; final String message; Failure(this.statusCode, this.message); }
class Loading extends ApiResponse {}
class Timeout extends ApiResponse { final Duration waited; Timeout(this.waited); }

// Task 1: describeResponse(ApiResponse) -> String using switch expression
//   Success 200 with body['items'] as List -> "Loaded N items"
//   Success 201 -> "Created successfully"
//   Failure 404 -> "Not found: <msg>", 401|403 -> "Auth error: <msg>", >=500 -> "Server error"
//   Loading -> "Loading...", Timeout >30s -> "critical", else seconds

// Task 2: classifyValue(Object?) -> String
//   null->"empty", negative int, zero, positive even/odd, double, short/long String,
//   empty/singleton/multi List, else "unknown: <runtimeType>"

void main() {
  final responses = [
    Success(200, {'items': [1, 2, 3]}), Success(201, {}), Success(204, {}),
    Failure(404, 'User not found'), Failure(401, 'Invalid token'),
    Failure(500, 'DB connection lost'), Loading(),
    Timeout(Duration(seconds: 45)), Timeout(Duration(seconds: 10)),
  ];
  for (final r in responses) { print(describeResponse(r)); }

  final values = [null, -5, 0, 4, 7, 3.14, 'Hi', 'This is a longer string', [], ['only'], [1,2,3], true];
  for (final v in values) { print(classifyValue(v)); }
}
```

**Verification**: Compiler should error if any case is missing (sealed exhaustiveness). Expected output:
```
Loaded 3 items
Created successfully
Success: 204
Not found: User not found
Auth error: Invalid token
Server error: DB connection lost
Loading...
Timed out after 45s (critical)
Timed out after 10s
empty
negative integer
zero
positive even integer
positive odd integer
decimal: 3.14
short text
long text (23 chars)
empty list
singleton: only
list of 3
unknown: bool
```

---

### Exercise 5 (Advanced): In-Memory Data Store

Design a queryable data store using Maps for primary/secondary indexes and Sets for id lookups.

```dart
// file: ex05_data_store.dart
// Build DataStore class that:
// 1. Stores records as Map<String, dynamic> with 'id' field
// 2. Primary index: Map<String, Map<String, dynamic>> for O(1) by id
// 3. Secondary indexes via createIndex('field') -> Map<dynamic, Set<String>>
// 4. Query methods:
//    - getById(id) -> record?
//    - findByField(field, value) -> List<record> (uses index if available)
//    - findWhere(predicate) -> List<record>
//    - query(Map criteria) -> List<record> (AND logic via set intersection)
// 5. Indexes stay in sync on insert and delete

void main() {
  final store = DataStore();
  store.createIndex('department');
  store.createIndex('role');

  for (final emp in [
    {'id': 'e1', 'name': 'Alice', 'department': 'Engineering', 'role': 'Senior', 'salary': 95000},
    {'id': 'e2', 'name': 'Bob', 'department': 'Engineering', 'role': 'Junior', 'salary': 65000},
    {'id': 'e3', 'name': 'Carol', 'department': 'Marketing', 'role': 'Senior', 'salary': 88000},
    {'id': 'e4', 'name': 'Dave', 'department': 'Marketing', 'role': 'Junior', 'salary': 55000},
    {'id': 'e5', 'name': 'Eve', 'department': 'Engineering', 'role': 'Lead', 'salary': 120000},
  ]) { store.insert(emp); }

  print(store.getById('e3')?['name']);  // Carol
  print(store.findByField('department', 'Engineering').map((e) => e['name'])); // (Alice, Bob, Eve)
  print(store.query({'department': 'Engineering', 'role': 'Senior'}).map((e) => e['name'])); // (Alice)
  store.delete('e1');
  print(store.findByField('department', 'Engineering').map((e) => e['name'])); // (Bob, Eve)
}
```

---

### Exercise 6 (Advanced): Collection Performance Lab

```dart
// file: ex06_performance_lab.dart
// Task 1: Lazy vs eager -- chain where->map->take(5) on 100K elements.
//         Compare with/without intermediate toList(). Time with Stopwatch.
// Task 2: Set.contains vs List.contains -- 10K lookups on 100K elements. Time both.
// Task 3: insert(0,...) vs add() for 50K elements. Explain WHY front is O(n) per op.
// Task 4: Deduplication -- compare toSet().toList(), LinkedHashSet.from(), and
//         manual seen-set approach on 200K ints with ~50% dupes. Verify same count.

void main() {
  // Run all four tasks with timing output.
  // Expected ratios: lazy 100x+ faster, Set 100x+ faster, front-insert dramatically slower
}
```

---

### Exercise 7 (Insane): JSON Query Engine

Build a mini jq-like query language over nested Map/List structures using pattern matching.

```dart
// file: ex07_json_query_engine.dart
// Build JsonQuery that supports:
// - select('.path.to[0].field') -- dot-path navigation
// - select('.arr[*].name') -- wildcard: collect values from all array elements
// - where('.field', predicate) -- filter arrays
// - aggregate('sum'|'avg'|'min'|'max') -- reduce numeric arrays
// - groupBy('.field') -- group array elements by a field value
// Use pattern matching to handle Map/List/null/primitive at each navigation step.

void main() {
  final data = {
    'company': 'TechCorp',
    'departments': [
      { 'name': 'Engineering', 'budget': 500000, 'teams': [
          { 'name': 'Backend', 'members': [
              {'name': 'Alice', 'role': 'Lead', 'salary': 120000},
              {'name': 'Bob', 'role': 'Senior', 'salary': 95000}]},
          { 'name': 'Frontend', 'members': [
              {'name': 'Carol', 'role': 'Lead', 'salary': 115000},
              {'name': 'Dave', 'role': 'Junior', 'salary': 65000}]}]},
      { 'name': 'Marketing', 'budget': 300000, 'teams': [
          { 'name': 'Digital', 'members': [
              {'name': 'Eve', 'role': 'Lead', 'salary': 105000},
              {'name': 'Frank', 'role': 'Senior', 'salary': 88000}]}]}
    ]
  };
  final q = JsonQuery(data);

  print(q.select('.company').value);                          // TechCorp
  print(q.select('.departments[0].name').value);              // Engineering
  print(q.select('.departments[*].name').value);              // [Engineering, Marketing]
  print(q.select('.departments[*].teams[*].members[*].name').value);
  // [Alice, Bob, Carol, Dave, Eve, Frank]
  print(q.select('.departments').where('.budget', (b) => (b as int) > 400000)
      .select('[*].name').value);                             // [Engineering]
  print(q.select('.departments[*].teams[*].members[*].salary')
      .aggregate('sum'));                                     // 588000
}
```

**Verification**: Build incrementally. First get simple dot paths working, then array indices, then wildcards, then where/aggregate. Each step should pass its tests before moving to the next.

---

### Exercise 8 (Insane): Reactive Collection with Diff Tracking

Build a reactive list that notifies on mutations with precise diffs, supports undo/redo and derived collections.

```dart
// file: ex08_reactive_collection.dart
// ReactiveList<T> that:
// 1. Intercepts mutations (add, remove, insert, update, clear)
// 2. Emits typed Change events: Insertion, Removal, Update, BatchChange, Reset
// 3. Multiple listeners via onChange(callback) returning unsubscribe function
// 4. transaction(() { ... }) batches mutations into single BatchChange
// 5. Derived collections: filtered(predicate) and mapped(transform)
//    return new ReactiveList that auto-syncs with source
// 6. undo()/redo() using stack of inverse changes
//    Batch undo reverses all sub-changes in reverse order

void main() {
  final list = ReactiveList<String>();
  final unsub = list.onChange((c) => print('Change: $c'));

  list.add('Alice');      // Change: Insertion(0, Alice)
  list.add('Bob');        // Change: Insertion(1, Bob)
  list.insert(1, 'Zara'); // Change: Insertion(1, Zara)
  list[2] = 'Bobby';      // Change: Update(2, Bob, Bobby)
  list.removeAt(0);       // Change: Removal(0, Alice)
  print('Current: ${list.toList()}'); // [Zara, Bobby, Carol...]

  list.transaction(() { list.add('Dave'); list.add('Eve'); list.removeAt(0); });
  // Single BatchChange event

  list.undo();  // reverts entire batch
  list.redo();  // re-applies batch

  final short = list.filtered((n) => n.length <= 4);
  list.add('Al');
  print('Short: ${short.toList()}'); // auto-updated

  unsub(); // stop notifications
}
```

**Verification**: Build layer by layer. First get basic add/remove with events. Then transaction batching. Then undo/redo. Derived collections last. Undo of a batch must reverse sub-changes in reverse order to keep indices correct.

---

## Summary

- **Control flow**: if/else for statements, ternary for expressions, switch expressions for multi-case
- **Pattern matching**: destructuring, `when` guards, sealed exhaustiveness
- **Loops**: for, for-in, while, do-while, break/continue with labels
- **Collections**: List (ordered), Set (unique, fast lookup), Map (key-value)
- **Declarative building**: spread, collection-if, collection-for
- **Pipelines**: map, where, fold, reduce, expand, take, skip, every, any
- **Immutability**: const and unmodifiable wrappers
- **Performance**: lazy vs eager, right collection type for the job

## What's Next

**Section 04: Dart OOP** -- classes, inheritance, mixins, extension methods. The collection operations and patterns from this section will combine with OOP to build clean, type-safe domain models.

## References

- [Dart Language Tour: Branches](https://dart.dev/language/branches)
- [Dart Language Tour: Collections](https://dart.dev/language/collections)
- [Dart Language Tour: Patterns](https://dart.dev/language/patterns)
- [Dart API: Iterable](https://api.dart.dev/stable/dart-core/Iterable-class.html)
- [Effective Dart: Usage](https://dart.dev/effective-dart/usage)
