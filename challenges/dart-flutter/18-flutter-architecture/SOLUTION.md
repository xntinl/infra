# Section 18 -- Solutions: Flutter Architecture

## How to Use This Guide

Work through each exercise on your own first. Architecture decisions are learned by making choices and seeing consequences, not by reading answers. This guide provides progressive hints, common mistakes, full solutions, and deep dives for each exercise.

---

## Exercise 1 -- Layer Separation

### Hints

**Hint 1**: Start with the domain. `Note` entity: plain Dart class, no Flutter imports, just fields and `isValid`.

**Hint 2**: `NoteRepository` goes in the domain. Returns `Future<Result<...>>`. Defines what, not how.

**Hint 3**: `NoteModel` is data layer. Mirrors the entity but adds `fromJson`/`toJson`/`toDomain()`. Model knows entity; entity never knows model.

**Hint 4**: `NoteRemoteDataSource` throws data exceptions. `NoteRepositoryImpl` catches them and returns `Result` types -- the boundary where exceptions become domain failures.

### Common Mistakes

**Importing Flutter in domain.** Use `package:meta/meta.dart` for `@immutable`, not `package:flutter/foundation.dart`.

**Returning raw exceptions from repository.** If `ServerException` escapes `NoteRepositoryImpl`, the layer boundary is broken.

**Putting `fromJson` in the entity.** Serialization is a data concern, not domain.

**Widget importing data source directly.** Presentation must depend on domain abstractions, never data implementations.

### Solution

```dart
// lib/domain/entities/note.dart
class Note {
  final String id, title, content;
  final DateTime createdAt;
  const Note({required this.id, required this.title, required this.content, required this.createdAt});
  bool get isValid => title.trim().isNotEmpty;
}

// lib/domain/repositories/note_repository.dart
abstract class NoteRepository {
  Future<Result<List<Note>>> getNotes();
  Future<Result<Note>> addNote({required String title, required String content});
}
```

```dart
// lib/data/models/note_model.dart
class NoteModel {
  final String id, title, content, createdAt;
  const NoteModel({required this.id, required this.title, required this.content, required this.createdAt});

  factory NoteModel.fromJson(Map<String, dynamic> json) => NoteModel(
    id: json['id'] as String, title: json['title'] as String,
    content: json['content'] as String, createdAt: json['created_at'] as String);

  Map<String, dynamic> toJson() => {'id': id, 'title': title, 'content': content, 'created_at': createdAt};
  Note toDomain() => Note(id: id, title: title, content: content, createdAt: DateTime.parse(createdAt));
}
```

```dart
// lib/data/datasources/note_remote_datasource.dart
class NoteRemoteDataSourceImpl implements NoteRemoteDataSource {
  final http.Client _client;
  final String _baseUrl;
  NoteRemoteDataSourceImpl({required http.Client client, required String baseUrl})
      : _client = client, _baseUrl = baseUrl;

  @override
  Future<List<NoteModel>> getNotes() async {
    final response = await _client.get(Uri.parse('$_baseUrl/notes'));
    if (response.statusCode != 200) {
      throw ServerException(statusCode: response.statusCode, message: 'Failed to fetch notes');
    }
    final List<dynamic> data = json.decode(response.body);
    return data.map((j) => NoteModel.fromJson(j as Map<String, dynamic>)).toList();
  }
}
```

```dart
// lib/data/repositories/note_repository_impl.dart
class NoteRepositoryImpl implements NoteRepository {
  final NoteRemoteDataSource _remoteDataSource;
  NoteRepositoryImpl({required NoteRemoteDataSource remoteDataSource}) : _remoteDataSource = remoteDataSource;

  @override
  Future<Result<List<Note>>> getNotes() async {
    try {
      final models = await _remoteDataSource.getNotes();
      return Success(models.map((m) => m.toDomain()).toList());
    } on ServerException catch (e) {
      return Failure(ServerFailure(e.message));
    } catch (e) {
      return Failure(ServerFailure('Unexpected error: $e'));
    }
  }
}
```

```dart
// lib/presentation/viewmodels/notes_viewmodel.dart
class NotesViewModel extends ChangeNotifier {
  final NoteRepository _repository;
  NotesViewModel({required NoteRepository repository}) : _repository = repository;

  ViewState _state = ViewState.idle;
  List<Note> _notes = [];
  String _errorMessage = '';
  ViewState get state => _state;
  List<Note> get notes => List.unmodifiable(_notes);
  String get errorMessage => _errorMessage;

  Future<void> loadNotes() async {
    _state = ViewState.loading; notifyListeners();
    final result = await _repository.getNotes();
    switch (result) {
      case Success(:final data): _notes = data; _state = ViewState.loaded;
      case Failure(:final failure): _errorMessage = failure.message; _state = ViewState.error;
    }
    notifyListeners();
  }
}
```

### Deep Dive

The key transformation is not the code volume -- it is the direction of dependencies. The widget only knows a view model. The view model only knows a repository interface. The repository implementation only knows a data source. If you switch from REST to GraphQL, you change one file. If you switch from Provider to Riverpod, the domain layer does not change at all.

---

## Exercise 2 -- Repository with Caching

### Hints

**Hint 1**: `WeatherLocalDataSource` uses `Map<String, WeatherModel>`. Include `fetchedAt` for staleness.

**Hint 2**: Four paths: remote success (cache and return), remote fail + fresh cache, remote fail + stale cache (return with warning), remote fail + no cache (return failure).

### Common Mistakes

**Caching the entity instead of the model.** Local storage needs the serializable form.

**Returning stale data silently.** Log a warning or signal staleness to the caller.

### Solution

```dart
// lib/data/datasources/weather_local_datasource.dart
class WeatherLocalDataSourceImpl implements WeatherLocalDataSource {
  final Map<String, WeatherModel> _cache = {};

  @override
  Future<void> cacheForecast(WeatherModel model) async => _cache[model.city.toLowerCase()] = model;

  @override
  Future<WeatherModel> getCachedForecast(String city) async {
    final cached = _cache[city.toLowerCase()];
    if (cached == null) throw CacheException(message: 'No cached forecast for $city');
    return cached;
  }
}
```

```dart
// lib/data/repositories/weather_repository_impl.dart
class WeatherRepositoryImpl implements WeatherRepository {
  final WeatherRemoteDataSource _remoteDataSource;
  final WeatherLocalDataSource _localDataSource;
  WeatherRepositoryImpl({required WeatherRemoteDataSource remoteDataSource,
      required WeatherLocalDataSource localDataSource})
      : _remoteDataSource = remoteDataSource, _localDataSource = localDataSource;

  @override
  Future<Result<WeatherForecast>> getForecast(String city) async {
    try {
      final model = await _remoteDataSource.getForecast(city);
      await _localDataSource.cacheForecast(model);
      return Success(model.toDomain());
    } on ServerException catch (e) {
      return _fallbackToCache(city, e.message);
    }
  }

  Future<Result<WeatherForecast>> _fallbackToCache(String city, String originalError) async {
    try {
      final cached = await _localDataSource.getCachedForecast(city);
      final forecast = cached.toDomain();
      if (forecast.isStale) print('WARNING: Returning stale forecast for $city');
      return Success(forecast);
    } on CacheException {
      return Failure(ServerFailure('Unable to fetch forecast: $originalError'));
    }
  }
}
```

```dart
// test/data/repositories/weather_repository_impl_test.dart
void main() {
  late WeatherRepositoryImpl repository;
  late MockWeatherRemoteDataSource mockRemote;
  late MockWeatherLocalDataSource mockLocal;

  setUp(() {
    mockRemote = MockWeatherRemoteDataSource();
    mockLocal = MockWeatherLocalDataSource();
    repository = WeatherRepositoryImpl(remoteDataSource: mockRemote, localDataSource: mockLocal);
  });

  test('returns remote data and caches on success', () async {
    mockRemote.resultToReturn = WeatherModel(city: 'London', temperatureCelsius: 15.0,
        condition: 'Cloudy', fetchedAt: DateTime.now());
    final result = await repository.getForecast('London');
    expect(result, isA<Success<WeatherForecast>>());
  });

  test('returns cached data when remote fails', () async {
    await mockLocal.cacheForecast(WeatherModel(city: 'London', temperatureCelsius: 14.0,
        condition: 'Rain', fetchedAt: DateTime.now()));
    mockRemote.exceptionToThrow = ServerException(statusCode: 500, message: 'Down');
    final result = await repository.getForecast('London');
    expect(result, isA<Success<WeatherForecast>>());
  });

  test('returns failure when both sources fail', () async {
    mockRemote.exceptionToThrow = ServerException(statusCode: 500, message: 'Down');
    final result = await repository.getForecast('London');
    expect(result, isA<Failure<WeatherForecast>>());
  });
}
```

---

## Exercise 3 -- Full Feature with Use Cases

### Hints

**Hint 1**: `GetTasks` fetches all, then sorts in the use case. Sorting is business logic, not a data concern.

**Hint 2**: `CompleteTask` creates a new `Task` instance (immutable entities) with `status: done` and `completedAt: DateTime.now()`.

**Hint 3**: `GetTaskStats` computes from the task list. Do not store stats separately.

### Common Mistakes

**Mutable entities.** Never `task.status = done`. Use `copyWith` or create a new instance.

**Sorting in the data source.** Sort order is a business rule -- keep it in the use case for testability.

**ViewModel calling repository directly.** Go through use cases. They are the single enforcement point for business rules.

### Solution

```dart
// lib/domain/usecases/get_tasks.dart
class GetTasks implements UseCase<List<Task>, GetTasksParams> {
  final TaskRepository _repository;
  GetTasks({required TaskRepository repository}) : _repository = repository;

  @override
  Future<Result<List<Task>>> call(GetTasksParams params) async {
    final result = await _repository.getAllTasks();
    return switch (result) {
      Failure(:final failure) => Failure(failure),
      Success(:final data) => Success(_sortAndFilter(data, params)),
    };
  }

  List<Task> _sortAndFilter(List<Task> tasks, GetTasksParams params) {
    var filtered = params.statusFilter != null
        ? tasks.where((t) => t.status == params.statusFilter).toList()
        : tasks.toList();
    filtered.sort((a, b) {
      final p = b.priority.index.compareTo(a.priority.index);
      return p != 0 ? p : a.createdAt.compareTo(b.createdAt);
    });
    return filtered;
  }
}
```

```dart
// lib/domain/usecases/complete_task.dart
class CompleteTask implements UseCase<Task, String> {
  final TaskRepository _repository;
  CompleteTask({required TaskRepository repository}) : _repository = repository;

  @override
  Future<Result<Task>> call(String taskId) async {
    final result = await _repository.getTaskById(taskId);
    return switch (result) {
      Failure(:final failure) => Failure(failure),
      Success(:final data) when data.status == TaskStatus.done =>
        const Failure(ValidationFailure('Task is already completed')),
      Success(:final data) => _repository.updateTask(Task(
          id: data.id, title: data.title, description: data.description,
          priority: data.priority, status: TaskStatus.done,
          createdAt: data.createdAt, completedAt: DateTime.now())),
    };
  }
}
```

```dart
// lib/domain/usecases/get_task_stats.dart
class TaskStats {
  final int total, todo, inProgress, done, overdue;
  const TaskStats({required this.total, required this.todo, required this.inProgress,
      required this.done, required this.overdue});
  double get completionRate => total == 0 ? 0 : done / total;
}

class GetTaskStats implements UseCase<TaskStats, void> {
  final TaskRepository _repository;
  GetTaskStats({required TaskRepository repository}) : _repository = repository;

  @override
  Future<Result<TaskStats>> call(void params) async {
    final result = await _repository.getAllTasks();
    return switch (result) {
      Failure(:final failure) => Failure(failure),
      Success(:final data) => Success(TaskStats(
          total: data.length,
          todo: data.where((t) => t.status == TaskStatus.todo).length,
          inProgress: data.where((t) => t.status == TaskStatus.inProgress).length,
          done: data.where((t) => t.status == TaskStatus.done).length,
          overdue: data.where((t) => t.isOverdue).length)),
    };
  }
}
```

### Deep Dive

For small datasets, computing stats in memory is fine. For millions of records, push aggregation to the database via a `getStats()` method on the repository interface. The architecture supports both without changing the use case contract.

---

## Exercise 4 -- Injectable and Advanced DI

### Common Mistakes

**Forgetting `build_runner` after changes.** Injectable uses code generation. No build = no registration.

**Multiple implementations without disambiguation.** Use `@Environment` or `@Named` when two classes implement the same interface.

**ViewModels as singletons.** Use `@injectable` (factory) for view models. Two pages sharing one view model corrupt each other's state.

### Solution

```dart
// lib/di/injection_container.dart
import 'package:get_it/get_it.dart';
import 'package:injectable/injectable.dart';
import 'injection_container.config.dart';

final getIt = GetIt.instance;

@InjectableInit()
Future<void> configureDependencies({String environment = 'prod'}) async =>
    getIt.init(environment: environment);
```

```dart
// lib/di/register_module.dart
@module
abstract class RegisterModule {
  @prod @lazySingleton
  Dio prodDio() => Dio(BaseOptions(baseUrl: 'https://api.production.com'));
  @dev @lazySingleton
  Dio devDio() => Dio(BaseOptions(baseUrl: 'https://api.staging.com'))
    ..interceptors.add(LogInterceptor(requestBody: true, responseBody: true));
  @test @lazySingleton
  Dio testDio() => Dio(BaseOptions(baseUrl: 'http://localhost:8080'));
}

// lib/data/datasources/auth_remote_datasource.dart
@LazySingleton(as: AuthRemoteDataSource)
class AuthRemoteDataSourceImpl implements AuthRemoteDataSource {
  final Dio _dio;
  AuthRemoteDataSourceImpl(this._dio);
  @override
  Future<UserModel> login(String email, String password) async {
    final response = await _dio.post('/auth/login', data: {'email': email, 'password': password});
    return UserModel.fromJson(response.data);
  }
}

// lib/presentation/viewmodels/login_viewmodel.dart
@injectable  // Factory, not singleton!
class LoginViewModel extends ChangeNotifier {
  final LoginUseCase _loginUseCase;
  LoginViewModel(this._loginUseCase);
  // ...state management as shown in Exercise 1 pattern
}
```

### Deep Dive

The key benefit of `injectable` over manual get_it: compile-time safety. Missing constructor dependencies produce build errors, not runtime crashes. The environment system swaps entire dependency subtrees at a single call site.

---

## Exercise 5 -- Multi-Feature Architecture

### Common Mistakes

**Circular feature dependencies.** If `auth` imports `cart` and `cart` imports `auth`, you have a cycle. Shared types go in `core/`. Cross-feature communication uses events or shared interfaces.

**Duplicated error handling.** Extract `safeApiCall` into `core/`:

```dart
// lib/core/network/safe_api_call.dart
Future<Result<T>> safeApiCall<T>(Future<T> Function() call) async {
  try { return Success(await call()); }
  on UnauthorizedException { return const Failure(UnauthorizedFailure('Session expired')); }
  on ServerException catch (e) { return Failure(ServerFailure(e.message)); }
  catch (e) { return Failure(ServerFailure('Unexpected: $e')); }
}
```

**Logger in every constructor.** Use the decorator pattern instead:

```dart
// lib/core/logging/logging_usecase.dart
class LoggingUseCase<T, P> implements UseCase<T, P> {
  final UseCase<T, P> _inner;
  final Logger _logger;
  LoggingUseCase({required UseCase<T, P> inner, required Logger logger})
      : _inner = inner, _logger = logger;

  @override
  Future<Result<T>> call(P params) async {
    final sw = Stopwatch()..start();
    _logger.info('[${_inner.runtimeType}] started');
    final result = await _inner(params);
    _logger.info('[${_inner.runtimeType}] completed in ${sw.elapsedMilliseconds}ms');
    return result;
  }
}
```

### Key Solution: AuthGuard

```dart
// lib/features/auth/presentation/widgets/auth_guard.dart
class AuthGuard extends StatelessWidget {
  final Widget child;
  final Widget loginPage;
  const AuthGuard({super.key, required this.child, required this.loginPage});

  @override
  Widget build(BuildContext context) {
    final authVm = context.watch<AuthViewModel>();
    return switch (authVm.authState) {
      AuthState.authenticated => child,
      AuthState.unauthenticated => loginPage,
      AuthState.loading => const Scaffold(body: Center(child: CircularProgressIndicator())),
    };
  }
}
```

---

## Exercise 6 -- Testing Strategy Per Layer

### Key Solution: Tests Per Layer

```dart
// test/domain/entities/product_test.dart
void main() {
  group('Product.canPurchase', () {
    final product = Product(id: '1', name: 'W', price: Money(cents: 999), stockQuantity: 5);
    test('true within stock', () => expect(product.canPurchase(3), isTrue));
    test('true at exact stock', () => expect(product.canPurchase(5), isTrue));
    test('false over stock', () => expect(product.canPurchase(6), isFalse));
    test('false at zero', () => expect(product.canPurchase(0), isFalse));
    test('false at negative', () => expect(product.canPurchase(-1), isFalse));
  });

  group('Money', () {
    test('addition', () => expect((Money(cents: 100) + Money(cents: 250)).cents, 350));
    test('multiplication', () => expect((Money(cents: 999) * 3).cents, 2997));
    test('amount', () => expect(Money(cents: 1050).amount, 10.50));
  });
}
```

```dart
// test/domain/usecases/add_to_cart_test.dart
void main() {
  late AddToCart useCase;
  late MockProductRepository mockProductRepo;
  late MockCartRepository mockCartRepo;

  setUp(() {
    mockProductRepo = MockProductRepository();
    mockCartRepo = MockCartRepository();
    useCase = AddToCart(cartRepository: mockCartRepo, productRepository: mockProductRepo);
  });

  test('succeeds with sufficient stock', () async {
    mockProductRepo.getByIdResult = Success(Product(id: 'p1', name: 'W',
        price: Money(cents: 999), stockQuantity: 10));
    mockCartRepo.addItemResult = Success(Cart(items: []));
    final result = await useCase(AddToCartParams(productId: 'p1', quantity: 3));
    expect(result, isA<Success<Cart>>());
  });

  test('fails with insufficient stock', () async {
    mockProductRepo.getByIdResult = Success(Product(id: 'p1', name: 'W',
        price: Money(cents: 999), stockQuantity: 2));
    final result = await useCase(AddToCartParams(productId: 'p1', quantity: 5));
    expect(result, isA<Failure<Cart>>());
  });

  test('propagates repository failure', () async {
    mockProductRepo.getByIdResult = Failure(NotFoundFailure('Not found'));
    final result = await useCase(AddToCartParams(productId: 'x', quantity: 1));
    expect((result as Failure).failure, isA<NotFoundFailure>());
  });
}
```

```dart
// test/presentation/viewmodels/product_list_viewmodel_test.dart
void main() {
  late ProductListViewModel vm;
  late MockGetProducts mockGetProducts;

  setUp(() {
    mockGetProducts = MockGetProducts();
    vm = ProductListViewModel(getProducts: mockGetProducts, addToCart: MockAddToCart());
  });

  test('initial state is idle', () => expect(vm.state, ViewState.idle));

  test('transitions loading -> loaded on success', () async {
    mockGetProducts.resultToReturn = Success([Product(id: '1', name: 'W',
        price: Money(cents: 100), stockQuantity: 10)]);
    final states = <ViewState>[];
    vm.addListener(() => states.add(vm.state));
    await vm.loadProducts();
    expect(states, [ViewState.loading, ViewState.loaded]);
    expect(vm.products, hasLength(1));
  });

  test('transitions loading -> error on failure', () async {
    mockGetProducts.resultToReturn = Failure(ServerFailure('Down'));
    await vm.loadProducts();
    expect(vm.state, ViewState.error);
    expect(vm.errorMessage, 'Down');
  });
}
```

### Deep Dive

The testing pyramid per layer: domain tests are fastest and most numerous (pure logic). Data tests verify the boundary with external systems. Presentation tests verify state management. Integration tests are fewest and slowest. When a test fails, you immediately know which layer has the bug.

---

## Exercise 7 -- Production E-Commerce Architecture

### Key Solution: Core Package

```dart
// packages/core/lib/src/types/result.dart
sealed class Result<T> {
  const Result();
  bool get isSuccess => this is Success<T>;
  T get dataOrThrow => switch (this) {
    Success(:final data) => data,
    Failure(:final failure) => throw StateError('Failure: ${failure.message}'),
  };
  Result<R> map<R>(R Function(T) transform) => switch (this) {
    Success(:final data) => Success(transform(data)),
    Failure(:final failure) => Failure(failure),
  };
}
class Success<T> extends Result<T> { final T data; const Success(this.data); }
class Failure<T> extends Result<T> { final AppFailure failure; const Failure(this.failure); }
```

```dart
// packages/domain_cart/lib/src/entities/cart.dart
class Cart {
  final List<CartItem> items;
  const Cart({required this.items});
  int get itemCount => items.fold(0, (s, i) => s + i.quantity);
  Money get totalPrice => items.fold(const Money(cents: 0), (s, i) => s + i.lineTotal);

  Cart addItem(CartItem newItem) {
    final idx = items.indexWhere((i) => i.productId == newItem.productId);
    if (idx >= 0) {
      final updated = List<CartItem>.from(items);
      updated[idx] = CartItem(productId: items[idx].productId, productName: items[idx].productName,
          unitPrice: items[idx].unitPrice, quantity: items[idx].quantity + newItem.quantity);
      return Cart(items: updated);
    }
    return Cart(items: [...items, newItem]);
  }
}
```

### Debugging Tips

**"Package not found" in monorepo.** Use `path` dependencies: `domain_products: {path: ../domain_products}`. Run `dart pub get` in each package.

**Circular package dependency.** Extract shared types into `core`. Reference products from cart by ID, not by importing the full entity package.

**DI ordering issues.** Register bottom-up: core first, then data sources, then repos, then use cases, then view models.

---

## Exercise 8 -- Micro-Frontend Architecture

### Key Solution: Module Registry with Topological Sort

```dart
// lib/core/module/module_registry.dart
class ModuleRegistry {
  final List<FeatureModule> _initializedModules = [];
  final ModuleContext _context;
  ModuleRegistry({required ModuleContext context}) : _context = context;

  Future<void> loadModules(List<FeatureModule> modules) async {
    final enabled = modules.where((m) => m.isEnabled(_context.featureFlags)).toList();
    final sorted = _topologicalSort(enabled);
    for (final module in sorted) {
      module.registerDependencies(_context.container);
      await module.initialize(_context);
      _initializedModules.add(module);
    }
  }

  List<FeatureModule> _topologicalSort(List<FeatureModule> modules) {
    final map = {for (final m in modules) m.moduleId: m};
    final inDegree = <String, int>{for (final m in modules) m.moduleId: 0};
    final adj = <String, List<String>>{for (final m in modules) m.moduleId: []};

    for (final m in modules) {
      for (final dep in m.dependencies) {
        if (map.containsKey(dep)) {
          adj[dep]!.add(m.moduleId);
          inDegree[m.moduleId] = inDegree[m.moduleId]! + 1;
        }
      }
    }

    final queue = [for (final e in inDegree.entries) if (e.value == 0) e.key];
    final sorted = <FeatureModule>[];
    while (queue.isNotEmpty) {
      final current = queue.removeAt(0);
      sorted.add(map[current]!);
      for (final neighbor in adj[current]!) {
        inDegree[neighbor] = inDegree[neighbor]! - 1;
        if (inDegree[neighbor] == 0) queue.add(neighbor);
      }
    }
    if (sorted.length != modules.length) {
      throw ModuleException('Circular dependency detected');
    }
    return sorted;
  }

  Map<String, WidgetBuilder> get activeRoutes =>
      {for (final m in _initializedModules) ...m.routes};
}
```

```dart
// lib/core/module/event_bus.dart
class EventBus {
  final _controller = StreamController<ModuleEvent>.broadcast();
  void publish(ModuleEvent event) => _controller.add(event);
  StreamSubscription<T> subscribe<T extends ModuleEvent>(void Function(T) handler) =>
      _controller.stream.where((e) => e is T).cast<T>().listen(handler);
  void dispose() => _controller.close();
}

abstract class ModuleEvent { String get sourceModule; }
class UserLoggedIn extends ModuleEvent { @override final sourceModule = 'auth'; final String userId; UserLoggedIn(this.userId); }
class UserLoggedOut extends ModuleEvent { @override final sourceModule = 'auth'; }
class CartUpdated extends ModuleEvent { @override final sourceModule = 'cart'; final int itemCount; CartUpdated(this.itemCount); }
```

### Alternatives Considered

**Riverpod instead of get_it.** Compile-time safe, scoped overrides. Trade-off: couples DI to the widget tree, harder for background services.

**Bloc instead of ChangeNotifier.** Enforces unidirectional flow with events/states. More verbose but more predictable. Better for large teams.

**Freezed for entities.** Generates immutable classes with `copyWith` and equality. Low marginal cost if already using build_runner.

**go_router for modular navigation.** ShellRoute and route-level redirects give nested navigation and deep linking out of the box.

---

## General Debugging Tips

**"Type is not a subtype" at runtime.** Check that `sl.registerLazySingleton<AbstractType>(() => Concrete())` uses the abstract type.

**UI not updating.** Using `context.read<T>()` (reads once) instead of `context.watch<T>()` (rebuilds on change)?

**Stale data after mutation.** Cache invalidation not running after create/update/delete. Clear cache entry, then fetch fresh.

**Tests pass alone, fail together.** Shared singleton state. Call `getIt.reset()` in `tearDown`.

**Slow build_runner.** Split into packages so code gen only reruns on changed packages. Use `--delete-conflicting-outputs`.
