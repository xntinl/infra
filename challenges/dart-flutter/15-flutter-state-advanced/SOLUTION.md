# Section 15 -- Solutions: Flutter Advanced State Management

## How to Use This File

Work through each exercise before looking here. When stuck, follow this order: 1) Read the **progressive hints** -- each reveals more without the full answer. 2) Check **common mistakes** for your exercise. 3) Only then read the **full solution**. 4) After solving, read the **deep dive** for production considerations the exercise does not mention.

---

## Exercise 1: Riverpod Counter and Provider Types

### Progressive Hints

1. `ProviderScope` must be the root widget -- above `MaterialApp`, not inside it.
2. The derived `isEvenProvider` uses `ref.watch(counterProvider)` in its body, creating a reactive dependency.
3. For the `FutureProvider`, return `Future.delayed` with a simulated value. The consumer gets `AsyncValue<String>` with loading/data/error states.
4. Buttons use `ref.read(counterProvider.notifier).state++` inside `onPressed`. Never `ref.watch` in callbacks.

### Full Solution

```dart
// file: lib/providers/counter_providers.dart
final appTitleProvider = Provider<String>((ref) => 'Riverpod Counter App');
final counterProvider = StateProvider<int>((ref) => 0);
final isEvenProvider = Provider<bool>((ref) => ref.watch(counterProvider).isEven);
final quoteProvider = FutureProvider<String>((ref) async {
  await Future.delayed(const Duration(seconds: 2));
  return 'The only way to do great work is to love what you do.';
});
```

```dart
// file: lib/main.dart
void main() => runApp(const ProviderScope(child: App()));

class App extends ConsumerWidget {
  const App({super.key});
  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return MaterialApp(title: ref.watch(appTitleProvider), home: const CounterScreen());
  }
}

class CounterScreen extends ConsumerWidget {
  const CounterScreen({super.key});
  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final count = ref.watch(counterProvider);
    final isEven = ref.watch(isEvenProvider);
    final quote = ref.watch(quoteProvider);
    return Scaffold(
      appBar: AppBar(title: Text(ref.watch(appTitleProvider))),
      body: Center(child: Column(mainAxisAlignment: MainAxisAlignment.center, children: [
        Text('Count: $count', style: const TextStyle(fontSize: 48)),
        Text(isEven ? 'Even' : 'Odd', style: TextStyle(fontSize: 24, color: isEven ? Colors.blue : Colors.orange)),
        const SizedBox(height: 24),
        quote.when(
          data: (text) => Text('"$text"', style: const TextStyle(fontStyle: FontStyle.italic)),
          loading: () => const CircularProgressIndicator(),
          error: (e, _) => Text('Error: $e'),
        ),
      ])),
      floatingActionButton: Row(mainAxisAlignment: MainAxisAlignment.end, children: [
        FloatingActionButton(heroTag: 'dec', onPressed: () => ref.read(counterProvider.notifier).state--, child: const Icon(Icons.remove)),
        const SizedBox(width: 16),
        FloatingActionButton(heroTag: 'inc', onPressed: () => ref.read(counterProvider.notifier).state++, child: const Icon(Icons.add)),
      ]),
    );
  }
}
```

### Common Mistakes

**Missing ProviderScope.** App crashes with `ProviderScope was not found`. Every Riverpod app needs exactly one at the root.

**Using ref.watch in onPressed.** Compiles but creates a leaked subscription inside a callback that fires once. Always `ref.read` in event handlers.

**Two FABs without heroTag.** Flutter throws "multiple heroes with the same tag" during navigation.

### Deep Dive

`isEvenProvider` demonstrates automatic dependency tracking. When `counterProvider` changes, every provider that called `ref.watch(counterProvider)` recomputes, and every widget watching those providers rebuilds. This forms a DAG -- you never manually subscribe or unsubscribe.

---

## Exercise 2: Cubit for Todo Management

### Progressive Hints

1. `Todo` needs `copyWith` for toggling. Without immutable updates, `BlocBuilder` won't detect the change -- Bloc uses `==`.
2. In `addTodo`, check `state is TodoLoaded` first. If still `TodoInitial`, there's no list to modify.
3. For delete SnackBar, use `listenWhen` comparing previous/current list lengths.
4. `BlocProvider` creates a new Cubit when the widget rebuilds. Place it above the navigator to preserve state across navigation.

### Full Solution

```dart
// file: lib/models/todo.dart
class Todo {
  final String id, title;
  final bool isComplete;
  const Todo({required this.id, required this.title, this.isComplete = false});
  Todo copyWith({String? id, String? title, bool? isComplete}) =>
      Todo(id: id ?? this.id, title: title ?? this.title, isComplete: isComplete ?? this.isComplete);
  @override
  bool operator ==(Object other) => identical(this, other) || other is Todo && id == other.id && title == other.title && isComplete == other.isComplete;
  @override
  int get hashCode => Object.hash(id, title, isComplete);
}
```

```dart
// file: lib/cubits/todo_cubit.dart
sealed class TodoState { const TodoState(); }
class TodoInitial extends TodoState { const TodoInitial(); }
class TodoLoaded extends TodoState {
  final List<Todo> todos;
  const TodoLoaded(this.todos);
  @override
  bool operator ==(Object other) => identical(this, other) || other is TodoLoaded && const ListEquality().equals(todos, other.todos);
  @override
  int get hashCode => Object.hashAll(todos);
}
class TodoError extends TodoState { final String message; const TodoError(this.message); }

class TodoCubit extends Cubit<TodoState> {
  TodoCubit() : super(const TodoInitial());
  Future<void> loadTodos() async {
    try { await Future.delayed(const Duration(milliseconds: 500)); emit(const TodoLoaded([])); }
    catch (e) { emit(TodoError(e.toString())); }
  }
  void addTodo(String title) {
    if (state is! TodoLoaded) return;
    emit(TodoLoaded([...(state as TodoLoaded).todos, Todo(id: DateTime.now().toString(), title: title)]));
  }
  void toggleTodo(String id) {
    if (state is! TodoLoaded) return;
    emit(TodoLoaded((state as TodoLoaded).todos.map((t) => t.id == id ? t.copyWith(isComplete: !t.isComplete) : t).toList()));
  }
  void deleteTodo(String id) {
    if (state is! TodoLoaded) return;
    emit(TodoLoaded((state as TodoLoaded).todos.where((t) => t.id != id).toList()));
  }
}
```

### Common Mistakes

**Mutating state directly.** `currentState.todos.add(newTodo); emit(currentState);` does nothing. Bloc sees same reference, skips emission. Always create new list instances.

**Forgetting `..loadTodos()` in BlocProvider.** `BlocProvider(create: (_) => TodoCubit()..loadTodos())` -- the cascade calls `loadTodos()` and still returns the Cubit.

**Using context.read in build().** Unlike Riverpod's `ref.watch`, Bloc's `context.read` does not subscribe. Use `BlocBuilder` or `context.watch` for reactive updates.

### Deep Dive

Cubit vs Bloc: Cubit is sufficient when method names document the transition (`addTodo`, `deleteTodo`). Bloc's named events add an audit trail -- every `TodoAdded`, `TodoDeleted` can be logged and replayed. For a checkout flow where you need to trace exactly what triggered each state change, Bloc's events become essential.

---

## Exercise 3: Multi-Provider Riverpod App

### Progressive Hints

1. `filteredProductsProvider` must handle `productListProvider` loading: use `ref.watch(productListProvider).valueOrNull ?? []`.
2. Pull-to-refresh: `ref.invalidate(productListProvider)` does not return a Future. Follow with `await ref.read(productListProvider.future)`.
3. `cartTotalProvider` must `ref.watch(cartProvider)` to recompute on cart changes.
4. `autoDispose` on `productListProvider` means navigating away and back refetches. Use `keepAlive` if stale data is acceptable.

### Full Solution

```dart
// file: lib/providers/product_providers.dart
final productRepositoryProvider = Provider<ProductRepository>((ref) => ProductRepository());

final productListProvider = FutureProvider.autoDispose<List<Product>>((ref) async {
  return ref.watch(productRepositoryProvider).fetchAll();
});

final productFilterProvider = StateProvider<ProductCategory?>((ref) => null);

final filteredProductsProvider = Provider<List<Product>>((ref) {
  final products = ref.watch(productListProvider).valueOrNull ?? [];
  final filter = ref.watch(productFilterProvider);
  if (filter == null) return products;
  return products.where((p) => p.category == filter).toList();
});

final cartProvider = NotifierProvider<CartNotifier, Cart>(CartNotifier.new);
class CartNotifier extends Notifier<Cart> {
  @override
  Cart build() => const Cart();
  void addItem(Product p) => state = state.addItem(p);
  void removeItem(String id) => state = state.removeItem(id);
  void clear() => state = state.clear();
}

final cartTotalProvider = Provider<double>((ref) {
  return ref.watch(cartProvider).items.fold(0.0, (sum, e) => sum + e.product.price * e.quantity);
});
```

### Common Mistakes

**Forgetting autoDispose.** Without it, `productListProvider` data stays in memory forever after the first fetch. For screen-scoped data, always autoDispose.

**Not propagating AsyncValue through derived providers.** Using `valueOrNull ?? []` silently hides the loading state. If another widget only reads `filteredProductsProvider`, it sees empty list instead of loading. Consider whether to propagate `AsyncValue`.

### Deep Dive

Provider dependencies form a DAG (directed acyclic graph). When `productFilterProvider` changes, only `filteredProductsProvider` recomputes -- not `productListProvider`. When `productListProvider` refetches, both `filteredProductsProvider` and watching widgets update. Riverpod guarantees you never rebuild more than necessary.

The `ref.invalidate` + `ref.read(provider.future)` pattern for pull-to-refresh is a common Riverpod idiom. `invalidate` marks the provider for recomputation but does not block. Reading the `.future` gives a `Future` that completes when the refetch finishes, which is what `RefreshIndicator.onRefresh` expects. Without the `await`, the refresh indicator would disappear instantly instead of waiting for the data.

For the product list screen, wrapping the `ListView` in a `RefreshIndicator` requires the list to be inside a scrollable that supports physics-based overscroll. The `productsAsync.when` handler should wrap the list inside `RefreshIndicator` only in the `data` case, while `loading` and `error` handle their own UI.

---

## Exercise 4: Bloc with Stream Transformers

### Progressive Hints

1. Add `bloc_concurrency` dependency. Import `package:bloc_concurrency/bloc_concurrency.dart`.
2. Debounce goes inside the handler, not the transformer. `restartable` cancels the previous handler on new event -- so `await Future.delayed(300ms)` gets cancelled, creating debounce.
3. `BlocObserver`: override `onTransition`. Register in `main()` with `Bloc.observer = AppBlocObserver()`.
4. Testing debounce: fire events rapidly, then `await Future.delayed(500ms)`. Expect one `SearchLoading` and one `SearchLoaded`.

### Full Solution

```dart
// file: lib/blocs/search_bloc.dart
class SearchBloc extends Bloc<SearchEvent, SearchState> {
  final SearchRepository _repo;
  SearchBloc({required SearchRepository repository}) : _repo = repository, super(const SearchInitial()) {
    on<SearchQueryChanged>(_onQuery, transformer: restartable());
    on<SearchFilterApplied>(_onFilter, transformer: sequential());
    on<SearchResultSelected>(_onSelected, transformer: droppable());
    on<SearchCleared>((_, emit) => emit(const SearchInitial()));
  }

  Future<void> _onQuery(SearchQueryChanged event, Emitter<SearchState> emit) async {
    if (event.query.trim().isEmpty) { emit(const SearchInitial()); return; }
    emit(const SearchLoading());
    await Future.delayed(const Duration(milliseconds: 300)); // cancelled by restartable on new event
    try {
      final results = await _repo.search(event.query);
      emit(results.isEmpty ? SearchEmpty(event.query) : SearchLoaded(results));
    } catch (e) { emit(SearchError(e.toString())); }
  }

  Future<void> _onFilter(SearchFilterApplied event, Emitter<SearchState> emit) async {
    if (state is! SearchLoaded) return;
    emit(const SearchLoading());
    try { emit(SearchLoaded(await _repo.filterResults((state as SearchLoaded).results, event.filter))); }
    catch (e) { emit(SearchError(e.toString())); }
  }

  void _onSelected(SearchResultSelected event, Emitter<SearchState> emit) {
    // Side effect only -- BlocListener handles navigation.
    // droppable prevents double-tap.
  }
}
```

```dart
// file: lib/observers/app_bloc_observer.dart
class AppBlocObserver extends BlocObserver {
  @override
  void onTransition(Bloc bloc, Transition transition) {
    super.onTransition(bloc, transition);
    print('[${bloc.runtimeType}] ${transition.event.runtimeType}: '
        '${transition.currentState.runtimeType} -> ${transition.nextState.runtimeType}');
  }

  @override
  void onError(BlocBase bloc, Object error, StackTrace stackTrace) {
    super.onError(bloc, error, stackTrace);
    print('[${bloc.runtimeType}] Error: $error');
  }
}
// Register in main(): Bloc.observer = AppBlocObserver();
```

### Common Mistakes

**Putting debounce in the transformer.** Works but is more complex. Simpler: `restartable()` + `Future.delayed` in handler. Restartable cancels the delay.

**Emitting after handler cancellation.** When restartable cancels a handler, subsequent `emit` calls are silently ignored. Safe, but be aware try/catch might still execute.

**Not closing the Bloc.** `BlocProvider` handles this automatically. In tests, call `bloc.close()` in `tearDown` or the test runner may hang.

### Deep Dive

Without `restartable()` on search, the handler for "f" might complete after "flutter", overwriting correct results with stale ones. `droppable()` on result selection prevents double-tap navigation -- the first event processes, the second is dropped while the first is active.

---

## Exercise 5: Riverpod Code Generation

### Progressive Hints

1. `pubspec.yaml`: `riverpod_annotation` in dependencies, `riverpod_generator` + `build_runner` in dev_dependencies.
2. Every file needs `part 'filename.g.dart';`.
3. `keepAlive` with timer: `ref.keepAlive()` returns `KeepAliveLink`, call `link.close()` when timer fires.
4. Opt out of autoDispose: `@Riverpod(keepAlive: true)`.

### Full Solution

```dart
// file: lib/providers/generated_providers.dart
part 'generated_providers.g.dart';

@Riverpod(keepAlive: true)
class UserSession extends _$UserSession {
  @override
  UserSessionData build() => const UserSessionData.guest();

  Future<void> login(String email, String password) async {
    state = await _authenticate(email, password);
  }
  void logout() => state = const UserSessionData.guest();
}

@riverpod
Future<Product> productById(Ref ref, String id) async {
  final link = ref.keepAlive();
  final timer = Timer(const Duration(minutes: 5), link.close);
  ref.onDispose(timer.cancel);
  return ref.watch(productRepoProvider).fetchById(id);
}

@riverpod
class PaginatedProducts extends _$PaginatedProducts {
  int _page = 0;
  bool _hasMore = true;

  @override
  Future<List<Product>> build() async {
    _page = 0;
    _hasMore = true;
    return _fetchPage(0);
  }

  Future<List<Product>> _fetchPage(int page) async {
    final results = await ref.read(productRepoProvider).fetchPage(page: page, pageSize: 20);
    if (results.length < 20) _hasMore = false;
    return results;
  }

  Future<void> fetchNextPage() async {
    if (!_hasMore) return;
    final current = state.valueOrNull ?? [];
    _page++;
    state = AsyncData([...current, ...await _fetchPage(_page)]);
  }
}

@riverpod
Stream<List<AppNotification>> notifications(Ref ref) {
  return ref.watch(notificationServiceProvider).notificationStream;
}
```

### Common Mistakes

**Forgetting `dart run build_runner build`.** Files don't exist until generated. Use `dart run build_runner watch` during development for automatic regeneration.

**Using ref.watch in methods other than build().** Inside `fetchNextPage()`, use `ref.read`. Only `build()` creates proper subscriptions with `ref.watch`.

**Forgetting the `part` directive.** Omitting `part 'filename.g.dart';` means generated code cannot connect to your source. Error messages are confusing.

### Deep Dive

Code generation trades a build step for type safety and reduced boilerplate. The generator inspects your function or class signature and produces the exact provider type. A function returning `Future<T>` becomes `FutureProvider<T>`, a class with `Future<T> build()` becomes `AsyncNotifierProvider`. The `family` modifier is inferred from extra parameters. This eliminates the common mistake of choosing the wrong provider type.

The `keepAlive` + timer pattern: user views product detail, data is fetched and cached. Navigate away -- `autoDispose` would normally destroy it, but `keepAlive` prevents that. Timer starts 5-minute countdown. Navigate back within 5 minutes: instant load, no spinner. After 5 minutes: timer fires `link.close()`, provider disposes on next check. Practical caching balancing memory with UX.

---

## Exercise 6: Bloc Performance and Batching

### Progressive Hints

1. Custom batching: collect events within a time window, process only the latest per symbol.
2. Store `Map<String, StockPrice>` updated per batch, not per event. Reduces emissions from hundreds/sec to roughly 10/sec.
3. Riverpod equivalent: `StreamProvider` + `NotifierProvider` with `ref.listen` batching updates on a timer.
4. Measure rebuilds: static counter in widget, increment in `build()`.

### Common Mistakes

**Emitting too many states.** 200 updates/sec = 200 rebuilds/sec. Flutter handles 60fps. Excess rebuilds waste battery and cause jank. Always batch high-frequency updates.

**Mutable Map in Bloc state.** Modifying a Map directly and emitting the same reference: Bloc sees same `==`, skips emission. Always `Map.from(state.prices)`.

### Full Solution (Key Parts)

```dart
// file: lib/blocs/stock_bloc.dart
class StockDashboardState {
  final Map<String, StockPrice> prices;
  final List<StockAlert> alerts;
  final Set<String> watchlist;
  final int updateCount;
  const StockDashboardState({this.prices = const {}, this.alerts = const [], this.watchlist = const {}, this.updateCount = 0});

  StockDashboardState copyWith({Map<String, StockPrice>? prices, List<StockAlert>? alerts, Set<String>? watchlist, int? updateCount}) {
    return StockDashboardState(
      prices: prices ?? this.prices, alerts: alerts ?? this.alerts,
      watchlist: watchlist ?? this.watchlist, updateCount: updateCount ?? this.updateCount);
  }
}

class StockTickerBloc extends Bloc<StockEvent, StockDashboardState> {
  StockTickerBloc() : super(const StockDashboardState()) {
    on<StockPriceUpdated>(_onPrice, transformer: restartable());
    on<StockAlertTriggered>(_onAlert, transformer: sequential());
    on<StockWatchlistToggled>(_onWatchlist, transformer: concurrent());
  }

  void _onPrice(StockPriceUpdated event, Emitter<StockDashboardState> emit) {
    final prev = state.prices[event.symbol]?.price ?? event.price;
    final updated = Map<String, StockPrice>.from(state.prices);
    updated[event.symbol] = StockPrice(symbol: event.symbol, price: event.price, previousPrice: prev, timestamp: event.timestamp);
    emit(state.copyWith(prices: updated, updateCount: state.updateCount + 1));
  }

  void _onAlert(StockAlertTriggered event, Emitter<StockDashboardState> emit) {
    emit(state.copyWith(alerts: [...state.alerts, StockAlert(symbol: event.symbol, message: event.message, timestamp: DateTime.now())]));
  }

  void _onWatchlist(StockWatchlistToggled event, Emitter<StockDashboardState> emit) {
    final updated = Set<String>.from(state.watchlist);
    updated.contains(event.symbol) ? updated.remove(event.symbol) : updated.add(event.symbol);
    emit(state.copyWith(watchlist: updated));
  }
}
```

### Deep Dive

The bottleneck is rarely the state library -- it is widget rebuilds. Both libraries support fine-grained control: `BlocBuilder.buildWhen` and `ref.watch(provider.select(...))`. Real optimization is selecting only what each widget needs: `ref.watch(stockProvider.select((s) => s.prices['AAPL']))`.

For the Riverpod equivalent, a `StreamProvider` can listen to the price stream, but it processes every emission. To batch, use a `NotifierProvider` that accumulates updates from `ref.listen` on the stream provider, then flushes on a periodic timer. This gives you the same batching behavior as the Bloc transformer approach.

---

## Exercise 7: E-Commerce Dual Implementation

### Progressive Hints

1. Design domain models first, completely independent of state libraries. Both implementations use the same models.
2. Optimistic (Riverpod): `final previous = state; state = optimistic; try { await api(); } catch (_) { state = previous; }`.
3. Optimistic (Bloc): emit optimistic first, await API, emit rollback on failure. Two states in sequence.
4. Persistence (Riverpod): `ref.listenSelf` triggers save on every change. In `build()`, restore from storage.
5. Persistence (Bloc): extend `HydratedBloc`, implement `toJson`/`fromJson`.
6. Testing rollback: `FailingCartRepository` throws on add. Verify: empty -> optimistic item -> rolled back to empty.

### Key Solution Pattern: Optimistic Updates

```dart
// Riverpod
Future<void> addItem(Product product) async {
  final previous = state;
  state = AsyncData(state.requireValue.withItem(product)); // optimistic
  try { await ref.read(cartRepoProvider).addItem(product.id); }
  catch (_) { state = previous; } // rollback
}

// Bloc
Future<void> _onItemAdded(CartItemAdded event, Emitter<CartState> emit) async {
  final previous = state;
  emit(state.withItem(event.product)); // optimistic
  try { await _repo.addItem(event.product.id); }
  catch (_) { emit(previous); } // rollback
}
```

### Common Mistakes

**Capturing rollback state after optimistic emit.** `final previous = state` must come before the optimistic update, not after. Otherwise you capture the optimistic state and "rollback" changes nothing.

**HydratedBloc with non-serializable state.** Every field needs JSON serialization. Freezed generates this with `@JsonSerializable`.

**Checkout Bloc accepting events from wrong states.** Guard every handler: `if (state is! CheckoutShippingStep) return;`.

### Common Mistakes

**Losing rollback state in async handlers.** `final previous = state` must come before the optimistic emit. Capturing after means you save the optimistic state and "rollback" changes nothing.

**HydratedBloc with non-serializable state.** Every field needs JSON serialization. Freezed generates this with `@JsonSerializable`. If you use a `Product` object in your cart state, it needs `toJson`/`fromJson` too.

**Checkout Bloc accepting events from wrong states.** Without `if (state is! CheckoutShippingStep) return;` guards, a user could submit shipping during the payment step, corrupting the flow. Always validate the current state before transitioning.

### Deep Dive: Comparative Analysis

After implementing both, you will notice these structural differences:

**Lines of code:** Bloc requires roughly 30-40% more code per feature. Every state change needs an event class, a state class, and an on-handler. Riverpod condenses this to a class with methods. With code generation, the gap widens further.

**Testing ergonomics:** `blocTest` reads like a specification: "given this build, when this act, expect these states." It automatically verifies the exact sequence. Riverpod's `ProviderContainer` testing is more imperative -- you call methods and assert state -- equally powerful but less structured.

**Debugging experience:** Bloc wins on traceability. `BlocObserver` gives a complete log of `event -> state` transitions across the entire app. Riverpod's `ProviderObserver` shows provider creation, updates, and disposal, but without named events the transitions are less structured.

**Refactoring cost:** Adding a coupon feature to the cart means a new event class + handler with Bloc, versus adding a method to the Notifier with Riverpod. Bloc forces you to think about the event's semantics upfront. Riverpod is faster for iteration.

**Performance:** Both are negligible for typical apps. The difference appears at high frequency (hundreds of updates/second) where Bloc's transformers provide built-in concurrency control.

---

## Exercise 8: Collaborative Real-Time Board

### Progressive Hints

1. Lamport timestamp: counter increments on every local op, updates to `max(local, remote) + 1` on remote op. Tie-break on client ID.
2. Offline queue: `List<Operation>`. Disconnected ops go to queue. On reconnect, drain in order with conflict resolution.
3. Optimistic: apply locally, tag as pending, confirm or rollback when server responds.
4. Convergence: both clients apply the same deterministic resolution. Same operations (any order) must produce same final state.

### Key Solution: Conflict Resolution

```dart
// file: lib/sync/sync_engine.dart
Operation resolveConflict(Operation local, Operation remote) {
  if (remote.timestamp > local.timestamp) return remote;
  if (local.timestamp > remote.timestamp) return local;
  // Tie: deterministic tiebreaker via client ID
  return remote.timestamp.clientId.compareTo(local.timestamp.clientId) > 0 ? remote : local;
}

void simulateReconnect() {
  _connectionState = ConnectionState.reconnecting;
  while (_pendingQueue.isNotEmpty) {
    final op = _pendingQueue.removeFirst();
    _outgoing.add(op);
  }
  _connectionState = ConnectionState.connected;
}
```

### Common Mistakes

**Non-deterministic conflict resolution.** If client A resolves "local wins" and client B resolves "remote wins" for the same pair, they diverge permanently. The algorithm must be deterministic -- Lamport timestamp + client ID provides a total order.

**Applying remote ops twice.** Server re-sends on reconnect. Without idempotency checks (`if (tasks.containsKey(taskId)) return;`), duplicates corrupt state.

**Mutating queue during iteration.** Use `removeFirst()` in a `while` loop, not `remove()` inside a `for` loop.

**Forgetting to merge Lamport clocks.** On receiving remote ops, `_clock = _clock.merge(remote.timestamp)`. Without this, local clock falls behind and loses every conflict.

### Deep Dive: Distributed State Fundamentals

**Lamport timestamps** provide causal ordering, not wall-clock time. The client ID tiebreaker makes ordering total -- necessary for deterministic resolution.

**Optimistic updates** are a UX optimization. Without them, every action shows a spinner. With them, the UI responds instantly and silently corrects on conflict. Good UI communicates pending status (subtle opacity) to set expectations.

**Convergence** is the hard problem. Our last-writer-wins approach is simple but lossy -- the losing operation is discarded. CRDTs and operational transformation preserve both operations by merging, but they are significantly more complex. For a task board, last-writer-wins is usually acceptable. For collaborative text editing, you need CRDTs.

### Key Implementation Detail: Riverpod Board Layer

The Riverpod implementation mirrors the Bloc structure but uses providers instead of events:

```dart
// file: lib/features/board_riverpod/board_providers.dart
final syncEngineProvider = Provider<SyncEngine>((ref) {
  final engine = SyncEngine(clientId: 'client-${DateTime.now().millisecondsSinceEpoch}');
  ref.onDispose(engine.dispose);
  return engine;
});

final boardProvider = AsyncNotifierProvider<BoardNotifier, BoardState>(BoardNotifier.new);

class BoardNotifier extends AsyncNotifier<BoardState> {
  @override
  Future<BoardState> build() {
    final engine = ref.watch(syncEngineProvider);
    engine.incoming.listen(_handleRemoteOperation);
    return Future.value(BoardState.initial());
  }

  void createTask(String title, String columnId) {
    final engine = ref.read(syncEngineProvider);
    final taskId = '${engine.clientId}-${DateTime.now().millisecondsSinceEpoch}';
    state = AsyncData(state.requireValue.withNewTask(taskId, title, columnId));
    engine.enqueue(CreateTask(operationId: taskId, timestamp: engine.nextTimestamp(), taskId: taskId, title: title, columnId: columnId));
  }

  void _handleRemoteOperation(Operation op) {
    final current = state.valueOrNull;
    if (current == null) return;
    state = AsyncData(current.applyOperation(op));
  }
}

final pendingOpsProvider = Provider<int>((ref) {
  return ref.watch(syncEngineProvider).pendingOperations;
});
```

The key difference: Riverpod uses methods on the notifier (imperative), while Bloc uses events processed by handlers (declarative). For the sync layer, both ultimately delegate to the same `SyncEngine`. The architectural choice affects how you structure the UI bindings but not the core sync logic.

---

## Debugging Tips for All Exercises

**Riverpod provider not updating:** Verify `ref.watch` (not `ref.read`) in `build()`. Verify your state class implements `==` correctly -- equal states skip rebuilds.

**Bloc state not emitting:** Bloc skips when `newState == currentState`. Without custom `==`, Dart uses reference equality. Freezed generates equality automatically.

**autoDispose too eager:** During route transitions, briefly no widget listens, triggering disposal. Use `ref.keepAlive()` or place provider in a longer-lived scope.

**HydratedBloc not restoring:** `HydratedBloc.storage` must be initialized before any HydratedBloc is created. Add error handling around `HydratedStorage.build()`.

## Alternatives Worth Knowing

**GetX:** All-in-one with less boilerplate but less structure. Controversial for its "magic" approach.

**signals (dart_signals):** Fine-grained reactivity from the web world (SolidJS, Angular). Newer, less battle-tested.

**MobX:** Observable-based reactive state. Mature Dart port, popular with React developers.

**Redux:** Single global store with reducers. Verbose but extremely predictable. Familiar to React developers.

Riverpod and Bloc together cover the vast majority of production Flutter use cases. Learn these two deeply before exploring alternatives.
