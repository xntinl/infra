# Section 12 -- Solutions: Flutter State Management Basics

## How to Use This File

Work through each exercise in the README before opening this file. For each exercise:

1. Read the **Progressive Hints** first -- they guide you without giving away the answer
2. If stuck after the hints, check **Common Mistakes** to see if you hit a known trap
3. Only then look at the **Full Solution**
4. After solving it, read the **Deep Dive** for the "why" behind the implementation

---

## Exercise 1: Counter with setState

### Progressive Hints

1. Your `StatefulWidget` needs exactly one piece of mutable state: an `int`. The `State` class owns it.
2. For the decrement guard, check the value before mutating. Show the `SnackBar` using `ScaffoldMessenger.of(context)`.
3. Extracting `CounterDisplay` as a `StatelessWidget` that takes `int count` proves that the child rebuilds even though it is stateless -- because its parent's `build` re-executes.
4. Place `debugPrint('Building _CounterPageState')` at the top of the parent's `build`, and `debugPrint('Building CounterDisplay')` in the child's `build`.

### Full Solution

```dart
// file: lib/counter_page.dart
import 'package:flutter/material.dart';

class CounterPage extends StatefulWidget {
  const CounterPage({super.key});

  @override
  State<CounterPage> createState() => _CounterPageState();
}

class _CounterPageState extends State<CounterPage> {
  int _count = 0;

  void _increment() => setState(() => _count++);

  void _decrement() {
    if (_count <= 0) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('Counter cannot go below zero')),
      );
      return;
    }
    setState(() => _count--);
  }

  void _reset() => setState(() => _count = 0);

  @override
  Widget build(BuildContext context) {
    debugPrint('Building _CounterPageState');
    return Scaffold(
      appBar: AppBar(title: const Text('Counter')),
      body: Center(
        child: Column(
          mainAxisAlignment: MainAxisAlignment.center,
          children: [
            CounterDisplay(count: _count),
            const SizedBox(height: 24),
            Row(
              mainAxisAlignment: MainAxisAlignment.center,
              children: [
                IconButton(icon: const Icon(Icons.remove), onPressed: _decrement),
                const SizedBox(width: 16),
                IconButton(icon: const Icon(Icons.refresh), onPressed: _reset),
                const SizedBox(width: 16),
                IconButton(icon: const Icon(Icons.add), onPressed: _increment),
              ],
            ),
          ],
        ),
      ),
    );
  }
}

class CounterDisplay extends StatelessWidget {
  final int count;
  const CounterDisplay({super.key, required this.count});

  @override
  Widget build(BuildContext context) {
    debugPrint('Building CounterDisplay');
    return Text(
      '$count',
      style: Theme.of(context).textTheme.displayLarge,
    );
  }
}
```

### Common Mistakes

**Calling `setState` with no mutation.** Writing `setState(() {})` still triggers a rebuild. The framework does not check whether state changed -- it always marks the widget dirty.

**Showing the SnackBar inside `setState`.** Side effects belong outside the callback. The `setState` closure should contain only state mutations.

**Not extracting the display widget.** Without a separate `CounterDisplay`, you cannot observe that the child's `build` re-runs on every parent `setState`.

### Deep Dive

`setState` calls `markNeedsBuild()` which adds the Element to the dirty list. On the next frame, the entire `build` method re-executes. The Element tree diffs new widgets against old ones to update RenderObjects. This is why `const` constructors matter -- `const CounterDisplay(count: 5)` returns a canonical instance, letting the Element tree skip the diff entirely.

---

## Exercise 2: ValueNotifier and ValueListenableBuilder

### Progressive Hints

1. Declare the `ValueNotifier<ThemeMode>` at the file level, outside any class. This is intentionally simple -- in production you would scope it with a Provider or service locator.
2. `ChoiceChip` has a `selected` property and an `onSelected` callback. Write to the notifier in the callback.
3. Both `ThemePreview` and `ThemeLabel` use `ValueListenableBuilder<ThemeMode>`. They are completely independent widgets that share no parent state.
4. No `StatefulWidget` should exist in your solution. If you feel the urge to create one, step back -- `ValueNotifier` handles the reactivity.

### Full Solution

```dart
// file: lib/theme_demo.dart
import 'package:flutter/material.dart';

// Shared notifier -- declared at file level, no StatefulWidget needed
final ValueNotifier<ThemeMode> themeModeNotifier = ValueNotifier(ThemeMode.system);

// Page composes three independent StatelessWidgets in a Column.
// None of them share a parent StatefulWidget.

class ThemeSelector extends StatelessWidget {
  const ThemeSelector({super.key});

  @override
  Widget build(BuildContext context) {
    return ValueListenableBuilder<ThemeMode>(
      valueListenable: themeModeNotifier,
      builder: (context, mode, _) {
        return Row(
          mainAxisAlignment: MainAxisAlignment.center,
          children: [
            ChoiceChip(
              label: const Text('Light'),
              selected: mode == ThemeMode.light,
              onSelected: (_) => themeModeNotifier.value = ThemeMode.light,
            ),
            // ... Dark and System chips follow the same pattern
          ],
        );
      },
    );
  }
}

// ThemePreview and ThemeLabel follow the same pattern:
// ValueListenableBuilder<ThemeMode> wrapping themeModeNotifier.
// Each reads the same notifier independently -- no prop drilling.
class ThemePreview extends StatelessWidget {
  const ThemePreview({super.key});

  @override
  Widget build(BuildContext context) {
    return ValueListenableBuilder<ThemeMode>(
      valueListenable: themeModeNotifier,
      builder: (context, mode, _) {
        final color = switch (mode) {
          ThemeMode.light => Colors.white,
          ThemeMode.dark => Colors.grey.shade900,
          ThemeMode.system => Colors.blueGrey,
        };
        return Container(width: 200, height: 200, color: color);
      },
    );
  }
}
```

### Common Mistakes

**Creating a `StatefulWidget`.** The point is that `ValueNotifier` replaces the need for `StatefulWidget`. If you used `setState`, redo the exercise.

**Forgetting to dispose the `ValueNotifier`.** Here the notifier is a top-level global, so disposal is unnecessary. In a real app, a widget-created `ValueNotifier` must be disposed in `dispose()`.

**Using `addListener` directly.** That requires a `StatefulWidget` to manage the listener lifecycle. `ValueListenableBuilder` handles subscription automatically.

---

## Exercise 3: InheritedWidget for User Session

### Progressive Hints

1. The `UserSession` data class should be immutable. Use `final` fields and a `const` constructor.
2. In `updateShouldNotify`, compare each field. Do not rely on `==` unless you have overridden it on `UserSession`.
3. `UserSessionProvider` is a `StatefulWidget` that holds `UserSession` in its `State`. It wraps children with `UserSessionWidget`. The `updateSession` method calls `setState` with the new session.
4. In `AdminPanel`, call `UserSessionWidget.maybeOf(context)`. If it returns null, show a fallback. This gracefully handles the widget being placed outside the provider scope.

### Full Solution

```dart
// file: lib/user_session.dart
import 'package:flutter/material.dart';

class UserSession {
  final String username;
  final String email;
  final String avatarUrl;
  final bool isAdmin;

  const UserSession({
    required this.username,
    required this.email,
    this.avatarUrl = '',
    this.isAdmin = false,
  });

  static const guest = UserSession(username: 'Guest', email: '');
}

class UserSessionWidget extends InheritedWidget {
  final UserSession session;
  final ValueChanged<UserSession> onSessionChanged;

  const UserSessionWidget({
    super.key,
    required this.session,
    required this.onSessionChanged,
    required super.child,
  });

  static UserSessionWidget of(BuildContext context) {
    final result = context.dependOnInheritedWidgetOfExactType<UserSessionWidget>();
    if (result == null) throw FlutterError('No UserSessionWidget ancestor found.');
    return result;
  }

  static UserSessionWidget? maybeOf(BuildContext context) =>
      context.dependOnInheritedWidgetOfExactType<UserSessionWidget>();

  @override
  bool updateShouldNotify(UserSessionWidget oldWidget) {
    return session.username != oldWidget.session.username ||
           session.email != oldWidget.session.email ||
           session.avatarUrl != oldWidget.session.avatarUrl ||
           session.isAdmin != oldWidget.session.isAdmin;
  }
}

class UserSessionProvider extends StatefulWidget {
  final Widget child;
  const UserSessionProvider({super.key, required this.child});

  @override
  State<UserSessionProvider> createState() => _UserSessionProviderState();
}

class _UserSessionProviderState extends State<UserSessionProvider> {
  UserSession _session = UserSession.guest;

  void _updateSession(UserSession s) => setState(() => _session = s);

  @override
  Widget build(BuildContext context) {
    return UserSessionWidget(
      session: _session,
      onSessionChanged: _updateSession,
      child: widget.child,
    );
  }
}

// Consumer widgets use UserSessionWidget.of(context).session to read data.
// UserAvatar and UserGreeting both call .of() which registers them as dependents.

// file: lib/admin_panel.dart -- demonstrates maybeOf for optional ancestry
class AdminPanel extends StatelessWidget {
  const AdminPanel({super.key});

  @override
  Widget build(BuildContext context) {
    final sessionWidget = UserSessionWidget.maybeOf(context);
    if (sessionWidget == null) {
      return const Text('No session available');
    }
    if (!sessionWidget.session.isAdmin) {
      return const SizedBox.shrink();
    }
    return const Card(
      child: Padding(
        padding: EdgeInsets.all(16),
        child: Text('Admin Panel: You have elevated privileges'),
      ),
    );
  }
}
```

### Common Mistakes

**Using `of` inside `AdminPanel`.** The exercise requires graceful handling outside the session scope. Using `of` would throw. Use `maybeOf` when the ancestor might not exist.

**Comparing sessions by reference in `updateShouldNotify`.** Without overriding `==` on `UserSession`, `session != oldWidget.session` compares identity, not content. Compare fields explicitly or override `==` and `hashCode`.

**Calling `dependOnInheritedWidgetOfExactType` inside `initState`.** The Element is not fully mounted yet. Use `didChangeDependencies` or access the InheritedWidget in `build`.

---

## Exercise 4: Provider Todo App

### Progressive Hints

1. Use `uuid` or a simple incrementing counter for todo IDs. Avoid using list index as an ID -- it breaks when items are deleted.
2. `MultiProvider` takes a list of providers. Order matters only if providers depend on each other.
3. For `Selector`, the selector function extracts the derived value: `context.select<TodoModel, List<TodoItem>>((model) => model.filteredItems(filter))`. The widget rebuilds only when the returned list changes.
4. In `AddTodoSheet`, use `context.read<TodoModel>()` because the sheet fires and forgets -- it does not need to listen for changes.

### Full Solution

```dart
// file: lib/todo_model.dart
import 'package:flutter/foundation.dart';

class TodoItem {
  final String id;
  final String title;
  final bool isCompleted;
  final DateTime createdAt;

  const TodoItem({
    required this.id,
    required this.title,
    this.isCompleted = false,
    required this.createdAt,
  });

  TodoItem copyWith({bool? isCompleted}) {
    return TodoItem(
      id: id,
      title: title,
      isCompleted: isCompleted ?? this.isCompleted,
      createdAt: createdAt,
    );
  }
}

enum TodoFilter { all, active, completed }

class TodoFilterModel extends ChangeNotifier {
  TodoFilter _filter = TodoFilter.all;
  TodoFilter get filter => _filter;

  void setFilter(TodoFilter value) {
    if (_filter == value) return;
    _filter = value;
    notifyListeners();
  }
}

class TodoModel extends ChangeNotifier {
  final List<TodoItem> _items = [];
  int _nextId = 0;

  List<TodoItem> get items => List.unmodifiable(_items);
  int get activeCount => _items.where((t) => !t.isCompleted).length;

  List<TodoItem> filteredBy(TodoFilter filter) => switch (filter) {
    TodoFilter.all => items,
    TodoFilter.active => _items.where((t) => !t.isCompleted).toList(),
    TodoFilter.completed => _items.where((t) => t.isCompleted).toList(),
  };

  void add(String title) {
    _items.add(TodoItem(id: '${_nextId++}', title: title, createdAt: DateTime.now()));
    notifyListeners();
  }

  void toggle(String id) {
    final i = _items.indexWhere((t) => t.id == id);
    if (i == -1) return;
    _items[i] = _items[i].copyWith(isCompleted: !_items[i].isCompleted);
    notifyListeners();
  }

  void delete(String id) { _items.removeWhere((t) => t.id == id); notifyListeners(); }
  void clearCompleted() { _items.removeWhere((t) => t.isCompleted); notifyListeners(); }
}

// file: lib/main_todo.dart -- key patterns shown, standard UI omitted
void main() {
  runApp(
    MultiProvider(
      providers: [
        ChangeNotifierProvider(create: (_) => TodoModel()),
        ChangeNotifierProvider(create: (_) => TodoFilterModel()),
      ],
      child: const MaterialApp(home: TodoPage()),
    ),
  );
}

// App bar uses Consumer to scope rebuilds to just the count text:
Consumer<TodoModel>(
  builder: (_, model, __) => Text('Todos (${model.activeCount} left)'),
)

// Bottom sheet uses context.read (not watch) -- fire and forget:
onPressed: () {
  context.read<TodoModel>().add(controller.text.trim());
  Navigator.pop(sheetContext);
}

// TodoList combines watch (for filter) and select (for filtered items):
final filter = context.watch<TodoFilterModel>().filter;
final todos = context.select<TodoModel, List<TodoItem>>(
  (model) => model.filteredBy(filter),
);

// Toggle uses context.read inside a callback:
onChanged: (_) => context.read<TodoModel>().toggle(todo.id)
```

### Common Mistakes

**Using `context.watch` in a callback.** `watch` subscribes the widget inside a callback that runs outside `build`. This throws at runtime. Use `context.read` in callbacks.

**Forgetting `Selector` compares by `==`.** `filteredBy` returns a new `List` every call. `List` equality is by reference, so `Selector` always rebuilds. Use `listEquals` in `shouldRebuild` or return a cached list.

**Using the bottom sheet's context for Provider.** The sheet has its own context. Capture the parent's `context` before `showModalBottomSheet` for `context.read`.

### Deep Dive

`Selector` stores the previous selected value. On `notifyListeners()`, it re-runs the selector, compares with `==`, and rebuilds only if the value differs. Much more efficient than `Consumer` when a model has many fields but the widget cares about one.

---

## Exercise 5: Multi-Feature App with Scoped Providers

### Progressive Hints

1. `ProxyProvider` creates a provider that depends on another. Use `ChangeNotifierProxyProvider<AuthModel, CartModel>` so that `CartModel` receives the current `AuthModel` on every change.
2. To scope `CatalogModel` to the catalog screen, wrap only that screen's subtree with `ChangeNotifierProvider<CatalogModel>`. When the user navigates away, the provider (and its model) is disposed.
3. In `CartModel`, add an `updateAuth(AuthModel auth)` method. If the user ID changed, clear the cart.
4. `ProxyProvider`'s `update` callback receives the previous instance. Reuse it and call an update method rather than creating a new `CartModel` every time.

### Full Solution

The key patterns are the models and the provider wiring. The UI widgets follow standard patterns from Exercise 4.

```dart
// file: lib/auth_model.dart
import 'package:flutter/foundation.dart';

class User {
  final String id;
  final String name;
  const User({required this.id, required this.name});
}

class AuthModel extends ChangeNotifier {
  User? _currentUser;
  User? get currentUser => _currentUser;
  bool get isAuthenticated => _currentUser != null;

  void login(String id, String name) {
    _currentUser = User(id: id, name: name);
    notifyListeners();
  }

  void logout() {
    _currentUser = null;
    notifyListeners();
  }
}

// file: lib/cart_model.dart -- the critical pattern: updateAuth clears per-user state
class CartModel extends ChangeNotifier {
  String? _userId;
  final List<CartItem> _items = [];

  void updateAuth(AuthModel auth) {
    final newUserId = auth.currentUser?.id;
    if (newUserId != _userId) {
      _userId = newUserId;
      _items.clear();
      notifyListeners();
    }
  }

  void addProduct(String id, String name, double price) { /* ... */ }
  void removeProduct(String id) { /* ... */ }
}

// file: lib/main_shop.dart -- provider wiring
void main() {
  runApp(
    MultiProvider(
      providers: [
        ChangeNotifierProvider(create: (_) => AuthModel()),
        ChangeNotifierProxyProvider<AuthModel, CartModel>(
          create: (_) => CartModel(),
          update: (_, auth, cart) {
            cart!.updateAuth(auth); // Reuse previous instance, do NOT create new
            return cart;
          },
        ),
        // CatalogModel is NOT here -- scoped to catalog screen only
      ],
      child: const MaterialApp(home: AppShell()),
    ),
  );
}

// file: lib/main_screen.dart -- scoped CatalogModel
class MainScreen extends StatelessWidget {
  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: ChangeNotifierProvider(
        // Created when MainScreen builds, disposed when it leaves the tree
        create: (_) => CatalogModel()..loadProducts(),
        child: const CatalogPage(),
      ),
    );
  }
}

// file: lib/catalog_page.dart -- Selector for per-item rebuild optimization
class CatalogPage extends StatelessWidget {
  @override
  Widget build(BuildContext context) {
    final products = context.watch<CatalogModel>().products;
    return ListView.builder(
      itemCount: products.length,
      itemBuilder: (context, index) {
        final product = products[index];
        return Selector<CatalogModel, bool>(
          selector: (_, model) =>
              model.products.firstWhere((p) => p.id == product.id).isFavorite,
          builder: (context, isFav, _) {
            return ListTile(
              title: Text(product.name),
              leading: IconButton(
                icon: Icon(isFav ? Icons.favorite : Icons.favorite_border),
                onPressed: () => context.read<CatalogModel>().toggleFavorite(product.id),
              ),
            );
          },
        );
      },
    );
  }
}
```

### Common Mistakes

**Creating a new `CartModel` in `ProxyProvider.update`.** Returning `CartModel()` instead of reusing the previous instance loses all cart state on every `AuthModel` change.

**Placing `CatalogModel` in the root `MultiProvider`.** It must be scoped to the catalog screen for proper disposal on navigation.

**Using `watch` when `select` suffices.** `context.watch<AuthModel>()` rebuilds on every change. Use `context.select` to narrow to the specific field.

---

## Exercise 6: Optimistic Updates with Rollback

### Progressive Hints

1. Save the pre-mutation state before applying the optimistic update. If the API fails, restore from the saved snapshot.
2. Track pending operations with a `Set<String>` of post IDs. Add before the API call, remove after (success or failure).
3. For the simulated failure, use a counter: `_likeAttempts++; if (_likeAttempts % 3 == 0) throw Exception('API error')`.
4. The `SnackBar` retry action captures the post ID in a closure: `SnackBarAction(label: 'Retry', onPressed: () => toggleLike(id))`.

### Full Solution

```dart
// file: lib/post_model.dart
import 'package:flutter/foundation.dart';

class Post {
  final String id;
  final String title;
  int likeCount;
  bool isLikedByUser;

  Post({required this.id, required this.title, this.likeCount = 0, this.isLikedByUser = false});

  Post snapshot() => Post(id: id, title: title, likeCount: likeCount, isLikedByUser: isLikedByUser);
}

class PostListModel extends ChangeNotifier {
  final List<Post> _posts = [];
  final Set<String> _pending = {};
  int _likeAttempts = 0;

  List<Post> get posts => List.unmodifiable(_posts);
  bool isPending(String id) => _pending.contains(id);

  void loadPosts() {
    _posts.addAll([
      Post(id: '1', title: 'Getting started with Flutter'),
      Post(id: '2', title: 'State management deep dive'),
      Post(id: '3', title: 'Building custom widgets'),
      Post(id: '4', title: 'Performance optimization tips'),
    ]);
    notifyListeners();
  }

  Future<bool> toggleLike(String id) async {
    final post = _posts.firstWhere((p) => p.id == id);
    final saved = post.snapshot();

    // Optimistic update
    post.isLikedByUser = !post.isLikedByUser;
    post.likeCount += post.isLikedByUser ? 1 : -1;
    _pending.add(id);
    notifyListeners();

    try {
      await _simulateApi();
      _pending.remove(id);
      notifyListeners();
      return true;
    } catch (_) {
      // Rollback
      post.isLikedByUser = saved.isLikedByUser;
      post.likeCount = saved.likeCount;
      _pending.remove(id);
      notifyListeners();
      return false;
    }
  }

  Future<void> _simulateApi() async {
    await Future.delayed(const Duration(seconds: 1));
    _likeAttempts++;
    if (_likeAttempts % 3 == 0) {
      throw Exception('Simulated API failure');
    }
  }
}
```

### Common Mistakes

**Not saving the snapshot before mutation.** Without a pre-mutation snapshot, rollback logic gets the direction wrong. Always snapshot first.

**Calling `notifyListeners` only once.** You need three calls: after optimistic update, after success, and after rollback.

**Not removing from `_pending` on failure.** Failed posts show a perpetual loading spinner.

---

## Exercise 7: InheritedWidget From Scratch

### Progressive Hints

1. Study `InheritedWidget`'s source code. The key is `InheritedElement`, not the widget itself.
2. `InheritedElement` overrides `_updateInheritance` to register in the Element's `_inheritedWidgets` HashMap. This makes `dependOnInheritedWidgetOfExactType` an O(1) lookup.
3. Since `_updateInheritance` is framework-private, build on top of `InheritedWidget` but reimplement the notification and theme merging logic.
4. For nested themes, each `CustomTheme` replaces the entry for its type in the inherited map. Closest ancestor wins.

### Full Solution

```dart
// file: lib/custom_inherited.dart
import 'package:flutter/widgets.dart';

// This exercise uses InheritedWidget's real mechanism but reimplements the
// pattern to understand the internals. The key insight: InheritedWidget works
// because InheritedElement overrides _updateInheritance to register itself
// in the Element tree's _inheritedWidgets HashMap. This is what makes
// dependOnInheritedWidgetOfExactType an O(1) lookup.

// Since _updateInheritance is framework-private, we build on top of
// InheritedWidget but reimplement the notification logic and theme merging.

class ThemeData {
  final Color primaryColor;
  final double fontSize;
  final double spacing;
  const ThemeData({this.primaryColor = const Color(0xFF2196F3), this.fontSize = 14.0, this.spacing = 8.0});
}

class CustomTheme extends InheritedWidget {
  final ThemeData themeData;
  const CustomTheme({super.key, required this.themeData, required super.child});

  static ThemeData of(BuildContext context) {
    final widget = context.dependOnInheritedWidgetOfExactType<CustomTheme>();
    if (widget == null) throw FlutterError('No CustomTheme ancestor.');
    return widget.themeData;
  }

  static ThemeData? maybeOf(BuildContext context) {
    return context.dependOnInheritedWidgetOfExactType<CustomTheme>()?.themeData;
  }

  @override
  bool updateShouldNotify(CustomTheme oldWidget) {
    return themeData.primaryColor != oldWidget.themeData.primaryColor ||
           themeData.fontSize != oldWidget.themeData.fontSize ||
           themeData.spacing != oldWidget.themeData.spacing;
  }
}

// For InheritedNotifier equivalent: use InheritedNotifier<ValueNotifier<ThemeData>>
// which automatically calls updateShouldNotify when the Listenable fires.
```

### Deep Dive

The Element tree maintains a `HashMap<Type, InheritedElement>` called `_inheritedWidgets`. Each `InheritedElement` copies its parent's map and adds itself. This makes `dependOnInheritedWidgetOfExactType<T>()` an O(1) HashMap lookup. When `updateShouldNotify` returns true, the framework iterates over registered dependents and calls `didChangeDependencies()` on each. Non-dependent widgets are untouched. Nested `InheritedWidget`s of the same type replace each other in the map -- the closest ancestor wins.

---

## Exercise 8: Build a Mini State Management Library

### Progressive Hints

1. `Store` holds state `T`, a `Reducer<T>`, and a `StreamController<T>`. `dispatch` runs the reducer and pushes to the stream.
2. Middleware wraps the dispatch chain. Each receives state, action, and `next`. It can transform, block, or forward.
3. Undo/redo: maintain a state list with a pointer. New dispatch pushes and truncates redo history.
4. `Computed` caches a selector result. Re-run on state change, notify only if derived value differs.
5. `StoreProvider` is an `InheritedWidget`. `StoreConnector` subscribes to the stream and rebuilds only when ViewModel changes.

### Full Solution

The library has five core pieces. Each file is small and focused.

```dart
// file: lib/mini_state/action.dart
abstract class Action { String get name; }
typedef Reducer<T> = T Function(T state, Action action);

// file: lib/mini_state/middleware.dart
typedef Next<T> = T Function(Action action);
typedef Middleware<T> = T Function(T state, Action action, Next<T> next);

class LoggingMiddleware<T> {
  T call(T state, Action action, Next<T> next) {
    print('[${DateTime.now()}] Action: ${action.name} | Before: $state');
    final newState = next(action);
    print('  After: $newState');
    return newState;
  }
}

// file: lib/mini_state/history.dart
class StateHistory<T> {
  final int maxDepth;
  final List<T> _states = [];
  int _pointer = -1;

  StateHistory({this.maxDepth = 50});

  void push(T state) {
    if (_pointer < _states.length - 1) {
      _states.removeRange(_pointer + 1, _states.length);
    }
    _states.add(state);
    if (_states.length > maxDepth) _states.removeAt(0);
    _pointer = _states.length - 1;
  }

  T? undo() => canUndo ? _states[--_pointer] : null;
  T? redo() => canRedo ? _states[++_pointer] : null;
  T? timeTravelTo(int i) => (i >= 0 && i < _states.length) ? _states[_pointer = i] : null;
  bool get canUndo => _pointer > 0;
  bool get canRedo => _pointer < _states.length - 1;
  int get length => _states.length;
  int get currentIndex => _pointer;
}

// file: lib/mini_state/computed.dart
class Computed<T, R> {
  final R Function(T state) _selector;
  R? _cached;
  bool _initialized = false;

  Computed(this._selector);
  R get value => _initialized ? _cached as R : (throw StateError('Call update() first'));

  /// Returns true if the derived value changed.
  bool update(T state) {
    final v = _selector(state);
    if (_initialized && _cached == v) return false;
    _cached = v;
    _initialized = true;
    return true;
  }
}
```

The `Store` is the heart -- it chains middleware, tracks history, and exposes a broadcast stream:

```dart
// file: lib/mini_state/store.dart
import 'dart:async';
import 'action.dart';
import 'middleware.dart';
import 'history.dart';

class Store<T> {
  T _state;
  final Reducer<T> _reducer;
  final List<Middleware<T>> _middlewares;
  final StateHistory<T> history;
  final _controller = StreamController<T>.broadcast();
  final List<Map<String, dynamic>> _actionLog = [];

  Store({required T initialState, required Reducer<T> reducer,
         List<Middleware<T>> middlewares = const [], int maxHistoryDepth = 50})
      : _state = initialState, _reducer = reducer, _middlewares = middlewares,
        history = StateHistory<T>(maxDepth: maxHistoryDepth) {
    history.push(initialState);
  }

  T get state => _state;
  Stream<T> get stream => _controller.stream;
  List<Map<String, dynamic>> get actionLog => List.unmodifiable(_actionLog);

  void dispatch(Action action) {
    Next<T> chain = (a) => _reducer(_state, a);
    for (final mw in _middlewares.reversed) {
      final next = chain;
      chain = (a) => mw(_state, a, next);
    }
    _state = chain(action);
    _actionLog.add({'action': action.name, 'timestamp': DateTime.now().toIso8601String()});
    history.push(_state);
    _controller.add(_state);
  }

  void undo()  { final s = history.undo();  if (s != null) { _state = s; _controller.add(s); } }
  void redo()  { final s = history.redo();  if (s != null) { _state = s; _controller.add(s); } }
  void timeTravelTo(int i) { final s = history.timeTravelTo(i); if (s != null) { _state = s; _controller.add(s); } }
  void dispose() => _controller.close();
}
```

The widget layer connects the store to the Flutter tree:

```dart
// file: lib/mini_state/provider.dart
import 'package:flutter/widgets.dart';
import 'store.dart';

class StoreProvider<T> extends InheritedWidget {
  final Store<T> store;
  const StoreProvider({super.key, required this.store, required super.child});

  static Store<T> of<T>(BuildContext context) {
    final p = context.dependOnInheritedWidgetOfExactType<StoreProvider<T>>();
    if (p == null) throw FlutterError('No StoreProvider<$T> found.');
    return p.store;
  }

  @override
  bool updateShouldNotify(StoreProvider<T> old) => store != old.store;
}

/// Rebuilds only when the ViewModel (derived from state) changes.
class StoreConnector<T, VM> extends StatelessWidget {
  final VM Function(T state) converter;
  final Widget Function(BuildContext context, VM vm) builder;
  const StoreConnector({super.key, required this.converter, required this.builder});

  @override
  Widget build(BuildContext context) {
    final store = StoreProvider.of<T>(context);
    return StreamBuilder<T>(
      initialData: store.state,
      stream: store.stream.distinct(),
      builder: (ctx, snap) => builder(ctx, converter(snap.data as T)),
    );
  }
}
```

For the devtools overlay, build a `StatelessWidget` that reads the store's `actionLog` and `history`, renders a `Slider` for time-travel, and undo/redo buttons. The slider's `onChanged` calls `store.timeTravelTo(index)`. Display current state by converting it to a `Map` or JSON string.

### Debugging Tips for All Exercises

**"setState() called after dispose()"** -- Check `mounted` before calling `setState` after any async gap.

**"Could not find the correct Provider"** -- Verify the generic type and that the widget is a descendant (not sibling) of the Provider.

**Infinite rebuild loops** -- Never mutate state unconditionally inside `build`. Mutations belong in event handlers or lifecycle callbacks.

**`StreamBuilder` stale on hot reload** -- Provide `initialData` or use `stream.distinct()`.

### Alternatives Worth Knowing

- **Riverpod** -- Provider rewritten without BuildContext dependency. Supports autodispose and compile-safe DI.
- **Bloc/Cubit** -- Event-driven, strict separation of events and states. More verbose but highly testable.
- **Signals** -- Fine-grained reactivity (SolidJS-inspired). Only the specific UI reading a signal updates, no widget rebuild.

The principles from this section -- separating state from UI, managing rebuild scope, thinking about state lifecycle -- apply regardless of which library you choose.
