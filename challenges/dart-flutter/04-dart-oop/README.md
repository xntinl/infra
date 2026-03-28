# Section 04 -- Dart Object-Oriented Programming

## Introduction

Every Dart value is an object. Even the number `42` is an instance of `int`, which extends `num`, which extends `Object`. Understanding OOP in Dart is not optional -- it is the foundation that Flutter's entire widget tree, state management, and rendering pipeline are built on.

This section takes you from defining simple classes to architecting type-safe systems with sealed hierarchies, class modifiers, and extension types. By the end you will be able to read and write the kind of code you encounter in production Flutter packages.

## Prerequisites

You must be comfortable with:

- **Section 01** -- Variables, types, and type inference
- **Section 02** -- Functions, closures, and higher-order functions
- **Section 03** -- Control flow, collections, and pattern matching basics

## Learning Objectives

After completing this section you will be able to:

1. **Define** classes with fields, methods, constructors, and static members
2. **Differentiate** between default, named, factory, const, and redirecting constructors
3. **Design** inheritance hierarchies using `extends`, `super`, and `@override`
4. **Apply** abstract classes and implicit interfaces to enforce contracts
5. **Compose** behavior with mixins and the `mixin class` declaration (Dart 3+)
6. **Extend** existing types using extension methods and extension types (Dart 3+)
7. **Implement** enhanced enums with fields, methods, and interface conformance
8. **Architect** sealed class hierarchies with exhaustive pattern matching
9. **Evaluate** object equality through `==`, `hashCode`, and `identical()`
10. **Construct** type-safe APIs using class modifiers: `base`, `interface`, `final`, `sealed`

---

## Core Concepts

### 1. Classes: The Building Block

A class bundles data (fields) and behavior (methods) into a single unit. In Dart, every class implicitly inherits from `Object`.

Why does this matter? Because Flutter widgets are classes. Layout objects are classes. State objects are classes. You cannot build anything meaningful without them.

```dart
// file: lib/bank_account.dart
class BankAccount {
  // Private field -- the underscore makes it library-private
  double _balance;
  final String owner;

  // Default constructor with initializer list
  BankAccount(this.owner, double initialDeposit)
      : _balance = initialDeposit {
    if (initialDeposit < 0) {
      throw ArgumentError('Initial deposit cannot be negative');
    }
  }

  // Getter -- computed property, no parentheses when calling
  double get balance => _balance;

  // Setter with validation
  set balance(double value) {
    if (value < 0) throw StateError('Balance cannot go negative');
    _balance = value;
  }

  // Instance method
  void deposit(double amount) {
    if (amount <= 0) throw ArgumentError('Deposit must be positive');
    _balance += amount;
  }

  // Static member -- belongs to the class, not instances
  static const double interestRate = 0.035;

  static double calculateInterest(double principal) {
    return principal * interestRate;
  }

  @override
  String toString() => 'BankAccount(owner: $owner, balance: $_balance)';
}
```

Key points: fields declared with `this.` in the constructor are syntactic sugar for assignment. Private members use an underscore prefix and are scoped to the library, not the class.

### 2. Constructors: Every Flavor

Dart offers more constructor variants than most languages. Each one solves a specific problem.

```dart
// file: lib/color.dart
class Color {
  final int red;
  final int green;
  final int blue;

  // Default constructor
  Color(this.red, this.green, this.blue);

  // Named constructor -- for clarity at the call site
  Color.fromHex(String hex)
      : red = int.parse(hex.substring(1, 3), radix: 16),
        green = int.parse(hex.substring(3, 5), radix: 16),
        blue = int.parse(hex.substring(5, 7), radix: 16);

  // Const constructor -- enables compile-time constants
  const Color.white() : red = 255, green = 255, blue = 255;

  // Redirecting constructor -- delegates to another constructor
  Color.red(int intensity) : this(intensity, 0, 0);

  // Factory constructor -- can return cached instances or subtypes
  static final Map<String, Color> _cache = {};

  factory Color.named(String name) {
    return _cache.putIfAbsent(name, () {
      return switch (name) {
        'red' => Color(255, 0, 0),
        'green' => Color(0, 255, 0),
        'blue' => Color(0, 0, 255),
        _ => throw ArgumentError('Unknown color: $name'),
      };
    });
  }

  @override
  String toString() => 'Color($red, $green, $blue)';
}
```

When should you use which? Use named constructors when the name communicates intent better than positional arguments. Use factory constructors when you need to return an existing instance (caching) or a subtype. Use const constructors when the object is immutable and you want compile-time evaluation.

### 3. Inheritance and Method Overriding

Inheritance models "is-a" relationships. Dart uses single inheritance with `extends`.

```dart
// file: lib/shapes.dart
class Shape {
  final String name;
  const Shape(this.name);

  double area() => 0;

  @override
  String toString() => '$name(area: ${area().toStringAsFixed(2)})';
}

class Circle extends Shape {
  final double radius;
  const Circle(this.radius) : super('Circle');

  @override
  double area() => 3.14159265 * radius * radius;
}

class Rectangle extends Shape {
  final double width;
  final double height;
  const Rectangle(this.width, this.height) : super('Rectangle');

  @override
  double area() => width * height;
}
```

The `@override` annotation is not enforced by the language, but the analyzer will warn you if you omit it. Always use it -- it protects you from silent bugs when a parent method signature changes.

### 4. Abstract Classes and Implicit Interfaces

Every class in Dart implicitly defines an interface. You can `implement` any class, which forces you to provide concrete implementations of all its members.

```dart
// file: lib/repository.dart
abstract class Repository<T> {
  Future<T?> findById(int id);
  Future<List<T>> findAll();
  Future<void> save(T entity);
  Future<void> delete(int id);
}

// Implementing the abstract class
class InMemoryRepository<T> implements Repository<T> {
  final Map<int, T> _store = {};
  final int Function(T) _getId;

  InMemoryRepository(this._getId);

  @override
  Future<T?> findById(int id) async => _store[id];

  @override
  Future<List<T>> findAll() async => _store.values.toList();

  @override
  Future<void> save(T entity) async {
    _store[_getId(entity)] = entity;
  }

  @override
  Future<void> delete(int id) async {
    _store.remove(id);
  }
}
```

The difference between `extends` and `implements`: when you extend, you inherit implementation. When you implement, you only inherit the contract. Use `implements` when you want to guarantee an API shape without coupling to a parent implementation.

### 5. Mixins: Composition Over Inheritance

Mixins solve the problem of sharing behavior across unrelated class hierarchies without the fragility of deep inheritance trees.

```dart
// file: lib/mixins.dart
mixin Serializable {
  Map<String, dynamic> toJson();

  String toJsonString() {
    return toJson().entries
        .map((e) => '"${e.key}": "${e.value}"')
        .join(', ');
  }
}

mixin Timestamped {
  DateTime? _createdAt;
  DateTime? _updatedAt;

  DateTime? get createdAt => _createdAt;
  DateTime? get updatedAt => _updatedAt;

  void markCreated() => _createdAt = DateTime.now();
  void markUpdated() => _updatedAt = DateTime.now();
}

// The 'on' clause restricts which classes can use this mixin
mixin Auditable on Timestamped {
  final List<String> _auditLog = [];

  void logChange(String description) {
    markUpdated();
    _auditLog.add('${DateTime.now()}: $description');
  }

  List<String> get auditTrail => List.unmodifiable(_auditLog);
}

// Dart 3+: mixin class can be used as both a mixin and a class
mixin class Identifiable {
  late final String id;

  void assignId(String value) {
    id = value;
  }
}
```

The `on` clause is important: `Auditable on Timestamped` means you can only apply `Auditable` to a class that already uses `Timestamped`. This creates a dependency chain that the compiler enforces.

### 6. Extension Methods and Extension Types

Extension methods let you add functionality to existing types without modifying them. Extension types (Dart 3+) create zero-cost wrapper types.

```dart
// file: lib/extensions.dart
extension StringValidation on String {
  bool get isValidEmail =>
      RegExp(r'^[\w\-.]+@([\w-]+\.)+[\w-]{2,4}$').hasMatch(this);

  String truncate(int maxLength, {String ellipsis = '...'}) {
    if (length <= maxLength) return this;
    return '${substring(0, maxLength - ellipsis.length)}$ellipsis';
  }
}

// Extension type -- zero-cost wrapper with a different API
// The representation type (int) is the actual runtime type.
// No allocation overhead: this compiles away entirely.
extension type UserId(int value) {
  // You can add methods specific to this "type"
  bool get isValid => value > 0;

  // Factory constructor for validation
  factory UserId.parse(String source) {
    final parsed = int.parse(source);
    if (parsed <= 0) throw FormatException('Invalid user ID: $source');
    return UserId(parsed);
  }
}

// Now you cannot accidentally pass a raw int where a UserId is expected,
// and vice versa -- even though at runtime it is just an int.
```

Why extension types instead of a regular wrapper class? Performance. A regular class allocates memory on the heap. An extension type erases to its representation type at runtime. Use them when you want type safety without runtime cost.

### 7. Enhanced Enums

Dart enums can have fields, methods, constructors, and even implement interfaces.

```dart
// file: lib/http_method.dart
enum HttpMethod implements Comparable<HttpMethod> {
  get('GET', idempotent: true),
  post('POST', idempotent: false),
  put('PUT', idempotent: true),
  patch('PATCH', idempotent: false),
  delete('DELETE', idempotent: true);

  final String value;
  final bool idempotent;

  const HttpMethod(this.value, {required this.idempotent});

  bool get hasBod => this != get && this != delete;

  @override
  int compareTo(HttpMethod other) => value.compareTo(other.value);

  static HttpMethod fromString(String method) {
    return HttpMethod.values.firstWhere(
      (m) => m.value == method.toUpperCase(),
      orElse: () => throw ArgumentError('Unknown HTTP method: $method'),
    );
  }
}
```

### 8. Sealed Classes and Exhaustive Pattern Matching

Sealed classes restrict which classes can extend or implement them to the same library. The compiler then knows every possible subtype, enabling exhaustive `switch` expressions.

```dart
// file: lib/result.dart
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
  const Failure(this.message, {this.error});
}

class Loading<T> extends Result<T> {
  const Loading();
}

// Exhaustive -- the compiler verifies every case is handled
String describeResult(Result<int> result) {
  return switch (result) {
    Success(:final value) => 'Got value: $value',
    Failure(:final message) => 'Failed: $message',
    Loading() => 'Loading...',
  };
}
```

This pattern is powerful for state management. If you add a new subtype, every `switch` that consumes the sealed type will produce a compile error until you handle the new case.

### 9. Operator Overloading and Object Equality

```dart
// file: lib/vector2d.dart
class Vector2D {
  final double x;
  final double y;
  const Vector2D(this.x, this.y);

  Vector2D operator +(Vector2D other) => Vector2D(x + other.x, y + other.y);
  Vector2D operator -(Vector2D other) => Vector2D(x - other.x, y - other.y);
  Vector2D operator *(double scalar) => Vector2D(x * scalar, y * scalar);

  double dot(Vector2D other) => x * other.x + y * other.y;
  double get magnitude => (x * x + y * y).sqrt();

  // Proper equality: override both == and hashCode
  @override
  bool operator ==(Object other) =>
      identical(this, other) ||
      other is Vector2D && x == other.x && y == other.y;

  @override
  int get hashCode => Object.hash(x, y);

  @override
  String toString() => 'Vector2D($x, $y)';
}
```

Three levels of equality in Dart: `identical()` checks reference equality (same object in memory). `==` checks value equality (you define what "equal" means). `hashCode` must be consistent with `==` -- equal objects must have the same hash code.

### 10. Class Modifiers (Dart 3+)

Class modifiers control how a class can be used outside the library where it is defined.

```dart
// file: lib/modifiers.dart

// base: can be extended but not implemented outside this library
base class Entity {
  final int id;
  const Entity(this.id);
}

// interface: can be implemented but not extended outside this library
interface class Printable {
  void prettyPrint();
}

// final: cannot be extended or implemented outside this library
final class DatabaseConnection {
  final String connectionString;
  DatabaseConnection(this.connectionString);
  void close() {}
}

// sealed: abstract + final, subtypes must be in the same library
sealed class PaymentMethod {}
class CreditCard extends PaymentMethod {
  final String last4;
  CreditCard(this.last4);
}
class BankTransfer extends PaymentMethod {
  final String iban;
  BankTransfer(this.iban);
}
```

When to use which: use `base` when you have implementation that subclasses depend on and you want to prevent external code from faking it with `implements`. Use `interface` when you want consumers to provide their own implementation. Use `final` when you want a completely locked-down API. Use `sealed` when you need exhaustive pattern matching.

### 11. Covariant Keyword

The `covariant` keyword relaxes type-checking on method parameters to allow narrower types in overrides.

```dart
// file: lib/covariant.dart
class Animal {
  String name;
  Animal(this.name);

  void chase(covariant Animal other) {
    print('$name chases ${other.name}');
  }
}

class Dog extends Animal {
  Dog(super.name);

  @override
  void chase(Dog other) {
    // Without covariant on Animal.chase, this would be a compile error
    // because Dog is narrower than Animal
    print('$name chases fellow dog ${other.name}');
  }
}
```

Use `covariant` sparingly. It trades compile-time safety for flexibility. If you pass a `Cat` to `Dog.chase` at runtime, you get a `TypeError`. Prefer generics when you can.

---

## Exercises

### Exercise 1 (Basic): Class Fundamentals

**Objective:** Create a `Product` class that models an e-commerce item.

**Instructions:**

1. Create a file `lib/product.dart`
2. Define a `Product` class with fields: `name` (String), `price` (double), `_stock` (int, private)
3. Add a default constructor that validates `price > 0` and `stock >= 0`
4. Add a named constructor `Product.outOfStock(name, price)` that sets stock to 0
5. Add a getter `stock` and a method `restock(int quantity)` that validates quantity > 0
6. Add a method `sell(int quantity)` that throws `StateError` if insufficient stock
7. Override `toString` to return a readable representation

**Verification:**

```dart
// file: bin/exercise1_test.dart
void main() {
  final laptop = Product('Laptop', 999.99, 50);
  print(laptop);                          // Product(Laptop, 999.99, stock: 50)
  laptop.sell(3);
  print(laptop.stock);                    // 47
  laptop.restock(10);
  print(laptop.stock);                    // 57

  final phone = Product.outOfStock('Phone', 499.99);
  print(phone.stock);                     // 0

  try { Product('Bad', -10, 5); }
  catch (e) { print(e); }                // ArgumentError

  try { phone.sell(1); }
  catch (e) { print(e); }                // StateError
}
```

Run: `dart run bin/exercise1_test.dart`

---

### Exercise 2 (Basic): Inheritance Chain

**Objective:** Build a vehicle hierarchy demonstrating `extends`, `super`, and `@override`.

**Instructions:**

1. Create `lib/vehicles.dart` with a base class `Vehicle` that has fields `make`, `model`, `year`, and a method `describe()` returning a string
2. Create `Car extends Vehicle` adding a `trunkCapacity` (double) field and overriding `describe()`
3. Create `ElectricCar extends Car` adding `batteryCapacity` (double) and `range` (int), overriding `describe()` to include all ancestor information
4. Use `super` properly in each constructor
5. Demonstrate the Liskov Substitution Principle: write a function that accepts `Vehicle` and works correctly with any subtype

**Verification:**

```dart
// file: bin/exercise2_test.dart
void main() {
  final car = Car('Toyota', 'Camry', 2024, 450.0);
  final ev = ElectricCar('Tesla', 'Model 3', 2024, 380.0, 75.0, 350);

  print(car.describe());
  print(ev.describe());

  void printVehicleInfo(Vehicle v) => print(v.describe());
  printVehicleInfo(car);                  // Works with Car
  printVehicleInfo(ev);                   // Works with ElectricCar

  print(ev is Car);                       // true
  print(ev is Vehicle);                   // true
}
```

Run: `dart run bin/exercise2_test.dart`

---

### Exercise 3 (Intermediate): Mixin Composition

**Objective:** Design a logging and persistence system using mixins.

**Instructions:**

1. Create `lib/entity_system.dart`
2. Define a mixin `Loggable` with a method `log(String message)` that stores messages in a private list and exposes `logs` as an unmodifiable list
3. Define a mixin `Validatable` with an abstract method `List<String> validate()` and a concrete method `bool get isValid => validate().isEmpty`
4. Define a `mixin Persistable on Validatable` with a method `Future<bool> save()` that calls `validate()` first and only persists if valid
5. Create a `mixin class BaseEntity` with an `id` field and a `copyWith` method stub
6. Create a class `User extends BaseEntity with Loggable, Validatable, Persistable` that has `name` and `email` fields
7. Implement `validate()` to check name is not empty and email contains `@`

**Verification:**

```dart
// file: bin/exercise3_test.dart
void main() async {
  final user = User(id: 1, name: 'Alice', email: 'alice@example.com');
  user.log('User created');
  print(user.isValid);                    // true
  print(await user.save());               // true
  print(user.logs);                       // [User created]

  final badUser = User(id: 2, name: '', email: 'invalid');
  print(badUser.isValid);                 // false
  print(badUser.validate());              // [Name cannot be empty, Invalid email]
  print(await badUser.save());            // false
}
```

Run: `dart run bin/exercise3_test.dart`

---

### Exercise 4 (Intermediate): Factory Constructors and Operator Overloading

**Objective:** Implement a `Money` class with caching, operator overloading, and proper equality.

**Instructions:**

1. Create `lib/money.dart`
2. Define `Money` with `amount` (int, in cents) and `currency` (String)
3. Add a factory constructor `Money.zero(String currency)` that caches and returns the same instance for each currency
4. Overload `+`, `-`, and `*` (by int). Addition and subtraction must throw if currencies differ
5. Implement `==` and `hashCode` properly
6. Add a `format()` method that returns something like `"USD 12.50"` (amount / 100)
7. Implement `Comparable<Money>` so `Money` instances can be sorted (same currency only)

**Verification:**

```dart
// file: bin/exercise4_test.dart
void main() {
  final a = Money(1250, 'USD');
  final b = Money(750, 'USD');
  print((a + b).format());                // USD 20.00
  print((a - b).format());                // USD 5.00
  print((a * 3).format());                // USD 37.50

  // Cached zero instances
  print(identical(Money.zero('USD'), Money.zero('USD')));  // true

  // Equality
  print(Money(100, 'EUR') == Money(100, 'EUR'));           // true
  print(Money(100, 'EUR') == Money(100, 'USD'));           // false

  // Currency mismatch
  try { Money(100, 'USD') + Money(100, 'EUR'); }
  catch (e) { print(e); }                // ArgumentError

  // Sorting
  final amounts = [Money(300, 'USD'), Money(100, 'USD'), Money(200, 'USD')];
  amounts.sort();
  print(amounts.map((m) => m.format()));  // (USD 1.00, USD 2.00, USD 3.00)
}
```

Run: `dart run bin/exercise4_test.dart`

---

### Exercise 5 (Advanced): Plugin System Architecture

**Objective:** Architect an extensible plugin system using abstract classes, mixins, extension types, and the `base`/`interface` modifiers.

**Instructions:**

1. Create `lib/plugin_system.dart`
2. Define `interface class PluginMetadata` with `name`, `version`, and `description` fields
3. Define `base class PluginLifecycle` with methods `initialize()`, `dispose()`, and a `bool get isInitialized`
4. Define `mixin PluginLogger on PluginLifecycle` providing structured logging tied to lifecycle
5. Define `abstract class Plugin extends PluginLifecycle implements PluginMetadata` as the main contract
6. Create an extension type `PluginId(String value)` with validation (alphanumeric + hyphens only)
7. Create a `PluginRegistry` that stores plugins by `PluginId`, supports registration, lookup, and lifecycle management (initialize all, dispose all)
8. Create two concrete plugins: `AuthPlugin` (with Loggable) and `CachePlugin`
9. Demonstrate the diamond problem: what happens if both a mixin and a parent class declare a method with the same name? Resolve it explicitly.

**Verification:**

```dart
// file: bin/exercise5_test.dart
void main() async {
  final registry = PluginRegistry();

  final auth = AuthPlugin(maxRetries: 3);
  final cache = CachePlugin(maxSize: 100);

  registry.register(PluginId('auth-plugin'), auth);
  registry.register(PluginId('cache-plugin'), cache);

  await registry.initializeAll();
  print(auth.isInitialized);             // true
  print(registry.lookup(PluginId('auth-plugin'))?.name);  // AuthPlugin

  // Invalid plugin ID
  try { PluginId('invalid id!'); }
  catch (e) { print(e); }                // FormatException

  await registry.disposeAll();
  print(auth.isInitialized);             // false
}
```

Run: `dart run bin/exercise5_test.dart`

---

### Exercise 6 (Advanced): Sealed State Machine

**Objective:** Build a type-safe order processing pipeline using sealed classes, enhanced enums, and exhaustive matching.

**Instructions:**

1. Create `lib/order_pipeline.dart`
2. Define an enhanced enum `Priority` with values `low`, `medium`, `high`, `critical`, each carrying a numeric weight and a `maxProcessingTime` (Duration)
3. Define a sealed class `OrderState` with subtypes: `Pending`, `Validated`, `Processing`, `Shipped`, `Delivered`, `Cancelled`, and `Failed`. Each carries relevant data (timestamps, tracking numbers, failure reasons, etc.)
4. Define a class `Order` with an `OrderState` field and methods for each transition (`validate()`, `process()`, `ship()`, etc.). Each method must only be callable from valid predecessor states -- use pattern matching to enforce this and throw `StateError` for illegal transitions
5. Add an extension method on `List<Order>` that groups orders by state type using pattern matching
6. Implement a `describeState(OrderState state)` function using exhaustive switching

**Verification:**

```dart
// file: bin/exercise6_test.dart
void main() {
  var order = Order(id: 'ORD-001', priority: Priority.high);
  print(order.state);                     // Pending

  order.validate();
  print(order.state);                     // Validated

  order.process();
  order.ship('TRACK-12345');
  print(order.state);                     // Shipped(tracking: TRACK-12345)

  order.deliver();
  print(describeState(order.state));      // Delivered at: ...

  // Illegal transition
  var order2 = Order(id: 'ORD-002', priority: Priority.low);
  try { order2.ship('TRACK-999'); }
  catch (e) { print(e); }                // StateError: Cannot ship from Pending

  // Cancellation from valid state
  var order3 = Order(id: 'ORD-003', priority: Priority.medium);
  order3.validate();
  order3.cancel('Customer request');
  print(order3.state);                    // Cancelled(reason: Customer request)
}
```

Run: `dart run bin/exercise6_test.dart`

---

### Exercise 7 (Insane): Entity-Component-System

**Objective:** Design a complete ECS game architecture using mixins, sealed classes, extension types, and class modifiers.

**Instructions:**

1. Create `lib/ecs.dart`
2. Define `extension type EntityId(int value)` for type-safe entity identification
3. Define `sealed class Component` with concrete subtypes: `Position(double x, double y)`, `Velocity(double dx, double dy)`, `Health(int current, int max)`, `Renderable(String sprite)`, `Collider(double radius)`
4. Define `base mixin ComponentStorage` that provides a `Map<EntityId, Component>` storage and typed query methods
5. Define `abstract base class System` with an `update(double deltaTime)` method and a `Set<Type> get requiredComponents` that declares dependencies
6. Implement concrete systems: `MovementSystem` (updates Position from Velocity), `CollisionSystem` (detects overlapping Colliders using Position), `RenderSystem` (outputs Renderable entities sorted by position)
7. Build a `World` class that manages entities, components, and systems. It must: assign `EntityId`s, add/remove components, query entities by component combination, and run all systems in dependency order
8. Implement a `WorldSnapshot` sealed class hierarchy for serializing world state
9. Use exhaustive pattern matching to serialize/deserialize each component type

**Verification:**

```dart
// file: bin/exercise7_test.dart
void main() {
  final world = World();

  final player = world.createEntity();
  world.addComponent(player, Position(0, 0));
  world.addComponent(player, Velocity(1, 0.5));
  world.addComponent(player, Health(100, 100));
  world.addComponent(player, Renderable('player.png'));
  world.addComponent(player, Collider(16.0));

  final enemy = world.createEntity();
  world.addComponent(enemy, Position(10, 10));
  world.addComponent(enemy, Velocity(-0.5, -0.5));
  world.addComponent(enemy, Health(50, 50));
  world.addComponent(enemy, Collider(12.0));

  world.addSystem(MovementSystem());
  world.addSystem(CollisionSystem());
  world.addSystem(RenderSystem());

  // Simulate 3 ticks
  for (var i = 0; i < 3; i++) {
    world.update(0.016);  // ~60fps
  }

  // Query entities with specific components
  final movable = world.query({Position, Velocity});
  print('Movable entities: ${movable.length}');     // 2

  final renderable = world.query({Position, Renderable});
  print('Renderable entities: ${renderable.length}'); // 1 (only player)

  // Snapshot
  final snapshot = world.takeSnapshot();
  print('Snapshot entity count: ${snapshot.entityCount}');  // 2

  // Remove component
  world.removeComponent<Velocity>(enemy);
  print(world.query({Position, Velocity}).length);  // 1
}
```

Run: `dart run bin/exercise7_test.dart`

---

### Exercise 8 (Insane): Type-Safe Builder with Class Modifiers

**Objective:** Implement a query builder that uses class modifiers and the type system to prevent invalid queries at compile time.

**Instructions:**

1. Create `lib/query_builder.dart`
2. Use sealed class hierarchies to model query clauses: `SelectClause`, `FromClause`, `WhereClause`, `JoinClause`, `OrderByClause`, `LimitClause`
3. Use class modifiers to enforce a builder protocol: `final class QueryBuilder<State>` where `State` is a sealed class representing the current builder phase
4. Define phases: `Empty`, `WithFrom`, `WithSelect`, `WithWhere`, `Complete`. Each transition method returns a new builder with the next phase type, making illegal transitions a compile error
5. Implement `WhereClause` as a sealed hierarchy: `Equals`, `GreaterThan`, `LessThan`, `And`, `Or`, `Not` -- allowing recursive composition
6. Use extension types for `TableName(String value)` and `ColumnName(String value)` with validation
7. Implement a `toSql()` method that uses exhaustive pattern matching to convert the builder state into a SQL string
8. Add a `QueryOptimizer` that traverses the sealed clause hierarchy and applies simplification rules (e.g., removing `And(x, AlwaysTrue)` to just `x`)

**Verification:**

```dart
// file: bin/exercise8_test.dart
void main() {
  final query = QueryBuilder()
      .from(TableName('users'))
      .select([ColumnName('id'), ColumnName('name'), ColumnName('email')])
      .where(And(
        Equals(ColumnName('active'), 'true'),
        Or(
          GreaterThan(ColumnName('age'), '18'),
          Equals(ColumnName('role'), 'admin'),
        ),
      ))
      .orderBy(ColumnName('name'), ascending: true)
      .limit(50)
      .build();

  print(query.toSql());
  // SELECT id, name, email FROM users
  // WHERE (active = 'true' AND (age > '18' OR role = 'admin'))
  // ORDER BY name ASC LIMIT 50

  // Optimizer
  final optimized = QueryOptimizer.optimize(
    And(Equals(ColumnName('x'), '1'), AlwaysTrue()),
  );
  print(optimized);                       // Equals(x, 1)

  // Type safety -- these should not compile if uncommented:
  // QueryBuilder().select([...]);       // Error: must call .from() first
  // QueryBuilder().from(...).build();   // Error: must call .select() first
}
```

Run: `dart run bin/exercise8_test.dart`

---

## Summary

This section covered the full spectrum of Dart's object-oriented features:

- **Classes** are the fundamental unit of encapsulation, combining fields, methods, getters/setters, and static members
- **Constructors** come in many forms, each solving a specific initialization problem
- **Inheritance** models "is-a" relationships while **mixins** model "can-do" capabilities
- **Abstract classes** define contracts; **implicit interfaces** let any class serve as an interface
- **Sealed classes** enable exhaustive pattern matching, eliminating an entire category of runtime errors
- **Extension methods** add functionality to types you do not own; **extension types** add type safety at zero runtime cost
- **Class modifiers** give library authors fine-grained control over how their types are used downstream
- **Operator overloading** and **proper equality** are essential for value objects

The progression from basic classes to sealed hierarchies with class modifiers mirrors real-world Dart development: you start simple and reach for advanced features only when they solve a genuine problem.

## What is Next

**Section 05 -- Dart Async Programming** covers Futures, Streams, async/await, Isolates, and concurrency patterns. The classes and sealed hierarchies you built here will serve as the data structures those async operations produce and consume.

## References

- [Dart Language Tour: Classes](https://dart.dev/language/classes)
- [Dart Language Tour: Mixins](https://dart.dev/language/mixins)
- [Dart Language Tour: Class Modifiers](https://dart.dev/language/class-modifiers)
- [Dart Language Tour: Extension Methods](https://dart.dev/language/extension-methods)
- [Dart Language Tour: Extension Types](https://dart.dev/language/extension-types)
- [Dart Language Tour: Sealed Classes](https://dart.dev/language/class-modifiers#sealed)
- [Dart Language Tour: Enums](https://dart.dev/language/enums)
- [Dart Language Tour: Patterns](https://dart.dev/language/patterns)
- [Effective Dart: Design](https://dart.dev/effective-dart/design)
