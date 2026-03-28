# Section 03 -- Solutions: Dart Control Flow & Collections

## How to Use This File

Work through README.md exercises first. Come here only when stuck. Each exercise provides: progressive hints, full solution with explanation, common mistakes, and debugging tips.

---

## Exercise 1: Loop Gymnastics

### Hints

**Hint 1**: Task 1 -- `for (final n in numbers)` with `if (n > 10)` inside the body.

**Hint 2**: Task 4 -- use a labeled outer loop `search: for (...)`. When you find a divisor in the inner loop, print it and `continue search;` to jump to the next number.

**Hint 3**: Task 5 -- do-while checks *after* executing the body. Sequence: 12(12), 7(19), 25(44), 3(47), 18(65). Count is 5 because the body runs once more after sum passes 50.

### Solution

```dart
// file: ex01_loop_gymnastics.dart
void main() {
  final numbers = [12, 7, 25, 3, 18, 42, 9, 31, 15, 6];

  print('--- Task 1 ---');
  for (final n in numbers) { if (n > 10) print(n); }

  print('--- Task 2 ---');
  for (var i = 0; i < numbers.length; i++) {
    if (i == 4) continue;
    if (i == 8) break;
    print('[$i] ${numbers[i]}');
  }

  print('--- Task 3 ---');
  var idx = 0;
  while (idx < numbers.length) {
    if (numbers[idx] % 7 == 0) { print('Found: ${numbers[idx]} at index $idx'); break; }
    idx++;
  }

  print('--- Task 4 ---');
  final divisors = [2, 3, 5, 7];
  search:
  for (final n in numbers) {
    for (final d in divisors) {
      if (n % d == 0) { print('$n is divisible by $d'); continue search; }
    }
    print('$n is divisible by $n');
  }

  print('--- Task 5 ---');
  var sum = 0, count = 0, i = 0;
  do { sum += numbers[i]; count++; i++; } while (sum <= 50 && i < numbers.length);
  print('Sum: $sum, Count: $count');
}
```

### Explanation

Task 4: The labeled `continue search` jumps to the outer loop immediately after finding the first divisor. Without the label, `continue` only skips the current inner iteration, and you would print multiple divisors for numbers like 12. For 31 (prime), the inner loop completes without matching, so the fallback prints "31 is divisible by 31".

Task 5: do-while guarantees at least one execution. After adding 3 (sum=47, still <=50), the loop continues. After adding 18 (sum=65, >50), the condition fails and the loop stops. Count is 5 because the body executed 5 times.

### Common Mistakes

- **`continue` without label in Task 4**: Affects inner loop only, prints all matching divisors instead of first.
- **`while` instead of `do-while` in Task 5**: Changes the boundary behavior. The do-while body runs once more after sum exceeds 50.
- **Break after print in Task 2**: If break comes after print at index 8, that index appears in output.

---

## Exercise 2: Collection Toolkit

### Hints

**Hint 1**: `sublist(0, 3)` for first 3, `sublist(cities.length - 3)` for last 3.

**Hint 2**: `putIfAbsent` takes a factory `() => value`, not a plain value. `update` takes a transformer `(old) => new`.

**Hint 3**: FizzBuzz -- check `% 15` before `% 3` or `% 5`. Most specific condition first.

### Solution

```dart
// file: ex02_collection_toolkit.dart
void main() {
  print('--- Task 1 ---');
  final cities = ['Tokyo','Paris','Cairo','Oslo','Lima','Berlin','Dublin','Kyoto','Helsinki','Amsterdam'];
  cities.sort();
  print('First 3: ${cities.sublist(0, 3)}');
  print('Last 3: ${cities.sublist(cities.length - 3)}');

  print('--- Task 2 ---');
  final teamA = {10, 20, 30, 40, 50};
  final teamB = {30, 40, 50, 60, 70};
  print('Union: ${teamA.union(teamB)}');
  print('Intersection: ${teamA.intersection(teamB)}');
  print('Only A: ${teamA.difference(teamB)}');
  print('Only B: ${teamB.difference(teamA)}');

  print('--- Task 3 ---');
  final students = <String, List<int>>{'Alice': [90, 85, 92], 'Bob': [78, 82, 88]};
  students.putIfAbsent('Carol', () => [95, 91]);
  students.update('Bob', (scores) => [...scores, 94]);
  for (final e in students.entries) {
    final avg = e.value.reduce((a, b) => a + b) / e.value.length;
    print('${e.key}: avg=${avg.toStringAsFixed(1)}');
  }

  print('--- Task 4 ---');
  final fb = [
    for (var i = 1; i <= 20; i++)
      if (i % 15 == 0) 'FizzBuzz'
      else if (i % 3 == 0) 'Fizz'
      else if (i % 5 == 0) 'Buzz'
      else '$i',
  ];
  print(fb);
}
```

### Common Mistakes

- **FizzBuzz order**: Checking `% 3` before `% 15` gives "Fizz" for 15 instead of "FizzBuzz".
- **`putIfAbsent('key', [1,2])`**: Second arg must be a function `() => [1,2]`, not a plain list.
- **`scores.add(94)` inside update**: Mutates the list but returns void. Use `[...scores, 94]` to return a new list.

---

## Exercise 3: Pipeline Processor

### Hints

**Hint 1**: Filter refunded once: `final active = transactions.where((t) => !t.refunded);`

**Hint 2**: For Task 2, call `toList()` before `sort` -- Iterables do not have sort. Sort mutates in place.

**Hint 3**: For Task 3, `fold` builds the map: `active.fold<Map<String, List<Transaction>>>({}, (map, t) { map.putIfAbsent(t.category, () => []).add(t); return map; })`.

### Solution

```dart
// file: ex03_pipeline_processor.dart
class Transaction {
  final String category; final double amount; final DateTime date; final bool refunded;
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

  final active = transactions.where((t) => !t.refunded);

  // Task 1
  final total = active.map((t) => t.amount).fold<double>(0, (s, a) => s + a);
  print('Total: \$${total.toStringAsFixed(2)}'); // 369.85

  // Task 2
  final top3 = (active.toList()..sort((a, b) => b.amount.compareTo(a.amount))).take(3);
  for (final t in top3) { print('  ${t.category}: ${t.amount}'); }

  // Task 3
  final grouped = active.fold<Map<String, List<Transaction>>>({}, (m, t) {
    m.putIfAbsent(t.category, () => []).add(t);
    return m;
  });
  for (final e in grouped.entries) {
    final catTotal = e.value.map((t) => t.amount).fold<double>(0, (s, a) => s + a);
    print('${e.key}: ${e.value.length} txns, \$${catTotal.toStringAsFixed(2)}');
  }

  // Task 4
  final monthly = active.fold<Map<String, double>>({}, (m, t) {
    final key = '${t.date.year}-${t.date.month.toString().padLeft(2, '0')}';
    m.update(key, (v) => v + t.amount, ifAbsent: () => t.amount);
    return m;
  });
  for (final e in monthly.entries) { print('${e.key}: \$${e.value.toStringAsFixed(2)}'); }

  // Task 5
  final under50 = grouped.entries
      .where((e) => e.value.every((t) => t.amount < 50))
      .map((e) => e.key).toList();
  print('All under \$50: $under50');
}
```

### Deep Dive: Lazy vs Eager

`where` and `map` return lazy Iterables -- no intermediate lists are created. `fold` drives evaluation, pulling one element at a time through the chain. If you insert `toList()` between steps, you force evaluation into a concrete list, wasting memory. For 11 items it is irrelevant; for millions it matters.

### Common Mistakes

- **`reduce` on possibly empty collection**: Throws. Use `fold` with an initial value.
- **Sorting an Iterable**: `where()` returns Iterable, not List. Call `toList()` first.
- **Dollar sign in string**: Use `\$` for literal dollar signs inside interpolated strings.

---

## Exercise 4: Switch Expression Mastery

### Hints

**Hint 1**: Nested pattern for items: `Success(statusCode: 200, body: {'items': List items})`.

**Hint 2**: OR pattern for auth: `Failure(statusCode: 401 || 403, message: var msg)`.

**Hint 3**: For `classifyValue(Object?)`: literal `0` matches zero; `int n when n < 0` matches negative; `int n when n.isEven` catches remaining positive even; bare `int()` catches positive odd.

### Solution

```dart
// file: ex04_switch_mastery.dart
sealed class ApiResponse {}
class Success extends ApiResponse { final int statusCode; final Map<String, dynamic> body; Success(this.statusCode, this.body); }
class Failure extends ApiResponse { final int statusCode; final String message; Failure(this.statusCode, this.message); }
class Loading extends ApiResponse {}
class Timeout extends ApiResponse { final Duration waited; Timeout(this.waited); }

String describeResponse(ApiResponse r) => switch (r) {
  Success(statusCode: 200, body: {'items': List items}) => 'Loaded ${items.length} items',
  Success(statusCode: 201) => 'Created successfully',
  Success(statusCode: var code) => 'Success: $code',
  Failure(statusCode: 404, message: var m) => 'Not found: $m',
  Failure(statusCode: 401 || 403, message: var m) => 'Auth error: $m',
  Failure(statusCode: >= 500, message: var m) => 'Server error: $m',
  Failure(statusCode: var code, message: var m) => 'Error $code: $m',
  Loading() => 'Loading...',
  Timeout(waited: var d) when d.inSeconds > 30 => 'Timed out after ${d.inSeconds}s (critical)',
  Timeout(waited: var d) => 'Timed out after ${d.inSeconds}s',
};

String classifyValue(Object? v) => switch (v) {
  null => 'empty',
  int n when n < 0 => 'negative integer',
  0 => 'zero',
  int n when n.isEven => 'positive even integer',
  int() => 'positive odd integer',
  double d => 'decimal: $d',
  String s when s.length > 10 => 'long text (${s.length} chars)',
  String() => 'short text',
  List l when l.isEmpty => 'empty list',
  List l when l.length == 1 => 'singleton: ${l.first}',
  List l => 'list of ${l.length}',
  Object v => 'unknown: ${v.runtimeType}',
};

void main() {
  for (final r in [
    Success(200, {'items': [1,2,3]}), Success(201, {}), Success(204, {}),
    Failure(404, 'User not found'), Failure(401, 'Invalid token'),
    Failure(500, 'DB connection lost'), Loading(),
    Timeout(Duration(seconds: 45)), Timeout(Duration(seconds: 10)),
  ]) { print(describeResponse(r)); }

  for (final v in [null, -5, 0, 4, 7, 3.14, 'Hi', 'This is a longer string', [], ['only'], [1,2,3], true]) {
    print(classifyValue(v));
  }
}
```

### Explanation

**Case ordering matters.** Dart evaluates patterns top-to-bottom. `Success(statusCode: var code)` catches all remaining Success values, so specific codes (200, 201) must come first. Similarly, `int n when n < 0` must precede `0` and the general int cases.

The `401 || 403` OR pattern matches either value in one case -- cleaner than duplicating the entire case.

Sealed class exhaustiveness means the compiler verifies you handled Success, Failure, Loading, and Timeout. Remove any case and you get a compile error.

### Common Mistakes

- **General case before specific**: `Success(statusCode: var code)` catches everything, making 200/201 unreachable.
- **Missing `int()` for odd**: After handling negative, zero, and positive even, you still need to match positive odd. `int()` (type check without binding) works as the catch-all for remaining ints.

---

## Exercise 5: In-Memory Data Store

### Hints

**Hint 1**: Primary index: `Map<String, Map<String, dynamic>>` keyed by record id.

**Hint 2**: Secondary: `Map<String, Map<dynamic, Set<String>>>` -- field name -> field value -> set of ids.

**Hint 3**: `query` with AND: intersect id-Sets from each criteria field. First field initializes the set, subsequent fields narrow via `intersection`.

### Solution

```dart
// file: ex05_data_store.dart
class DataStore {
  final Map<String, Map<String, dynamic>> _primary = {};
  final Map<String, Map<dynamic, Set<String>>> _indexes = {};

  void createIndex(String field) {
    _indexes[field] = {};
    for (final e in _primary.entries) {
      final v = e.value[field];
      if (v != null) _indexes[field]!.putIfAbsent(v, () => {}).add(e.key);
    }
  }

  void insert(Map<String, dynamic> record) {
    final id = record['id'] as String;
    _primary[id] = Map.of(record); // copy to prevent external mutation
    for (final f in _indexes.keys) {
      final v = record[f];
      if (v != null) _indexes[f]!.putIfAbsent(v, () => {}).add(id);
    }
  }

  void delete(String id) {
    final record = _primary.remove(id);
    if (record == null) return;
    for (final f in _indexes.keys) {
      final v = record[f];
      if (v != null) {
        _indexes[f]![v]?.remove(id);
        if (_indexes[f]![v]?.isEmpty ?? false) _indexes[f]!.remove(v);
      }
    }
  }

  Map<String, dynamic>? getById(String id) => _primary[id];

  List<Map<String, dynamic>> findByField(String field, dynamic value) {
    if (_indexes.containsKey(field)) {
      return (_indexes[field]![value] ?? {}).map((id) => _primary[id]!).toList();
    }
    return _primary.values.where((r) => r[field] == value).toList();
  }

  List<Map<String, dynamic>> findWhere(bool Function(Map<String, dynamic>) pred) =>
      _primary.values.where(pred).toList();

  List<Map<String, dynamic>> query(Map<String, dynamic> criteria) {
    Set<String>? ids;
    for (final c in criteria.entries) {
      final match = _indexes.containsKey(c.key)
          ? (_indexes[c.key]![c.value] ?? <String>{})
          : _primary.entries.where((e) => e.value[c.key] == c.value).map((e) => e.key).toSet();
      ids = ids == null ? match : ids.intersection(match);
    }
    return (ids ?? {}).map((id) => _primary[id]!).toList();
  }
}
```

### Explanation

The two-level indexing is the key design: primary gives O(1) by id, secondary gives O(1) by any indexed field. The `query` method with AND logic intersects id-Sets, which is O(min(n,m)) per intersection and only shrinks the result.

Storing `Map.of(record)` instead of the reference prevents callers from mutating stored data. Cleaning up empty sets on delete prevents memory leaks in long-running applications.

### Common Mistakes

- **Storing reference instead of copy**: External code can corrupt store data.
- **Not updating indexes on delete**: Stale ids cause null errors on lookup.
- **Forgetting `ifAbsent` in putIfAbsent for indexes**: First insert for a field value needs to create the Set.

---

## Exercise 6: Collection Performance Lab

### Hints

**Hint 1**: `Stopwatch` -- `final sw = Stopwatch()..start(); /* work */ sw.stop(); print(sw.elapsedMicroseconds);`

**Hint 2**: Lazy wins because `take(5)` short-circuits. Only 5 elements pass through the entire chain vs 100K in the eager version.

**Hint 3**: `insert(0, x)` shifts all existing elements right. Total work: 0+1+2+...+(n-1) = O(n^2). `add` is amortized O(1).

### Solution

```dart
// file: ex06_performance_lab.dart
import 'dart:collection';
import 'dart:math';

void main() {
  final rng = Random(42);

  // Task 1: Lazy vs Eager
  final big = List.generate(100000, (i) => i);
  final swE = Stopwatch()..start();
  big.where((n) => n % 3 == 0).toList().map((n) => n * n).toList().take(5).toList();
  swE.stop();
  final swL = Stopwatch()..start();
  big.where((n) => n % 3 == 0).map((n) => n * n).take(5).toList();
  swL.stop();
  print('Eager: ${swE.elapsedMicroseconds}us, Lazy: ${swL.elapsedMicroseconds}us');

  // Task 2: Set vs List contains
  final listD = List.generate(100000, (i) => i);
  final setD = listD.toSet();
  final lookups = List.generate(10000, (_) => rng.nextInt(200000));
  final swLi = Stopwatch()..start();
  for (final v in lookups) { listD.contains(v); }
  swLi.stop();
  final swSe = Stopwatch()..start();
  for (final v in lookups) { setD.contains(v); }
  swSe.stop();
  print('List: ${swLi.elapsedMicroseconds}us, Set: ${swSe.elapsedMicroseconds}us');

  // Task 3: Front insert vs append
  final front = <int>[];
  final swF = Stopwatch()..start();
  for (var i = 0; i < 50000; i++) { front.insert(0, i); }
  swF.stop();
  final back = <int>[];
  final swB = Stopwatch()..start();
  for (var i = 0; i < 50000; i++) { back.add(i); }
  swB.stop();
  print('Front: ${swF.elapsedMilliseconds}ms, Back: ${swB.elapsedMilliseconds}ms');

  // Task 4: Deduplication
  final dupes = List.generate(200000, (_) => rng.nextInt(100000));
  final sw1 = Stopwatch()..start(); final d1 = dupes.toSet().toList(); sw1.stop();
  final sw2 = Stopwatch()..start(); final d2 = LinkedHashSet<int>.from(dupes).toList(); sw2.stop();
  final sw3 = Stopwatch()..start();
  final seen = <int>{}; final d3 = <int>[];
  for (final n in dupes) { if (seen.add(n)) d3.add(n); }
  sw3.stop();
  print('Dedup counts: ${d1.length}, ${d2.length}, ${d3.length} (should match)');
  print('Times: ${sw1.elapsedMicroseconds}us, ${sw2.elapsedMicroseconds}us, ${sw3.elapsedMicroseconds}us');
}
```

### Explanation

Task 1: Lazy `take(5)` short-circuits the entire chain. Only ~15 elements pass through `where` (first 15 multiples of 3) to produce 5 squared values. Eager evaluates all 100K elements through both `where` and `map`, allocating two full intermediate lists.

Task 3: `insert(0, x)` is O(n) because every existing element must shift right by one position in memory. For n insertions, total work is O(n^2). `add` is amortized O(1) thanks to geometric capacity growth.

---

## Exercise 7: JSON Query Engine

### Hints

**Hint 1**: Parse paths into segments: field names, integer indices, wildcards. Regex: `r'([^\.\[\]]+)|\[(\d+|\*)\]'`.

**Hint 2**: Wildcards "fan out" -- map over list elements with remaining path, then flatten results.

**Hint 3**: Use a sealed class for segments (`_FieldSegment`, `_IndexSegment`, `_WildcardSegment`) for exhaustive pattern matching in navigation.

**Hint 4**: Build incrementally: simple dot paths first, then indices, then wildcards, then where/aggregate.

### Solution

```dart
// file: ex07_json_query_engine.dart
sealed class _Seg {}
class _Field extends _Seg { final String name; _Field(this.name); }
class _Index extends _Seg { final int i; _Index(this.i); }
class _Wild extends _Seg {}

class QueryResult {
  final dynamic value;
  QueryResult(this.value);

  QueryResult select(String path) => QueryResult(_nav(value, _parse(path)));
  QueryResult where(String path, bool Function(dynamic) pred) {
    if (value is! List) throw StateError('where requires List');
    return QueryResult((value as List).where((item) => pred(_nav(item, _parse(path)))).toList());
  }
  dynamic aggregate(String op) {
    final list = value as List;
    return switch (op) {
      'sum' => list.fold<num>(0, (s, v) => s + (v as num)),
      'avg' => list.fold<num>(0, (s, v) => s + (v as num)) / list.length,
      'min' => list.cast<num>().reduce((a, b) => a < b ? a : b),
      'max' => list.cast<num>().reduce((a, b) => a > b ? a : b),
      _ => throw ArgumentError('Unknown: $op'),
    };
  }
  QueryResult groupBy(String path) {
    final grouped = <dynamic, List>{};
    for (final item in value as List) {
      grouped.putIfAbsent(_nav(item, _parse(path)), () => []).add(item);
    }
    return QueryResult(grouped);
  }

  static List<_Seg> _parse(String path) {
    final segs = <_Seg>[];
    final raw = path.startsWith('.') ? path.substring(1) : path;
    if (raw.isEmpty) return segs;
    for (final m in RegExp(r'([^\.\[\]]+)|\[(\d+|\*)\]').allMatches(raw)) {
      if (m.group(1) != null) segs.add(_Field(m.group(1)!));
      else if (m.group(2) == '*') segs.add(_Wild());
      else segs.add(_Index(int.parse(m.group(2)!)));
    }
    return segs;
  }

  static dynamic _nav(dynamic cur, List<_Seg> segs) {
    for (var i = 0; i < segs.length; i++) {
      if (cur == null) return null;
      final rest = segs.sublist(i + 1);
      switch (segs[i]) {
        case _Field(name: var f):
          if (cur is List) return cur.expand((e) { final r = _nav(e, [segs[i], ...rest]); return r is List ? r : [r]; }).toList();
          if (cur is Map) { cur = cur[f]; } else { return null; }
        case _Index(i: var idx):
          if (cur is List && idx < cur.length) { cur = cur[idx]; } else { return null; }
        case _Wild():
          if (cur is List) return cur.expand((e) { final r = _nav(e, rest); return r is List ? r : [r]; }).toList();
          return null;
      }
    }
    return cur;
  }
}

class JsonQuery {
  final dynamic _data;
  JsonQuery(this._data);
  QueryResult select(String path) => QueryResult(_data).select(path);
}
```

### Explanation

The hardest part is multi-level wildcards. `departments[*].teams[*].members[*].name` fans out at three levels. At each wildcard, `_nav` maps over list elements and recurses with remaining segments, then flattens. When a field segment encounters a List (from a prior wildcard fan-out), it treats each element as a Map to navigate.

The sealed `_Seg` class gives exhaustive matching in the switch, so adding a new segment type forces handling it everywhere.

### Common Mistakes

- **Not flattening after wildcards**: Produces nested lists `[[Alice, Bob], [Carol, Dave]]` instead of flat `[Alice, Bob, Carol, Dave]`.
- **Mutating QueryResult**: Each method should return new QueryResult, not modify the existing one.

---

## Exercise 8: Reactive Collection with Diff Tracking

### Hints

**Hint 1**: Start with basic list wrapper + listener list. `onChange` adds to list, returns closure that removes.

**Hint 2**: For transactions: boolean `_inTransaction` flag. When true, buffer changes in a list. On end, emit single `BatchChange`.

**Hint 3**: Undo: each Change has an inverse (Insertion <-> Removal, Update swaps old/new). Store on `_undoStack`. Use `_suppressEvents` flag during undo to avoid re-recording.

**Hint 4**: Batch undo must reverse sub-changes in reverse order. Removing at index 3 then 0 is not the same as 0 then 3.

### Solution

```dart
// file: ex08_reactive_collection.dart
sealed class Change<T> {}
class Insertion<T> extends Change<T> { final int index; final T element; Insertion(this.index, this.element); @override String toString() => 'Insertion($index, $element)'; }
class Removal<T> extends Change<T> { final int index; final T element; Removal(this.index, this.element); @override String toString() => 'Removal($index, $element)'; }
class Update<T> extends Change<T> { final int index; final T oldValue, newValue; Update(this.index, this.oldValue, this.newValue); @override String toString() => 'Update($index, $oldValue, $newValue)'; }
class BatchChange<T> extends Change<T> { final List<Change<T>> changes; BatchChange(this.changes); @override String toString() => 'BatchChange($changes)'; }
class Reset<T> extends Change<T> { final List<T> prev; Reset(this.prev); }

class ReactiveList<T> {
  final List<T> _data = [];
  final List<void Function(Change<T>)> _listeners = [];
  final List<Change<T>> _undoStack = [], _redoStack = [];
  bool _inTx = false, _suppress = false;
  List<Change<T>> _pending = [];

  void Function() onChange(void Function(Change<T>) fn) { _listeners.add(fn); return () => _listeners.remove(fn); }

  void _emit(Change<T> c) {
    if (_suppress) return;
    if (_inTx) { _pending.add(c); return; }
    _undoStack.add(c); _redoStack.clear();
    for (final fn in List.of(_listeners)) fn(c);
  }

  void add(T e) { _data.add(e); _emit(Insertion(_data.length - 1, e)); }
  void insert(int i, T e) { _data.insert(i, e); _emit(Insertion(i, e)); }
  void removeAt(int i) { final e = _data.removeAt(i); _emit(Removal(i, e)); }
  void operator []=(int i, T v) { final old = _data[i]; _data[i] = v; _emit(Update(i, old, v)); }
  T operator [](int i) => _data[i];
  List<T> toList() => List.unmodifiable(_data);

  void transaction(void Function() block) {
    _inTx = true; _pending = [];
    block();
    _inTx = false;
    if (_pending.isNotEmpty) {
      final batch = BatchChange(List.of(_pending));
      _undoStack.add(batch); _redoStack.clear();
      for (final fn in List.of(_listeners)) fn(batch);
    }
  }

  void undo() { if (_undoStack.isEmpty) return; final c = _undoStack.removeLast(); _suppress = true; _inverse(c); _suppress = false; _redoStack.add(c); }
  void redo() { if (_redoStack.isEmpty) return; final c = _redoStack.removeLast(); _suppress = true; _forward(c); _suppress = false; _undoStack.add(c); }

  void _inverse(Change<T> c) { switch (c) {
    case Insertion(index: var i): _data.removeAt(i);
    case Removal(index: var i, element: var e): _data.insert(i, e);
    case Update(index: var i, oldValue: var o): _data[i] = o;
    case BatchChange(changes: var cs): for (final x in cs.reversed) _inverse(x);
    case Reset(prev: var p): _data..clear()..addAll(p);
  }}

  void _forward(Change<T> c) { switch (c) {
    case Insertion(index: var i, element: var e): _data.insert(i, e);
    case Removal(index: var i): _data.removeAt(i);
    case Update(index: var i, newValue: var v): _data[i] = v;
    case BatchChange(changes: var cs): for (final x in cs) _forward(x);
    case Reset(): _data.clear();
  }}

  ReactiveList<T> filtered(bool Function(T) pred) {
    final d = ReactiveList<T>();
    for (final item in _data) { if (pred(item)) d._data.add(item); }
    onChange((_) { d._data..clear()..addAll(_data.where(pred)); });
    return d;
  }
}
```

### Explanation

The `_suppress` flag is critical. Without it, undo/redo operations re-emit changes, corrupting the undo stack. Batch undo reverses sub-changes in reverse order because index-based operations are order-dependent: if the batch inserted at index 3 then removed at index 0, undoing in forward order gives wrong indices.

Derived collections use full recomputation on any source change. This is O(n) per change but always correct. An optimized version would analyze the specific Change type and apply a targeted update.

### Common Mistakes

- **Undo pushing to undo stack**: Creates infinite corruption. Always suppress during undo/redo.
- **Batch undo in forward order**: Indices shift as you undo, giving wrong positions. Always reverse.
- **Iterating `_listeners` while unsubscribing**: Use `List.of(_listeners)` to copy before iterating.

### Additional Resources

- [Observer pattern](https://refactoring.guru/design-patterns/observer)
- [Event sourcing](https://martinfowler.com/eaaDev/EventSourcing.html)
- [Dart `collection` package](https://pub.dev/packages/collection)
