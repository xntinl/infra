# Section 15 -- Flutter Advanced State Management: Riverpod and Bloc

## Introduction

In Section 12 you learned how `setState`, `InheritedWidget`, and Provider handle state for straightforward apps. That works until your app grows past a handful of screens -- coordinating auth tokens with API calls with cached data with WebSocket events. Provider's limitations compound: circular dependencies, runtime-checked `ProxyProvider` chains, testing that requires full widget trees, and disposal guesswork.

Provider is coupled to the widget tree. Every provider must live inside a widget ancestor, which means you cannot access state from a utility class, a background isolate, or a test without pumping widgets. Dependency management is fragile -- reorder your `MultiProvider` list incorrectly and you get a runtime exception instead of a compile error. Testing requires building widget trees just to inject a mock, which is unnecessary ceremony for business logic.

This section introduces **Riverpod** and **Bloc**, two battle-tested solutions that eliminate these problems from fundamentally different angles. Riverpod treats state as a reactive provider graph outside the widget tree -- compile-safe dependency injection, automatic disposal, fine-grained rebuilds. Bloc enforces unidirectional data flow where every state change traces to a named event -- auditable, testable by construction. Neither is universally better. By the end you will choose the right one for a given feature, or combine them in the same codebase when that makes sense.

## Prerequisites

- **Section 09** -- Widget fundamentals, StatelessWidget vs StatefulWidget
- **Section 12** -- setState, InheritedWidget, Provider basics, ChangeNotifier
- **Section 05** -- Futures, Streams, async/await
- **Section 04** -- Sealed classes, abstract classes, mixins
- **Section 08** -- Dart generics and the type system

## Learning Objectives

1. **Differentiate** between Riverpod's provider types and select the appropriate one
2. **Construct** reactive data pipelines using `ref.watch`, `ref.read`, and `ref.listen`
3. **Design** providers with `autoDispose`, `family`, and dependency chains
4. **Generate** type-safe providers using `riverpod_generator`
5. **Implement** Bloc with events, states, transitions, and stream transformers
6. **Model** states using sealed classes and freezed for immutable data
7. **Evaluate** trade-offs between Riverpod and Bloc
8. **Test** state management in isolation with unit tests, mocks, and provider overrides
9. **Persist** state across app restarts using hydrated_bloc
10. **Architect** side effect handling for navigation, snackbars, and analytics

---

## Core Concepts

### 1. Riverpod Provider Types

Providers are global declarations, lazily instantiated, automatically scoped. They live outside the widget tree. `ProviderScope` at the root is just a state container.

```dart
// file: lib/providers/app_providers.dart
final appNameProvider = Provider<String>((ref) => 'My App');

final counterProvider = StateProvider<int>((ref) => 0);

final userProfileProvider = FutureProvider<UserProfile>((ref) async {
  final api = ref.watch(apiClientProvider);
  return api.fetchProfile();
});

final messagesProvider = StreamProvider<List<Message>>((ref) {
  return ref.watch(chatServiceProvider).messageStream;
});

final todoListProvider =
    NotifierProvider<TodoListNotifier, List<Todo>>(TodoListNotifier.new);

class TodoListNotifier extends Notifier<List<Todo>> {
  @override
  List<Todo> build() => [];

  void addTodo(Todo todo) => state = [...state, todo];
  void toggleComplete(String id) {
    state = [for (final t in state)
      if (t.id == id) t.copyWith(isComplete: !t.isComplete) else t];
  }
}

final catalogProvider =
    AsyncNotifierProvider<CatalogNotifier, List<Product>>(CatalogNotifier.new);

class CatalogNotifier extends AsyncNotifier<List<Product>> {
  @override
  Future<List<Product>> build() => ref.watch(productRepoProvider).fetchAll();

  Future<void> refresh() async {
    state = const AsyncLoading();
    state = await AsyncValue.guard(() => ref.read(productRepoProvider).fetchAll());
  }
}
```

The choice of provider type communicates intent. `Provider` says "this value is derived and read-only." `StateProvider` says "this is simple mutable state like a toggle or selected index." `NotifierProvider` says "this state has named transitions encapsulated in a class." `FutureProvider` says "this is an async value that might be loading or failed." `AsyncNotifierProvider` combines async loading with named mutation methods. The type system carries this information to every consumer.

### 2. ref.watch vs ref.read vs ref.listen

These three methods are how widgets and providers interact. Using the wrong one is the single most common Riverpod mistake, so understanding the distinction is critical.

```dart
// file: lib/screens/product_screen.dart
class ProductScreen extends ConsumerWidget {
  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final products = ref.watch(catalogProvider); // REACTIVE -- rebuilds widget
    ref.listen(authProvider, (prev, next) {      // SIDE EFFECTS -- no rebuild
      if (next == AuthState.unauthenticated)
        Navigator.of(context).pushReplacementNamed('/login');
    });
    return products.when(
      data: (list) => ListView.builder(itemCount: list.length, itemBuilder: (_, i) => ProductTile(product: list[i])),
      loading: () => const CircularProgressIndicator(),
      error: (e, _) => Text('Error: $e'),
    );
  }
}

class AddButton extends ConsumerWidget {
  final Product product;
  const AddButton({required this.product});
  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return ElevatedButton(
      onPressed: () => ref.read(cartProvider.notifier).addItem(product), // NON-REACTIVE -- one-shot read
      child: const Text('Add'),
    );
  }
}
```

Rule: `ref.watch` in `build()`, `ref.read` in callbacks. Violating this creates leaked subscriptions or stale data.

### 3. autoDispose, family, and keepAlive

```dart
// file: lib/providers/advanced_providers.dart
final searchProvider = FutureProvider.autoDispose<List<SearchResult>>((ref) async {
  final query = ref.watch(searchQueryProvider);
  if (query.isEmpty) return [];
  return ref.watch(searchApiProvider).search(query);
});

final cachedProfileProvider = FutureProvider.autoDispose<UserProfile>((ref) async {
  final link = ref.keepAlive();
  final timer = Timer(const Duration(minutes: 5), link.close);
  ref.onDispose(timer.cancel);
  return ref.watch(apiClientProvider).fetchProfile();
});

final productByIdProvider =
    FutureProvider.autoDispose.family<Product, String>((ref, id) async {
  return ref.watch(productRepoProvider).fetchById(id);
});

final filteredTodosProvider = Provider<List<Todo>>((ref) {
  final todos = ref.watch(todoListProvider);
  final filter = ref.watch(todoFilterProvider);
  return switch (filter) {
    TodoFilter.all => todos,
    TodoFilter.active => todos.where((t) => !t.isComplete).toList(),
    TodoFilter.completed => todos.where((t) => t.isComplete).toList(),
  };
});
```

`autoDispose` prevents memory leaks for screen-scoped data. `family` parameterizes one declaration for every entity ID. `keepAlive` with a timer creates time-based caching.

### 4. Riverpod Code Generation

```dart
// file: lib/providers/generated_providers.dart
part 'generated_providers.g.dart';

@riverpod
String appVersion(Ref ref) => '2.1.0'; // Generates Provider<String>

@riverpod
Future<Product> productById(Ref ref, String id) async { // Generates FutureProvider.autoDispose.family
  return ref.watch(productRepoProvider).fetchById(id);
}

@riverpod
class CartNotifier extends _$CartNotifier { // Generates NotifierProvider
  @override
  Cart build() => const Cart.empty();
  void addItem(Product p, {int qty = 1}) => state = state.withItem(p, quantity: qty);
}
```

Run `dart run build_runner build`. The generator infers the correct provider type, applies `autoDispose` by default, and handles `family` boilerplate.

### 5. Bloc: Cubit (Simplified)

Bloc is built on the idea that every state change should be traceable. A Cubit is the simplified version: you call methods directly, and each method emits a new state. It is appropriate when method names alone document what happened.

```dart
// file: lib/cubits/todo_cubit.dart
sealed class TodoState { const TodoState(); }
class TodoInitial extends TodoState { const TodoInitial(); }
class TodoLoaded extends TodoState { final List<Todo> todos; const TodoLoaded(this.todos); }
class TodoError extends TodoState { final String message; const TodoError(this.message); }

class TodoCubit extends Cubit<TodoState> {
  final TodoRepository _repo;
  TodoCubit(this._repo) : super(const TodoInitial());

  Future<void> loadTodos() async {
    try { emit(TodoLoaded(await _repo.fetchAll())); }
    catch (e) { emit(TodoError(e.toString())); }
  }

  void addTodo(String title) {
    if (state is! TodoLoaded) return;
    final current = (state as TodoLoaded).todos;
    emit(TodoLoaded([...current, Todo(id: uuid(), title: title)]));
  }
}
```

### 6. Bloc: Event-Driven

Full Bloc adds an event layer -- every action becomes a named event object, providing an audit trail.

```dart
// file: lib/blocs/auth_bloc.dart
sealed class AuthEvent { const AuthEvent(); }
class AuthLoginRequested extends AuthEvent { final String email, password; const AuthLoginRequested({required this.email, required this.password}); }
class AuthLogoutRequested extends AuthEvent { const AuthLogoutRequested(); }

sealed class AuthState { const AuthState(); }
class AuthUnauthenticated extends AuthState { const AuthUnauthenticated(); }
class AuthLoading extends AuthState { const AuthLoading(); }
class AuthAuthenticated extends AuthState { final User user; final String token; const AuthAuthenticated({required this.user, required this.token}); }
class AuthFailure extends AuthState { final String message; const AuthFailure(this.message); }

class AuthBloc extends Bloc<AuthEvent, AuthState> {
  final AuthRepository _authRepo;
  final TokenStorage _tokenStorage;

  AuthBloc({required AuthRepository authRepo, required TokenStorage tokenStorage})
      : _authRepo = authRepo, _tokenStorage = tokenStorage, super(const AuthUnauthenticated()) {
    on<AuthLoginRequested>(_onLogin);
    on<AuthLogoutRequested>(_onLogout);
  }

  Future<void> _onLogin(AuthLoginRequested event, Emitter<AuthState> emit) async {
    emit(const AuthLoading());
    try {
      final result = await _authRepo.login(event.email, event.password);
      await _tokenStorage.saveToken(result.token);
      emit(AuthAuthenticated(user: result.user, token: result.token));
    } catch (e) { emit(AuthFailure(e.toString())); }
  }

  Future<void> _onLogout(AuthLogoutRequested event, Emitter<AuthState> emit) async {
    await _tokenStorage.clearToken();
    emit(const AuthUnauthenticated());
  }
}
```

### 7. Bloc Widgets and Stream Transformers

Bloc provides dedicated widgets for providing, rebuilding, and reacting to state. The separation between `BlocBuilder` (rebuilds UI) and `BlocListener` (triggers side effects) is intentional -- mixing them leads to navigation during build, snackbars firing multiple times, and timing bugs. `BlocConsumer` combines both when you genuinely need a widget that rebuilds and triggers side effects.

```dart
// file: lib/screens/auth_screen.dart
// BlocBuilder -- rebuilds on state change
BlocBuilder<AuthBloc, AuthState>(
  buildWhen: (prev, curr) => prev.runtimeType != curr.runtimeType,
  builder: (context, state) => switch (state) {
    AuthUnauthenticated() => const LoginForm(),
    AuthLoading() => const CircularProgressIndicator(),
    AuthAuthenticated(:final user) => Text('Welcome, ${user.name}'),
    AuthFailure(:final message) => Text('Error: $message'),
  },
)

// BlocListener -- side effects only, no rebuild
BlocListener<AuthBloc, AuthState>(
  listenWhen: (prev, curr) => curr is AuthFailure,
  listener: (context, state) {
    if (state is AuthFailure) ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(state.message)));
  },
)

// BlocConsumer -- combines both when you need UI rebuild + side effects
```

When events arrive faster than they can be processed, stream transformers control the queuing strategy. The `bloc_concurrency` package provides four transformers that replace manual debounce, throttle, and cancellation logic.

```dart
// file: lib/blocs/search_bloc.dart
import 'package:bloc_concurrency/bloc_concurrency.dart';

class SearchBloc extends Bloc<SearchEvent, SearchState> {
  SearchBloc(this._repo) : super(const SearchInitial()) {
    on<SearchQueryChanged>(_onQuery, transformer: restartable());   // cancel previous on new keystroke
    on<SearchFilterApplied>(_onFilter, transformer: sequential());  // one at a time, in order
    on<SearchSubmitted>(_onSubmit, transformer: droppable());       // ignore while processing
    on<SearchBookmarkToggled>(_onBookmark, transformer: concurrent()); // all in parallel
  }

  Future<void> _onQuery(SearchQueryChanged event, Emitter<SearchState> emit) async {
    if (event.query.isEmpty) { emit(const SearchInitial()); return; }
    emit(const SearchLoading());
    await Future.delayed(const Duration(milliseconds: 300)); // debounce via restartable
    try { emit(SearchLoaded(await _repo.search(event.query))); }
    catch (e) { emit(SearchError(e.toString())); }
  }
}
```

The `restartable` transformer on search is critical: without it, a slow network response from an old query could overwrite results from a newer query. The `droppable` transformer on submit prevents double-tap submissions.

### 8. State Modeling with Sealed Classes and Freezed

```dart
// file: lib/models/cart_state.dart
@freezed
class CartItem with _$CartItem {
  const factory CartItem({required String productId, required String name, required int quantity, required double unitPrice}) = _CartItem;
  factory CartItem.fromJson(Map<String, dynamic> json) => _$CartItemFromJson(json);
}

@freezed
sealed class CartState with _$CartState {
  const factory CartState.empty() = CartEmpty;
  const factory CartState.active({required List<CartItem> items, required double subtotal}) = CartActive;
  const factory CartState.error({required String message, required List<CartItem> previousItems}) = CartError;
}
```

Freezed generates `copyWith`, `==`, `hashCode`, `toString`, and JSON serialization. Combined with sealed classes, every state variant carries exactly the data it needs.

### 9. State Persistence with hydrated_bloc

Some state should survive app restarts -- authentication status, user preferences, draft content, shopping carts. `hydrated_bloc` adds automatic serialization and deserialization to any Bloc or Cubit. You extend `HydratedCubit` or `HydratedBloc` instead of the base class, implement two methods, and initialize storage once at startup. The library writes state to disk on every change and restores it on next launch.

```dart
// file: lib/cubits/theme_cubit.dart
class ThemeCubit extends HydratedCubit<ThemeMode> {
  ThemeCubit() : super(ThemeMode.system);
  void setTheme(ThemeMode mode) => emit(mode);

  @override
  ThemeMode fromJson(Map<String, dynamic> json) => ThemeMode.values[json['index'] as int];
  @override
  Map<String, dynamic> toJson(ThemeMode state) => {'index': state.index};
}

// In main.dart:
// WidgetsFlutterBinding.ensureInitialized();
// HydratedBloc.storage = await HydratedStorage.build(storageDirectory: await getApplicationDocumentsDirectory());
// runApp(const App());
```

### 10. Testing Both Approaches

```dart
// file: test/auth_bloc_test.dart
blocTest<AuthBloc, AuthState>(
  'emits [Loading, Authenticated] on successful login',
  build: () { when(() => mockRepo.login(any(), any())).thenAnswer((_) async => authResult); return AuthBloc(authRepo: mockRepo, tokenStorage: mockStorage); },
  act: (bloc) => bloc.add(const AuthLoginRequested(email: 'a@b.com', password: 'pass')),
  expect: () => [const AuthLoading(), AuthAuthenticated(user: testUser, token: 'abc')],
);

// file: test/todo_notifier_test.dart
test('addTodo appends to the list', () {
  final container = ProviderContainer(overrides: [todoRepoProvider.overrideWithValue(MockRepo())]);
  container.read(todoListProvider.notifier).addTodo(Todo(id: '1', title: 'Test'));
  expect(container.read(todoListProvider), hasLength(1));
  container.dispose();
});
```

Bloc tests use `blocTest` which provides a declarative `build/act/expect/verify` structure. Riverpod tests use `ProviderContainer` with overrides -- no widget tree, no `BuildContext`, just pure Dart. Both approaches let you test business logic without touching Flutter widgets.

### 11. Riverpod vs Bloc: When to Use Which

This is not a religious choice. Each tool has structural advantages for specific scenarios.

**Choose Riverpod** when your concern is dependency injection and reactive data flow -- fetching, caching, invalidating, loading/error states. Its provider graph models "this depends on that" naturally.

**Choose Bloc** when your concern is complex business logic with many transitions. Named events make every state change auditable. Stream transformers handle concurrency. If your feature has rules like "X only from state Y, rollback to W on failure," Bloc makes those rules visible.

**Combine them** when different parts of your app have different needs. Riverpod for the data layer, Bloc for complex feature logic. They do not conflict.

---

## Exercises

### Exercise 1 (Basic): Riverpod Counter and Provider Types

**Objective:** Set up a Riverpod app with `Provider`, `StateProvider`, derived `Provider`, and `FutureProvider`.

**Instructions:** 1) Create a Flutter project with `flutter_riverpod`. 2) Wrap `MaterialApp` in `ProviderScope`. 3) Create: a `Provider<String>` for app title, a `StateProvider<int>` for a counter, a `Provider<bool>` deriving even/odd, a `FutureProvider<String>` simulating a 2-second async quote fetch. 4) Build a `ConsumerWidget` displaying all four. 5) Add increment/decrement buttons using `ref.read`. 6) Handle `AsyncValue` with `.when()`.

**Verification:**

```dart
// file: test/counter_providers_test.dart
void main() {
  test('counter starts at 0', () {
    final container = ProviderContainer();
    expect(container.read(counterProvider), 0);
    container.dispose();
  });
  test('isEven derives from counter', () {
    final container = ProviderContainer();
    expect(container.read(isEvenProvider), true);
    container.read(counterProvider.notifier).state = 3;
    expect(container.read(isEvenProvider), false);
    container.dispose();
  });
}
```

Run the app with `flutter run`. Verify counter increments, even/odd updates reactively, and quote shows a loading indicator before appearing.

**Transition:** Exercise 2 builds the same concepts with Bloc for direct comparison.

---

### Exercise 2 (Basic): Cubit for Todo Management

**Objective:** Implement a todo list with `Cubit`, `BlocBuilder`, and `BlocListener`.

**Instructions:** 1) Add `flutter_bloc`. 2) Create `Todo` model with `copyWith`. 3) Define sealed `TodoState`: `TodoInitial`, `TodoLoaded(List<Todo>)`, `TodoError(String)`. 4) Create `TodoCubit` with `loadTodos()`, `addTodo()`, `toggleTodo()`, `deleteTodo()`. 5) Use `BlocProvider` + `BlocBuilder` with exhaustive `switch`. 6) Add `BlocListener` showing a SnackBar on deletion.

**Verification:**

```dart
// file: test/todo_cubit_test.dart
void main() {
  late TodoCubit cubit;
  setUp(() => cubit = TodoCubit());
  tearDown(() => cubit.close());

  blocTest<TodoCubit, TodoState>(
    'addTodo adds to the list',
    build: () => cubit,
    seed: () => const TodoLoaded([]),
    act: (c) => c.addTodo('Write tests'),
    expect: () => [isA<TodoLoaded>().having((s) => s.todos.length, 'count', 1)],
  );
}
```

Run the app and verify you can add, toggle, and delete todos with SnackBar confirmation on deletion.

**Transition:** Exercise 3 combines multiple Riverpod providers in a realistic data-fetching scenario.

---

### Exercise 3 (Intermediate): Multi-Provider Riverpod App

**Objective:** Build product browsing with `FutureProvider`, `NotifierProvider`, and provider dependencies.

**Instructions:** 1) Create `ProductRepository` simulating API calls with delays and occasional failures. 2) Create providers: `productRepositoryProvider`, `productListProvider` (FutureProvider.autoDispose), `productFilterProvider` (StateProvider), `filteredProductsProvider` (derived), `cartProvider` (NotifierProvider), `cartTotalProvider` (derived). 3) Build product list with loading/error/data states. 4) Add filter dropdown that reactively updates the list. 5) Add cart badge updating via `cartItemCountProvider`. 6) Implement pull-to-refresh with `ref.invalidate`. 7) Test with provider overrides.

**Verification:**

```dart
// file: test/product_providers_test.dart
void main() {
  test('cart total reflects contents', () {
    final container = ProviderContainer();
    final notifier = container.read(cartProvider.notifier);
    notifier.addItem(Product(id: '1', name: 'Widget', price: 9.99, category: ProductCategory.electronics));
    notifier.addItem(Product(id: '2', name: 'Gadget', price: 19.99, category: ProductCategory.electronics));
    expect(container.read(cartTotalProvider), closeTo(29.98, 0.01));
    container.dispose();
  });
}
```

Run the app. Verify filter changes instantly update the list, adding items updates the cart badge, and pull-to-refresh shows loading before reloading.

**Transition:** Exercise 4 implements equivalent complexity with Bloc's event-driven model.

---

### Exercise 4 (Intermediate): Bloc with Stream Transformers

**Objective:** Implement search with full Bloc, events, `restartable`/`sequential`/`droppable` transformers, and tests.

**Instructions:** 1) Define events: `SearchQueryChanged`, `SearchFilterApplied`, `SearchResultSelected`, `SearchCleared`. 2) Define sealed states: `SearchInitial`, `SearchLoading`, `SearchLoaded`, `SearchEmpty`, `SearchError`. 3) Use `restartable` on query (with 300ms debounce in handler), `sequential` on filter, `droppable` on result selection. 4) Add `BlocObserver` logging transitions. 5) Build UI with `BlocConsumer`. 6) Test debounce: fire 6 rapid query events, verify only one search executes.

**Verification:**

```dart
// file: test/search_bloc_test.dart
blocTest<SearchBloc, SearchState>(
  'debounces rapid queries, processes only the last one',
  build: () => SearchBloc(repository: FakeSearchRepository()),
  act: (bloc) async {
    bloc.add(const SearchQueryChanged('f'));
    bloc.add(const SearchQueryChanged('fl'));
    bloc.add(const SearchQueryChanged('flutter'));
    await Future.delayed(const Duration(milliseconds: 500));
  },
  expect: () => [const SearchLoading(), isA<SearchLoaded>()],
);
```

Run the app and type rapidly. Verify only the final query triggers a search, loading appears once, and results display correctly.

**Transition:** Exercises 5-6 push both approaches to production-grade complexity.

---

### Exercise 5 (Advanced): Riverpod Code Gen + Cache Strategy

**Objective:** Design a scalable data layer with `riverpod_generator`, `autoDispose`, `family`, and `keepAlive`.

**Instructions:** 1) Set up `riverpod_annotation`, `riverpod_generator`, `build_runner`. 2) Create with `@riverpod`: session NotifierProvider (keepAlive: true), product-by-ID FutureProvider.family with 5-minute `keepAlive` timer, paginated AsyncNotifierProvider with `fetchNextPage()`/`refresh()`, notifications StreamProvider. 3) Cache strategy: product detail = keepAlive 5min, search = plain autoDispose, session = app lifetime. 4) Build product detail screen showing instant cache hits vs loading. 5) Test disposal lifecycle with ProviderContainer.

**Verification:**

```dart
// file: test/generated_providers_test.dart
test('productById caches after first fetch', () async {
  final container = ProviderContainer(overrides: [productRepoProvider.overrideWithValue(FakeRepo())]);
  final product = await container.read(productByIdProvider('abc').future);
  expect(product.id, 'abc');
  final cached = container.read(productByIdProvider('abc'));
  expect(cached, isA<AsyncData<Product>>()); // No loading state -- cached
  container.dispose();
});
```

Run `dart run build_runner build` to generate code. Run the app and navigate between product detail pages. Cached pages appear instantly without a loading indicator.

**Transition:** Exercise 6 applies the same production concerns to Bloc.

---

### Exercise 6 (Advanced): Bloc Performance and Batching

**Objective:** Build a stock ticker dashboard with Bloc batching, then compare with equivalent Riverpod implementation.

**Instructions:** 1) Simulate 10 price updates/second for 20 stocks. 2) `StockTickerBloc`: `StockPriceUpdated` with custom batching (100ms window), `StockAlertTriggered` sequential, `StockWatchlistToggled` concurrent. 3) State: `Map<String, StockPrice>`, alerts list, watchlist set (using freezed). 4) Add `BlocObserver` measuring events/second and emission latency. 5) Build equivalent with Riverpod `StreamProvider` + `NotifierProvider`. 6) Benchmark both: rebuild count, memory, responsiveness. Document findings in test comments.

**Verification:**

```dart
// file: test/stock_performance_test.dart
test('Bloc batches rapid updates', () async {
  final bloc = StockTickerBloc();
  final states = <StockDashboardState>[];
  final sub = bloc.stream.listen(states.add);
  for (var i = 0; i < 100; i++) {
    bloc.add(StockPriceUpdated(symbol: 'AAPL', price: 150.0 + i * 0.1, timestamp: DateTime.now()));
  }
  await Future.delayed(const Duration(seconds: 1));
  await sub.cancel();
  await bloc.close();
  expect(states.length, lessThan(20));
});
```

Run the dashboard. Verify stock prices update smoothly without jank, alerts appear in order, and watchlist toggles instantly.

**Transition:** Exercises 7-8 combine everything into production-scale architectures.

---

### Exercise 7 (Insane): E-Commerce Dual Implementation

**Objective:** Implement cart + checkout with both Riverpod and Bloc, including optimistic updates, error recovery, persistence, analytics, and test suites. Write a trade-off analysis.

**Instructions:** 1) Shared domain: `Cart`, `CartItem`, `Order`, `PaymentMethod`, `ShippingAddress` (freezed). `CartRepository`, `OrderRepository`, `PaymentService` abstracts with fake implementations and configurable failure rates. 2) Riverpod (`lib/features/cart_riverpod/`): `AsyncNotifierProvider` cart with optimistic add/remove (save previous state, emit optimistic, rollback on failure). `NotifierProvider` checkout flow (shipping -> payment -> confirm). `ref.listen` analytics. `ref.listenSelf` persistence to local storage. 3) Bloc (`lib/features/cart_bloc/`): `HydratedBloc` cart with optimistic events. `CheckoutBloc` with sequential transformer. `BlocObserver` analytics. 4) Toggle UI switching implementations at runtime. 5) 15+ tests per implementation: every transition, optimistic rollback, persistence round-trip, end-to-end checkout. 6) Write comparative analysis (in a Dart file as comments): LOC, testing ergonomics, debugging, refactoring cost, performance.

**Verification:** Both implementations produce identical behavior. Optimistic rollback test: failing repository causes add -> rollback to empty. Persistence test: serialize, deserialize, verify equality. All tests pass for both.

**Transition:** Exercise 8 tackles the hardest problem -- distributed real-time state.

---

### Exercise 8 (Insane): Collaborative Real-Time Board with WebSocket Sync

**Objective:** Build a collaborative task board with WebSocket sync, optimistic updates, Lamport timestamp conflict resolution, and offline queue. Implement with both Riverpod and Bloc side by side.

**Instructions:** 1) Domain: `Board` with `Column`s and `Task`s. Sealed `Operation` hierarchy (CreateTask, MoveTask, EditTask, DeleteTask) with Lamport timestamps. 2) Sync engine: WebSocket connection (simulated with StreamController), outgoing queue with retry, incoming stream applying remote ops, conflict resolution (higher Lamport timestamp wins, client ID tiebreaker), offline mode (detect disconnect, queue locally, replay on reconnect). 3) Riverpod layer: `AsyncNotifierProvider` board, `Provider` sync engine, `StreamProvider` for incoming ops, `pendingOperationsProvider` for queue length. 4) Bloc layer: `BoardBloc` with events for all operations plus `RemoteOperationReceived` (sequential transformer) and `ConnectionStateChanged`. `SyncStatusCubit` for connection state. 5) Split-screen demo UI with control panel simulating disconnect, conflicting remote ops, and message reordering. 6) Tests: conflict resolution for every combination (create-create, move-move, edit-edit, delete-while-editing), offline queue (disconnect, 5 ops, reconnect, verify all sent), optimistic rollback with conflicting remote op, convergence (2 simulated clients with interleaved ops both reach same final state).

**Verification:** Higher Lamport timestamp wins conflict. Offline queue drains on reconnect. Two clients converge to identical state after interleaved operations. Split-screen demo shows both sides synchronized.

---

## Summary

- **Riverpod** provides compile-safe provider graphs with `autoDispose`, `family`, and `keepAlive` for fine-grained lifecycle. Natural for data-fetching, caching, and DI.
- **Bloc** enforces unidirectional flow through named events and explicit transitions. Stream transformers control concurrency. Natural for complex business logic and audit trails.
- **State modeling** with sealed classes and freezed eliminates illegal states at compile time.
- **Testing** is straightforward for both: `ProviderContainer` with overrides (Riverpod), `blocTest` with declarative expectations (Bloc).
- **Persistence** via `hydrated_bloc` or manual Riverpod patterns survives app restarts.
- **Side effects** stay separate from rebuilds via `ref.listen` and `BlocListener`.

## What is Next

**Section 16 -- Flutter Animations** covers implicit animations, explicit `AnimationController`, hero transitions, staggered animations, and `CustomPainter`. You will connect animations to state changes -- animating a cart badge when the cart updates, or transitioning between sealed state variants.

## References

- [Riverpod Documentation](https://riverpod.dev)
- [Riverpod 2.x Migration Guide](https://riverpod.dev/docs/migration/from_state_notifier)
- [riverpod_generator](https://pub.dev/packages/riverpod_generator)
- [Bloc Library](https://bloclibrary.dev)
- [bloc_concurrency](https://pub.dev/packages/bloc_concurrency)
- [hydrated_bloc](https://pub.dev/packages/hydrated_bloc)
- [freezed](https://pub.dev/packages/freezed)
- [bloc_test](https://pub.dev/packages/bloc_test)
- [Flutter State Management Options](https://docs.flutter.dev/data-and-backend/state-mgmt/options)
