# Section 06 -- Dart Generics and the Type System

## Introduction

Every program deals with data, and most programs deal with data of *different shapes*. The question is whether your code knows the shape of the data it handles, or whether it just hopes for the best.

Without generics, you face a brutal choice: write one rigid class per data type (a `StringStack`, an `IntStack`, a `WidgetStack`...) or collapse everything into `dynamic` and lose every guarantee the type system gives you. Generics eliminate that trade-off. They let you write a single `Stack<T>` that is fully type-safe for any `T`, checked at compile time, with zero runtime overhead.

But Dart's type system goes further than most. Unlike Java, Dart generics are *reified* -- type arguments survive at runtime, which means you can inspect them, branch on them, and use them in ways that are simply impossible in languages with type erasure. Dart 3 added extension types, giving you zero-cost wrappers that exist only at compile time. And Dart's covariant-by-default approach to generic variance is convenient but hides real dangers you need to understand before they bite you in production.

This section covers all of it: the mechanics, the patterns, the traps, and the architectural techniques that separate code that merely compiles from code that is genuinely safe.

## Prerequisites

You should be comfortable with everything from Sections 01 through 05:

- Variables, types, and type inference (Section 01)
- Functions, closures, and higher-order functions (Section 02)
- Collections: `List`, `Map`, `Set` and their typed variants (Section 03)
- Classes, inheritance, abstract classes, mixins, interfaces (Section 04)
- Futures, Streams, and async/await (Section 05)

If `abstract class` or `implements` still feels shaky, revisit Section 04 first. Generics build heavily on those foundations.

## Learning Objectives

By the end of this section you will be able to:

1. **Define** generic classes and functions with single and multiple type parameters.
2. **Apply** bounded type parameters to constrain generic types and access their interface.
3. **Explain** Dart's covariant-by-default approach and **identify** code that compiles but fails at runtime due to unsound covariance.
4. **Construct** generic type aliases using `typedef` for complex function and class signatures.
5. **Implement** extension types (Dart 3+) as zero-cost wrappers for domain modeling.
6. **Differentiate** reified generics from erased generics and **leverage** runtime type information.
7. **Design** generic architectural patterns: repositories, event buses, middleware pipelines, and dependency injection containers.
8. **Evaluate** advanced generic code for type safety, combining pattern matching with type promotion.

---

## Core Concepts

### 1. Generic Classes -- Parameterizing Over Types

A generic class declares one or more *type parameters* that act as placeholders until the caller supplies concrete types.

```dart
// file: generic_stack.dart

class Stack<T> {
  final List<T> _items = [];

  void push(T item) => _items.add(item);

  T pop() {
    if (_items.isEmpty) {
      throw StateError('Cannot pop from an empty stack');
    }
    return _items.removeLast();
  }

  T get peek => _items.last;
  bool get isEmpty => _items.isEmpty;
  int get length => _items.length;
}

void main() {
  final intStack = Stack<int>();
  intStack.push(1);
  intStack.push(2);
  print(intStack.pop()); // 2

  // This will not compile:
  // intStack.push('hello'); // Error: String is not int

  final stringStack = Stack<String>();
  stringStack.push('dart');
  print(stringStack.peek); // dart
}
```

The `<T>` after the class name is not decoration. It tells the compiler: "everywhere you see `T` in this class, substitute the actual type the caller provides." The result is full static checking -- `intStack.push('hello')` is a compile error, not a runtime surprise.

**Multiple type parameters** work the same way:

```dart
// file: pair.dart

class Pair<A, B> {
  final A first;
  final B second;

  const Pair(this.first, this.second);

  Pair<B, A> swap() => Pair<B, A>(second, first);

  @override
  String toString() => 'Pair($first, $second)';
}

void main() {
  final coordinates = Pair<String, double>('latitude', 37.7749);
  print(coordinates);             // Pair(latitude, 37.7749)
  print(coordinates.swap());      // Pair(37.7749, latitude)

  // Type inference works too:
  final inferred = Pair(42, true);
  // Dart infers Pair<int, bool>
}
```

### 2. Generic Functions and Methods

You do not need a generic class to use generics. Standalone functions and methods can have their own type parameters:

```dart
// file: generic_functions.dart

T firstWhere<T>(List<T> items, bool Function(T) predicate) {
  for (final item in items) {
    if (predicate(item)) return item;
  }
  throw StateError('No element matching predicate');
}

List<R> mapList<T, R>(List<T> source, R Function(T) transform) {
  return [for (final item in source) transform(item)];
}

void main() {
  final numbers = [1, 2, 3, 4, 5];
  final firstEven = firstWhere<int>(numbers, (n) => n.isEven);
  print(firstEven); // 2

  final strings = mapList<int, String>(numbers, (n) => 'item_$n');
  print(strings); // [item_1, item_2, item_3, item_4, item_5]
}
```

Notice that the type parameter `<T>` comes after the function name and before the parameter list. Dart can usually infer `T` and `R` from the arguments, so you often omit them at the call site.

### 3. Bounded Type Parameters

An unbounded `<T>` accepts anything -- `int`, `String`, `Null`, your custom class. But what if you need to call methods on `T`? You need a *bound*.

```dart
// file: bounded_types.dart

abstract class Measurable {
  double get measurement;
}

class Box implements Measurable {
  final double width;
  final double height;
  Box(this.width, this.height);

  @override
  double get measurement => width * height;
}

class Circle implements Measurable {
  final double radius;
  Circle(this.radius);

  @override
  double get measurement => 3.14159 * radius * radius;
}

T largest<T extends Measurable>(List<T> items) {
  if (items.isEmpty) throw ArgumentError('List must not be empty');
  return items.reduce(
    (a, b) => a.measurement >= b.measurement ? a : b,
  );
}

void main() {
  final boxes = [Box(2, 3), Box(4, 5), Box(1, 1)];
  final biggestBox = largest(boxes); // Returns Box, not Measurable
  print(biggestBox.measurement);     // 20.0

  final circles = [Circle(1), Circle(3), Circle(2)];
  final biggestCircle = largest(circles);
  print(biggestCircle.radius);       // 3 -- we kept the Circle type
}
```

The bound `T extends Measurable` does two things: it lets you call `.measurement` inside the function, and it constrains callers to pass only `Measurable` subtypes. The return type is `T`, not `Measurable`, so the caller gets back the specific type they passed in. This precision matters.

### 4. Covariance -- Convenient and Dangerous

Dart treats generic types as *covariant* by default. That means if `Cat` extends `Animal`, then `List<Cat>` is treated as a subtype of `List<Animal>`. This is convenient for reading, but unsound for writing:

```dart
// file: covariance_trap.dart

class Animal {
  String get name => 'animal';
}

class Cat extends Animal {
  @override
  String get name => 'cat';
  void purr() => print('purrr');
}

class Dog extends Animal {
  @override
  String get name => 'dog';
  void bark() => print('woof');
}

void addAnimal(List<Animal> animals) {
  animals.add(Dog()); // Compiles fine
}

void main() {
  List<Cat> cats = [Cat(), Cat()];

  // This compiles because List<Cat> is-a List<Animal> in Dart:
  addAnimal(cats);

  // But now cats contains a Dog. This will fail at runtime:
  for (final cat in cats) {
    cat.purr(); // Runtime error when it hits the Dog
  }
}
```

This code compiles with zero warnings. The type system says "List<Cat> is a List<Animal>, so passing it to `addAnimal` is fine." But `addAnimal` inserts a `Dog` into a list that is supposed to contain only cats. The crash happens later, far from the cause.

**Why does Dart allow this?** Pragmatism. The alternative (declaration-site variance like in Kotlin or C#) adds complexity to every generic declaration. Dart inserts a *runtime check* on writes to catch this, but the compile-time guarantee is gone. Be aware of this whenever you pass a typed collection to a function that writes to it.

### 5. Generic Type Aliases

Complex generic signatures get unreadable fast. `typedef` lets you name them:

```dart
// file: type_aliases.dart

typedef JsonMap = Map<String, dynamic>;
typedef Converter<T> = T Function(JsonMap json);
typedef Predicate<T> = bool Function(T value);
typedef AsyncCallback<T> = Future<T> Function();

class ApiClient {
  Future<T> fetch<T>(String url, Converter<T> fromJson) async {
    // Simulating network call
    final json = <String, dynamic>{'id': 1, 'name': 'Dart'};
    return fromJson(json);
  }
}

class User {
  final int id;
  final String name;
  User({required this.id, required this.name});

  static User fromJson(JsonMap json) =>
      User(id: json['id'] as int, name: json['name'] as String);
}

void main() async {
  final client = ApiClient();
  final user = await client.fetch<User>('/users/1', User.fromJson);
  print(user.name); // Dart
}
```

`typedef JsonMap = Map<String, dynamic>` is not creating a new type -- it is an alias. The compiler treats `JsonMap` and `Map<String, dynamic>` as identical. The value is readability and consistency: change the alias in one place and every usage updates.

### 6. Extension Types (Dart 3+)

Extension types create compile-time wrappers around an existing type. They have zero runtime cost because the wrapper is erased -- the underlying representation type is all that exists at runtime.

```dart
// file: extension_types.dart

extension type UserId(int value) {
  bool get isValid => value > 0;
}

extension type Email(String value) {
  bool get isValid => value.contains('@') && value.contains('.');
}

extension type Meters(double value) implements double {
  Meters operator +(Meters other) => Meters(value + other.value);
  Meters operator *(double factor) => Meters(value * factor);

  String get formatted => '${value.toStringAsFixed(2)}m';
}

class UserService {
  // The signature makes invalid states unrepresentable:
  void deleteUser(UserId id) {
    if (!id.isValid) throw ArgumentError('Invalid user ID: ${id.value}');
    print('Deleting user ${id.value}');
  }

  void sendEmail(UserId to, Email address) {
    print('Sending to user ${to.value} at ${address.value}');
  }
}

void main() {
  final userId = UserId(42);
  final email = Email('dart@flutter.dev');

  final service = UserService();
  service.deleteUser(userId);
  service.sendEmail(userId, email);

  // This will NOT compile:
  // service.deleteUser(42);      // int is not UserId
  // service.sendEmail(userId, 'raw@string.com'); // String is not Email

  // With `implements double`, Meters can be used where double is expected:
  final distance = Meters(5.0);
  print(distance.formatted); // 5.00m
}
```

The key distinction from a regular class: at runtime, `UserId(42)` is just the `int` 42. There is no object allocation, no wrapper overhead. The `implements` clause on `Meters` exposes the representation type's interface, so `Meters` can be used anywhere a `double` is expected.

**When extension types do NOT protect you:** If you cast to the representation type (or use `dynamic`), the wrapper disappears. Extension types are a *static* safety net only.

### 7. Type Reification

In Java, `List<String>` and `List<int>` are the same type at runtime -- the generic argument is erased. Dart is different. Generic type arguments are *reified*: they exist at runtime.

```dart
// file: reification.dart

void describeList<T>(List<T> items) {
  print('Runtime type: ${items.runtimeType}');
  print('T is int: ${T == int}');
  print('T is String: ${T == String}');
  print('items is List<int>: ${items is List<int>}');
}

void main() {
  describeList<int>([1, 2, 3]);
  // Runtime type: List<int>
  // T is int: true
  // T is String: false
  // items is List<int>: true

  describeList<String>(['a', 'b']);
  // Runtime type: List<String>
  // T is int: false
  // T is String: true
  // items is List<int>: false
}
```

This enables patterns that are impossible with erased generics: runtime type checks on generic containers, factory methods that branch on `T`, and serialization logic that inspects nested type arguments. It also means `is` checks on generic types actually work, which makes pattern matching with generics far more useful.

### 8. Type Promotion with Generics and Pattern Matching

Dart 3 pattern matching works together with generics to narrow types inside branches:

```dart
// file: type_promotion.dart

sealed class Result<T> {
  const Result();
}

class Success<T> extends Result<T> {
  final T value;
  const Success(this.value);
}

class Failure<T> extends Result<T> {
  final String message;
  final Object? error;
  const Failure(this.message, [this.error]);
}

String describe<T>(Result<T> result) {
  return switch (result) {
    Success<T>(value: final v) => 'Success: $v',
    Failure<T>(message: final m, error: final e?) => 'Failure: $m ($e)',
    Failure<T>(message: final m) => 'Failure: $m',
  };
}

void main() {
  final Result<int> ok = Success(42);
  final Result<int> err = Failure('not found', 404);

  print(describe(ok));  // Success: 42
  print(describe(err)); // Failure: not found (404)
}
```

The `sealed` keyword ensures exhaustiveness: the compiler knows every subtype of `Result<T>`, so it can verify that every `switch` branch is covered. Combined with reified generics, this gives you type-safe algebraic data types.

---

## Exercises

### Exercise 1 -- Generic Stack and Pair (Basic)

**Objective:** Build foundational generic classes with proper error handling.

**Instructions:**

1. Implement a `Stack<T>` class with `push`, `pop`, `peek`, `isEmpty`, `length`, and a `toList` method that returns elements from top to bottom.
2. Implement a `Pair<A, B>` class with `first`, `second`, a `swap()` method, and a `map` method: `Pair<C, D> map<C, D>(C Function(A) mapFirst, D Function(B) mapSecond)`.
3. All operations that fail on empty state must throw `StateError` with a descriptive message.

```dart
// file: exercise_01_starter.dart

class Stack<T> {
  // TODO: Implement with a private List<T>
  // push(T item), pop() -> T, peek -> T, isEmpty, length, toList()
}

class Pair<A, B> {
  // TODO: Implement with final fields
  // first, second, swap(), map(), toString()
}

void main() {
  // Test Stack
  final stack = Stack<int>();
  stack.push(10);
  stack.push(20);
  stack.push(30);
  assert(stack.length == 3);
  assert(stack.peek == 30);
  assert(stack.pop() == 30);
  assert(stack.toList().first == 20); // top to bottom

  // Test empty stack error
  final emptyStack = Stack<String>();
  try {
    emptyStack.pop();
    assert(false, 'Should have thrown');
  } on StateError catch (e) {
    print('Caught: $e'); // Expected
  }

  // Test Pair
  final pair = Pair<String, int>('age', 30);
  assert(pair.swap().first == 30);
  final mapped = pair.map((s) => s.length, (i) => i.toDouble());
  assert(mapped.first == 3);    // 'age'.length
  assert(mapped.second == 30.0);

  print('Exercise 01 passed');
}
```

**Verification:** All assertions pass, the empty-stack error is caught and printed, and the final "passed" message appears.

---

### Exercise 2 -- Bounded Types and Generic Functions (Basic)

**Objective:** Use bounded type parameters to write type-safe generic functions.

**Instructions:**

1. Define an abstract class `Rankable` with an `int get rank` property.
2. Implement `Player` and `Card` classes that implement `Rankable`.
3. Write a generic function `T topRanked<T extends Rankable>(List<T> items)` that returns the highest-ranked item.
4. Write a generic function `List<T> filterByMinRank<T extends Rankable>(List<T> items, int minRank)` that returns only items with rank >= minRank.
5. Verify that the return types are the specific subtypes, not `Rankable`.

```dart
// file: exercise_02_starter.dart

abstract class Rankable {
  int get rank;
}

// TODO: Implement Player with name and rank
// TODO: Implement Card with suit, value, and rank (value as rank)

// TODO: T topRanked<T extends Rankable>(List<T> items)
// TODO: List<T> filterByMinRank<T extends Rankable>(List<T> items, int minRank)

void main() {
  final players = [
    Player('Alice', 1500),
    Player('Bob', 2100),
    Player('Carol', 1800),
  ];

  final best = topRanked(players);
  // best is Player, not Rankable -- we can access .name
  print('Best player: ${best.name} with rank ${best.rank}');
  assert(best.name == 'Bob');

  final elitePlayers = filterByMinRank(players, 1700);
  assert(elitePlayers.length == 2);
  assert(elitePlayers.every((p) => p.rank >= 1700));
  // elitePlayers is List<Player>, so .name is accessible
  print('Elite: ${elitePlayers.map((p) => p.name)}');

  final cards = [Card('hearts', 10), Card('spades', 14), Card('diamonds', 7)];
  final bestCard = topRanked(cards);
  print('Best card: ${bestCard.suit} ${bestCard.value}');
  assert(bestCard.suit == 'spades');

  print('Exercise 02 passed');
}
```

**Verification:** All assertions pass. The return types preserve the concrete `Player` and `Card` types, not the `Rankable` base.

---

### Exercise 3 -- Generic Repository Pattern (Intermediate)

**Objective:** Build a generic repository with CRUD operations and type-safe queries.

**Instructions:**

1. Define an abstract class `Entity` with `String get id`.
2. Create a `Repository<T extends Entity>` class backed by an in-memory `Map<String, T>`.
3. Implement: `save(T entity)`, `T? findById(String id)`, `List<T> findAll()`, `List<T> findWhere(bool Function(T) predicate)`, `void delete(String id)`, `int get count`.
4. Create `User` and `Product` entity classes.
5. Demonstrate that `Repository<User>` and `Repository<Product>` are fully independent and type-safe.

```dart
// file: exercise_03_starter.dart

abstract class Entity {
  String get id;
}

class Repository<T extends Entity> {
  // TODO: Implement with Map<String, T> storage
}

// TODO: User implements Entity (id, name, email)
// TODO: Product implements Entity (id, title, price)

void main() {
  final users = Repository<User>();
  users.save(User('u1', 'Alice', 'alice@test.com'));
  users.save(User('u2', 'Bob', 'bob@test.com'));

  assert(users.count == 2);
  assert(users.findById('u1')?.name == 'Alice');
  assert(users.findById('missing') == null);

  final emailUsers = users.findWhere((u) => u.email.contains('alice'));
  assert(emailUsers.length == 1);

  users.delete('u1');
  assert(users.count == 1);

  final products = Repository<Product>();
  products.save(Product('p1', 'Widget', 9.99));
  products.save(Product('p2', 'Gadget', 29.99));

  final expensive = products.findWhere((p) => p.price > 20);
  assert(expensive.length == 1);
  assert(expensive.first.title == 'Gadget');

  // This must NOT compile if uncommented:
  // users.save(Product('p3', 'Oops', 1.0)); // Type error

  print('Exercise 03 passed');
}
```

**Verification:** All assertions pass. The commented-out line produces a compile error if uncommented.

---

### Exercise 4 -- Type-Safe Event Bus with Extension Types (Intermediate)

**Objective:** Combine generics with extension types to build a publish-subscribe event bus where event types are enforced at compile time.

**Instructions:**

1. Create extension types `EventChannel<T>(String value)` for type-safe channel identifiers.
2. Create an `EventBus` class where:
   - `void on<T>(EventChannel<T> channel, void Function(T) handler)` registers a listener.
   - `void emit<T>(EventChannel<T> channel, T event)` dispatches an event to all registered handlers for that channel.
   - `void off<T>(EventChannel<T> channel, void Function(T) handler)` removes a listener.
   - `void clear()` removes all listeners.
3. The key constraint: you cannot emit a `String` on a channel typed as `EventChannel<int>`.

```dart
// file: exercise_04_starter.dart

extension type EventChannel<T>(String value) {
  // This is a zero-cost wrapper: at runtime it's just a String
}

class EventBus {
  // TODO: Internal storage for handlers keyed by channel
  // Hint: You will need Map<String, List<Function>> since Dart
  // cannot store generic types in a homogeneous map. The type
  // safety comes from the public API, not the internal storage.
}

// Domain events
class UserLoggedIn {
  final String userId;
  final DateTime timestamp;
  UserLoggedIn(this.userId, this.timestamp);
}

class CartUpdated {
  final List<String> itemIds;
  final double total;
  CartUpdated(this.itemIds, this.total);
}

void main() {
  // Define typed channels
  const loginChannel = EventChannel<UserLoggedIn>('user.login');
  const cartChannel = EventChannel<CartUpdated>('cart.updated');

  final bus = EventBus();
  final events = <String>[];

  bus.on<UserLoggedIn>(loginChannel, (event) {
    events.add('Login: ${event.userId}');
  });

  bus.on<CartUpdated>(cartChannel, (event) {
    events.add('Cart: \$${event.total}');
  });

  bus.emit<UserLoggedIn>(loginChannel, UserLoggedIn('u1', DateTime.now()));
  bus.emit<CartUpdated>(cartChannel, CartUpdated(['a', 'b'], 49.99));

  assert(events.length == 2);
  assert(events[0] == 'Login: u1');
  assert(events[1] == 'Cart: \$49.99');

  // This should NOT compile if uncommented:
  // bus.emit<String>(loginChannel, 'wrong type');

  print('Exercise 04 passed');
}
```

**Verification:** All assertions pass. The commented-out line would fail compile-time type checking.

---

### Exercise 5 -- Generic Cache with Eviction Policies (Advanced)

**Objective:** Design a generic cache that supports pluggable eviction strategies using bounded generics and generic interfaces.

**Instructions:**

1. Define an abstract class `EvictionPolicy<K>` with methods: `void recordAccess(K key)`, `void recordInsertion(K key)`, `K selectVictim()`, `void recordRemoval(K key)`.
2. Implement `LruPolicy<K>` (least recently used) and `LfuPolicy<K>` (least frequently used).
3. Implement `Cache<K, V>` that takes a max capacity and an `EvictionPolicy<K>`.
4. `Cache` API: `void put(K key, V value)`, `V? get(K key)`, `bool containsKey(K key)`, `int get size`, `void invalidate(K key)`.
5. When the cache is full and a new item is inserted, the eviction policy selects which key to remove.

```dart
// file: exercise_05_starter.dart

abstract class EvictionPolicy<K> {
  void recordAccess(K key);
  void recordInsertion(K key);
  K selectVictim();
  void recordRemoval(K key);
}

// TODO: LruPolicy<K> -- track access order, evict the least recently used
// TODO: LfuPolicy<K> -- track access frequency, evict the least frequently used

class Cache<K, V> {
  final int maxCapacity;
  final EvictionPolicy<K> _policy;
  // TODO: internal storage, delegation to policy

  Cache({required this.maxCapacity, required EvictionPolicy<K> policy})
      : _policy = policy;

  // TODO: put, get, containsKey, size, invalidate
}

void main() {
  // LRU test
  final lruCache = Cache<String, int>(
    maxCapacity: 3,
    policy: LruPolicy<String>(),
  );

  lruCache.put('a', 1);
  lruCache.put('b', 2);
  lruCache.put('c', 3);
  lruCache.get('a');       // Access 'a', making 'b' the LRU
  lruCache.put('d', 4);   // Should evict 'b'

  assert(lruCache.get('b') == null, 'b should have been evicted');
  assert(lruCache.get('a') == 1);
  assert(lruCache.get('d') == 4);
  assert(lruCache.size == 3);

  // LFU test
  final lfuCache = Cache<String, int>(
    maxCapacity: 3,
    policy: LfuPolicy<String>(),
  );

  lfuCache.put('x', 10);
  lfuCache.put('y', 20);
  lfuCache.put('z', 30);
  lfuCache.get('x');  // freq: x=2, y=1, z=1
  lfuCache.get('x');  // freq: x=3, y=1, z=1
  lfuCache.get('z');  // freq: x=3, y=1, z=2
  lfuCache.put('w', 40); // Evict 'y' (lowest freq)

  assert(lfuCache.get('y') == null, 'y should have been evicted');
  assert(lfuCache.get('x') == 10);
  assert(lfuCache.size == 3);

  print('Exercise 05 passed');
}
```

**Verification:** All assertions pass for both LRU and LFU eviction strategies.

---

### Exercise 6 -- Covariance Bug Hunt (Advanced)

**Objective:** Identify, explain, and fix real covariance bugs in collection hierarchies.

**Instructions:**

Study the following code carefully. It compiles without errors but contains multiple runtime failures caused by Dart's covariant generics.

1. Identify every line that will cause a runtime error and explain why.
2. For each bug, write a fixed version that is both type-safe and still useful.
3. Write a `safeCast` generic function that checks types before performing operations.

```dart
// file: exercise_06_starter.dart

class Shape {
  double get area => 0;
}

class Rectangle extends Shape {
  final double width, height;
  Rectangle(this.width, this.height);
  @override
  double get area => width * height;
}

class Circle extends Shape {
  final double radius;
  Circle(this.radius);
  @override
  double get area => 3.14159 * radius * radius;
}

// BUG 1: Find the covariance trap
void addDefaultShape(List<Shape> shapes) {
  shapes.add(Circle(1.0));
}

// BUG 2: Another covariance issue
Map<String, Shape> mergeShapeMaps(
  Map<String, Shape> target,
  Map<String, Shape> source,
) {
  target.addAll(source);
  return target;
}

// BUG 3: Covariance in callbacks
void processShapes(List<Shape> shapes, void Function(Shape) processor) {
  for (final shape in shapes) {
    processor(shape);
  }
}

void main() {
  // Scenario 1
  List<Rectangle> rectangles = [Rectangle(2, 3)];
  addDefaultShape(rectangles); // What happens here?

  // Scenario 2
  Map<String, Rectangle> rectMap = {'r1': Rectangle(1, 2)};
  Map<String, Circle> circleMap = {'c1': Circle(5)};
  mergeShapeMaps(rectMap, circleMap); // What happens here?

  // Scenario 3
  List<Circle> circles = [Circle(1), Circle(2)];
  processShapes(circles, (shape) {
    // Inside here, 'shape' is typed as Shape.
    // Is this safe? What if we downcast?
    final circle = shape as Circle;
    print('Radius: ${circle.radius}');
  });

  // TODO: Write fixed versions for each scenario
  // TODO: Implement safeCast<T>(dynamic value) -> T?
}
```

**Verification:** You must produce code where the fixed versions prevent runtime errors at compile time rather than relying on runtime checks. Document each bug with a comment explaining the root cause.

---

### Exercise 7 -- Type-Safe Dependency Injection Container (Insane)

**Objective:** Build a fully type-safe DI container with scoping, lazy instantiation, and singleton vs transient lifetimes -- all enforced through generics.

**Instructions:**

1. Create a `ServiceLifetime` enum with `singleton` and `transient` values.
2. Create a `Container` class that supports:
   - `register<T>(T Function(Container) factory, {ServiceLifetime lifetime})` -- registers a factory for type `T`.
   - `T resolve<T>()` -- resolves an instance of `T`, respecting lifetime.
   - `Container createScope()` -- creates a child scope that inherits parent registrations but has its own singleton instances.
3. Singletons are created once per scope and reused. Transients are created fresh each time.
4. If a type is not registered, throw a descriptive `StateError`.
5. Support type-safe overrides in child scopes: a child can re-register a type, shadowing the parent.

```dart
// file: exercise_07_starter.dart

enum ServiceLifetime { singleton, transient }

class Container {
  // TODO: Implement registration storage, singleton cache, parent reference
  // Think carefully about how to key registrations by type.
  // Hint: use Type objects as map keys, since Dart generics are reified.
}

// Example services to wire up:
abstract class Logger {
  void log(String message);
}

class ConsoleLogger implements Logger {
  @override
  void log(String message) => print('[LOG] $message');
}

class SilentLogger implements Logger {
  final messages = <String>[];
  @override
  void log(String message) => messages.add(message);
}

abstract class UserRepository {
  String findUser(String id);
}

class InMemoryUserRepository implements UserRepository {
  final Logger _logger;
  InMemoryUserRepository(this._logger);

  @override
  String findUser(String id) {
    _logger.log('Finding user $id');
    return 'User($id)';
  }
}

class AuthService {
  final UserRepository _repo;
  final Logger _logger;
  AuthService(this._repo, this._logger);

  String authenticate(String userId) {
    _logger.log('Authenticating $userId');
    return _repo.findUser(userId);
  }
}

void main() {
  final container = Container();

  // Register services
  container.register<Logger>(
    (_) => ConsoleLogger(),
    lifetime: ServiceLifetime.singleton,
  );
  container.register<UserRepository>(
    (c) => InMemoryUserRepository(c.resolve<Logger>()),
    lifetime: ServiceLifetime.singleton,
  );
  container.register<AuthService>(
    (c) => AuthService(c.resolve<UserRepository>(), c.resolve<Logger>()),
    lifetime: ServiceLifetime.transient,
  );

  // Singleton behavior
  final logger1 = container.resolve<Logger>();
  final logger2 = container.resolve<Logger>();
  assert(identical(logger1, logger2), 'Singletons must be the same instance');

  // Transient behavior
  final auth1 = container.resolve<AuthService>();
  final auth2 = container.resolve<AuthService>();
  assert(!identical(auth1, auth2), 'Transients must be different instances');

  // Scoping
  final testScope = container.createScope();
  testScope.register<Logger>(
    (_) => SilentLogger(),
    lifetime: ServiceLifetime.singleton,
  );

  final scopedLogger = testScope.resolve<Logger>();
  assert(scopedLogger is SilentLogger, 'Child scope should use its own Logger');

  // Parent logger unchanged
  assert(container.resolve<Logger>() is ConsoleLogger);

  // Child scope resolves UserRepository using its own Logger
  final scopedRepo = testScope.resolve<UserRepository>();
  scopedRepo.findUser('test');
  // The SilentLogger should have captured the log
  assert((testScope.resolve<Logger>() as SilentLogger).messages.isNotEmpty);

  // Unregistered type
  try {
    container.resolve<String>();
    assert(false, 'Should have thrown');
  } on StateError catch (e) {
    print('Caught: $e');
  }

  print('Exercise 07 passed');
}
```

**Verification:** All assertions pass. Singletons are shared within their scope, transients are fresh, child scopes shadow parents, and unregistered types throw `StateError`.

---

### Exercise 8 -- Generic Middleware Pipeline (Insane)

**Objective:** Build a type-safe middleware pipeline where each middleware step can transform the type flowing through the pipeline, and the compiler tracks the type at every stage.

**Instructions:**

1. Create a `Middleware<TIn, TOut>` abstract class with `Future<TOut> process(TIn input)`.
2. Create a `Pipeline` class that chains middleware steps. The pipeline must enforce that the output type of step N matches the input type of step N+1 at compile time.
3. Implement a `then` method: if the pipeline currently goes from `A` to `B`, calling `.then(Middleware<B, C>())` produces a pipeline from `A` to `C`.
4. Implement an `execute(TIn input)` method that runs all steps in sequence and returns `Future<TOut>`.
5. Build a realistic example: a request processing pipeline that transforms `RawRequest -> ValidatedRequest -> AuthenticatedRequest -> Response`.

```dart
// file: exercise_08_starter.dart

abstract class Middleware<TIn, TOut> {
  Future<TOut> process(TIn input);
}

class Pipeline<TIn, TOut> {
  // TODO: Store the transformation function
  // Hint: Internally, the pipeline can be represented as a single
  // Future<TOut> Function(TIn) that composes all steps.

  // TODO: then<TNext>(Middleware<TOut, TNext> next) -> Pipeline<TIn, TNext>
  // TODO: Future<TOut> execute(TIn input)
}

// Domain types for the example
class RawRequest {
  final String path;
  final Map<String, String> headers;
  final String body;
  RawRequest(this.path, this.headers, this.body);
}

class ValidatedRequest {
  final String path;
  final Map<String, String> headers;
  final Map<String, dynamic> parsedBody;
  ValidatedRequest(this.path, this.headers, this.parsedBody);
}

class AuthenticatedRequest {
  final String path;
  final String userId;
  final Map<String, dynamic> parsedBody;
  AuthenticatedRequest(this.path, this.userId, this.parsedBody);
}

class Response {
  final int statusCode;
  final String body;
  Response(this.statusCode, this.body);
}

// TODO: Implement ValidationMiddleware extends Middleware<RawRequest, ValidatedRequest>
// TODO: Implement AuthMiddleware extends Middleware<ValidatedRequest, AuthenticatedRequest>
// TODO: Implement HandlerMiddleware extends Middleware<AuthenticatedRequest, Response>

void main() async {
  // Build the pipeline -- the compiler tracks types through each step:
  final pipeline = Pipeline<RawRequest, RawRequest>.identity()
      .then(ValidationMiddleware())
      .then(AuthMiddleware())
      .then(HandlerMiddleware());

  // pipeline is Pipeline<RawRequest, Response> -- fully typed

  final request = RawRequest(
    '/api/users',
    {'Authorization': 'Bearer token123'},
    '{"name": "Alice"}',
  );

  final response = await pipeline.execute(request);
  assert(response.statusCode == 200);
  print('Response: ${response.statusCode} ${response.body}');

  // Test failure case: missing auth header
  final badRequest = RawRequest('/api/users', {}, '{"name": "Bob"}');
  try {
    await pipeline.execute(badRequest);
    assert(false, 'Should have thrown');
  } catch (e) {
    print('Pipeline error: $e');
  }

  print('Exercise 08 passed');
}
```

**Verification:** The pipeline processes a valid request end-to-end, rejects an invalid request with an error, and the types are enforced at each step by the compiler. `Pipeline<RawRequest, Response>` is the inferred type of the composed pipeline.

---

## Summary

Generics are not an optional feature you sprinkle on when things get complicated. They are the primary mechanism for writing reusable code that does not sacrifice type safety. In this section you worked through:

- **Generic classes and functions** as the basic building blocks for parameterized code.
- **Bounded types** that constrain generic parameters while preserving specific return types.
- **Covariance traps** that compile cleanly but crash at runtime -- and how to design around them.
- **Type aliases** for readable, maintainable generic signatures.
- **Extension types** for zero-cost compile-time wrappers that make invalid states unrepresentable.
- **Reified generics** that survive at runtime, enabling patterns impossible in languages with type erasure.
- **Pattern matching** with sealed generic classes for exhaustive type-safe branching.
- **Architectural patterns** (repositories, event buses, DI containers, middleware pipelines) that use generics as their structural backbone.

The common thread: generics move checks from runtime to compile time. Every bug the compiler catches is a bug your users never see.

## What's Next

Section 07 covers **Error Handling and Null Safety** -- how Dart's sound null safety interacts with the type system you just mastered, how to design error hierarchies, and when to use exceptions versus `Result` types (which, as you now know, are generic sealed classes).

## References

- [Dart Language Tour: Generics](https://dart.dev/language/generics)
- [Dart Language: Extension Types](https://dart.dev/language/extension-types)
- [Dart Language: Type System](https://dart.dev/language/type-system)
- [Dart Language: Patterns](https://dart.dev/language/patterns)
- [Effective Dart: Design -- Types](https://dart.dev/effective-dart/design#types)
- [Dart API Reference: Type class](https://api.dart.dev/stable/dart-core/Type-class.html)
- [Understanding Dart Generics (reification)](https://dart.dev/resources/dart-cheatsheet)
