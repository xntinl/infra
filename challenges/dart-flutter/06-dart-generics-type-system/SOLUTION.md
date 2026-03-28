# Section 06 Solutions -- Dart Generics and the Type System

## How to Use This File

Work through each exercise on your own first. Seriously. The struggle is where the learning happens.

If you get stuck, use this progression:

1. **Read the hints** -- they point you in the right direction without giving the answer.
2. **Check common mistakes** -- you might be hitting a known trap.
3. **Look at the solution** only after a genuine attempt.
4. **Read the deep dive** after solving it, even if your solution works. Understanding *why* something works matters more than making it work.

---

## Exercise 1 -- Generic Stack and Pair

### Progressive Hints

**Hint 1:** `Stack<T>` is backed by a `List<T>`. The "top" of the stack is the last element of the list. `push` is `add`, `pop` is `removeLast`.

**Hint 2:** `toList` should return a reversed copy. Do not return the internal list directly -- that would let callers mutate your stack's internals.

**Hint 3:** `Pair.map` needs its own type parameters `<C, D>` separate from the class parameters `<A, B>`. The return type is `Pair<C, D>`.

### Common Mistakes

**Returning the internal list from toList:** If you write `List<T> toList() => _items`, callers can `add` or `removeAt` and corrupt your stack. Always return a copy: `_items.reversed.toList()`.

**Forgetting error handling on peek:** `peek` on an empty stack should also throw, not just `pop`. Both access `.last` on an empty list, which throws a `StateError` by default, but it is better to throw your own with context.

**Making Pair mutable:** `Pair` should have `final` fields. If `first` and `second` are mutable, `swap()` becomes confusing -- does it mutate or return a new instance? Always return new.

### Full Solution

```dart
// file: exercise_01_solution.dart

class Stack<T> {
  final List<T> _items = [];

  void push(T item) => _items.add(item);

  T pop() {
    if (_items.isEmpty) {
      throw StateError('Cannot pop from an empty stack');
    }
    return _items.removeLast();
  }

  T get peek {
    if (_items.isEmpty) {
      throw StateError('Cannot peek at an empty stack');
    }
    return _items.last;
  }

  bool get isEmpty => _items.isEmpty;
  int get length => _items.length;

  List<T> toList() => _items.reversed.toList();

  @override
  String toString() => 'Stack(${_items.reversed.join(', ')})';
}

class Pair<A, B> {
  final A first;
  final B second;

  const Pair(this.first, this.second);

  Pair<B, A> swap() => Pair<B, A>(second, first);

  Pair<C, D> map<C, D>(C Function(A) mapFirst, D Function(B) mapSecond) {
    return Pair<C, D>(mapFirst(first), mapSecond(second));
  }

  @override
  String toString() => 'Pair($first, $second)';

  @override
  bool operator ==(Object other) =>
      other is Pair<A, B> && other.first == first && other.second == second;

  @override
  int get hashCode => Object.hash(first, second);
}

void main() {
  final stack = Stack<int>();
  stack.push(10);
  stack.push(20);
  stack.push(30);
  assert(stack.length == 3);
  assert(stack.peek == 30);
  assert(stack.pop() == 30);
  assert(stack.toList().first == 20);

  final emptyStack = Stack<String>();
  try {
    emptyStack.pop();
    assert(false, 'Should have thrown');
  } on StateError catch (e) {
    print('Caught: $e');
  }

  final pair = Pair<String, int>('age', 30);
  assert(pair.swap().first == 30);
  final mapped = pair.map((s) => s.length, (i) => i.toDouble());
  assert(mapped.first == 3);
  assert(mapped.second == 30.0);

  print('Exercise 01 passed');
}
```

### Deep Dive

The `map` method on `Pair` introduces *method-level type parameters* (`<C, D>`) that are independent from the class-level parameters (`<A, B>`). This is a critical distinction. The class knows `A` and `B` at construction time, but `map` introduces `C` and `D` only when called. Dart infers `C` and `D` from the return types of the two functions you pass in, so you rarely need to write them explicitly.

The `==` override uses `is Pair<A, B>` -- this works because Dart generics are reified. In Java, `other is Pair<A, B>` would always be true regardless of the actual type parameters due to erasure. In Dart, it genuinely checks the type arguments.

---

## Exercise 2 -- Bounded Types and Generic Functions

### Progressive Hints

**Hint 1:** `topRanked` reduces a list to one item by comparing `.rank`. Use `reduce` or a simple loop.

**Hint 2:** The return type is `T`, not `Rankable`. This is the whole point of the bound. If you wrote the return type as `Rankable`, callers would lose access to subtype-specific members.

**Hint 3:** `filterByMinRank` returns `List<T>`, preserving the specific type. Use `.where(...).toList()`.

### Common Mistakes

**Returning Rankable instead of T:** If your function signature is `Rankable topRanked(List<Rankable> items)`, it works but callers lose the specific type. They cannot call `best.name` without a cast. The generic bound preserves the exact type.

**Empty list handling:** Always validate inputs. `reduce` throws on empty lists with a confusing error. Throw your own `ArgumentError` with context.

### Full Solution

```dart
// file: exercise_02_solution.dart

abstract class Rankable {
  int get rank;
}

class Player implements Rankable {
  final String name;
  @override
  final int rank;
  Player(this.name, this.rank);
}

class Card implements Rankable {
  final String suit;
  final int value;
  Card(this.suit, this.value);

  @override
  int get rank => value;
}

T topRanked<T extends Rankable>(List<T> items) {
  if (items.isEmpty) {
    throw ArgumentError('Cannot find top ranked in an empty list');
  }
  return items.reduce((a, b) => a.rank >= b.rank ? a : b);
}

List<T> filterByMinRank<T extends Rankable>(List<T> items, int minRank) {
  return items.where((item) => item.rank >= minRank).toList();
}

void main() {
  final players = [
    Player('Alice', 1500),
    Player('Bob', 2100),
    Player('Carol', 1800),
  ];

  final best = topRanked(players);
  print('Best player: ${best.name} with rank ${best.rank}');
  assert(best.name == 'Bob');

  final elitePlayers = filterByMinRank(players, 1700);
  assert(elitePlayers.length == 2);
  assert(elitePlayers.every((p) => p.rank >= 1700));
  print('Elite: ${elitePlayers.map((p) => p.name)}');

  final cards = [Card('hearts', 10), Card('spades', 14), Card('diamonds', 7)];
  final bestCard = topRanked(cards);
  print('Best card: ${bestCard.suit} ${bestCard.value}');
  assert(bestCard.suit == 'spades');

  print('Exercise 02 passed');
}
```

### Deep Dive

Notice the difference between `T topRanked<T extends Rankable>(List<T> items)` and a hypothetical `Rankable topRanked(List<Rankable> items)`. Both compile. Both return the correct element. But the generic version returns `Player` when you pass `List<Player>`, while the non-generic version returns `Rankable` and forces the caller to cast. The generic approach pushes knowledge *forward* through the type system instead of discarding it.

This principle -- "preserve the most specific type" -- is foundational in generic API design. Whenever you find yourself writing a downcast (`as Player`), ask whether a bounded generic could eliminate it.

---

## Exercise 3 -- Generic Repository Pattern

### Progressive Hints

**Hint 1:** The storage is `final Map<String, T> _store = {}`. The key is `entity.id`.

**Hint 2:** `findWhere` uses the predicate on `_store.values`. Return a new list, not a view.

**Hint 3:** `delete` should be silent if the key does not exist (idempotent), or you can return a `bool`. The exercise does not assert on the return, so either works.

### Common Mistakes

**Exposing the internal map:** If `findAll` returns `_store.values.toList()` that is fine, but returning `_store.values` directly gives callers a live view that changes when the repository changes. Always `.toList()`.

**Forgetting that save is an upsert:** If you call `save` twice with the same `id`, it should overwrite. A `Map` does this naturally, but if you used a `List` internally, you would need to check for duplicates.

### Full Solution

```dart
// file: exercise_03_solution.dart

abstract class Entity {
  String get id;
}

class Repository<T extends Entity> {
  final Map<String, T> _store = {};

  void save(T entity) {
    _store[entity.id] = entity;
  }

  T? findById(String id) => _store[id];

  List<T> findAll() => _store.values.toList();

  List<T> findWhere(bool Function(T) predicate) {
    return _store.values.where(predicate).toList();
  }

  void delete(String id) {
    _store.remove(id);
  }

  int get count => _store.length;
}

class User implements Entity {
  @override
  final String id;
  final String name;
  final String email;
  User(this.id, this.name, this.email);
}

class Product implements Entity {
  @override
  final String id;
  final String title;
  final double price;
  Product(this.id, this.title, this.price);
}

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

  print('Exercise 03 passed');
}
```

### Deep Dive

This pattern scales well. In a real application, you would define `Repository<T extends Entity>` as an abstract class (or interface) and provide both `InMemoryRepository<T>` and `FirestoreRepository<T>` implementations. Because the interface is generic, you swap implementations without changing any calling code. The `T extends Entity` bound is important: it guarantees that every entity has an `id`, which is the minimum contract a repository needs to store and retrieve items.

Notice that `findWhere` takes `bool Function(T)`, not `bool Function(Entity)`. This means the predicate receives a `User` when called on `Repository<User>`, so you can filter by `name` or `email` without casting. Again: the generic preserves the specific type through the entire call chain.

---

## Exercise 4 -- Type-Safe Event Bus with Extension Types

### Progressive Hints

**Hint 1:** Internally, the handlers map is `Map<String, List<Function>>`. You lose type safety inside the class, but the public API (which uses `EventChannel<T>`) enforces it. This is a common pattern: type-safe facade over type-erased internals.

**Hint 2:** In `emit`, cast each stored handler to `void Function(T)` before calling it. This is safe because `on` already constrained the type when the handler was registered.

**Hint 3:** `off` needs to remove a specific function reference from the list. Dart compares function references with `==`, which works for top-level and static functions but not for closures created inline. Document this limitation.

### Common Mistakes

**Trying to use a generic map:** You cannot create `Map<EventChannel<T>, List<void Function(T)>>` because each channel has a different `T`. The map must use a common representation internally.

**Forgetting the null check in emit:** If no handlers are registered for a channel, `_handlers[channel.value]` is null. Use `?` or default to empty list.

### Full Solution

```dart
// file: exercise_04_solution.dart

extension type EventChannel<T>(String value) {}

class EventBus {
  final Map<String, List<Function>> _handlers = {};

  void on<T>(EventChannel<T> channel, void Function(T) handler) {
    _handlers.putIfAbsent(channel.value, () => []).add(handler);
  }

  void emit<T>(EventChannel<T> channel, T event) {
    final handlers = _handlers[channel.value];
    if (handlers == null) return;
    for (final handler in handlers) {
      (handler as void Function(T))(event);
    }
  }

  void off<T>(EventChannel<T> channel, void Function(T) handler) {
    _handlers[channel.value]?.remove(handler);
  }

  void clear() {
    _handlers.clear();
  }
}

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

  print('Exercise 04 passed');
}
```

### Deep Dive

This exercise demonstrates a real tension in generic design. The *external API* is type-safe: `emit<UserLoggedIn>(loginChannel, 'not a login event')` will not compile because the channel's type parameter and the event type must match. But *internally*, the class uses `Map<String, List<Function>>` with a cast. This is not a failure of the design -- it is a deliberate trade-off. You cannot have a heterogeneous map where each entry has a different generic type. The solution is to enforce types at the boundary and trust the internal cast.

The extension type `EventChannel<T>(String value)` costs nothing at runtime. At runtime, `loginChannel` is just the string `'user.login'`. The `<T>` parameter exists only during compilation to correlate the channel with its event type. This is the ideal use case for extension types: adding compile-time meaning to a primitive value.

---

## Exercise 5 -- Generic Cache with Eviction Policies

### Progressive Hints

**Hint 1 (LRU):** Use a `LinkedHashMap<K, void>` or a simple `List<K>` to track access order. On every access, move the key to the end. The victim is the key at the front.

**Hint 2 (LFU):** Use a `Map<K, int>` to track frequency counts. The victim is the key with the lowest count. Break ties arbitrarily (first found is fine).

**Hint 3 (Cache.get):** When `get` hits a cached value, call `_policy.recordAccess(key)` so the policy updates its tracking data. When `get` misses, return `null` without calling the policy.

**Hint 4 (Eviction):** In `put`, if the cache is at capacity and the key is new, call `_policy.selectVictim()` to get the key to remove, then remove it from both the storage map and the policy (via `recordRemoval`), before inserting the new entry.

### Common Mistakes

**Not calling recordAccess on get:** If `get` does not inform the policy, LRU will evict based on insertion order, not access order. This is the most common bug.

**Evicting even when updating an existing key:** If you `put` a key that already exists, you are updating, not inserting. Do not evict. Check `containsKey` first.

**LFU tie-breaking:** When multiple keys have the same minimum frequency, you need a consistent tie-breaking strategy. The simplest is "first encountered in the map iteration," but be aware that `Map` iteration order in Dart is insertion order.

### Full Solution

```dart
// file: exercise_05_solution.dart

abstract class EvictionPolicy<K> {
  void recordAccess(K key);
  void recordInsertion(K key);
  K selectVictim();
  void recordRemoval(K key);
}

class LruPolicy<K> implements EvictionPolicy<K> {
  final List<K> _accessOrder = [];

  @override
  void recordAccess(K key) {
    _accessOrder.remove(key);
    _accessOrder.add(key);
  }

  @override
  void recordInsertion(K key) {
    _accessOrder.add(key);
  }

  @override
  K selectVictim() {
    if (_accessOrder.isEmpty) {
      throw StateError('No entries to evict');
    }
    return _accessOrder.first;
  }

  @override
  void recordRemoval(K key) {
    _accessOrder.remove(key);
  }
}

class LfuPolicy<K> implements EvictionPolicy<K> {
  final Map<K, int> _frequency = {};

  @override
  void recordAccess(K key) {
    _frequency[key] = (_frequency[key] ?? 0) + 1;
  }

  @override
  void recordInsertion(K key) {
    _frequency[key] = 1;
  }

  @override
  K selectVictim() {
    if (_frequency.isEmpty) {
      throw StateError('No entries to evict');
    }
    K? victim;
    int minFreq = -1;
    for (final entry in _frequency.entries) {
      if (minFreq == -1 || entry.value < minFreq) {
        minFreq = entry.value;
        victim = entry.key;
      }
    }
    return victim as K;
  }

  @override
  void recordRemoval(K key) {
    _frequency.remove(key);
  }
}

class Cache<K, V> {
  final int maxCapacity;
  final EvictionPolicy<K> _policy;
  final Map<K, V> _store = {};

  Cache({required this.maxCapacity, required EvictionPolicy<K> policy})
      : _policy = policy {
    if (maxCapacity <= 0) {
      throw ArgumentError('maxCapacity must be positive, got $maxCapacity');
    }
  }

  void put(K key, V value) {
    if (_store.containsKey(key)) {
      _store[key] = value;
      _policy.recordAccess(key);
      return;
    }

    if (_store.length >= maxCapacity) {
      final victim = _policy.selectVictim();
      _store.remove(victim);
      _policy.recordRemoval(victim);
    }

    _store[key] = value;
    _policy.recordInsertion(key);
  }

  V? get(K key) {
    final value = _store[key];
    if (value != null || _store.containsKey(key)) {
      _policy.recordAccess(key);
    }
    return value;
  }

  bool containsKey(K key) => _store.containsKey(key);
  int get size => _store.length;

  void invalidate(K key) {
    if (_store.containsKey(key)) {
      _store.remove(key);
      _policy.recordRemoval(key);
    }
  }
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
  lruCache.get('a');
  lruCache.put('d', 4);

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
  lfuCache.get('x');
  lfuCache.get('x');
  lfuCache.get('z');
  lfuCache.put('w', 40);

  assert(lfuCache.get('y') == null, 'y should have been evicted');
  assert(lfuCache.get('x') == 10);
  assert(lfuCache.size == 3);

  print('Exercise 05 passed');
}
```

### Deep Dive

The Strategy pattern (`EvictionPolicy<K>`) decouples the cache from its eviction logic. This is generics doing real architectural work: `Cache<K, V>` does not know or care which policy it uses, and `EvictionPolicy<K>` does not know it lives inside a cache. Each can be tested independently.

The `get` method has a subtle bug risk: what if the value stored is `null`? If `V` is nullable (e.g., `Cache<String, int?>`), then `get` returning `null` is ambiguous -- is the key missing or is the value `null`? The `containsKey` check inside `get` handles this: if the key exists, we record the access regardless of the value. But the *caller* still cannot distinguish "key not found" from "value is null." A production cache would return an `Optional<V>` or a custom wrapper to make this explicit.

The `LruPolicy` using a `List` has O(n) `remove` for every access. A production implementation would use a doubly-linked list with a hash map for O(1) operations. The exercise keeps it simple to focus on the generic design.

---

## Exercise 6 -- Covariance Bug Hunt

### Progressive Hints

**Hint 1:** In Scenario 1, `List<Rectangle>` is passed where `List<Shape>` is expected. The function adds a `Circle`. What type does the list expect?

**Hint 2:** In Scenario 2, `Map<String, Rectangle>` is passed as `Map<String, Shape>`. The source map contains `Circle` values. What happens when circles get inserted into a rectangle map?

**Hint 3:** Scenario 3 is actually safe *if* the list truly contains only `Circle`s. But the function accepts `List<Shape>`, so a caller could pass mixed shapes in the future.

### Common Mistakes

**Thinking the compiler catches these:** It does not. Dart's covariance is unsound by design. The compiler allows `List<Rectangle>` where `List<Shape>` is expected. The runtime throws a `TypeError` when a `Circle` is added.

**Fixing with runtime checks instead of types:** Adding `if (item is Rectangle)` checks hides the problem. The fix should make invalid calls impossible at compile time.

### Full Solution

```dart
// file: exercise_06_solution.dart

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

// ---- BUG ANALYSIS ----

// BUG 1: addDefaultShape(List<Shape> shapes) adds a Circle.
//   When called with List<Rectangle>, the runtime type of the list
//   is List<Rectangle>. The .add(Circle(...)) passes static checking
//   (Circle is a Shape), but FAILS at runtime because the list's
//   reified element type is Rectangle, not Shape.
//
// FIX 1: Accept a read-only view, or use a generic bound.

// Safe version -- does not write to the list:
List<Shape> withDefaultShape(Iterable<Shape> shapes) {
  return [...shapes, Circle(1.0)];
}

// Alternative -- generic, caller controls the type:
void addDefault<T extends Shape>(List<T> shapes, T defaultShape) {
  shapes.add(defaultShape);
}

// BUG 2: mergeShapeMaps inserts Circle values into a Map<String, Rectangle>.
//   Same issue: the runtime type of the target map rejects Circle values.
//
// FIX 2: Return a new map instead of mutating the target.

Map<String, Shape> mergeShapeMaps(
  Map<String, Shape> a,
  Map<String, Shape> b,
) {
  return {...a, ...b}; // New map, no mutation of originals
}

// BUG 3: processShapes itself is safe if you do not downcast.
//   The bug is the `shape as Circle` cast inside the callback.
//   If the list ever contains non-Circle shapes, it crashes.
//
// FIX 3: Use type checks, not casts.

void processShapes(List<Shape> shapes, void Function(Shape) processor) {
  for (final shape in shapes) {
    processor(shape);
  }
}

T? safeCast<T>(dynamic value) {
  if (value is T) return value;
  return null;
}

void main() {
  // Fix 1 in action
  List<Rectangle> rectangles = [Rectangle(2, 3)];
  final combined = withDefaultShape(rectangles);
  print('Combined shapes: ${combined.length}'); // 2, no crash
  // Original list untouched:
  assert(rectangles.length == 1);

  // Fix 2 in action
  Map<String, Rectangle> rectMap = {'r1': Rectangle(1, 2)};
  Map<String, Circle> circleMap = {'c1': Circle(5)};
  final merged = mergeShapeMaps(rectMap, circleMap);
  print('Merged keys: ${merged.keys}'); // {r1, c1}, no crash
  assert(merged.length == 2);

  // Fix 3 in action with safeCast
  List<Shape> mixed = [Circle(1), Rectangle(2, 3), Circle(2)];
  processShapes(mixed, (shape) {
    final circle = safeCast<Circle>(shape);
    if (circle != null) {
      print('Circle radius: ${circle.radius}');
    } else {
      print('Not a circle: ${shape.runtimeType}');
    }
  });

  // safeCast edge cases
  assert(safeCast<int>(42) == 42);
  assert(safeCast<int>('hello') == null);
  assert(safeCast<String>(null) == null);

  print('Exercise 06 passed');
}
```

### Deep Dive

Dart chose covariant generics as a pragmatic default. The alternative -- declaration-site variance (Kotlin's `out`/`in`, C#'s `out`/`in`) -- forces every generic class author to decide whether type parameters are used in output positions only (covariant/`out`), input positions only (contravariant/`in`), or both (invariant). This is more correct but significantly harder to teach and use.

Dart's approach means the type system is technically unsound: code that passes all static checks can still crash at runtime with a `TypeError`. The runtime inserts checks on writes to generic containers to catch these violations. The practical lesson: never pass a `List<Specific>` to a function that *writes* to a `List<General>`. If the function only reads, you are safe. If it writes, either accept a generic `T` parameter or return a new collection.

The `safeCast<T>` function leverages reified generics. `value is T` works because `T` is a real type at runtime. In Java, this would be erased and `value is T` would not compile.

---

## Exercise 7 -- Type-Safe Dependency Injection Container

### Progressive Hints

**Hint 1:** Use `Type` as the map key: `Map<Type, _Registration>`. Since Dart generics are reified, `T` in `register<T>` is a real `Type` object you can store and look up.

**Hint 2:** Create a private class `_Registration` that holds the factory function and the lifetime. The factory is `Function` internally (type-erased), but the public API keeps it safe.

**Hint 3:** For scoping, the child `Container` holds a reference to its parent. `resolve` checks the child's registrations first, then falls back to the parent. Singleton instances are stored per-scope, not per-registration.

**Hint 4:** When a child scope resolves a type registered only in the parent, the *factory* comes from the parent but the *singleton instance* lives in the child. This is critical: the child's factory calls might resolve dependencies to the child's own overrides.

### Common Mistakes

**Using the parent's singleton cache for child resolutions:** If child and parent share singleton instances, overriding `Logger` in the child has no effect when the parent already created its singleton. Each scope needs its own singleton cache.

**Forgetting to pass the resolving container to the factory:** The factory signature is `T Function(Container)`. You must pass the *current* container (the one being resolved against), not the container where the type was registered. This ensures dependency resolution respects scope.

**Using `T.toString()` as a key instead of `T` itself:** Avoid this. `Type` objects in Dart are proper objects that work as map keys with identity-based equality.

### Full Solution

```dart
// file: exercise_07_solution.dart

enum ServiceLifetime { singleton, transient }

class _Registration {
  final Function factory;
  final ServiceLifetime lifetime;
  _Registration(this.factory, this.lifetime);
}

class Container {
  final Container? _parent;
  final Map<Type, _Registration> _registrations = {};
  final Map<Type, Object> _singletons = {};

  Container({Container? parent}) : _parent = parent;

  void register<T>(
    T Function(Container) factory, {
    ServiceLifetime lifetime = ServiceLifetime.transient,
  }) {
    _registrations[T] = _Registration(factory, lifetime);
  }

  T resolve<T>() {
    // Check for a singleton already created in this scope
    final cached = _singletons[T];
    if (cached != null) return cached as T;

    // Find the registration: local first, then parent
    final registration = _findRegistration<T>();
    if (registration == null) {
      throw StateError(
        'No registration found for type $T. '
        'Did you forget to call container.register<$T>(...)?',
      );
    }

    final instance = (registration.factory as T Function(Container))(this);

    if (registration.lifetime == ServiceLifetime.singleton) {
      _singletons[T] = instance as Object;
    }

    return instance;
  }

  _Registration? _findRegistration<T>() {
    return _registrations[T] ?? _parent?._findRegistration<T>();
  }

  Container createScope() {
    return Container(parent: this);
  }
}

// Services

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

### Deep Dive

The entire DI container works because Dart generics are reified. `T` inside `register<T>` and `resolve<T>` is a real `Type` object at runtime. You can use it as a map key, compare it, print it. In Java, this pattern requires passing `Class<T>` explicitly because the type parameter is erased.

The scoping design is the hardest part. When `testScope.resolve<UserRepository>()` runs, it finds no local registration for `UserRepository`, so it falls back to the parent's registration. The parent's factory is `(c) => InMemoryUserRepository(c.resolve<Logger>())`. But `c` is `testScope`, not `container`. So when the factory calls `c.resolve<Logger>()`, it resolves the *child's* `SilentLogger`, not the parent's `ConsoleLogger`. This is why the factory receives the container as a parameter: it enables dependency resolution to respect the current scope.

One limitation: this container does not detect circular dependencies. If `A` depends on `B` and `B` depends on `A`, `resolve` enters infinite recursion and eventually overflows the stack. A production container would track "currently resolving" types and throw a clear error on cycles.

---

## Exercise 8 -- Generic Middleware Pipeline

### Progressive Hints

**Hint 1:** The pipeline stores a single composed function: `Future<TOut> Function(TIn)`. This is the key insight. You do not store a list of middleware -- you compose them into one function.

**Hint 2:** The `identity` factory constructor creates a pipeline where the function just returns the input: `Pipeline.identity()` produces `Pipeline<T, T>` with `(input) async => input`.

**Hint 3:** `then<TNext>` creates a new `Pipeline<TIn, TNext>` whose internal function calls the current pipeline's function first, then passes the result to the next middleware.

**Hint 4:** Each middleware can throw exceptions. The pipeline does not catch them -- they propagate naturally through the `Future` chain. This is the correct behavior: the caller decides how to handle errors.

### Common Mistakes

**Storing middleware in a list and losing types:** If you store `List<Middleware>`, you lose the type chain. The whole point is that `then` returns a new `Pipeline` with an updated output type. Composition, not accumulation.

**Making Pipeline mutable:** `then` should return a *new* Pipeline, not mutate the current one. The original pipeline remains usable and unchanged.

**Forgetting async:** Each middleware returns `Future<TOut>`, so you must `await` the intermediate results when composing.

### Full Solution

```dart
// file: exercise_08_solution.dart

abstract class Middleware<TIn, TOut> {
  Future<TOut> process(TIn input);
}

class Pipeline<TIn, TOut> {
  final Future<TOut> Function(TIn) _execute;

  Pipeline._(this._execute);

  factory Pipeline.identity() {
    return Pipeline<TIn, TIn>._(
      (input) async => input,
    ) as Pipeline<TIn, TOut>;
    // This cast is safe only when TIn == TOut, which is the
    // only way identity() is called.
  }

  Pipeline<TIn, TNext> then<TNext>(Middleware<TOut, TNext> next) {
    return Pipeline<TIn, TNext>._((input) async {
      final intermediate = await _execute(input);
      return next.process(intermediate);
    });
  }

  Future<TOut> execute(TIn input) => _execute(input);
}

// Domain types

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

// Middleware implementations

class ValidationMiddleware extends Middleware<RawRequest, ValidatedRequest> {
  @override
  Future<ValidatedRequest> process(RawRequest input) async {
    if (input.body.isEmpty) {
      throw FormatException('Request body cannot be empty');
    }

    // Simulate JSON parsing (in real code, use dart:convert)
    final bodyContent = input.body
        .replaceAll('{', '')
        .replaceAll('}', '')
        .replaceAll('"', '');
    final pairs = bodyContent.split(',').map((pair) {
      final parts = pair.trim().split(':');
      return MapEntry(parts[0].trim(), parts[1].trim());
    });

    return ValidatedRequest(
      input.path,
      input.headers,
      Map.fromEntries(pairs),
    );
  }
}

class AuthMiddleware extends Middleware<ValidatedRequest, AuthenticatedRequest> {
  @override
  Future<AuthenticatedRequest> process(ValidatedRequest input) async {
    final authHeader = input.headers['Authorization'];
    if (authHeader == null || !authHeader.startsWith('Bearer ')) {
      throw StateError('Missing or invalid Authorization header');
    }

    final token = authHeader.substring('Bearer '.length);
    // Simulate token validation
    final userId = 'user_${token.hashCode.abs() % 1000}';

    return AuthenticatedRequest(input.path, userId, input.parsedBody);
  }
}

class HandlerMiddleware extends Middleware<AuthenticatedRequest, Response> {
  @override
  Future<Response> process(AuthenticatedRequest input) async {
    return Response(
      200,
      'Handled ${input.path} for ${input.userId} '
          'with data: ${input.parsedBody}',
    );
  }
}

void main() async {
  final pipeline = Pipeline<RawRequest, RawRequest>.identity()
      .then(ValidationMiddleware())
      .then(AuthMiddleware())
      .then(HandlerMiddleware());

  // pipeline is Pipeline<RawRequest, Response>

  final request = RawRequest(
    '/api/users',
    {'Authorization': 'Bearer token123'},
    '{"name": "Alice"}',
  );

  final response = await pipeline.execute(request);
  assert(response.statusCode == 200);
  print('Response: ${response.statusCode} ${response.body}');

  // Failure case
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

### Deep Dive

This is the most architecturally significant exercise. The pipeline achieves something remarkable: each call to `.then()` shifts the output type. After `.then(ValidationMiddleware())`, the pipeline output is `ValidatedRequest`. After `.then(AuthMiddleware())`, it becomes `AuthenticatedRequest`. The compiler tracks this chain and would refuse to compile `.then(AuthMiddleware())` directly after `identity()` because `AuthMiddleware` expects `ValidatedRequest`, not `RawRequest`.

The internal representation is a single composed function `Future<TOut> Function(TIn)`. Each `.then()` wraps the previous function with a new one. There is no list of middleware, no dynamic dispatch, no reflection. It is functions all the way down, and the type system guarantees correctness at every step.

The `identity()` factory uses a cast that is technically unsafe in general but safe in practice because it is only meaningful when `TIn == TOut`. A cleaner alternative is a static method: `static Pipeline<T, T> identity<T>() => Pipeline<T, T>._((input) async => input)`. The exercise uses the factory constructor form to keep the call site clean.

This pattern appears in real frameworks: middleware chains in shelf (Dart's HTTP server library), interceptors in dio (HTTP client), and transformation pipelines in stream processing. The generic type chain is what makes them composable without losing type information.

---

## Debugging Tips

**"type X is not a subtype of type Y":** This is Dart's reified generics telling you that a runtime type check failed. Common causes: covariance violations (inserting wrong type into a typed collection), incorrect casts inside type-erased generic internals, or passing a `List<dynamic>` where a `List<Specific>` is expected. Check the full error message -- it tells you both the actual and expected types.

**"type argument does not conform to bounds":** You tried to use a type that does not satisfy the `extends` constraint. Check the bound and make sure your type implements or extends the required type.

**Extension types disappearing:** If you cast an extension type to `dynamic` or to its representation type, you lose the wrapper. `(userId as dynamic)` is now just `int`. Extension types are compile-time only. Design your APIs to avoid `dynamic` if you rely on extension type safety.

**Generic method type inference failing:** Sometimes Dart cannot infer type arguments from context. When you see unexpected `dynamic` types, add explicit type arguments at the call site: `foo<String>(...)` instead of `foo(...)`.

**Reified type comparisons:** `T == int` works, but be careful with nullable types. `int?` and `int` are different `Type` objects. If your generic container needs to handle nullable types, test for both.
