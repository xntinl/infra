# Section 12 -- Flutter State Management Basics

## Introduction

Every Flutter app is a function of state: `UI = f(state)`. Tap a button, the counter increments, the screen repaints. Toggle dark mode, every themed widget updates. Add an item to a cart, the badge in the app bar reflects the new count. The question is never whether you need state management -- it is how you manage it without turning your codebase into an unpredictable mess.

The naive approach works at first. You call `setState` inside a `StatefulWidget`, the framework rebuilds, done. But as your app grows, you face three hard problems: sharing state between widgets that are not in a direct parent-child relationship, keeping rebuilds efficient so that changing one field does not repaint the entire screen, and making state changes predictable enough to debug at 2 AM when production is broken.

This section takes you from `setState` through `InheritedWidget` to the Provider package, building your mental model of how Flutter's reactive framework actually propagates state changes through the widget tree.

## Prerequisites

You must be comfortable with:

- **Section 09** -- Flutter setup, StatelessWidget, StatefulWidget, widget lifecycle
- **Section 10** -- Layouts, widget composition, the widget tree structure
- **Section 11** -- Navigation, routing, and passing data between screens

## Learning Objectives

After completing this section you will be able to:

1. **Distinguish** between ephemeral state and app state, choosing the correct scope for each
2. **Apply** `setState` correctly, understanding which subtree rebuilds and why
3. **Lift** state to a common ancestor to share data between sibling widgets
4. **Implement** `InheritedWidget` with `of` and `maybeOf` patterns for dependency injection
5. **Use** `ChangeNotifier` and `ValueNotifier` to decouple state logic from widgets
6. **Integrate** the Provider package with `ChangeNotifierProvider`, `Consumer`, `Selector`, and `ProxyProvider`
7. **Evaluate** when `setState` is sufficient versus when a dedicated state management solution is warranted
8. **Construct** reactive UIs with `ValueListenableBuilder` and `StreamBuilder`

---

## Core Concepts

### 1. Ephemeral State vs App State

Before writing any code, you need a framework for deciding where state lives. Get this wrong and you will either over-engineer a counter or under-engineer a shopping cart.

**Ephemeral state** (also called UI state or local state) is state that belongs to a single widget. A text field's current value, whether an animation is playing, the current page index in a `PageView`. No other widget needs this information, and losing it on a rebuild is acceptable.

**App state** (also called shared state or application state) is state that multiple parts of your app need. The authenticated user, a shopping cart, notification preferences. This state must survive navigation and be accessible from widgets that have no direct relationship.

The rule: if only one widget cares, keep it local with `setState`. If multiple widgets care, lift it up or use a state management solution.

### 2. setState -- Mechanics and Scope

`setState` is the most fundamental state primitive. It does exactly two things: it runs the callback you pass (where you mutate state), then it marks the widget as dirty so the framework schedules a rebuild.

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

  void _increment() {
    setState(() {
      _count++;
    });
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Counter')),
      body: Center(
        child: Text('$_count', style: Theme.of(context).textTheme.headlineLarge),
      ),
      floatingActionButton: FloatingActionButton(
        onPressed: _increment,
        child: const Icon(Icons.add),
      ),
    );
  }
}
```

Critical detail: `setState` rebuilds the entire `build` method of that `State` object. Every widget returned from `build` is reconstructed. Flutter's diffing engine (the Element tree) is efficient enough that unchanged subtrees are not re-rendered, but the `build` method still runs. This means expensive computations inside `build` run on every `setState` call.

A common mistake that will crash your app:

```dart
// file: lib/bad_set_state.dart
// WRONG: calling setState after the widget is removed from the tree
void _loadData() async {
  final data = await api.fetchData();
  // If the user navigated away, this State is disposed
  setState(() {
    _data = data; // Throws: setState() called after dispose()
  });
}

// CORRECT: check mounted before calling setState
void _loadDataSafe() async {
  final data = await api.fetchData();
  if (!mounted) return;
  setState(() {
    _data = data;
  });
}
```

### 3. Lifting State Up

When two sibling widgets need the same state, neither can own it. The solution is to move the state to their common parent and pass it down.

```dart
// file: lib/temperature_converter.dart
import 'package:flutter/material.dart';

class TemperatureConverter extends StatefulWidget {
  const TemperatureConverter({super.key});

  @override
  State<TemperatureConverter> createState() => _TemperatureConverterState();
}

class _TemperatureConverterState extends State<TemperatureConverter> {
  double _celsius = 0;

  void _updateCelsius(double value) {
    setState(() => _celsius = value);
  }

  @override
  Widget build(BuildContext context) {
    return Column(
      children: [
        CelsiusInput(celsius: _celsius, onChanged: _updateCelsius),
        FahrenheitDisplay(celsius: _celsius),
      ],
    );
  }
}

class CelsiusInput extends StatelessWidget {
  final double celsius;
  final ValueChanged<double> onChanged;

  const CelsiusInput({super.key, required this.celsius, required this.onChanged});

  @override
  Widget build(BuildContext context) {
    return Slider(
      value: celsius,
      min: -40,
      max: 100,
      onChanged: onChanged,
    );
  }
}

class FahrenheitDisplay extends StatelessWidget {
  final double celsius;
  const FahrenheitDisplay({super.key, required this.celsius});

  @override
  Widget build(BuildContext context) {
    final fahrenheit = celsius * 9 / 5 + 32;
    return Text('${fahrenheit.toStringAsFixed(1)} F');
  }
}
```

This works but does not scale. When the parent is five levels up, you end up passing state through every intermediate widget -- a pattern called **prop drilling**. Each intermediate widget receives and forwards props it does not use, creating coupling and noise.

### 4. InheritedWidget -- The Framework's Built-In Solution

`InheritedWidget` solves prop drilling by allowing descendants to look up an ancestor directly, skipping every widget in between. When you call `Theme.of(context)` or `MediaQuery.of(context)`, you are using `InheritedWidget`.

```dart
// file: lib/app_state_widget.dart
import 'package:flutter/material.dart';

class AppState {
  final String username;
  final bool isDarkMode;

  const AppState({required this.username, required this.isDarkMode});
}

class AppStateWidget extends InheritedWidget {
  final AppState state;
  final void Function(AppState) onStateChanged;

  const AppStateWidget({
    super.key,
    required this.state,
    required this.onStateChanged,
    required super.child,
  });

  static AppStateWidget of(BuildContext context) {
    final widget = context.dependOnInheritedWidgetOfExactType<AppStateWidget>();
    if (widget == null) {
      throw FlutterError('AppStateWidget.of() called without an AppStateWidget ancestor.');
    }
    return widget;
  }

  static AppStateWidget? maybeOf(BuildContext context) {
    return context.dependOnInheritedWidgetOfExactType<AppStateWidget>();
  }

  @override
  bool updateShouldNotify(AppStateWidget oldWidget) {
    return state.username != oldWidget.state.username ||
           state.isDarkMode != oldWidget.state.isDarkMode;
  }
}
```

How it actually works: when a widget calls `dependOnInheritedWidgetOfExactType<T>`, the framework registers that widget's Element as a dependent of the `InheritedWidget`'s Element. When `updateShouldNotify` returns true, the framework walks the list of dependents and marks each for rebuild. This is an O(dependents) operation, not an O(tree) walk.

The `of` pattern throws if the ancestor is missing (a programming error you want to catch early). The `maybeOf` pattern returns null for optional dependencies.

### 5. ChangeNotifier and ValueNotifier

`ChangeNotifier` implements the observer pattern. It maintains a list of listeners and notifies them when state changes. It decouples your state logic from the widget layer entirely.

```dart
// file: lib/cart_model.dart
import 'package:flutter/foundation.dart';

class CartItem {
  final String id;
  final String name;
  final double price;
  int quantity;

  CartItem({required this.id, required this.name, required this.price, this.quantity = 1});
}

class CartModel extends ChangeNotifier {
  final Map<String, CartItem> _items = {};

  List<CartItem> get items => List.unmodifiable(_items.values);
  int get totalItems => _items.values.fold(0, (sum, item) => sum + item.quantity);
  double get totalPrice => _items.values.fold(0, (sum, item) => sum + item.price * item.quantity);

  void addItem(String id, String name, double price) {
    if (_items.containsKey(id)) {
      _items[id]!.quantity++;
    } else {
      _items[id] = CartItem(id: id, name: name, price: price);
    }
    notifyListeners();
  }

  void removeItem(String id) {
    _items.remove(id);
    notifyListeners();
  }

  void clear() {
    _items.clear();
    notifyListeners();
  }
}
```

`ValueNotifier<T>` is a simpler variant that holds a single value and notifies when it changes. Pair it with `ValueListenableBuilder` for lightweight reactive widgets:

```dart
// file: lib/theme_toggle.dart
import 'package:flutter/material.dart';

final isDarkMode = ValueNotifier<bool>(false);

class ThemeToggle extends StatelessWidget {
  const ThemeToggle({super.key});

  @override
  Widget build(BuildContext context) {
    return ValueListenableBuilder<bool>(
      valueListenable: isDarkMode,
      builder: (context, dark, child) {
        return Switch(
          value: dark,
          onChanged: (value) => isDarkMode.value = value,
        );
      },
    );
  }
}
```

The `child` parameter in `ValueListenableBuilder` (and `Consumer`, `AnimatedBuilder`, etc.) is an optimization: widgets passed as `child` are built once and reused across rebuilds.

### 6. Provider -- The Standard Approach

Provider wraps `InheritedWidget` with a developer-friendly API. It eliminates the boilerplate of writing `InheritedWidget` subclasses and adds lifecycle management, lazy initialization, and scoped disposal.

```dart
// file: lib/main_with_provider.dart
import 'package:flutter/material.dart';
import 'package:provider/provider.dart';
import 'cart_model.dart';

void main() {
  runApp(
    ChangeNotifierProvider(
      create: (_) => CartModel(),
      child: const MyApp(),
    ),
  );
}

class MyApp extends StatelessWidget {
  const MyApp({super.key});

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      home: const ProductListPage(),
    );
  }
}

class ProductListPage extends StatelessWidget {
  const ProductListPage({super.key});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Products'),
        actions: [
          // Consumer rebuilds only this subtree when CartModel changes
          Consumer<CartModel>(
            builder: (context, cart, child) {
              return Badge(
                label: Text('${cart.totalItems}'),
                child: child,
              );
            },
            child: const Icon(Icons.shopping_cart),
          ),
        ],
      ),
      body: ListView(
        children: [
          ListTile(
            title: const Text('Widget Handbook'),
            subtitle: const Text('\$29.99'),
            trailing: IconButton(
              icon: const Icon(Icons.add_shopping_cart),
              onPressed: () {
                // context.read does NOT listen -- use for event handlers
                context.read<CartModel>().addItem('1', 'Widget Handbook', 29.99);
              },
            ),
          ),
        ],
      ),
    );
  }
}
```

Key Provider patterns:

- **`context.watch<T>()`** -- listens for changes, triggers rebuild. Use in `build` methods.
- **`context.read<T>()`** -- reads once, no subscription. Use in callbacks and event handlers.
- **`Consumer<T>`** -- scopes rebuilds to just the subtree inside the builder.
- **`Selector<T, S>`** -- rebuilds only when the selected value changes, ignoring other mutations.

```dart
// file: lib/cart_total_widget.dart
import 'package:flutter/material.dart';
import 'package:provider/provider.dart';
import 'cart_model.dart';

class CartTotalWidget extends StatelessWidget {
  const CartTotalWidget({super.key});

  @override
  Widget build(BuildContext context) {
    // Only rebuilds when totalPrice changes, not on every CartModel notification
    final total = context.select<CartModel, double>((cart) => cart.totalPrice);
    return Text('Total: \$${total.toStringAsFixed(2)}');
  }
}
```

### 7. StreamBuilder and Reactive Patterns

For data that arrives over time -- WebSocket messages, database listeners, periodic updates -- `StreamBuilder` provides a declarative bridge between `Stream` and widgets.

```dart
// file: lib/live_clock.dart
import 'package:flutter/material.dart';

class LiveClock extends StatelessWidget {
  const LiveClock({super.key});

  @override
  Widget build(BuildContext context) {
    return StreamBuilder<DateTime>(
      stream: Stream.periodic(
        const Duration(seconds: 1),
        (_) => DateTime.now(),
      ),
      builder: (context, snapshot) {
        if (!snapshot.hasData) return const CircularProgressIndicator();
        final time = snapshot.data!;
        return Text(
          '${time.hour.toString().padLeft(2, '0')}:'
          '${time.minute.toString().padLeft(2, '0')}:'
          '${time.second.toString().padLeft(2, '0')}',
          style: Theme.of(context).textTheme.headlineMedium,
        );
      },
    );
  }
}
```

### 8. When setState Is Enough

Not everything needs Provider. Here is a decision framework:

- **Single widget, UI-only state** (animation playing, tab index, form field focus) -- `setState`.
- **Two or three closely related widgets** -- lift state to the parent, pass via constructor.
- **State needed across multiple unrelated screens** -- Provider or another state management solution.
- **Server-pushed data or long-lived streams** -- `StreamBuilder` or `StreamProvider`.

The anti-pattern to avoid is the "god widget" -- a single `StatefulWidget` at the root that owns all state and passes everything down through ten levels of constructors. It rebuilds the entire app on every change and makes the code impossible to maintain.

---

## Exercises

### Exercise 1 (Basic): Counter with setState

**Objective:** Build a counter with increment, decrement, and reset, demonstrating `setState` scope.

**Instructions:**

1. Create `lib/counter_page.dart` with a `StatefulWidget` that displays a counter
2. Add three buttons: increment (+1), decrement (-1), and reset (back to 0)
3. Decrement must not go below 0 -- show a `SnackBar` when the user tries
4. Extract the display into a separate `StatelessWidget` called `CounterDisplay` that receives the count as a parameter
5. Add a `debugPrint` call inside both `build` methods to observe which widgets rebuild

**Verification:**

```dart
// file: bin/exercise1_test.dart
// Run: flutter run lib/counter_page.dart
// 1. Tap + three times -> display shows 3
// 2. Tap - once -> display shows 2
// 3. Tap reset -> display shows 0
// 4. Tap - at 0 -> SnackBar appears with warning message
// 5. Check console: both build methods print on every setState call
```

---

### Exercise 2 (Basic): ValueNotifier and ValueListenableBuilder

**Objective:** Use `ValueNotifier` to share a theme preference between widgets without `setState`.

**Instructions:**

1. Create `lib/theme_demo.dart` with a `ValueNotifier<ThemeMode>` declared at the top level
2. Create a `ThemeSelector` widget with three `ChoiceChip` widgets (Light, Dark, System) that write to the notifier
3. Create a `ThemePreview` widget that reads the notifier via `ValueListenableBuilder` and displays a colored container matching the selected theme
4. Place both widgets in a `Column` inside a `Scaffold` -- they communicate only through the shared `ValueNotifier`, with no parent `StatefulWidget`
5. Add a third widget `ThemeLabel` that also listens to the same notifier and displays the current mode as text

**Verification:**

```dart
// file: bin/exercise2_test.dart
// Run: flutter run lib/theme_demo.dart
// 1. Tap "Dark" chip -> preview container turns dark, label reads "ThemeMode.dark"
// 2. Tap "Light" chip -> preview turns light, label updates
// 3. No StatefulWidget exists in the tree -- confirm by searching the source
```

---

### Exercise 3 (Intermediate): InheritedWidget for User Session

**Objective:** Implement a custom `InheritedWidget` to share user session data across the app.

**Instructions:**

1. Create `lib/user_session.dart` with a `UserSession` data class holding `username`, `email`, `avatarUrl`, and `isAdmin`
2. Create `UserSessionWidget extends InheritedWidget` with proper `of` (throws) and `maybeOf` (returns null) static methods
3. Implement `updateShouldNotify` to compare fields individually, not by reference
4. Create a `UserSessionProvider` StatefulWidget that wraps `UserSessionWidget` and exposes an `updateSession` method
5. Create three consumer widgets in separate files: `UserAvatar`, `UserGreeting`, and `AdminPanel` (only visible when `isAdmin` is true)
6. `AdminPanel` must use `maybeOf` to gracefully handle being rendered outside a session context

**Verification:**

```dart
// file: bin/exercise3_test.dart
// Run: flutter run lib/main_session.dart
// 1. App starts with default user -> greeting shows "Hello, Guest"
// 2. Tap "Login" -> session updates -> greeting shows "Hello, Alice", avatar loads
// 3. Toggle admin -> AdminPanel appears/disappears
// 4. Place AdminPanel outside UserSessionWidget -> it shows "No session" instead of crashing
```

---

### Exercise 4 (Intermediate): Provider Todo App

**Objective:** Build a todo application using `ChangeNotifierProvider` with filtering and persistence.

**Instructions:**

1. Create `lib/todo_model.dart` with a `TodoItem` class (id, title, isCompleted, createdAt) and a `TodoModel extends ChangeNotifier`
2. `TodoModel` must support: add, toggle, delete, and clear completed
3. Create `lib/todo_filter.dart` with a `TodoFilter` enum (all, active, completed) and a `TodoFilterModel extends ChangeNotifier`
4. Use `MultiProvider` to provide both models at the app root
5. Create a `TodoList` widget that uses `Selector` to rebuild only when the filtered list changes, not on every model notification
6. Create an `AddTodoSheet` bottom sheet that reads `TodoModel` with `context.read` (not `watch`) to add items
7. Display a count of remaining active items in the app bar using `Consumer`

**Verification:**

```dart
// file: bin/exercise4_test.dart
// Run: flutter run lib/main_todo.dart
// 1. Add "Buy groceries" and "Read Dart docs" -> list shows both
// 2. Toggle "Buy groceries" to completed -> strikethrough appears
// 3. Switch filter to "Active" -> only "Read Dart docs" visible
// 4. Switch filter to "Completed" -> only "Buy groceries" visible
// 5. App bar shows "1 item left" when one is active
// 6. Clear completed -> "Buy groceries" is removed
```

---

### Exercise 5 (Advanced): Multi-Feature App with Scoped Providers

**Objective:** Architect an app with authentication, a product catalog, and a shopping cart using scoped and dependent providers.

**Instructions:**

1. Create an `AuthModel extends ChangeNotifier` with `login`, `logout`, `currentUser`, and `isAuthenticated`
2. Create a `CatalogModel extends ChangeNotifier` that loads products and supports search/filter
3. Create a `CartModel extends ChangeNotifier` that depends on `AuthModel` (cart is per-user)
4. Use `ProxyProvider` to inject `AuthModel` into `CartModel` so the cart clears on logout
5. Scope the `CatalogModel` provider to only the catalog screen (it should not exist when the user is on the profile screen)
6. Use `Selector` on the catalog page so that toggling a product's favorite status does not rebuild the entire list
7. Implement a `CheckoutPage` that reads from both `CartModel` and `AuthModel`

**Verification:**

```dart
// file: bin/exercise5_test.dart
// Run: flutter run lib/main_shop.dart
// 1. Login as "alice" -> cart is empty, catalog loads
// 2. Add two products to cart -> badge updates in real time
// 3. Navigate to checkout -> shows cart items and user info
// 4. Logout -> cart is cleared, redirected to login
// 5. Login as "bob" -> fresh empty cart (per-user state)
// 6. Navigate away from catalog -> CatalogModel is disposed (verify via debugPrint in dispose)
```

---

### Exercise 6 (Advanced): Optimistic Updates with Rollback

**Objective:** Implement optimistic UI updates that roll back on failure, demonstrating real-world state handling.

**Instructions:**

1. Create `lib/post_model.dart` with a `Post` class (id, title, likeCount, isLikedByUser) and a `PostListModel extends ChangeNotifier`
2. Implement a `toggleLike` method that: immediately updates the UI (optimistic), sends a fake API call (delayed future), and rolls back if the call fails
3. Simulate failures: every third like should fail. Show a `SnackBar` with "Failed to update, reverted" on failure
4. Use `Selector` so that liking one post does not rebuild the entire list -- only the affected `PostCard` rebuilds
5. Add a retry mechanism: if the optimistic update fails, offer a "Retry" action in the `SnackBar`
6. Track pending operations: show a subtle loading indicator on posts with in-flight API calls

**Verification:**

```dart
// file: bin/exercise6_test.dart
// Run: flutter run lib/main_posts.dart
// 1. Tap like on post 1 -> heart fills immediately (optimistic)
// 2. After 1 second, if API succeeds -> stays liked
// 3. Like a third post -> API fails -> heart reverts, SnackBar appears
// 4. Tap "Retry" in SnackBar -> like attempt repeats
// 5. While API is in-flight, a small spinner appears on the post card
// 6. Only the tapped PostCard rebuilds -- add debugPrint to verify
```

---

### Exercise 7 (Insane): InheritedWidget From Scratch

**Objective:** Implement a custom `InheritedWidget` equivalent to understand the Element tree lookup mechanism, then use it to build a theme system.

**Instructions:**

1. Create `lib/custom_inherited.dart` -- do NOT use Flutter's `InheritedWidget`
2. Implement a `CustomInheritedWidget extends ProxyWidget` that:
   - Creates a `CustomInheritedElement` that overrides `_updateInheritance` to register itself in the Element's inherited widgets map
   - Stores dependents in a `Set<Element>` and notifies them via `didChangeDependencies`
   - Implements `updateShouldNotify` logic
3. Implement the `of(BuildContext context)` static method by manually walking up the element tree using `context.getElementForInheritedWidgetOfExactType` and then registering the dependency
4. Build a `CustomTheme` on top of your implementation with `colors`, `typography`, and `spacing` properties
5. Create a demo app with three levels of nested `CustomTheme` (app-level, screen-level, component-level) where inner themes override outer ones
6. Add an `InheritedNotifier` equivalent that automatically listens to a `Listenable` and triggers updates
7. Write a test that proves dependents are notified only when `updateShouldNotify` returns true, and that non-dependents are not rebuilt

**Verification:**

```dart
// file: bin/exercise7_test.dart
// Run: flutter run lib/main_custom_theme.dart
// 1. App-level theme sets blue primary color
// 2. Screen-level theme overrides to green -- children of that screen see green
// 3. Component-level theme overrides to red -- only that component and its children see red
// 4. Changing the app-level theme triggers rebuilds in dependents at all levels
// 5. A widget that does NOT call CustomTheme.of() does NOT rebuild -- verify with debugPrint
// 6. Console logs show exactly which Elements were notified and rebuilt
```

---

### Exercise 8 (Insane): Build a Mini State Management Library

**Objective:** Create a state management library from scratch that supports scoped state, computed values, middleware, undo/redo, and devtools-like inspection.

**Instructions:**

1. Create `lib/mini_state/store.dart` -- a `Store<T>` class that holds immutable state, exposes a `Stream<T>` of changes, and supports `dispatch(Action)` for updates
2. Create `lib/mini_state/action.dart` -- an `Action` base class and a `Reducer<T>` typedef `T Function(T state, Action action)`
3. Create `lib/mini_state/middleware.dart` -- a `Middleware<T>` typedef that can intercept, transform, or reject actions before they reach the reducer. Implement a `LoggingMiddleware` that prints every action and state transition
4. Create `lib/mini_state/computed.dart` -- a `Computed<T, R>` class that derives a value from state and only recomputes when dependencies change (memoized)
5. Create `lib/mini_state/history.dart` -- undo/redo support by maintaining a history stack of states with a configurable max depth. Support `undo()`, `redo()`, `canUndo`, `canRedo`, and `timeTravelTo(int index)`
6. Create `lib/mini_state/provider.dart` -- a `StoreProvider` InheritedWidget and a `StoreConnector<T, VM>` widget (similar to Redux's `connect`) that selects a ViewModel from the store and rebuilds only when the ViewModel changes
7. Create `lib/mini_state/devtools.dart` -- a `DevToolsOverlay` widget that displays: current state as JSON, action history with timestamps, a slider for time-travel debugging, and undo/redo buttons
8. Build a demo: a note-taking app where you can add, edit, and delete notes. Every action goes through middleware, state is computed for filtered views, and the devtools overlay lets you time-travel through every change

**Verification:**

```dart
// file: bin/exercise8_test.dart
// Run: flutter run lib/main_mini_state.dart
// 1. Add three notes -> action history shows AddNote x3
// 2. Open devtools overlay -> current state shows all three notes as JSON
// 3. Tap undo -> last note disappears, redo becomes available
// 4. Drag time-travel slider to position 1 -> only the first note is visible
// 5. Drag slider back to position 3 -> all three notes reappear
// 6. Edit a note -> LoggingMiddleware prints: "Action: EditNote, Before: {...}, After: {...}"
// 7. Filter notes by keyword -> Computed value updates, list rebuilds with only matching notes
// 8. Console shows that non-matching StoreConnectors did NOT rebuild
```

---

## Summary

State management in Flutter is a spectrum, not a binary choice. `setState` handles local, ephemeral state inside a single widget. Lifting state up works when a handful of closely related widgets need to share data. `InheritedWidget` provides framework-level dependency injection that skips intermediate widgets entirely. Provider wraps `InheritedWidget` with lifecycle management, scoping, and optimized selectors.

The key insights: `setState` rebuilds the entire `build` method of its `State` object. `InheritedWidget` notifies only registered dependents. `Selector` narrows rebuilds further by comparing derived values. `ValueNotifier` and `StreamBuilder` give you reactive widgets without the full Provider setup.

The wrong question is "which state management library should I use?" The right question is "what kind of state am I managing and how widely is it shared?"

## What's Next

**Section 13 -- Flutter Forms and Input** builds on these state management patterns to handle form validation, text editing controllers, focus management, and complex multi-step forms where state and user input intersect.

## References

- [Flutter docs: State management](https://docs.flutter.dev/data-and-backend/state-mgmt)
- [Flutter docs: Ephemeral vs app state](https://docs.flutter.dev/data-and-backend/state-mgmt/ephemeral-vs-app)
- [InheritedWidget class](https://api.flutter.dev/flutter/widgets/InheritedWidget-class.html)
- [Provider package](https://pub.dev/packages/provider)
- [ChangeNotifier class](https://api.flutter.dev/flutter/foundation/ChangeNotifier-class.html)
- [ValueListenableBuilder class](https://api.flutter.dev/flutter/widgets/ValueListenableBuilder-class.html)
- [StreamBuilder class](https://api.flutter.dev/flutter/widgets/StreamBuilder-class.html)
- [Flutter docs: Simple app state management](https://docs.flutter.dev/data-and-backend/state-mgmt/simple)
