# Section 02: Dart Functions & Closures

## Introduction

In most languages, functions are second-class citizens -- they exist to be called and nothing more. Dart takes a different stance. Functions in Dart are objects. They have a type, they can be stored in variables, passed as arguments, and returned from other functions. This property, called "first-class functions," is not just a theoretical nicety. It unlocks patterns like callbacks, middleware chains, lazy evaluation, and functional pipelines that would otherwise require verbose class hierarchies.

This section builds your fluency with Dart's function system from basic declarations through closures, higher-order functions, generators, and ultimately toward functional programming patterns you will use daily in Flutter (widget builders, event handlers, stream transformers).

## Prerequisites

- Section 01 completed (variables, types, type inference, `final`/`const`)
- Dart SDK installed and `dart run` working from terminal
- Familiarity with basic control flow (`if`, `for`, `while`)

## Learning Objectives

By the end of this section you will be able to:

1. **Declare** functions using named, anonymous, and arrow syntax and explain when each form is appropriate
2. **Differentiate** between positional, named, required, and optional parameters with default values
3. **Apply** first-class function concepts by assigning functions to variables, passing them as arguments, and returning them
4. **Analyze** how closures capture variables from their lexical scope, including mutable state
5. **Construct** data transformation pipelines using higher-order functions (`map`, `where`, `fold`, `reduce`, `forEach`, `expand`)
6. **Define** function types using `typedef` and use them as contracts
7. **Implement** extension methods, recursive patterns, and tear-offs
8. **Create** generator functions using `sync*` and `async*` for lazy sequences and streams
9. **Design** a function composition library with curry, partial application, pipe, and compose
10. **Build** a lazy evaluation engine combining closures with generators

---

## Core Concepts

### 1. Function Declaration Styles

Dart gives you three ways to write a function. The choice is not cosmetic -- each form communicates intent.

```dart
// file: lib/01_declaration_styles.dart

// Named function: use for top-level and class-level logic.
// The return type is explicit. The body uses braces.
int multiply(int a, int b) {
  return a * b;
}

// Arrow syntax: use when the body is a single expression.
// The arrow (=>) replaces { return ...; } -- it is NOT a lambda,
// it is shorthand for a one-expression body.
int square(int x) => x * x;

// Anonymous function (lambda): use when passing behavior inline.
// Stored in a variable here for clarity.
final greet = (String name) {
  return 'Hello, $name';
};

// Anonymous with arrow syntax: the shortest form.
final double toDouble = (int n) => n.toDouble();

void main() {
  print(multiply(3, 4));    // 12
  print(square(5));          // 25
  print(greet('Dart'));      // Hello, Dart
  print(toDouble(7));        // 7.0
}
```

A named function declared at the top level is hoisted -- you can call it before its declaration in the file. An anonymous function stored in a variable follows normal variable scoping rules.

### 2. Parameters: Positional, Named, Optional, Default

Parameter design is API design. Dart forces you to be explicit about what callers must provide and what they may omit.

```dart
// file: lib/02_parameters.dart

// Required positional parameters: order matters, all mandatory.
String formatName(String first, String last) => '$first $last';

// Optional positional parameters: wrapped in []. Order still matters.
// Default values via =.
String formatGreeting(String name, [String title = 'Mr.']) {
  return 'Hello, $title $name';
}

// Named parameters: wrapped in {}. Order does not matter at call site.
// Use 'required' keyword when the param must be provided.
double calculatePrice({
  required double base,
  double taxRate = 0.21,
  double discount = 0.0,
}) {
  return base * (1 + taxRate) - discount;
}

void main() {
  print(formatName('Ada', 'Lovelace'));
  // Ada Lovelace

  print(formatGreeting('Turing'));
  // Hello, Mr. Turing

  print(formatGreeting('Hopper', 'Rear Admiral'));
  // Hello, Rear Admiral Hopper

  print(calculatePrice(base: 100.0));
  // 121.0

  print(calculatePrice(base: 100.0, discount: 10.0, taxRate: 0.10));
  // 100.0
}
```

Rule of thumb: use named parameters when a function takes more than two arguments or when the arguments are the same type (two `double` values next to each other are a bug waiting to happen).

### 3. First-Class Functions

"First-class" means functions are values. You can do everything with a function that you can do with an integer or a string.

```dart
// file: lib/03_first_class.dart

// A function that takes another function as an argument.
int applyOperation(int a, int b, int Function(int, int) operation) {
  return operation(a, b);
}

// A function that returns a function.
int Function(int) makeAdder(int addend) {
  return (int value) => value + addend;
}

void main() {
  // Assign a function to a variable.
  final subtract = (int a, int b) => a - b;

  print(applyOperation(10, 3, subtract));  // 7
  print(applyOperation(10, 3, (a, b) => a * b)); // 30

  // Store the returned function.
  final addFive = makeAdder(5);
  print(addFive(10)); // 15
  print(addFive(20)); // 25

  // Functions in collections.
  final operations = <String, int Function(int, int)>{
    'add': (a, b) => a + b,
    'sub': (a, b) => a - b,
    'mul': (a, b) => a * b,
  };

  operations.forEach((name, fn) {
    print('$name(6, 2) = ${fn(6, 2)}');
  });
}
```

### 4. Closures and Lexical Scope

A closure is a function that remembers the variables from the scope where it was created, even after that scope has finished executing. Think of it as a function carrying a backpack of captured variables.

```dart
// file: lib/04_closures.dart

// The returned function "closes over" the variable count.
// Each call to makeCounter() creates a NEW count variable,
// so each counter is independent.
Function makeCounter() {
  int count = 0;
  return () {
    count++;
    return count;
  };
}

// Closure over mutable state: the inner function sees
// the SAME variable, not a copy.
List<Function> buildMultipliers(List<int> factors) {
  final multipliers = <Function>[];
  for (final factor in factors) {
    multipliers.add((int x) => x * factor);
  }
  return multipliers;
}

void main() {
  final counterA = makeCounter();
  final counterB = makeCounter();

  print(counterA()); // 1
  print(counterA()); // 2
  print(counterB()); // 1 -- independent state

  final multipliers = buildMultipliers([2, 3, 5]);
  print(multipliers[0](10)); // 20
  print(multipliers[1](10)); // 30
  print(multipliers[2](10)); // 50
}
```

The classic closure trap occurs when capturing a loop variable by reference in languages with `var`-style scoping. Dart's `for (final ...)` creates a new binding per iteration, avoiding this trap. However, if you use a mutable variable declared outside the loop, all closures will share it.

### 5. Higher-Order Functions

Higher-order functions either take functions as arguments, return functions, or both. Dart's `Iterable` provides a rich set.

```dart
// file: lib/05_higher_order.dart

void main() {
  final numbers = [1, 2, 3, 4, 5, 6, 7, 8, 9, 10];

  // map: transform each element.
  final squares = numbers.map((n) => n * n).toList();
  print(squares); // [1, 4, 9, 16, 25, 36, 49, 64, 81, 100]

  // where: filter elements.
  final evens = numbers.where((n) => n.isEven).toList();
  print(evens); // [2, 4, 6, 8, 10]

  // fold: accumulate a value with an initial seed.
  final sum = numbers.fold<int>(0, (acc, n) => acc + n);
  print(sum); // 55

  // reduce: like fold but uses the first element as seed.
  final product = numbers.reduce((acc, n) => acc * n);
  print(product); // 3628800

  // forEach: side effects only, returns void.
  numbers.forEach((n) => print('Item: $n'));

  // expand: flatMap -- each element becomes zero or more elements.
  final nested = [[1, 2], [3, 4], [5]];
  final flat = nested.expand((list) => list).toList();
  print(flat); // [1, 2, 3, 4, 5]

  // Chaining: find the sum of squares of even numbers.
  final result = numbers
      .where((n) => n.isEven)
      .map((n) => n * n)
      .fold<int>(0, (acc, n) => acc + n);
  print(result); // 4 + 16 + 36 + 64 + 100 = 220
}
```

These functions return lazy `Iterable` objects (except `forEach`, `fold`, `reduce`). Call `.toList()` only when you need materialization.

### 6. Function Types and typedef

When function signatures appear repeatedly, `typedef` gives them a name. This improves readability and makes refactoring safer.

```dart
// file: lib/06_typedef.dart

// Without typedef, this signature repeats everywhere:
// bool Function(int) predicate

typedef Predicate<T> = bool Function(T);
typedef Mapper<T, R> = R Function(T);
typedef Reducer<T> = T Function(T, T);

List<T> customFilter<T>(List<T> items, Predicate<T> test) {
  return items.where(test).toList();
}

List<R> customMap<T, R>(List<T> items, Mapper<T, R> transform) {
  return items.map(transform).toList();
}

void main() {
  final numbers = [1, 2, 3, 4, 5, 6];

  final evens = customFilter(numbers, (n) => n.isEven);
  print(evens); // [2, 4, 6]

  final strings = customMap(numbers, (n) => 'num_$n');
  print(strings); // [num_1, num_2, num_3, num_4, num_5, num_6]
}
```

### 7. Extension Methods, Tear-offs, and Recursion

```dart
// file: lib/07_extensions_tearoffs_recursion.dart

// Extension methods attach new behavior to existing types.
extension IntParsing on String {
  int? toIntOrNull() => int.tryParse(this);
}

// Recursive function with a base case.
int factorial(int n) {
  if (n < 0) throw ArgumentError('Negative input: $n');
  if (n <= 1) return 1;
  return n * factorial(n - 1);
}

// Tail-call style via accumulator (Dart does not optimize tail calls,
// but the pattern avoids deep stack frames when converted to a loop).
int factorialTail(int n, [int accumulator = 1]) {
  if (n <= 1) return accumulator;
  return factorialTail(n - 1, n * accumulator);
}

void main() {
  // Extension method usage.
  print('42'.toIntOrNull());   // 42
  print('abc'.toIntOrNull());  // null

  // Tear-off: referencing a method without calling it.
  // Instead of (n) => n.isEven, use the tear-off:
  final numbers = [1, 2, 3, 4, 5];
  print(numbers.where((n) => n.isEven).toList()); // [2, 4]

  // Constructor tear-off.
  final values = ['1', '2', '3'];
  final parsed = values.map(int.parse).toList();
  print(parsed); // [1, 2, 3]

  // Recursion.
  print(factorial(6));     // 720
  print(factorialTail(6)); // 720
}
```

A tear-off is syntactic shorthand: `int.parse` instead of `(s) => int.parse(s)`. It produces the same function object but reads more cleanly.

### 8. Generator Functions

Generators produce sequences lazily. `sync*` yields an `Iterable`; `async*` yields a `Stream`.

```dart
// file: lib/08_generators.dart

// Synchronous generator: produces values on demand.
Iterable<int> range(int start, int end, [int step = 1]) sync* {
  for (int i = start; i < end; i += step) {
    yield i;
  }
}

// yield* delegates to another iterable.
Iterable<int> flatten(List<List<int>> nested) sync* {
  for (final list in nested) {
    yield* list;
  }
}

// Asynchronous generator: produces a stream.
Stream<int> countDown(int from) async* {
  for (int i = from; i >= 0; i--) {
    await Future.delayed(Duration(milliseconds: 100));
    yield i;
  }
}

void main() async {
  // Lazy: nothing executes until iteration.
  final nums = range(0, 10, 2);
  print(nums.toList()); // [0, 2, 4, 6, 8]

  final flat = flatten([[1, 2], [3], [4, 5, 6]]);
  print(flat.toList()); // [1, 2, 3, 4, 5, 6]

  // Stream consumption.
  await for (final value in countDown(3)) {
    print('T-$value');
  }
  // T-3, T-2, T-1, T-0
}
```

Generators are the foundation for lazy pipelines. They compute values only when the consumer asks for the next element, which matters when working with large or infinite sequences.

---

## Exercises

### Exercise 1 (Basic): Function Declaration Sampler

**Objective:** Practice all three declaration styles and parameter types.

**Instructions:**

Create a file with the following functions:

1. A named function `celsiusToFahrenheit` that takes a `double` and returns a `double` using arrow syntax.
2. A named function `formatTemperature` that takes a required `double value` and an optional named parameter `String unit` defaulting to `'C'`. Return a string like `"23.5 C"`.
3. An anonymous function stored in a variable `isPositive` that takes an `int` and returns `bool`.
4. A function `describeNumber` that takes a required positional `int` and an optional positional `String` label defaulting to `'value'`. Return `"$label: $number is even/odd"`.

```dart
// file: exercises/ex01_declaration_sampler.dart

// TODO: Implement celsiusToFahrenheit using arrow syntax.
// Formula: (celsius * 9/5) + 32

// TODO: Implement formatTemperature with named optional parameter.

// TODO: Implement isPositive as anonymous function in a variable.

// TODO: Implement describeNumber with optional positional parameter.

void main() {
  // Verification step 1: Basic conversion.
  assert(celsiusToFahrenheit(0) == 32.0);
  assert(celsiusToFahrenheit(100) == 212.0);
  print('celsiusToFahrenheit: OK');

  // Verification step 2: Default and explicit unit.
  assert(formatTemperature(value: 23.5) == '23.5 C');
  assert(formatTemperature(value: 73.4, unit: 'F') == '73.4 F');
  print('formatTemperature: OK');

  // Verification step 3: Anonymous function.
  assert(isPositive(5) == true);
  assert(isPositive(-1) == false);
  assert(isPositive(0) == false);
  print('isPositive: OK');

  // Verification step 4: Optional positional param.
  assert(describeNumber(4) == 'value: 4 is even');
  assert(describeNumber(3, 'count') == 'count: 3 is odd');
  print('describeNumber: OK');

  print('--- Exercise 1 PASSED ---');
}
```

**Verification:**

```bash
dart run exercises/ex01_declaration_sampler.dart
# Expected: all four OK lines followed by "--- Exercise 1 PASSED ---"
```

---

### Exercise 2 (Basic): Higher-Order Function Warm-up

**Objective:** Use `map`, `where`, `fold`, and `expand` on a list of data.

**Instructions:**

Given a list of product prices, write functions that:

1. `applyDiscount`: takes a `List<double>` and a `double discountPercent`, returns a new list with each price reduced by that percentage using `map`.
2. `filterExpensive`: takes a `List<double>` and a `double threshold`, returns only prices above the threshold using `where`.
3. `totalCost`: takes a `List<double>` and returns the sum using `fold`.
4. `expandBundles`: takes a `List<List<double>>` (bundles of prices) and returns a flat `List<double>` using `expand`.

```dart
// file: exercises/ex02_higher_order_warmup.dart

// TODO: Implement applyDiscount using map.
// A 10% discount on 100.0 yields 90.0.

// TODO: Implement filterExpensive using where.

// TODO: Implement totalCost using fold.

// TODO: Implement expandBundles using expand.

void main() {
  final prices = [29.99, 49.99, 9.99, 79.99, 14.99];

  // Verification step 1: Discount.
  final discounted = applyDiscount(prices, 10);
  assert(discounted.length == 5);
  assert((discounted[0] - 26.991).abs() < 0.01);
  print('applyDiscount: OK');

  // Verification step 2: Filter.
  final expensive = filterExpensive(prices, 30.0);
  assert(expensive.length == 2);
  assert(expensive.contains(49.99));
  assert(expensive.contains(79.99));
  print('filterExpensive: OK');

  // Verification step 3: Total.
  final total = totalCost(prices);
  assert((total - 184.95).abs() < 0.01);
  print('totalCost: OK');

  // Verification step 4: Expand bundles.
  final bundles = [[10.0, 20.0], [30.0], [40.0, 50.0]];
  final flat = expandBundles(bundles);
  assert(flat.length == 5);
  assert((totalCost(flat) - 150.0).abs() < 0.01);
  print('expandBundles: OK');

  print('--- Exercise 2 PASSED ---');
}
```

**Verification:**

```bash
dart run exercises/ex02_higher_order_warmup.dart
# Expected: four OK lines and "--- Exercise 2 PASSED ---"
```

---

### Exercise 3 (Intermediate): Data Pipeline Builder

**Objective:** Build a composable pipeline using first-class functions and closures.

**Instructions:**

Create a `Pipeline` class that chains transformations on a list. Each transformation is a function. The pipeline should:

1. Accept transformations via an `addStep` method that takes a `List<T> Function(List<T>)`.
2. Execute all steps in order via an `execute` method.
3. Support a `removeLast` method to undo the last added step.
4. Provide a static `from` factory that takes a list of step functions.

Then use this pipeline to process a list of integers: filter evens, double each value, sort descending, take the first 3.

```dart
// file: exercises/ex03_pipeline.dart

// TODO: Implement the Pipeline class.
// Hint: store steps in a List<List<T> Function(List<T>)>.

class Pipeline<T> {
  // TODO: fields and constructor

  // TODO: addStep

  // TODO: removeLast

  // TODO: execute(List<T> input) -> List<T>

  // TODO: static from(List<List<T> Function(List<T>)> steps) -> Pipeline<T>
}

void main() {
  // Verification step 1: Build a pipeline step by step.
  final pipeline = Pipeline<int>();
  pipeline.addStep((list) => list.where((n) => n.isEven).toList());
  pipeline.addStep((list) => list.map((n) => n * 2).toList());
  pipeline.addStep((list) => list..sort((a, b) => b.compareTo(a)));
  pipeline.addStep((list) => list.take(3).toList());

  final input = [5, 3, 8, 1, 4, 7, 2, 9, 6, 10];
  final result = pipeline.execute(input);
  assert(result.length == 3);
  assert(result[0] == 20);  // 10*2
  assert(result[1] == 16);  // 8*2
  assert(result[2] == 12);  // 6*2
  print('Pipeline step-by-step: OK');

  // Verification step 2: removeLast undoes take(3).
  pipeline.removeLast();
  final fullResult = pipeline.execute(input);
  assert(fullResult.length == 5); // all evens doubled, sorted desc
  print('removeLast: OK');

  // Verification step 3: static factory.
  final quickPipe = Pipeline<int>.from([
    (list) => list.where((n) => n > 5).toList(),
    (list) => list.map((n) => n * 10).toList(),
  ]);
  final quickResult = quickPipe.execute([1, 2, 6, 8, 3]);
  assert(quickResult.length == 2);
  assert(quickResult.contains(60));
  assert(quickResult.contains(80));
  print('Pipeline.from factory: OK');

  // Verification step 4: empty pipeline returns input unchanged.
  final empty = Pipeline<int>();
  assert(empty.execute([1, 2, 3]).length == 3);
  print('Empty pipeline: OK');

  print('--- Exercise 3 PASSED ---');
}
```

**Verification:**

```bash
dart run exercises/ex03_pipeline.dart
# Expected: four OK lines and "--- Exercise 3 PASSED ---"
```

---

### Exercise 4 (Intermediate): Memoization with Closures

**Objective:** Create a generic memoization wrapper that uses closures to cache results.

**Instructions:**

Write a function `memoize` that:

1. Takes a `R Function(T)` and returns a `R Function(T)` with caching.
2. Uses a `Map<T, R>` inside the closure to store results.
3. Returns cached values for repeated inputs.

Then write `memoize2` for two-argument functions using a string key strategy.

Test with a computationally expensive function (simulated with a counter to track calls).

```dart
// file: exercises/ex04_memoize.dart

// TODO: Implement memoize<T, R> that wraps a single-argument function.
// The cache Map lives inside the closure.

// TODO: Implement memoize2<T1, T2, R> for two-argument functions.
// Hint: create a composite key from both arguments.

void main() {
  // Track how many times the expensive function actually runs.
  int callCount = 0;

  int expensiveSquare(int n) {
    callCount++;
    return n * n;
  }

  // Verification step 1: memoize caches results.
  final memoSquare = memoize(expensiveSquare);
  assert(memoSquare(4) == 16);
  assert(memoSquare(4) == 16);
  assert(memoSquare(5) == 25);
  assert(callCount == 2); // Only 2 actual calls, not 3.
  print('memoize single-arg: OK');

  // Verification step 2: different inputs are cached separately.
  callCount = 0;
  final memoAbs = memoize((int n) {
    callCount++;
    return n.abs();
  });
  memoAbs(-3);
  memoAbs(3);
  memoAbs(-3);
  assert(callCount == 2); // -3 and 3 are different keys.
  print('memoize separate keys: OK');

  // Verification step 3: memoize2 for two arguments.
  int addCallCount = 0;
  int expensiveAdd(int a, int b) {
    addCallCount++;
    return a + b;
  }

  final memoAdd = memoize2(expensiveAdd);
  assert(memoAdd(3, 4) == 7);
  assert(memoAdd(3, 4) == 7);
  assert(memoAdd(4, 3) == 7);
  assert(addCallCount == 2); // (3,4) cached, (4,3) is a different key.
  print('memoize2: OK');

  // Verification step 4: works with string functions.
  final memoUpper = memoize((String s) => s.toUpperCase());
  assert(memoUpper('hello') == 'HELLO');
  assert(memoUpper('hello') == 'HELLO');
  print('memoize with strings: OK');

  print('--- Exercise 4 PASSED ---');
}
```

**Verification:**

```bash
dart run exercises/ex04_memoize.dart
# Expected: four OK lines and "--- Exercise 4 PASSED ---"
```

---

### Exercise 5 (Advanced): Middleware Chain via Function Composition

**Objective:** Design a middleware system where each middleware is a function that wraps the next, similar to HTTP middleware in web frameworks.

**Instructions:**

A middleware has this shape: `Response Function(Request) Function(Response Function(Request))`. In simpler terms, it takes the "next" handler and returns a new handler.

1. Define `Request` and `Response` classes with relevant fields.
2. Define a `Middleware` typedef.
3. Write a `composeMiddleware` function that takes a list of middleware and a final handler, returning a single handler.
4. Implement three middleware: `loggingMiddleware` (records request path), `authMiddleware` (rejects if no token), `timingMiddleware` (measures duration).

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

// TODO: Implement composeMiddleware that chains [Middleware] around a Handler.
// The first middleware in the list is the outermost wrapper.

// TODO: Implement loggingMiddleware that adds the request path to a log list.

// TODO: Implement authMiddleware that returns 401 if 'Authorization' header
// is missing, otherwise passes to next.

// TODO: Implement timingMiddleware that adds an 'X-Duration' header to
// the response (use Stopwatch).

void main() {
  final logs = <String>[];

  Middleware loggingMiddleware = // TODO
  Middleware authMiddleware = // TODO
  Middleware timingMiddleware = // TODO

  final Handler finalHandler = (Request req) {
    return Response(200, 'OK: ${req.path}');
  };

  // Verification step 1: compose and call with valid request.
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

  // Verification step 2: missing auth returns 401.
  logs.clear();
  final noAuthReq = Request('/api/secret');
  final noAuthResp = app(noAuthReq);
  assert(noAuthResp.statusCode == 401);
  assert(logs.contains('/api/secret')); // Logging still runs (it is outer).
  print('Auth rejection: OK');

  // Verification step 3: timing header is present.
  final timedResp = app(validReq);
  assert(timedResp.headers.containsKey('X-Duration'));
  print('Timing header: OK');

  // Verification step 4: empty middleware list returns handler as-is.
  final naked = composeMiddleware([], finalHandler);
  final nakedResp = naked(Request('/raw'));
  assert(nakedResp.statusCode == 200);
  print('Empty middleware: OK');

  // Verification step 5: single middleware works.
  final single = composeMiddleware([loggingMiddleware], finalHandler);
  logs.clear();
  single(Request('/single'));
  assert(logs.contains('/single'));
  print('Single middleware: OK');

  print('--- Exercise 5 PASSED ---');
}
```

**Verification:**

```bash
dart run exercises/ex05_middleware.dart
# Expected: five OK lines and "--- Exercise 5 PASSED ---"
```

---

### Exercise 6 (Advanced): Closure Memory Analysis with Generators

**Objective:** Understand how closures retain references and combine that knowledge with generator functions to build a controlled resource lifecycle.

**Instructions:**

1. Write a `resourceTracker` generator (`sync*`) that yields `Resource` objects. Each resource holds a `dispose` callback (a closure). The generator tracks all yielded resources in an internal list.
2. Write a `ResourcePool` class that uses the generator to allocate resources on demand, tracks active resources, and disposes all on `closeAll`.
3. Demonstrate that closures inside disposed resources no longer affect shared state.

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

// TODO: Implement resourceGenerator that yields Resource objects.
// Each resource's dispose closure should decrement an active count
// and add the resource id to a disposed-ids list.

// TODO: Implement ResourcePool using the generator.

void main() {
  final disposedIds = <int>[];
  int activeCount = 0;

  // TODO: Create the generator and pool.

  // Verification step 1: allocate resources.
  // Allocate 3 resources from the pool.
  // assert(activeCount == 3);
  print('Allocation: OK');

  // Verification step 2: dispose one resource.
  // Dispose resource with id 1.
  // assert(activeCount == 2);
  // assert(disposedIds.contains(1));
  print('Single dispose: OK');

  // Verification step 3: double dispose is safe.
  // Disposing the same resource again should not change counts.
  // assert(activeCount == 2);
  print('Double dispose safety: OK');

  // Verification step 4: closeAll disposes remaining.
  // assert(activeCount == 0);
  // assert(disposedIds.length == 3);
  print('Close all: OK');

  // Verification step 5: generator produces new ids after pool reset.
  // Allocate 2 more -- ids should continue from 4.
  // assert(activeCount == 2);
  print('Continued generation: OK');

  print('--- Exercise 6 PASSED ---');
}
```

**Verification:**

```bash
dart run exercises/ex06_closure_generators.dart
# Expected: five OK lines and "--- Exercise 6 PASSED ---"
```

---

### Exercise 7 (Insane): Functional Programming Toolkit

**Objective:** Implement a library of core functional programming utilities using only Dart functions, closures, and generics.

**Instructions:**

Implement the following:

1. `curry2<A, B, R>`: converts a `R Function(A, B)` into `R Function(B) Function(A)`.
2. `curry3<A, B, C, R>`: same pattern for three arguments.
3. `partial<A, B, R>`: takes `R Function(A, B)` and an `A`, returns `R Function(B)`.
4. `compose<A, B, C>`: takes `C Function(B)` and `B Function(A)`, returns `C Function(A)`. Right-to-left execution.
5. `pipe<A, B, C>`: like compose but left-to-right.
6. `composeN`: takes a list of `dynamic Function(dynamic)` and returns a single composed function (right-to-left).
7. `pipeN`: same but left-to-right.
8. `memoizeWithTTL<T, R>`: like `memoize` but entries expire after a `Duration`. Uses `DateTime.now()` comparison.

```dart
// file: exercises/ex07_fp_toolkit.dart

// TODO: Implement curry2
// TODO: Implement curry3
// TODO: Implement partial
// TODO: Implement compose (2 functions)
// TODO: Implement pipe (2 functions)
// TODO: Implement composeN (list of functions, right-to-left)
// TODO: Implement pipeN (list of functions, left-to-right)
// TODO: Implement memoizeWithTTL

void main() async {
  // --- curry2 ---
  int add(int a, int b) => a + b;
  final curriedAdd = curry2(add);
  assert(curriedAdd(3)(4) == 7);
  final add10 = curriedAdd(10);
  assert(add10(5) == 15);
  print('curry2: OK');

  // --- curry3 ---
  String joinThree(String a, String b, String c) => '$a-$b-$c';
  final curriedJoin = curry3(joinThree);
  assert(curriedJoin('x')('y')('z') == 'x-y-z');
  print('curry3: OK');

  // --- partial ---
  final add5 = partial(add, 5);
  assert(add5(3) == 8);
  assert(add5(0) == 5);
  print('partial: OK');

  // --- compose ---
  final double2 = (int n) => n * 2;
  final toString_ = (int n) => 'val:$n';
  final composed = compose<int, int, String>(toString_, double2);
  assert(composed(5) == 'val:10');
  print('compose: OK');

  // --- pipe ---
  final piped = pipe<int, int, String>(double2, toString_);
  assert(piped(5) == 'val:10');
  print('pipe: OK');

  // --- composeN ---
  final addOne = (dynamic n) => (n as int) + 1;
  final triple = (dynamic n) => (n as int) * 3;
  final negate = (dynamic n) => -(n as int);
  // composeN applies right to left: negate(triple(addOne(2))) = -(3*3) = -9
  final composedN = composeN([negate, triple, addOne]);
  assert(composedN(2) == -9);
  print('composeN: OK');

  // --- pipeN ---
  // pipeN applies left to right: negate(triple(addOne(2)))... no:
  // addOne(2)=3, triple(3)=9, negate(9)=-9
  final pipedN = pipeN([addOne, triple, negate]);
  assert(pipedN(2) == -9);
  print('pipeN: OK');

  // --- memoizeWithTTL ---
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
  assert(ttlCallCount == 1); // Cached.
  print('memoizeWithTTL cached: OK');

  // Wait for TTL to expire.
  await Future.delayed(Duration(milliseconds: 300));
  assert(memoSlow(4) == 16);
  assert(ttlCallCount == 2); // Re-computed after expiry.
  print('memoizeWithTTL expired: OK');

  print('--- Exercise 7 PASSED ---');
}
```

**Verification:**

```bash
dart run exercises/ex07_fp_toolkit.dart
# Expected: nine OK lines and "--- Exercise 7 PASSED ---"
# If memoizeWithTTL fails, check that your cache stores timestamps
# and compares elapsed time against the TTL duration.
```

---

### Exercise 8 (Insane): Lazy Evaluation Engine

**Objective:** Build a lazy evaluation engine that defers computation until values are actually needed, supports chaining operations, and handles infinite sequences -- all powered by closures and generators.

**Instructions:**

Implement a `Lazy<T>` class and a `LazySequence<T>` class:

`Lazy<T>`:
- Wraps a `T Function()` thunk (a zero-argument closure).
- Evaluates the thunk at most once (`isEvaluated` flag).
- Supports `map`, `flatMap`, and `where` (returns a new Lazy).

`LazySequence<T>`:
- Backed by a `sync*` generator internally.
- Supports `lazyMap`, `lazyWhere`, `lazyTake`, `lazyZip` operations that return new `LazySequence` objects without triggering evaluation.
- Has a `toList()` method that materializes the sequence.
- Supports creating infinite sequences via a `LazySequence.generate` factory.

```dart
// file: exercises/ex08_lazy_engine.dart

// TODO: Implement Lazy<T> with deferred single evaluation.

// TODO: Implement LazySequence<T> backed by generators.
// - LazySequence(Iterable<T> Function() generator)
// - LazySequence.generate(T Function(int index) factory) -- infinite
// - lazyMap<R>(R Function(T) transform) -> LazySequence<R>
// - lazyWhere(bool Function(T) test) -> LazySequence<T>
// - lazyTake(int count) -> LazySequence<T>
// - lazyZip<R>(LazySequence<R> other) -> LazySequence<(T, R)>
// - toList() -> List<T>

void main() {
  // --- Lazy<T> tests ---

  // Verification step 1: deferred evaluation.
  int evalCount = 0;
  final lazy = Lazy(() {
    evalCount++;
    return 42;
  });
  assert(evalCount == 0); // Not evaluated yet.
  assert(lazy.value == 42);
  assert(evalCount == 1);
  assert(lazy.value == 42);
  assert(evalCount == 1); // Cached.
  print('Lazy deferred eval: OK');

  // Verification step 2: Lazy.map chains without evaluating.
  int mapEvalCount = 0;
  final lazyMapped = Lazy(() {
    mapEvalCount++;
    return 10;
  }).map((n) => n * 2);
  assert(mapEvalCount == 0);
  assert(lazyMapped.value == 20);
  assert(mapEvalCount == 1);
  print('Lazy.map: OK');

  // --- LazySequence<T> tests ---

  // Verification step 3: operations do not trigger evaluation.
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

  assert(genCount == 0); // Nothing evaluated yet.
  final result = transformed.toList();
  assert(result.length == 5);
  assert(result[0] == 0);
  assert(result[1] == 20);
  assert(result[4] == 80);
  assert(genCount < 100); // Did not evaluate all 100 elements.
  print('LazySequence chaining: OK');

  // Verification step 4: infinite sequence with take.
  final naturals = LazySequence.generate((i) => i + 1);
  final firstTen = naturals.lazyTake(10).toList();
  assert(firstTen.length == 10);
  assert(firstTen.last == 10);
  print('Infinite sequence: OK');

  // Verification step 5: zip two sequences.
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

**Verification:**

```bash
dart run exercises/ex08_lazy_engine.dart
# Expected: five OK lines and "--- Exercise 8 PASSED ---"
# Key check: genCount must be less than 100 -- this proves laziness.
# If genCount == 100, your generator is materializing eagerly.
```

---

## Summary

Functions in Dart are not just blocks of reusable code -- they are objects with types, capable of being composed, cached, and chained. You have worked through:

- Three declaration styles and when each communicates the right intent
- Parameter design as API design: positional vs. named, required vs. optional
- First-class functions as the foundation for callbacks, builders, and handlers
- Closures as functions that carry state from their creation scope
- Higher-order functions for declarative data transformation
- `typedef` for naming function signatures
- Extension methods, tear-offs, and recursion patterns
- Generator functions for lazy, on-demand value production
- Functional patterns (curry, compose, pipe) built from first principles
- Lazy evaluation engines combining closures with generators

## What's Next

**Section 03: Control Flow & Collections** -- You will explore Dart's pattern matching (Dart 3 switch expressions), collection types (`List`, `Set`, `Map`, `Queue`), collection-if/for, spread operators, and how to combine control flow with the functional patterns from this section.

## References

- [Dart Language Tour: Functions](https://dart.dev/language/functions)
- [Dart Language Tour: Typedefs](https://dart.dev/language/typedefs)
- [Dart Language Tour: Generators](https://dart.dev/language/functions#generators)
- [Dart Language Tour: Extension Methods](https://dart.dev/language/extension-methods)
- [Effective Dart: Usage](https://dart.dev/effective-dart/usage)
- [Dart API: Iterable](https://api.dart.dev/stable/dart-core/Iterable-class.html)
