# Section 04 -- Solutions: Dart Object-Oriented Programming

## How to Use This File

Work through each exercise on your own first. When you get stuck:

1. Read the **Progressive Hints** -- they guide your thinking without giving the answer
2. Check **Common Mistakes** to see if you hit a known pitfall
3. Only then look at the **Full Solution**
4. After solving it, read the **Deep Dive** to understand the underlying principles

---

## Exercise 1: Class Fundamentals

### Progressive Hints

1. Start with the class declaration and the three fields. Remember that `_stock` uses an underscore for library-private access.
2. The default constructor should use `this.name` and `this.price` for automatic assignment. Use an initializer list for `_stock` validation.
3. For the named constructor, use the `: this(...)` pattern or an initializer list to set stock to 0.
4. The `sell` method needs two checks: quantity > 0 and quantity <= _stock.

### Full Solution

```dart
// file: lib/product.dart
class Product {
  final String name;
  final double price;
  int _stock;

  Product(this.name, this.price, int stock) : _stock = stock {
    if (price <= 0) {
      throw ArgumentError('Price must be positive, got: $price');
    }
    if (stock < 0) {
      throw ArgumentError('Stock cannot be negative, got: $stock');
    }
  }

  Product.outOfStock(this.name, this.price) : _stock = 0 {
    if (price <= 0) {
      throw ArgumentError('Price must be positive, got: $price');
    }
  }

  int get stock => _stock;

  void restock(int quantity) {
    if (quantity <= 0) {
      throw ArgumentError('Restock quantity must be positive, got: $quantity');
    }
    _stock += quantity;
  }

  void sell(int quantity) {
    if (quantity <= 0) {
      throw ArgumentError('Sell quantity must be positive, got: $quantity');
    }
    if (quantity > _stock) {
      throw StateError(
        'Insufficient stock: requested $quantity but only $_stock available',
      );
    }
    _stock -= quantity;
  }

  @override
  String toString() => 'Product($name, $price, stock: $_stock)';
}
```

### Common Mistakes

**Putting validation in the constructor body instead of failing fast.** The initializer list runs before the body. If you validate `price` in the body but assign it in the parameter list, the field is already set when validation runs. This works but is conceptually sloppy -- prefer initializer list assertions when possible:

```dart
// Better: fail before the object is fully constructed
Product(this.name, this.price, int stock)
    : assert(price > 0, 'Price must be positive'),
      _stock = stock;
```

**Making `_stock` final.** If stock is final, `sell()` and `restock()` cannot modify it. Only use `final` for fields that never change after construction.

**Forgetting the named constructor validation.** `Product.outOfStock` still needs to validate price. Do not duplicate logic -- consider extracting a `_validatePrice` static method if the validation grows.

---

## Exercise 2: Inheritance Chain

### Progressive Hints

1. Start with `Vehicle`. It needs a constructor and a `describe()` method that returns a string.
2. `Car` extends `Vehicle` and calls `super(make, model, year)`. Add `trunkCapacity` as a new field.
3. `ElectricCar` extends `Car`. Its constructor must pass through all parent fields. Use `super.` syntax or explicit `super()` calls.
4. The Liskov function should use the `Vehicle` type and call `describe()`. If it works with all subtypes without casting, you have demonstrated LSP.

### Full Solution

```dart
// file: lib/vehicles.dart
class Vehicle {
  final String make;
  final String model;
  final int year;

  const Vehicle(this.make, this.model, this.year);

  String describe() => '$year $make $model';
}

class Car extends Vehicle {
  final double trunkCapacity;

  const Car(super.make, super.model, super.year, this.trunkCapacity);

  @override
  String describe() => '${super.describe()} (trunk: ${trunkCapacity}L)';
}

class ElectricCar extends Car {
  final double batteryCapacity;
  final int range;

  const ElectricCar(
    super.make,
    super.model,
    super.year,
    super.trunkCapacity,
    this.batteryCapacity,
    this.range,
  );

  @override
  String describe() =>
      '${super.describe()} [EV: ${batteryCapacity}kWh, ${range}km range]';
}
```

### Common Mistakes

**Not calling `super.describe()` in overrides.** If `ElectricCar.describe()` builds its own string from scratch, you duplicate the format logic from `Vehicle` and `Car`. When the parent format changes, the child is out of sync. Always delegate upward.

**Using `super.make` syntax (Dart 3+) incorrectly.** The `super.` parameter syntax only works in the constructor parameter list, not in initializer lists. If you need to transform a value before passing it to super, use the traditional `: super(transformedValue)` form.

**Thinking `const` constructors require `const` invocation.** A `const` constructor can be called without `const` -- the object is simply allocated at runtime instead of compile time. The `const` keyword on the constructor is a capability, not a mandate.

---

## Exercise 3: Mixin Composition

### Progressive Hints

1. Define `Loggable` first. It needs a `List<String> _logs` and a method to add entries. Expose logs as `List.unmodifiable`.
2. `Validatable` has an abstract `validate()` that returns `List<String>`. The concrete `isValid` checks if that list is empty.
3. `Persistable on Validatable` means you can only apply it to classes that also use `Validatable`. The `save()` method calls `validate()`.
4. `mixin class BaseEntity` is both a class you can extend and a mixin you can apply. Give it an `id` field.
5. The `User` class declaration order matters: `extends BaseEntity with Loggable, Validatable, Persistable`.

### Full Solution

```dart
// file: lib/entity_system.dart
mixin Loggable {
  final List<String> _logs = [];

  void log(String message) {
    _logs.add(message);
  }

  List<String> get logs => List.unmodifiable(_logs);
}

mixin Validatable {
  List<String> validate();

  bool get isValid => validate().isEmpty;
}

mixin Persistable on Validatable {
  bool _persisted = false;

  bool get isPersisted => _persisted;

  Future<bool> save() async {
    final errors = validate();
    if (errors.isNotEmpty) {
      return false;
    }
    // Simulate async persistence
    await Future.delayed(Duration(milliseconds: 10));
    _persisted = true;
    return true;
  }
}

mixin class BaseEntity {
  final int id;

  BaseEntity({required this.id});

  BaseEntity copyWith({int? id}) {
    return BaseEntity(id: id ?? this.id);
  }
}

class User extends BaseEntity with Loggable, Validatable, Persistable {
  final String name;
  final String email;

  User({required super.id, required this.name, required this.email});

  @override
  List<String> validate() {
    final errors = <String>[];
    if (name.isEmpty) errors.add('Name cannot be empty');
    if (!email.contains('@')) errors.add('Invalid email');
    return errors;
  }

  @override
  String toString() => 'User(id: $id, name: $name, email: $email)';
}
```

### Common Mistakes

**Declaring `Persistable` without `on Validatable`.** Without the `on` clause, `Persistable` cannot call `validate()` because it does not know it exists. The `on` clause tells the compiler "this mixin requires the host class to also use Validatable."

**Applying mixins in the wrong order.** `Persistable on Validatable` means `Validatable` must appear before `Persistable` in the `with` clause. If you write `with Persistable, Validatable`, the compiler cannot satisfy the `on` constraint and you get an error.

**Making `_logs` a getter returning a new list each time.** If you write `List<String> get _logs => []`, you get a fresh empty list on every access. Fields declared in mixins are stored on the instance, just like class fields.

### Deep Dive: Mixin Linearization

Dart uses C3 linearization to resolve mixin conflicts. When multiple mixins declare a method with the same name, the last mixin in the `with` clause wins. For `class A with B, C`, if both `B` and `C` define `foo()`, then `C.foo()` is the one that gets called. You can still reach `B.foo()` using `super.foo()` from within `C`, because `super` walks the linearization chain.

---

## Exercise 4: Factory Constructors and Operator Overloading

### Progressive Hints

1. Store the amount in cents (int) to avoid floating-point precision issues. This is a standard pattern for financial calculations.
2. The factory constructor needs a `static final Map<String, Money>` for the cache. Return `_cache.putIfAbsent(currency, () => Money(0, currency))`.
3. For operator `+`, check `currency == other.currency` before adding amounts.
4. For `==`, check both `amount` and `currency`. Use `identical(this, other)` as an early return for performance.
5. Implement `Comparable<Money>` by comparing amounts. Throw if currencies differ.

### Full Solution

```dart
// file: lib/money.dart
class Money implements Comparable<Money> {
  final int amount;  // in cents
  final String currency;

  const Money(this.amount, this.currency);

  static final Map<String, Money> _zeroCache = {};

  factory Money.zero(String currency) {
    return _zeroCache.putIfAbsent(currency, () => Money(0, currency));
  }

  Money operator +(Money other) {
    _assertSameCurrency(other);
    return Money(amount + other.amount, currency);
  }

  Money operator -(Money other) {
    _assertSameCurrency(other);
    return Money(amount - other.amount, currency);
  }

  Money operator *(int multiplier) {
    return Money(amount * multiplier, currency);
  }

  void _assertSameCurrency(Money other) {
    if (currency != other.currency) {
      throw ArgumentError(
        'Cannot operate on different currencies: $currency vs ${other.currency}',
      );
    }
  }

  String format() {
    final dollars = amount ~/ 100;
    final cents = (amount % 100).abs();
    final sign = amount < 0 ? '-' : '';
    return '$currency $sign$dollars.${cents.toString().padLeft(2, '0')}';
  }

  @override
  int compareTo(Money other) {
    _assertSameCurrency(other);
    return amount.compareTo(other.amount);
  }

  @override
  bool operator ==(Object other) =>
      identical(this, other) ||
      other is Money && amount == other.amount && currency == other.currency;

  @override
  int get hashCode => Object.hash(amount, currency);

  @override
  String toString() => 'Money($amount, $currency)';
}
```

### Common Mistakes

**Using `double` for amount.** Floating-point arithmetic produces rounding errors: `0.1 + 0.2 != 0.3`. Financial applications store values in the smallest currency unit (cents) as integers.

**Forgetting to override `hashCode` when overriding `==`.** If two `Money` objects are equal according to `==` but have different hash codes, hash-based collections (Map, Set) will behave incorrectly. Always override both together.

**Not checking `identical(this, other)` in `==`.** This is a performance optimization: if the two references point to the same object, skip field comparisons. It also handles the case where `Money.zero('USD') == Money.zero('USD')` without any field checking.

**Making the cache instance-level instead of static.** Factory constructors are static by nature. The cache must be a `static` field, otherwise each "instance" would have its own cache, defeating the purpose.

### Alternative: Using `equatable` Package

In production Dart code, many teams use the `equatable` package to auto-generate `==` and `hashCode`. However, understanding the manual implementation is essential because `equatable` has limitations (it uses `runtimeType` comparison, which can break with inheritance).

---

## Exercise 5: Plugin System Architecture

### Progressive Hints

1. Start with `interface class PluginMetadata` -- three final String fields, a constructor.
2. `base class PluginLifecycle` needs a `bool _initialized` flag. `initialize()` sets it to true, `dispose()` sets it to false.
3. The `mixin PluginLogger on PluginLifecycle` can override `initialize()` and `dispose()` to add logging around the lifecycle, calling `super.initialize()`.
4. `Plugin` is abstract and extends `PluginLifecycle` while implementing `PluginMetadata`.
5. For the diamond problem: if both a mixin and the parent class define `initialize()`, the mixin's version wins due to linearization. Call `super.initialize()` to invoke the parent chain.

### Full Solution

```dart
// file: lib/plugin_system.dart
interface class PluginMetadata {
  final String name;
  final String version;
  final String description;

  const PluginMetadata({
    required this.name,
    required this.version,
    required this.description,
  });
}

base class PluginLifecycle {
  bool _initialized = false;

  bool get isInitialized => _initialized;

  Future<void> initialize() async {
    _initialized = true;
  }

  Future<void> dispose() async {
    _initialized = false;
  }
}

mixin PluginLogger on PluginLifecycle {
  final List<String> _logEntries = [];

  List<String> get logEntries => List.unmodifiable(_logEntries);

  void _log(String message) {
    _logEntries.add('[${DateTime.now().toIso8601String()}] $message');
  }

  @override
  Future<void> initialize() async {
    _log('Initializing...');
    await super.initialize();
    _log('Initialized successfully');
  }

  @override
  Future<void> dispose() async {
    _log('Disposing...');
    await super.dispose();
    _log('Disposed');
  }
}

extension type PluginId(String value) {
  PluginId.validated(String raw) : value = raw {
    if (!RegExp(r'^[a-z0-9\-]+$').hasMatch(raw)) {
      throw FormatException(
        'Plugin ID must be alphanumeric with hyphens only, got: $raw',
      );
    }
  }

  factory PluginId(String raw) = PluginId.validated;
}

abstract class Plugin extends PluginLifecycle implements PluginMetadata {}

class AuthPlugin extends Plugin with PluginLogger {
  final int maxRetries;

  AuthPlugin({required this.maxRetries});

  @override
  String get name => 'AuthPlugin';
  @override
  String get version => '1.0.0';
  @override
  String get description => 'Handles authentication and authorization';
}

class CachePlugin extends Plugin {
  final int maxSize;

  CachePlugin({required this.maxSize});

  @override
  String get name => 'CachePlugin';
  @override
  String get version => '1.0.0';
  @override
  String get description => 'In-memory caching with LRU eviction';
}

class PluginRegistry {
  final Map<String, Plugin> _plugins = {};

  void register(PluginId id, Plugin plugin) {
    if (_plugins.containsKey(id.value)) {
      throw StateError('Plugin already registered: ${id.value}');
    }
    _plugins[id.value] = plugin;
  }

  Plugin? lookup(PluginId id) => _plugins[id.value];

  Future<void> initializeAll() async {
    for (final plugin in _plugins.values) {
      await plugin.initialize();
    }
  }

  Future<void> disposeAll() async {
    for (final plugin in _plugins.values) {
      await plugin.dispose();
    }
  }
}
```

### Common Mistakes

**Forgetting `on PluginLifecycle` in the mixin.** Without the `on` clause, `PluginLogger` cannot call `super.initialize()` because the compiler does not know that the host class has that method.

**Using `implements` instead of `extends` for `PluginLifecycle`.** If `Plugin` implements `PluginLifecycle` instead of extending it, it does not inherit the implementation. Every concrete plugin would need to re-implement `_initialized`, `initialize()`, and `dispose()`.

**Not handling the diamond problem.** When `AuthPlugin extends Plugin with PluginLogger`, both `Plugin` (via `PluginLifecycle`) and `PluginLogger` define `initialize()`. Dart's linearization means `PluginLogger.initialize()` wins. It calls `super.initialize()`, which goes to `PluginLifecycle.initialize()`. This works correctly as long as every override calls `super`.

---

## Exercise 6: Sealed State Machine

### Progressive Hints

1. Start with the `Priority` enum. Each value carries a weight (int) and a `maxProcessingTime` (Duration).
2. Define `sealed class OrderState` and then one subclass per state. `Pending` has a `createdAt`, `Shipped` has a `trackingNumber`, `Failed` has a `reason`, etc.
3. The `Order` class holds an `OrderState` field. Each transition method pattern-matches on the current state to decide if the transition is legal.
4. For the extension method on `List<Order>`, use `whereType` or pattern matching to group by state type.
5. The `describeState` function must handle every sealed subtype -- the compiler will tell you if you miss one.

### Full Solution

```dart
// file: lib/order_pipeline.dart
enum Priority implements Comparable<Priority> {
  low(1, Duration(hours: 72)),
  medium(2, Duration(hours: 48)),
  high(3, Duration(hours: 24)),
  critical(4, Duration(hours: 6));

  final int weight;
  final Duration maxProcessingTime;

  const Priority(this.weight, this.maxProcessingTime);

  @override
  int compareTo(Priority other) => weight.compareTo(other.weight);
}

// Each state carries the data relevant to that phase
sealed class OrderState { const OrderState(); }
class Pending extends OrderState { final DateTime createdAt; const Pending(this.createdAt); }
class Validated extends OrderState { final DateTime validatedAt; const Validated(this.validatedAt); }
class Processing extends OrderState { final DateTime startedAt; const Processing(this.startedAt); }
class Shipped extends OrderState { final String trackingNumber; final DateTime shippedAt; const Shipped(this.trackingNumber, this.shippedAt); }
class Delivered extends OrderState { final DateTime deliveredAt; const Delivered(this.deliveredAt); }
class Cancelled extends OrderState { final String reason; final DateTime cancelledAt; const Cancelled(this.reason, this.cancelledAt); }
class Failed extends OrderState { final String reason; final Object? error; const Failed(this.reason, {this.error}); }

class Order {
  final String id;
  final Priority priority;
  OrderState _state;

  Order({required this.id, required this.priority})
      : _state = Pending(DateTime.now());

  OrderState get state => _state;

  void validate() {
    _state = switch (_state) {
      Pending() => Validated(DateTime.now()),
      _ => throw StateError('Cannot validate from ${_state.runtimeType}'),
    };
  }

  void process() {
    _state = switch (_state) {
      Validated() => Processing(DateTime.now()),
      _ => throw StateError('Cannot process from ${_state.runtimeType}'),
    };
  }

  void ship(String trackingNumber) {
    _state = switch (_state) {
      Processing() => Shipped(trackingNumber, DateTime.now()),
      _ => throw StateError('Cannot ship from ${_state.runtimeType}'),
    };
  }

  void deliver() {
    _state = switch (_state) {
      Shipped() => Delivered(DateTime.now()),
      _ => throw StateError('Cannot deliver from ${_state.runtimeType}'),
    };
  }

  void cancel(String reason) {
    _state = switch (_state) {
      Pending() || Validated() || Processing() =>
        Cancelled(reason, DateTime.now()),
      _ => throw StateError('Cannot cancel from ${_state.runtimeType}'),
    };
  }

  void fail(String reason, {Object? error}) {
    _state = switch (_state) {
      Delivered() || Cancelled() || Failed() =>
        throw StateError('Cannot fail from ${_state.runtimeType}'),
      _ => Failed(reason, error: error),
    };
  }
}

String describeState(OrderState state) {
  return switch (state) {
    Pending(:final createdAt) => 'Order pending since $createdAt',
    Validated(:final validatedAt) => 'Order validated at $validatedAt',
    Processing(:final startedAt) => 'Order processing since $startedAt',
    Shipped(:final trackingNumber) => 'Order shipped, tracking: $trackingNumber',
    Delivered(:final deliveredAt) => 'Delivered at: $deliveredAt',
    Cancelled(:final reason) => 'Order cancelled: $reason',
    Failed(:final reason) => 'Order failed: $reason',
  };
}

extension OrderListGrouping on List<Order> {
  Map<String, List<Order>> groupByState() {
    final groups = <String, List<Order>>{};
    for (final order in this) {
      final key = switch (order.state) {
        Pending() => 'pending',
        Validated() => 'validated',
        Processing() => 'processing',
        Shipped() => 'shipped',
        Delivered() => 'delivered',
        Cancelled() => 'cancelled',
        Failed() => 'failed',
      };
      groups.putIfAbsent(key, () => []).add(order);
    }
    return groups;
  }
}
```

### Common Mistakes

**Not making `OrderState` sealed.** Without `sealed`, the compiler cannot verify exhaustive matching. You would need a default `_` case in every switch, defeating the purpose of the pattern.

**Allowing cancellation from Shipped or Delivered.** Think about what "cancel" means in a real system. Once goods are shipped, cancellation is a different process (return, refund). Modeling these constraints in code prevents bugs.

**Using `if/else` chains instead of pattern matching.** While functionally equivalent, `switch` with sealed classes gives you compile-time exhaustiveness. An `if/else` chain does not.

---

## Exercise 7: Entity-Component-System

### Progressive Hints

1. Start with `EntityId` as an extension type wrapping `int`. Add a `static int _nextId = 0` counter in `World`.
2. Define each `Component` subtype in the sealed hierarchy. They are pure data containers.
3. `World` needs three collections: a `Set<EntityId>` of alive entities, a `Map<Type, Map<EntityId, Component>>` for component storage (indexed by component type), and a `List<System>` for registered systems.
4. The `query` method takes a `Set<Type>` and returns all entity IDs that have all the requested component types.
5. Each `System` accesses components through the `World` reference. Pass the world to `update()` or inject it on registration.

### Full Solution

The key architectural decisions are shown below. Implement `CollisionSystem` and `RenderSystem` following the same pattern as `MovementSystem`.

```dart
// file: lib/ecs.dart
extension type EntityId(int value) {
  bool get isValid => value >= 0;
}

sealed class Component { const Component(); }
class Position extends Component { double x, y; Position(this.x, this.y); }
class Velocity extends Component { final double dx, dy; const Velocity(this.dx, this.dy); }
class Health extends Component { int current; final int max; Health(this.current, this.max); }
class Renderable extends Component { final String sprite; const Renderable(this.sprite); }
class Collider extends Component { final double radius; const Collider(this.radius); }

abstract base class System {
  Set<Type> get requiredComponents;
  void update(World world, double deltaTime);
}

final class MovementSystem extends System {
  @override
  Set<Type> get requiredComponents => {Position, Velocity};

  @override
  void update(World world, double deltaTime) {
    for (final entity in world.query({Position, Velocity})) {
      final pos = world.getComponent<Position>(entity)!;
      final vel = world.getComponent<Velocity>(entity)!;
      pos.x += vel.dx * deltaTime;
      pos.y += vel.dy * deltaTime;
    }
  }
}
// CollisionSystem: query {Position, Collider}, check pairwise distances
// RenderSystem: query {Position, Renderable}, sort by Y then X, print

class World {
  int _nextId = 0;
  final Set<int> _entities = {};
  // KEY INSIGHT: index by component type for O(1) type-based lookups
  final Map<Type, Map<int, Component>> _components = {};
  final List<System> _systems = [];

  EntityId createEntity() {
    final id = EntityId(_nextId++);
    _entities.add(id.value);
    return id;
  }

  void addComponent<T extends Component>(EntityId entity, T component) {
    _components.putIfAbsent(T, () => {})[entity.value] = component;
  }

  T? getComponent<T extends Component>(EntityId entity) {
    return _components[T]?[entity.value] as T?;
  }

  void removeComponent<T extends Component>(EntityId entity) {
    _components[T]?.remove(entity.value);
  }

  Set<EntityId> query(Set<Type> componentTypes) {
    return _entities
        .where((id) => componentTypes.every(
              (type) => _components[type]?.containsKey(id) ?? false))
        .map(EntityId.new)
        .toSet();
  }

  void addSystem(System system) => _systems.add(system);

  void update(double deltaTime) {
    for (final system in _systems) {
      system.update(this, deltaTime);
    }
  }

  // Exhaustive serialization via sealed class matching
  Map<String, dynamic> _serializeComponent(Component c) => switch (c) {
    Position(:final x, :final y) => {'x': x, 'y': y},
    Velocity(:final dx, :final dy) => {'dx': dx, 'dy': dy},
    Health(:final current, :final max) => {'current': current, 'max': max},
    Renderable(:final sprite) => {'sprite': sprite},
    Collider(:final radius) => {'radius': radius},
  };
}
```

### Common Mistakes

**Storing components as `Map<EntityId, List<Component>>`.** This makes type-based queries O(n) per entity. The correct structure is `Map<Type, Map<EntityId, Component>>` for O(1) lookups.

**Forgetting to clean up components when destroying an entity.** Always iterate all component stores on entity destruction to avoid memory leaks.

**Making `Position` immutable.** ECS components are mutable data bags. If `Position` is immutable, `MovementSystem` must create and re-register new instances every frame.

---

## Exercise 8: Type-Safe Builder with Class Modifiers

### Progressive Hints

1. Define sealed hierarchies for clauses first. `WhereClause` is recursive: `And(WhereClause left, WhereClause right)`.
2. The builder pattern uses generic type parameters to track state. `QueryBuilder<Empty>`, `QueryBuilder<WithFrom>`, etc. Each method returns a builder with a different type parameter.
3. Extension types for `TableName` and `ColumnName` add validation at construction time.
4. The `toSql()` method walks the clause hierarchy recursively. Pattern matching on `WhereClause` is where sealed classes shine.
5. The optimizer is a recursive function that pattern-matches on `WhereClause` subtypes and applies simplification rules.

### Full Solution

The core insight is using sealed phase markers with typed extensions to restrict which methods are available at each step.

```dart
// file: lib/query_builder.dart
extension type TableName(String value) {
  factory TableName(String raw) {
    if (!RegExp(r'^[a-z_][a-z0-9_]*$').hasMatch(raw)) {
      throw FormatException('Invalid table name: $raw');
    }
    return TableName._(raw);
  }
  const TableName._(this.value);
}

extension type ColumnName(String value) {
  factory ColumnName(String raw) {
    if (!RegExp(r'^[a-z_][a-z0-9_]*$').hasMatch(raw)) {
      throw FormatException('Invalid column name: $raw');
    }
    return ColumnName._(raw);
  }
  const ColumnName._(this.value);
}

// Recursive where clause hierarchy
sealed class WhereClause { const WhereClause(); }
class Equals extends WhereClause { final ColumnName column; final String value; const Equals(this.column, this.value); }
class GreaterThan extends WhereClause { final ColumnName column; final String value; const GreaterThan(this.column, this.value); }
class LessThan extends WhereClause { final ColumnName column; final String value; const LessThan(this.column, this.value); }
class And extends WhereClause { final WhereClause left, right; const And(this.left, this.right); }
class Or extends WhereClause { final WhereClause left, right; const Or(this.left, this.right); }
class Not extends WhereClause { final WhereClause clause; const Not(this.clause); }
class AlwaysTrue extends WhereClause { const AlwaysTrue(); }
class AlwaysFalse extends WhereClause { const AlwaysFalse(); }

// Phase markers -- sealed so they form a closed set
sealed class BuilderPhase {}
final class Empty extends BuilderPhase {}
final class WithFrom extends BuilderPhase {}
final class WithSelect extends BuilderPhase {}
final class WithWhere extends BuilderPhase {}

final class QueryBuilder<Phase extends BuilderPhase> {
  final TableName? _table;
  final List<ColumnName> _columns;
  final WhereClause? _where;
  final (ColumnName, bool)? _orderBy;
  final int? _limit;

  QueryBuilder._({TableName? table, List<ColumnName> columns = const [],
      WhereClause? where, (ColumnName, bool)? orderBy, int? limit})
      : _table = table, _columns = columns, _where = where,
        _orderBy = orderBy, _limit = limit;

  factory QueryBuilder() => QueryBuilder._() as QueryBuilder<Phase>;
}

// THE KEY TRICK: phase-specific extensions restrict available methods
extension QueryBuilderEmpty on QueryBuilder<Empty> {
  QueryBuilder<WithFrom> from(TableName table) =>
      QueryBuilder._(table: table);
}

extension QueryBuilderWithFrom on QueryBuilder<WithFrom> {
  QueryBuilder<WithSelect> select(List<ColumnName> columns) =>
      QueryBuilder._(table: _table, columns: columns);
}

extension QueryBuilderWithSelect on QueryBuilder<WithSelect> {
  QueryBuilder<WithWhere> where(WhereClause clause) =>
      QueryBuilder._(table: _table, columns: _columns, where: clause);
  Query build() => Query(table: _table!, columns: _columns);
}

extension QueryBuilderWithWhere on QueryBuilder<WithWhere> {
  QueryBuilder<WithWhere> orderBy(ColumnName col, {bool ascending = true}) =>
      QueryBuilder._(table: _table, columns: _columns, where: _where,
          orderBy: (col, ascending));
  QueryBuilder<WithWhere> limit(int count) =>
      QueryBuilder._(table: _table, columns: _columns, where: _where,
          orderBy: _orderBy, limit: count);
  Query build() => Query(table: _table!, columns: _columns,
      whereClause: _where, orderBy: _orderBy, limit: _limit);
}
```

The `Query.toSql()` method uses exhaustive pattern matching on `WhereClause`. The `QueryOptimizer` applies Boolean algebra rules (identity, annihilation, double negation) recursively:

```dart
class QueryOptimizer {
  static WhereClause optimize(WhereClause clause) => switch (clause) {
    And(:final left, AlwaysTrue()) => optimize(left),
    And(AlwaysTrue(), :final right) => optimize(right),
    And(_, AlwaysFalse()) => const AlwaysFalse(),
    And(AlwaysFalse(), _) => const AlwaysFalse(),
    Or(:final left, AlwaysFalse()) => optimize(left),
    Or(AlwaysFalse(), :final right) => optimize(right),
    Or(_, AlwaysTrue()) => const AlwaysTrue(),
    Not(Not(:final clause)) => optimize(clause),
    And(:final left, :final right) => And(optimize(left), optimize(right)),
    Or(:final left, :final right) => Or(optimize(left), optimize(right)),
    Not(:final clause) => Not(optimize(clause)),
    _ => clause,
  };
}
```

### Common Mistakes

**Putting all methods on `QueryBuilder` directly.** The type parameter does nothing unless methods are gated behind phase-specific extensions. Without extensions, `QueryBuilder<Empty>` and `QueryBuilder<WithFrom>` expose the same API.

**Forgetting `AlwaysTrue`/`AlwaysFalse` in the sealed hierarchy.** The optimizer needs identity and annihilation elements for Boolean algebra. Without them, simplification rules have nothing to reduce.

**Making `QueryBuilder` mutable.** Each method must return a new instance. Mutation breaks method chaining when the same builder is reused in multiple chains.

---

## General Debugging Tips

1. **"The type X is not a subtype of type Y"** -- This usually means you are using `implements` where you need `extends`, or vice versa. Check whether you want to inherit implementation or just the interface contract.

2. **"Mixin X can only be applied to classes that extend Y"** -- You have a mixin with an `on` clause, and the target class does not satisfy it. Make sure the class extends (or mixes in) the required type before the problematic mixin in the `with` clause.

3. **"The switch is not exhaustive"** -- Your sealed class has a subtype you are not matching. Add the missing case or add a wildcard `_` (though with sealed classes, prefer explicit cases for safety).

4. **"Can't access instance member in initializer"** -- Initializer lists run before `this` is available. You cannot call instance methods or access non-final fields in an initializer list. Use the constructor body instead, or make the computation static.

5. **"Extension type erasure surprises"** -- Remember that extension types erase at runtime. `is` checks against extension types always behave as checks against the representation type. If you need runtime type discrimination, use a regular class.

6. **Hash code contract violation** -- If objects that are `==` have different hash codes, `Set` and `Map` will behave incorrectly (duplicates appear, lookups fail). Always override `hashCode` when you override `==`, and use `Object.hash()` for combining multiple fields.
