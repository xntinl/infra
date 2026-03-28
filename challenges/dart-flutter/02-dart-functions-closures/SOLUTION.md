# Section 02: Solutions -- Dart Functions & Closures

## How to Use This File

Work through each exercise in the README before looking here. For each exercise, this file provides:

1. **Progressive hints** -- read one at a time, try again after each
2. **Full solution** with line-by-line explanation
3. **Common mistakes** and how to recognize them
4. **Deep dives** into the underlying concepts
5. **Alternative approaches** worth knowing

---

## Exercise 1: Function Declaration Sampler

### Hints

**Hint 1:** Arrow syntax `=>` replaces `{ return ...; }`. It only works for a single expression. If you need multiple statements, use braces.

**Hint 2:** Named parameters go inside `{}` in the declaration. Optional positional parameters go inside `[]`. You cannot mix `{}` and `[]` in the same function.

**Hint 3:** An anonymous function stored in a variable needs an explicit type on the variable (or use `final` with inference). The function itself does not have a name after the `=`.

### Solution

```dart
// file: exercises/ex01_declaration_sampler.dart

double celsiusToFahrenheit(double celsius) => (celsius * 9 / 5) + 32;

String formatTemperature({required double value, String unit = 'C'}) {
  return '$value $unit';
}

final isPositive = (int n) => n > 0;

String describeNumber(int number, [String label = 'value']) {
  final parity = number.isEven ? 'even' : 'odd';
  return '$label: $number is $parity';
}

void main() {
  assert(celsiusToFahrenheit(0) == 32.0);
  assert(celsiusToFahrenheit(100) == 212.0);
  print('celsiusToFahrenheit: OK');

  assert(formatTemperature(value: 23.5) == '23.5 C');
  assert(formatTemperature(value: 73.4, unit: 'F') == '73.4 F');
  print('formatTemperature: OK');

  assert(isPositive(5) == true);
  assert(isPositive(-1) == false);
  assert(isPositive(0) == false);
  print('isPositive: OK');

  assert(describeNumber(4) == 'value: 4 is even');
  assert(describeNumber(3, 'count') == 'count: 3 is odd');
  print('describeNumber: OK');

  print('--- Exercise 1 PASSED ---');
}
```

### Explanation

- `celsiusToFahrenheit` uses arrow syntax because the body is a single arithmetic expression. The return type `double` is explicit to make the API clear.
- `formatTemperature` uses named parameters because both `value` and `unit` are conceptually distinct; `value` is `required` because there is no sensible default temperature.
- `isPositive` is stored in a `final` variable. Dart infers its type as `bool Function(int)`. Notice that `0` returns `false` -- zero is not positive.
- `describeNumber` uses optional positional syntax `[]` because the `label` has a clear default and the call site reads naturally without a name (`describeNumber(4)` vs. `describeNumber(4, label: 'value')`).

### Common Mistakes

**Mistake 1:** Using `=>` with multiple statements.
```dart
// WRONG: arrow syntax does not support multiple statements.
double celsiusToFahrenheit(double c) => {
  var result = c * 9 / 5;
  return result + 32; // Syntax error
}
```
The `=>` expects exactly one expression. If you need intermediate variables, use the block form with `{}` and `return`.

**Mistake 2:** Forgetting `required` on named parameters without defaults.
```dart
// WRONG: value has no default and is not required.
String formatTemperature({double value, String unit = 'C'}) { ... }
// Dart 3 will give: "The parameter 'value' can't have a value of 'null'
// because of its type, but the implicit default value is 'null'."
```

**Mistake 3:** Writing `isPositive(0)` expecting `true`. Zero is neither positive nor negative. The `>` operator correctly excludes it.

---

## Exercise 2: Higher-Order Function Warm-up

### Hints

**Hint 1:** `map` transforms each element and returns an `Iterable`. Call `.toList()` to get a `List` back.

**Hint 2:** For `applyDiscount`, the formula is `price * (1 - discountPercent / 100)`. A 10% discount on 100.0 yields 90.0, not 10.0.

**Hint 3:** `fold` requires a type parameter when the accumulator type differs from the element type. Even when they match, being explicit helps: `fold<double>(0.0, ...)`.

### Solution

```dart
// file: exercises/ex02_higher_order_warmup.dart

List<double> applyDiscount(List<double> prices, double discountPercent) {
  return prices.map((p) => p * (1 - discountPercent / 100)).toList();
}

List<double> filterExpensive(List<double> prices, double threshold) {
  return prices.where((p) => p > threshold).toList();
}

double totalCost(List<double> prices) {
  return prices.fold<double>(0.0, (sum, p) => sum + p);
}

List<double> expandBundles(List<List<double>> bundles) {
  return bundles.expand((bundle) => bundle).toList();
}

void main() {
  final prices = [29.99, 49.99, 9.99, 79.99, 14.99];

  final discounted = applyDiscount(prices, 10);
  assert(discounted.length == 5);
  assert((discounted[0] - 26.991).abs() < 0.01);
  print('applyDiscount: OK');

  final expensive = filterExpensive(prices, 30.0);
  assert(expensive.length == 2);
  assert(expensive.contains(49.99));
  assert(expensive.contains(79.99));
  print('filterExpensive: OK');

  final total = totalCost(prices);
  assert((total - 184.95).abs() < 0.01);
  print('totalCost: OK');

  final bundles = [[10.0, 20.0], [30.0], [40.0, 50.0]];
  final flat = expandBundles(bundles);
  assert(flat.length == 5);
  assert((totalCost(flat) - 150.0).abs() < 0.01);
  print('expandBundles: OK');

  print('--- Exercise 2 PASSED ---');
}
```

### Explanation

- `applyDiscount` uses `map` to produce a new list. The original list is untouched -- this is the functional style. The discount formula converts the percentage to a multiplier first.
- `filterExpensive` uses `where`, which is Dart's `filter`. The predicate returns `true` for items to keep.
- `totalCost` uses `fold` with an explicit initial value of `0.0`. The `<double>` type parameter tells Dart the accumulator is a `double`. Without it, you would get type inference issues if the list were empty.
- `expandBundles` uses `expand`, which is a flatMap operation. Each inner list is yielded element-by-element into the output.

### Common Mistakes

**Mistake 1:** Using `reduce` instead of `fold` for `totalCost`.
```dart
// WRONG: reduce throws on empty lists.
double totalCost(List<double> prices) => prices.reduce((a, b) => a + b);
// StateError: "No element" when prices is empty.
```
`fold` is safer because it has an initial value. Use `reduce` only when you are certain the list is non-empty and the seed should be the first element.

**Mistake 2:** Forgetting `.toList()` after `map` or `where`.
```dart
// This returns Iterable<double>, not List<double>.
List<double> applyDiscount(...) => prices.map((p) => p * 0.9);
// Type error: Iterable<double> is not List<double>.
```

**Mistake 3:** Discount formula inverted.
```dart
// WRONG: this adds the discount instead of subtracting.
prices.map((p) => p * (1 + discountPercent / 100))
```

### Deep Dive: Lazy vs. Eager

`map`, `where`, and `expand` return lazy `Iterable` objects. They do not compute results until you iterate. This means chaining `.where(...).map(...)` does not create intermediate lists -- it creates a pipeline that processes each element through both operations in a single pass. Calling `.toList()` triggers the full evaluation. In performance-sensitive code, staying lazy as long as possible avoids unnecessary allocations.

---

## Exercise 3: Data Pipeline Builder

### Hints

**Hint 1:** Store steps as `List<List<T> Function(List<T>)>`. The `execute` method iterates through steps, feeding the output of each into the next.

**Hint 2:** For `removeLast`, use the `List.removeLast()` method. Guard against calling it on an empty list.

**Hint 3:** The `from` factory is a named constructor. Use `Pipeline<T>()..` syntax or just assign the steps directly.

### Solution

```dart
// file: exercises/ex03_pipeline.dart

class Pipeline<T> {
  final List<List<T> Function(List<T>)> _steps = [];

  Pipeline();

  Pipeline.from(List<List<T> Function(List<T>)> steps) {
    _steps.addAll(steps);
  }

  void addStep(List<T> Function(List<T>) step) {
    _steps.add(step);
  }

  void removeLast() {
    if (_steps.isNotEmpty) {
      _steps.removeLast();
    }
  }

  List<T> execute(List<T> input) {
    var current = input;
    for (final step in _steps) {
      current = step(current);
    }
    return current;
  }
}

void main() {
  final pipeline = Pipeline<int>();
  pipeline.addStep((list) => list.where((n) => n.isEven).toList());
  pipeline.addStep((list) => list.map((n) => n * 2).toList());
  pipeline.addStep((list) => list..sort((a, b) => b.compareTo(a)));
  pipeline.addStep((list) => list.take(3).toList());

  final input = [5, 3, 8, 1, 4, 7, 2, 9, 6, 10];
  final result = pipeline.execute(input);
  assert(result.length == 3);
  assert(result[0] == 20);
  assert(result[1] == 16);
  assert(result[2] == 12);
  print('Pipeline step-by-step: OK');

  pipeline.removeLast();
  final fullResult = pipeline.execute(input);
  assert(fullResult.length == 5);
  print('removeLast: OK');

  final quickPipe = Pipeline<int>.from([
    (list) => list.where((n) => n > 5).toList(),
    (list) => list.map((n) => n * 10).toList(),
  ]);
  final quickResult = quickPipe.execute([1, 2, 6, 8, 3]);
  assert(quickResult.length == 2);
  assert(quickResult.contains(60));
  assert(quickResult.contains(80));
  print('Pipeline.from factory: OK');

  final empty = Pipeline<int>();
  assert(empty.execute([1, 2, 3]).length == 3);
  print('Empty pipeline: OK');

  print('--- Exercise 3 PASSED ---');
}
```

### Explanation

The `Pipeline` is essentially `fold` in disguise. The `execute` method folds over the list of steps, using the input as the initial value and each step function as the reducer. You could write it as:

```dart
List<T> execute(List<T> input) {
  return _steps.fold(input, (current, step) => step(current));
}
```

Both forms are equivalent. The explicit loop is slightly easier to debug because you can add logging between steps.

The sort step uses `list..sort(...)` -- the cascade operator (`..`) modifies the list in place and returns it. This is important because `List.sort()` returns `void`. Without the cascade, you would need a separate line.

### Common Mistakes

**Mistake 1:** Using `fold` in `execute` but with the wrong type annotation.
```dart
// WRONG: Dart cannot infer that fold's accumulator is List<T>.
return _steps.fold(input, (current, step) => step(current));
// Fix: add type parameter.
return _steps.fold<List<T>>(input, (current, step) => step(current));
```

**Mistake 2:** Mutating the input list in a step.
```dart
// DANGEROUS: sort() modifies in place.
pipeline.addStep((list) { list.sort(); return list; });
// This mutates the original input. Use: list.toList()..sort() instead.
```

**Mistake 3:** Forgetting that `removeLast` on an empty list throws a `StateError`.

### Alternative Approach: Immutable Pipeline

Instead of mutating the steps list, return a new `Pipeline` from each operation. This is more functional but less practical for this exercise:

```dart
Pipeline<T> withStep(List<T> Function(List<T>) step) {
  return Pipeline.from([..._steps, step]);
}
```

---

## Exercise 4: Memoization with Closures

### Hints

**Hint 1:** The cache `Map` must live inside the returned closure, not outside `memoize`. Each call to `memoize` creates a fresh cache.

**Hint 2:** For `memoize2`, you need a composite key. The simplest approach is a string: `'$a|$b'`. For production code, you would use a record `(T1, T2)` as the key.

**Hint 3:** The returned function must have the same signature as the original. Type parameters ensure this.

### Solution

```dart
// file: exercises/ex04_memoize.dart

R Function(T) memoize<T, R>(R Function(T) fn) {
  final cache = <T, R>{};
  return (T arg) {
    if (cache.containsKey(arg)) {
      return cache[arg] as R;
    }
    final result = fn(arg);
    cache[arg] = result;
    return result;
  };
}

R Function(T1, T2) memoize2<T1, T2, R>(R Function(T1, T2) fn) {
  final cache = <String, R>{};
  return (T1 a, T2 b) {
    final key = '$a|$b';
    if (cache.containsKey(key)) {
      return cache[key] as R;
    }
    final result = fn(a, b);
    cache[key] = result;
    return result;
  };
}

void main() {
  int callCount = 0;

  int expensiveSquare(int n) {
    callCount++;
    return n * n;
  }

  final memoSquare = memoize(expensiveSquare);
  assert(memoSquare(4) == 16);
  assert(memoSquare(4) == 16);
  assert(memoSquare(5) == 25);
  assert(callCount == 2);
  print('memoize single-arg: OK');

  callCount = 0;
  final memoAbs = memoize((int n) {
    callCount++;
    return n.abs();
  });
  memoAbs(-3);
  memoAbs(3);
  memoAbs(-3);
  assert(callCount == 2);
  print('memoize separate keys: OK');

  int addCallCount = 0;
  int expensiveAdd(int a, int b) {
    addCallCount++;
    return a + b;
  }

  final memoAdd = memoize2(expensiveAdd);
  assert(memoAdd(3, 4) == 7);
  assert(memoAdd(3, 4) == 7);
  assert(memoAdd(4, 3) == 7);
  assert(addCallCount == 2);
  print('memoize2: OK');

  final memoUpper = memoize((String s) => s.toUpperCase());
  assert(memoUpper('hello') == 'HELLO');
  assert(memoUpper('hello') == 'HELLO');
  print('memoize with strings: OK');

  print('--- Exercise 4 PASSED ---');
}
```

### Explanation

The key insight is that `cache` lives in the closure's scope. When `memoize` returns the inner function, that function retains a reference to `cache`. Every subsequent call to the returned function accesses the same `Map`. This is the closure pattern at its most practical: invisible, private state attached to a function.

For `memoize2`, the string key `'$a|$b'` works for most types because Dart's `toString` produces distinct strings for distinct values. However, this breaks for objects with ambiguous `toString` output. A Dart 3 record `(T1, T2)` used as a map key would be more robust since records implement `==` and `hashCode` based on their fields.

### Common Mistakes

**Mistake 1:** Declaring the cache outside the closure.
```dart
// WRONG: all memoized functions share the same cache.
final _globalCache = {};
R Function(T) memoize<T, R>(R Function(T) fn) {
  return (arg) { ... _globalCache ... };
}
```

**Mistake 2:** Using `cache[arg]` without `containsKey` check, then hitting null issues.
```dart
// WRONG: if R is non-nullable, cache[arg] returns R?, causing type error.
return cache[arg] ?? fn(arg); // Does not store result!
```

**Mistake 3:** For `memoize2`, using a separator that could appear in the arguments themselves.
```dart
// FRAGILE: if a = "hello|world" and b = "foo", key collides with
// a = "hello" and b = "world|foo".
final key = '$a|$b';
```
For production code, use records: `final key = (a, b);` which avoids this entirely.

### Deep Dive: Memory Implications

A memoized function holds strong references to every input and output it has ever seen. In a long-running application, this is a memory leak. Solutions include:
- LRU (Least Recently Used) eviction with a max cache size
- TTL (Time To Live) expiration -- see Exercise 7
- Weak references (Dart has `WeakReference` and `Expando` but they are limited)

---

## Exercise 5: Middleware Chain via Function Composition

### Hints

**Hint 1:** `composeMiddleware` folds the middleware list from right to left. The rightmost middleware wraps the final handler first, then the next middleware wraps that result, and so on.

**Hint 2:** Use `List.reversed` and `fold` (or `foldRight` manually). The initial value for the fold is the final `Handler`.

**Hint 3:** Each middleware receives the "next" handler and returns a new handler. The logging middleware should run its logic then call `next(request)`.

### Solution

```dart
// file: exercises/ex05_middleware.dart

class Request {
  final String path;
  final Map<String, String> headers;
  final String body;

  Request(this.path, {this.headers = const {}, this.body = ''});
}

class Response {
  final int statusCode;
  final String body;
  final Map<String, String> headers;

  Response(this.statusCode, this.body, {this.headers = const {}});
}

typedef Handler = Response Function(Request);
typedef Middleware = Handler Function(Handler);

Handler composeMiddleware(List<Middleware> middlewares, Handler handler) {
  return middlewares.reversed.fold(handler, (next, middleware) {
    return middleware(next);
  });
}

void main() {
  final logs = <String>[];

  Middleware loggingMiddleware = (Handler next) {
    return (Request req) {
      logs.add(req.path);
      return next(req);
    };
  };

  Middleware authMiddleware = (Handler next) {
    return (Request req) {
      if (!req.headers.containsKey('Authorization')) {
        return Response(401, 'Unauthorized');
      }
      return next(req);
    };
  };

  Middleware timingMiddleware = (Handler next) {
    return (Request req) {
      final sw = Stopwatch()..start();
      final response = next(req);
      sw.stop();
      return Response(
        response.statusCode,
        response.body,
        headers: {...response.headers, 'X-Duration': '${sw.elapsedMicroseconds}us'},
      );
    };
  };

  final Handler finalHandler = (Request req) {
    return Response(200, 'OK: ${req.path}');
  };

  final app = composeMiddleware(
    [loggingMiddleware, authMiddleware, timingMiddleware],
    finalHandler,
  );

  final validReq = Request('/api/data', headers: {'Authorization': 'Bearer xyz'});
  final resp = app(validReq);
  assert(resp.statusCode == 200);
  assert(resp.body.contains('/api/data'));
  assert(logs.contains('/api/data'));
  print('Valid request: OK');

  logs.clear();
  final noAuthReq = Request('/api/secret');
  final noAuthResp = app(noAuthReq);
  assert(noAuthResp.statusCode == 401);
  assert(logs.contains('/api/secret'));
  print('Auth rejection: OK');

  final timedResp = app(validReq);
  assert(timedResp.headers.containsKey('X-Duration'));
  print('Timing header: OK');

  final naked = composeMiddleware([], finalHandler);
  final nakedResp = naked(Request('/raw'));
  assert(nakedResp.statusCode == 200);
  print('Empty middleware: OK');

  final single = composeMiddleware([loggingMiddleware], finalHandler);
  logs.clear();
  single(Request('/single'));
  assert(logs.contains('/single'));
  print('Single middleware: OK');

  print('--- Exercise 5 PASSED ---');
}
```

### Explanation

The composition follows the "onion" model. If middleware are `[A, B, C]` and the handler is `H`, the composed handler is `A(B(C(H)))`. When a request arrives:
1. A runs its pre-logic, calls B
2. B runs its pre-logic, calls C
3. C runs its pre-logic, calls H
4. H produces a response
5. C runs post-logic (timing wraps the response)
6. B runs post-logic
7. A runs post-logic

The `fold` with `reversed` achieves this by starting with `H`, wrapping it with `C`, then `B`, then `A`. Without `reversed`, the order would be inverted.

The logging middleware captures the `logs` list from its outer scope -- this is a closure over mutable external state, which is intentional here (the middleware needs to report somewhere).

The auth middleware short-circuits: when it returns a 401 response without calling `next`, the remaining middleware and handler never execute.

### Common Mistakes

**Mistake 1:** Folding left-to-right without reversing.
```dart
// WRONG: this makes the LAST middleware the outermost wrapper.
middlewares.fold(handler, (next, mw) => mw(next));
// The first middleware in the list should be outermost.
```

**Mistake 2:** Mutating the response headers using `[]=` on an unmodifiable map.
```dart
// WRONG: const {} is unmodifiable.
response.headers['X-Duration'] = '...'; // Runtime error.
// Fix: create a new Response with spread headers.
```

**Mistake 3:** Forgetting that auth middleware logs should still appear. The logging middleware is outermost, so it always runs. If auth were outer and logging inner, unauthorized requests would not be logged.

### Deep Dive: This Is How Real Frameworks Work

This pattern is used in:
- **shelf** (Dart HTTP server): middleware is `Handler Function(Handler)`
- **Express.js**: `app.use((req, res, next) => ...)`
- **Redux**: middleware wraps the dispatch function

The key insight is that a function that takes a function and returns a function of the same type is infinitely composable. You can stack as many as you want without changing the interface.

---

## Exercise 6: Closure Memory Analysis with Generators

### Hints

**Hint 1:** The generator function uses `sync*` and `yield`. It should increment a counter for each resource ID. The `activeCount` and `disposedIds` variables are captured from the outer scope.

**Hint 2:** The `ResourcePool` needs an `Iterator` from the generator, not the `Iterable`. Call `.iterator` once and use `moveNext()` / `current` to pull values on demand.

**Hint 3:** The double-dispose safety comes from the `_disposed` flag in `Resource.release()`. The closure decrements `activeCount`, but `release` guards the call.

### Solution

```dart
// file: exercises/ex06_closure_generators.dart

class Resource {
  final int id;
  final void Function() dispose;
  bool _disposed = false;

  Resource(this.id, this.dispose);

  bool get isDisposed => _disposed;

  void release() {
    if (!_disposed) {
      dispose();
      _disposed = true;
    }
  }
}

Iterable<Resource> resourceGenerator({
  required List<int> disposedIds,
  required int Function() getActive,
  required void Function(int) setActive,
}) sync* {
  int nextId = 1;
  while (true) {
    final id = nextId++;
    setActive(getActive() + 1);
    yield Resource(id, () {
      setActive(getActive() - 1);
      disposedIds.add(id);
    });
  }
}

class ResourcePool {
  final Iterator<Resource> _iterator;
  final List<Resource> _active = [];

  ResourcePool(Iterable<Resource> generator)
      : _iterator = generator.iterator;

  Resource allocate() {
    _iterator.moveNext();
    final resource = _iterator.current;
    _active.add(resource);
    return resource;
  }

  void release(Resource resource) {
    resource.release();
    _active.remove(resource);
  }

  void closeAll() {
    for (final resource in _active) {
      resource.release();
    }
    _active.clear();
  }

  int get activeResourceCount => _active.length;
}

void main() {
  final disposedIds = <int>[];
  int activeCount = 0;

  final gen = resourceGenerator(
    disposedIds: disposedIds,
    getActive: () => activeCount,
    setActive: (v) => activeCount = v,
  );

  final pool = ResourcePool(gen);

  // Verification step 1: allocate resources.
  final r1 = pool.allocate();
  final r2 = pool.allocate();
  final r3 = pool.allocate();
  assert(activeCount == 3);
  assert(r1.id == 1);
  assert(r2.id == 2);
  assert(r3.id == 3);
  print('Allocation: OK');

  // Verification step 2: dispose one resource.
  pool.release(r1);
  assert(activeCount == 2);
  assert(disposedIds.contains(1));
  print('Single dispose: OK');

  // Verification step 3: double dispose is safe.
  r1.release(); // Already disposed, should be no-op.
  assert(activeCount == 2);
  print('Double dispose safety: OK');

  // Verification step 4: closeAll disposes remaining.
  pool.closeAll();
  assert(activeCount == 0);
  assert(disposedIds.length == 3);
  print('Close all: OK');

  // Verification step 5: generator produces new ids after pool reset.
  final r4 = pool.allocate();
  final r5 = pool.allocate();
  assert(activeCount == 2);
  assert(r4.id == 4);
  assert(r5.id == 5);
  print('Continued generation: OK');

  print('--- Exercise 6 PASSED ---');
}
```

### Explanation

The generator uses `sync*` with an infinite `while (true)` loop. It yields one `Resource` at a time and suspends until the consumer asks for the next. This is the essential property of generators: they produce values on demand.

Each `Resource`'s `dispose` closure captures the `disposedIds` list and the `activeCount` access functions from the generator's scope. When `release()` calls `dispose()`, the closure modifies the shared mutable state. The `_disposed` flag ensures this happens at most once per resource.

The `ResourcePool` holds an `Iterator`, not an `Iterable`. This is important: each call to `.iterator` on an `Iterable` from a `sync*` function creates a new iterator that starts from the beginning. By holding a single iterator, we ensure resource IDs continue incrementing across multiple `allocate()` calls.

### Common Mistakes

**Mistake 1:** Calling `.iterator` on every `allocate` call.
```dart
// WRONG: creates a new iterator each time, restarting from id=1.
Resource allocate() {
  final iter = _iterable.iterator;
  iter.moveNext();
  return iter.current;
}
```

**Mistake 2:** Capturing `activeCount` by value in the closure instead of by reference.
```dart
// WRONG: this captures the value of activeCount at creation time.
final currentActive = activeCount;
yield Resource(id, () {
  activeCount = currentActive - 1; // Always restores to creation-time value.
});
```
The solution uses getter/setter callbacks to ensure the closure always reads the current value.

**Mistake 3:** Forgetting that `closeAll` iterates a list while modifying it.
```dart
// WRONG: removing from _active while iterating causes ConcurrentModificationError.
for (final r in _active) {
  release(r); // This removes r from _active during iteration!
}
```
The solution iterates `_active` but only calls `resource.release()` (not `pool.release()`), then clears the list after the loop.

### Deep Dive: Generator Lifecycle

When a `sync*` function yields, its execution state is frozen: local variables, the program counter, everything. When the consumer calls `moveNext()`, execution resumes from exactly where it left off. This is cooperative multitasking at the function level. The generator does not "know" when it will be resumed -- it just yields and waits.

This is the same mechanism that powers `async*` generators for streams, except those can also `await` between yields.

---

## Exercise 7: Functional Programming Toolkit

### Hints

**Hint 1:** `curry2` transforms `f(a, b)` into `g(a)(b)`. The outer function takes `a` and returns a new function that takes `b` and calls the original with both.

**Hint 2:** `compose(f, g)` means `f(g(x))` -- right to left. `pipe(f, g)` means `g(f(x))` -- left to right. They are mirrors.

**Hint 3:** `composeN` and `pipeN` use `fold`/`reduce` on the function list. For `composeN`, fold from the right; for `pipeN`, fold from the left.

**Hint 4:** `memoizeWithTTL` stores both the result and the timestamp. On cache hit, check if `DateTime.now().difference(storedTime) > ttl`.

### Solution

```dart
// file: exercises/ex07_fp_toolkit.dart

R Function(B) Function(A) curry2<A, B, R>(R Function(A, B) fn) {
  return (A a) => (B b) => fn(a, b);
}

R Function(C) Function(B) Function(A) curry3<A, B, C, R>(
    R Function(A, B, C) fn) {
  return (A a) => (B b) => (C c) => fn(a, b, c);
}

R Function(B) partial<A, B, R>(R Function(A, B) fn, A a) {
  return (B b) => fn(a, b);
}

C Function(A) compose<A, B, C>(C Function(B) f, B Function(A) g) {
  return (A a) => f(g(a));
}

C Function(A) pipe<A, B, C>(B Function(A) f, C Function(B) g) {
  return (A a) => g(f(a));
}

dynamic Function(dynamic) composeN(List<dynamic Function(dynamic)> fns) {
  return fns.reversed.reduce((composed, fn) {
    return (dynamic x) => composed(fn(x));
  });
}

dynamic Function(dynamic) pipeN(List<dynamic Function(dynamic)> fns) {
  return fns.reduce((composed, fn) {
    return (dynamic x) => fn(composed(x));
  });
}

R Function(T) memoizeWithTTL<T, R>(
  R Function(T) fn, {
  required Duration ttl,
}) {
  final cache = <T, ({R value, DateTime timestamp})>{};
  return (T arg) {
    final entry = cache[arg];
    if (entry != null && DateTime.now().difference(entry.timestamp) < ttl) {
      return entry.value;
    }
    final result = fn(arg);
    cache[arg] = (value: result, timestamp: DateTime.now());
    return result;
  };
}

void main() async {
  int add(int a, int b) => a + b;
  final curriedAdd = curry2(add);
  assert(curriedAdd(3)(4) == 7);
  final add10 = curriedAdd(10);
  assert(add10(5) == 15);
  print('curry2: OK');

  String joinThree(String a, String b, String c) => '$a-$b-$c';
  final curriedJoin = curry3(joinThree);
  assert(curriedJoin('x')('y')('z') == 'x-y-z');
  print('curry3: OK');

  final add5 = partial(add, 5);
  assert(add5(3) == 8);
  assert(add5(0) == 5);
  print('partial: OK');

  final double2 = (int n) => n * 2;
  final toString_ = (int n) => 'val:$n';
  final composed = compose<int, int, String>(toString_, double2);
  assert(composed(5) == 'val:10');
  print('compose: OK');

  final piped = pipe<int, int, String>(double2, toString_);
  assert(piped(5) == 'val:10');
  print('pipe: OK');

  final addOne = (dynamic n) => (n as int) + 1;
  final triple = (dynamic n) => (n as int) * 3;
  final negate = (dynamic n) => -(n as int);
  final composedN = composeN([negate, triple, addOne]);
  assert(composedN(2) == -9);
  print('composeN: OK');

  final pipedN = pipeN([addOne, triple, negate]);
  assert(pipedN(2) == -9);
  print('pipeN: OK');

  int ttlCallCount = 0;
  final memoSlow = memoizeWithTTL<int, int>(
    (n) {
      ttlCallCount++;
      return n * n;
    },
    ttl: Duration(milliseconds: 200),
  );

  assert(memoSlow(4) == 16);
  assert(memoSlow(4) == 16);
  assert(ttlCallCount == 1);
  print('memoizeWithTTL cached: OK');

  await Future.delayed(Duration(milliseconds: 300));
  assert(memoSlow(4) == 16);
  assert(ttlCallCount == 2);
  print('memoizeWithTTL expired: OK');

  print('--- Exercise 7 PASSED ---');
}
```

### Explanation

**curry2 and curry3:** Currying converts a multi-argument function into a chain of single-argument functions. `curry2(add)` returns a function that takes `a` and returns a function that takes `b` and finally computes `add(a, b)`. This is useful for creating specialized versions: `curry2(add)(10)` gives you an "add 10" function.

**partial:** Partial application fixes one argument upfront. The difference from currying: `partial` takes the function AND the first argument together, returning a function of the remaining arguments. Currying takes only the function and returns a chain.

**compose and pipe:** These combine two functions into one. `compose(f, g)` reads right-to-left (mathematical notation), while `pipe(f, g)` reads left-to-right (data flow notation). The choice is stylistic; many developers find `pipe` more intuitive.

**composeN and pipeN:** These generalize to N functions using `reduce`. The `composeN` reverses the list first so that the rightmost function runs first. The `pipeN` processes left-to-right naturally with `reduce`. Both use `dynamic` because Dart's type system cannot express a heterogeneous chain of typed transformations in a list.

**memoizeWithTTL:** The cache stores Dart 3 records `({R value, DateTime timestamp})`. On each call, we check whether the cached entry has expired by comparing the current time against the stored timestamp. If expired, we recompute and store the fresh result. This prevents the unbounded memory growth of a plain memoize.

### Common Mistakes

**Mistake 1:** Confusing compose and pipe order.
```dart
// compose(f, g)(x) = f(g(x))  -- g runs first
// pipe(f, g)(x)    = g(f(x))  -- f runs first
// If you reverse them, all assertions fail.
```

**Mistake 2:** Using `fold` with an identity function as initial value for `composeN`.
```dart
// This works but is less clean:
fns.fold<dynamic Function(dynamic)>((x) => x, (acc, fn) => (x) => acc(fn(x)));
// reduce is simpler when the list is guaranteed non-empty.
```

**Mistake 3:** TTL comparison using `>` instead of `>=` or vice versa.
```dart
// Using > means exactly-at-TTL values are still cached.
// Using >= means they expire. The test uses a 300ms delay for a 200ms TTL,
// so either works. But be intentional about boundary behavior.
```

### Alternative: Type-Safe Pipe with Extensions

For type safety without `dynamic`, you can build a typed pipeline using extension methods:

```dart
extension Pipe<A> on A {
  B pipe<B>(B Function(A) fn) => fn(this);
}

// Usage:
final result = 5.pipe(double2).pipe(toString_);
// Fully typed at each step.
```

This avoids the type erasure of `composeN`/`pipeN` but requires a different calling convention.

---

## Exercise 8: Lazy Evaluation Engine

### Hints

**Hint 1:** `Lazy<T>` needs three things: the thunk (`T Function()`), a nullable `_value` field, and an `_isEvaluated` bool. The `value` getter checks the flag and either returns the cached result or evaluates the thunk.

**Hint 2:** `LazySequence` wraps an `Iterable<T> Function()` -- a function that produces an iterable. This is critical: storing the function (not the iterable) ensures re-evaluation is possible and the generator is not consumed prematurely.

**Hint 3:** `lazyMap`, `lazyWhere`, and `lazyTake` each return a NEW `LazySequence` whose generator function wraps the original. They do not evaluate anything -- they just build up a description of the computation.

**Hint 4:** For `lazyZip`, create a generator that manually advances two iterators in lockstep. Use Dart 3 records `(T, R)` for the pairs.

### Solution

```dart
// file: exercises/ex08_lazy_engine.dart

class Lazy<T> {
  final T Function() _thunk;
  late final T _value;
  bool _isEvaluated = false;

  Lazy(this._thunk);

  T get value {
    if (!_isEvaluated) {
      _value = _thunk();
      _isEvaluated = true;
    }
    return _value;
  }

  bool get isEvaluated => _isEvaluated;

  Lazy<R> map<R>(R Function(T) transform) {
    return Lazy(() => transform(value));
  }

  Lazy<R> flatMap<R>(Lazy<R> Function(T) transform) {
    return Lazy(() => transform(value).value);
  }
}

class LazySequence<T> {
  final Iterable<T> Function() _generator;

  LazySequence(this._generator);

  factory LazySequence.generate(T Function(int index) factory) {
    return LazySequence(() sync* {
      int i = 0;
      while (true) {
        yield factory(i++);
      }
    });
  }

  LazySequence<R> lazyMap<R>(R Function(T) transform) {
    return LazySequence(() sync* {
      for (final item in _generator()) {
        yield transform(item);
      }
    });
  }

  LazySequence<T> lazyWhere(bool Function(T) test) {
    return LazySequence(() sync* {
      for (final item in _generator()) {
        if (test(item)) {
          yield item;
        }
      }
    });
  }

  LazySequence<T> lazyTake(int count) {
    return LazySequence(() sync* {
      int taken = 0;
      for (final item in _generator()) {
        if (taken >= count) break;
        yield item;
        taken++;
      }
    });
  }

  LazySequence<(T, R)> lazyZip<R>(LazySequence<R> other) {
    return LazySequence(() sync* {
      final iterA = _generator().iterator;
      final iterB = other._generator().iterator;
      while (iterA.moveNext() && iterB.moveNext()) {
        yield (iterA.current, iterB.current);
      }
    });
  }

  List<T> toList() => _generator().toList();
}

void main() {
  int evalCount = 0;
  final lazy = Lazy(() {
    evalCount++;
    return 42;
  });
  assert(evalCount == 0);
  assert(lazy.value == 42);
  assert(evalCount == 1);
  assert(lazy.value == 42);
  assert(evalCount == 1);
  print('Lazy deferred eval: OK');

  int mapEvalCount = 0;
  final lazyMapped = Lazy(() {
    mapEvalCount++;
    return 10;
  }).map((n) => n * 2);
  assert(mapEvalCount == 0);
  assert(lazyMapped.value == 20);
  assert(mapEvalCount == 1);
  print('Lazy.map: OK');

  int genCount = 0;
  final seq = LazySequence(() sync* {
    for (int i = 0; i < 100; i++) {
      genCount++;
      yield i;
    }
  });

  final transformed = seq
      .lazyWhere((n) => n.isEven)
      .lazyMap((n) => n * 10)
      .lazyTake(5);

  assert(genCount == 0);
  final result = transformed.toList();
  assert(result.length == 5);
  assert(result[0] == 0);
  assert(result[1] == 20);
  assert(result[4] == 80);
  assert(genCount < 100);
  print('LazySequence chaining: OK');

  final naturals = LazySequence.generate((i) => i + 1);
  final firstTen = naturals.lazyTake(10).toList();
  assert(firstTen.length == 10);
  assert(firstTen.last == 10);
  print('Infinite sequence: OK');

  final letters = LazySequence(() sync* {
    yield 'a'; yield 'b'; yield 'c';
  });
  final nums = LazySequence(() sync* {
    yield 1; yield 2; yield 3;
  });
  final zipped = letters.lazyZip(nums).toList();
  assert(zipped.length == 3);
  assert(zipped[0] == ('a', 1));
  assert(zipped[2] == ('c', 3));
  print('LazySequence.zip: OK');

  print('--- Exercise 8 PASSED ---');
}
```

### Explanation

**Lazy<T>** implements the "thunk" pattern from functional programming. A thunk is a zero-argument function that defers a computation. The `late final` keyword on `_value` means Dart allocates space for it but does not initialize it until the first assignment. Combined with `_isEvaluated`, this guarantees single evaluation.

`Lazy.map` does NOT evaluate the original thunk. It creates a new `Lazy` whose thunk calls the original's `value` getter (which triggers evaluation only when the new Lazy is itself evaluated). This is composition of deferred computations.

**LazySequence<T>** stores a function that returns an `Iterable`, not the iterable itself. This is the fundamental difference from a regular list. Each `lazyMap`, `lazyWhere`, `lazyTake` wraps the previous generator function in a new one that applies the transformation. No computation happens until `toList()` (or any iteration) triggers the chain.

The `genCount < 100` assertion is the proof of laziness. The sequence has 100 elements, but we only need 5 even numbers (0, 2, 4, 6, 8). The generator produces elements 0 through 8 (nine elements) and stops because `lazyTake(5)` breaks the loop. Without laziness, all 100 elements would be generated.

**LazySequence.generate** creates an infinite sequence. The generator function contains `while (true)` -- it never terminates on its own. This only works because generators are lazy: they yield one value and suspend. The `lazyTake` downstream ensures we only pull a finite number of values.

**lazyZip** manually advances two iterators in lockstep. It stops when either iterator is exhausted. The result is a sequence of Dart 3 records `(T, R)`.

### Common Mistakes

**Mistake 1:** Storing `Iterable<T>` instead of `Iterable<T> Function()`.
```dart
// WRONG: the iterable is consumed on first toList() call.
class LazySequence<T> {
  final Iterable<T> _iterable; // Once consumed, gone forever.
}
```
By storing the factory function, each call to `toList()` creates a fresh iterable from a fresh generator invocation.

**Mistake 2:** Using `_generator().toList()` inside `lazyMap`, which triggers eager evaluation.
```dart
// WRONG: this evaluates the entire source sequence immediately.
LazySequence<R> lazyMap<R>(R Function(T) transform) {
  final all = _generator().toList(); // Eager!
  return LazySequence(() => all.map(transform));
}
```

**Mistake 3:** Infinite sequence without `lazyTake`.
```dart
// HANGS: toList() on an infinite generator never terminates.
final all = LazySequence.generate((i) => i).toList(); // Out of memory.
```
Always apply `lazyTake` before materializing an infinite sequence.

**Mistake 4:** Using `for (final item in _generator())` inside `lazyZip` for both sources.
```dart
// WRONG: you cannot interleave two for-in loops.
// You need manual iterator control with moveNext()/current.
```

### Deep Dive: Why Lazy Evaluation Matters

Lazy evaluation is not just an academic exercise. It appears in:
- **Flutter's `ListView.builder`**: builds only visible widgets, not all 10,000
- **Dart's `Iterable` methods**: `map`, `where`, `take` are all lazy
- **Stream processing**: events are produced and consumed on demand
- **Pagination**: fetch the next page only when the user scrolls

The `LazySequence` you built is a simplified version of what Dart's `Iterable` already does internally. Understanding this mechanism helps you avoid premature materialization (calling `.toList()` too early) and write memory-efficient data pipelines.

### Debugging Tips

If your lazy sequence produces wrong results:
1. Add `print` statements inside the generator to trace which elements are actually produced
2. Check that `genCount` matches expectations -- it reveals whether laziness is working
3. Test each operation in isolation before chaining: does `lazyWhere` alone work? Does `lazyMap` alone work?
4. For infinite sequences, always test with `lazyTake` first to avoid hangs

---

## Additional Resources

- [Dart Language Tour: Functions](https://dart.dev/language/functions) -- the official reference for all function syntax
- [Effective Dart](https://dart.dev/effective-dart) -- style and design guidelines that apply to function design
- [Dart Records](https://dart.dev/language/records) -- used in Exercise 7 (TTL cache) and Exercise 8 (zip)
- [Functional Programming in Dart](https://dart.dev/guides/language/language-tour#functions) -- community patterns
- [dartz package](https://pub.dev/packages/dartz) -- a mature functional programming library for Dart if you want to see production-grade implementations of the patterns from Exercise 7
