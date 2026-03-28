# Section 18 -- Flutter Architecture: Clean Architecture, MVVM, Repository Pattern & DI

## Introduction

You can build a Flutter app in a single file. It works -- until it does not. The moment your app grows past a handful of screens, the cracks appear. A change in your API response format forces edits in twelve widgets. A new team member spends three days finding where "the logic lives." Unit testing becomes impossible because every function drags in half the framework.

Architecture is the set of decisions that make your codebase survive contact with reality: new features, team growth, changing requirements. Good architecture means that when you ask "where does this belong?" the answer is obvious, and when you change one thing, only the things that should change actually change.

This section teaches three interlocking patterns. Clean Architecture gives you layered structure with a strict dependency rule. The Repository Pattern creates a seam between domain logic and the outside world. Dependency Injection wires those layers without tight coupling. None of these are Flutter-specific -- your domain logic should not know it runs inside Flutter at all.

## Prerequisites

- **Sections 09-11**: Widget tree, layouts, navigation and routing
- **Sections 12 and 15**: Basic and advanced state management (Provider, Riverpod, Bloc)
- **Section 14**: Networking, HTTP clients, JSON serialization
- **Section 17**: Unit testing, widget testing, mocking with Mockito
- **Sections 04 and 06**: OOP, abstract classes, interfaces, generics

## Learning Objectives

1. **Distinguish** the three layers of Clean Architecture and **explain** the dependency rule
2. **Design** domain entities, use cases, and repository interfaces with zero framework dependencies
3. **Implement** data layer components that adapt external APIs to domain contracts
4. **Construct** view models using ChangeNotifier or Bloc that expose UI state without leaking domain internals
5. **Apply** the Repository Pattern to abstract data sources behind a testable interface
6. **Configure** dependency injection using get_it and injectable
7. **Evaluate** feature-based vs layer-based project structures for a given team and scope
8. **Architect** a multi-feature application with shared domain, modular packages, and per-layer testing

---

## Core Concepts

### 1. Clean Architecture: The Dependency Rule

Source code dependencies must point inward -- outer layers toward inner layers, never the reverse. The domain depends on nothing. Data and presentation depend on the domain. They never know about each other.

```dart
// lib/core/architecture_overview.dart
//  Presentation  -->  Domain  <--  Data
//  (widgets,         (entities,    (models, data sources,
//   view models)      use cases,    repo implementations)
//                     repo interfaces)

// Domain: pure Dart, no Flutter, no packages
class User {
  final String id;
  final String email;
  final String name;
  const User({required this.id, required this.email, required this.name});
}

abstract class UserRepository {
  Future<User> getUserById(String id);
  Future<List<User>> searchUsers(String query);
}

// Data layer CAN import domain (valid: data depends on domain)
// Presentation CAN import domain (valid)
// Presentation CANNOT import data (VIOLATION of dependency rule)
```

### 2. Domain Layer: Entities, Use Cases, Repository Interfaces

Entities are plain Dart classes with business logic -- validation, computed properties. Use cases are single-purpose callable classes. Repository interfaces declare what data the domain needs without specifying how.

```dart
// lib/domain/entities/product.dart
class Product {
  final String id;
  final String name;
  final Money price;
  final int stockQuantity;
  const Product({required this.id, required this.name, required this.price, required this.stockQuantity});
  bool get isInStock => stockQuantity > 0;
  bool get isLowStock => stockQuantity > 0 && stockQuantity <= 5;
  bool canPurchase(int quantity) => quantity > 0 && quantity <= stockQuantity;
}

class Money {
  final int cents;
  final String currency;
  const Money({required this.cents, this.currency = 'USD'});
  double get amount => cents / 100.0;
  Money operator +(Money other) => Money(cents: cents + other.cents, currency: currency);
  Money operator *(int quantity) => Money(cents: cents * quantity, currency: currency);
}
```

```dart
// lib/domain/usecases/add_to_cart.dart
sealed class Result<T> { const Result(); }
class Success<T> extends Result<T> { final T data; const Success(this.data); }
class Failure<T> extends Result<T> { final AppFailure failure; const Failure(this.failure); }

sealed class AppFailure { final String message; const AppFailure(this.message); }
class ServerFailure extends AppFailure { const ServerFailure(super.message); }
class ValidationFailure extends AppFailure { const ValidationFailure(super.message); }
class NotFoundFailure extends AppFailure { const NotFoundFailure(super.message); }

abstract class UseCase<Type, Params> { Future<Result<Type>> call(Params params); }

class AddToCart implements UseCase<Cart, AddToCartParams> {
  final CartRepository _cartRepository;
  final ProductRepository _productRepository;
  AddToCart({required CartRepository cartRepository, required ProductRepository productRepository})
      : _cartRepository = cartRepository, _productRepository = productRepository;

  @override
  Future<Result<Cart>> call(AddToCartParams params) async {
    final productResult = await _productRepository.getById(params.productId);
    return switch (productResult) {
      Failure(:final failure) => Failure(failure),
      Success(:final data) => data.canPurchase(params.quantity)
          ? _cartRepository.addItem(productId: params.productId, quantity: params.quantity)
          : const Failure(ValidationFailure('Requested quantity exceeds available stock')),
    };
  }
}
```

### 3. Data Layer: Models, Data Sources, Repository Implementations

Models know about serialization. Entities do not. The data layer converts between the two. Data sources handle raw I/O. The repository implementation ties data sources together and translates exceptions into domain failures.

```dart
// lib/data/models/product_model.dart
class ProductModel {
  final String id, name, description, currency;
  final int priceCents, stock;
  const ProductModel({required this.id, required this.name, required this.description,
      required this.priceCents, required this.currency, required this.stock});

  factory ProductModel.fromJson(Map<String, dynamic> json) => ProductModel(
    id: json['id'] as String, name: json['name'] as String,
    description: json['description'] as String, priceCents: json['price_cents'] as int,
    currency: json['currency'] as String? ?? 'USD', stock: json['stock'] as int,
  );

  Product toDomain() => Product(id: id, name: name,
    price: Money(cents: priceCents, currency: currency), stockQuantity: stock);
}
```

```dart
// lib/data/repositories/product_repository_impl.dart
class ProductRepositoryImpl implements ProductRepository {
  final ProductRemoteDataSource _remoteDataSource;
  final ProductLocalDataSource _localDataSource;
  ProductRepositoryImpl({required ProductRemoteDataSource remoteDataSource,
      required ProductLocalDataSource localDataSource})
      : _remoteDataSource = remoteDataSource, _localDataSource = localDataSource;

  @override
  Future<Result<Product>> getById(String id) async {
    try {
      final model = await _remoteDataSource.getProductById(id);
      await _localDataSource.cacheProduct(model);
      return Success(model.toDomain());
    } on NotFoundException {
      return const Failure(NotFoundFailure('Product not found'));
    } on ServerException catch (e) {
      try {
        final cached = await _localDataSource.getProductById(id);
        return Success(cached.toDomain());
      } on CacheException {
        return Failure(ServerFailure('Server error: ${e.message}'));
      }
    }
  }
}
```

### 4. Presentation Layer: MVVM with ViewModel

The "View" is your widget. The "ViewModel" holds presentation state and coordinates with use cases. The "Model" is your domain layer.

```dart
// lib/presentation/viewmodels/product_list_viewmodel.dart
enum ViewState { idle, loading, loaded, error }

class ProductListViewModel extends ChangeNotifier {
  final GetProducts _getProducts;
  final AddToCart _addToCart;
  ProductListViewModel({required GetProducts getProducts, required AddToCart addToCart})
      : _getProducts = getProducts, _addToCart = addToCart;

  ViewState _state = ViewState.idle;
  List<Product> _products = [];
  String _errorMessage = '';
  ViewState get state => _state;
  List<Product> get products => List.unmodifiable(_products);
  String get errorMessage => _errorMessage;

  Future<void> loadProducts() async {
    _state = ViewState.loading;
    notifyListeners();
    final result = await _getProducts(const GetProductsParams());
    switch (result) {
      case Success(:final data): _products = data; _state = ViewState.loaded;
      case Failure(:final failure): _errorMessage = failure.message; _state = ViewState.error;
    }
    notifyListeners();
  }
}
```

```dart
// lib/presentation/pages/product_list_page.dart
class ProductListPage extends StatelessWidget {
  const ProductListPage({super.key});
  @override
  Widget build(BuildContext context) {
    final vm = context.watch<ProductListViewModel>();
    return Scaffold(
      appBar: AppBar(title: const Text('Products')),
      body: switch (vm.state) {
        ViewState.idle || ViewState.loading => const Center(child: CircularProgressIndicator()),
        ViewState.error => Center(child: Column(mainAxisAlignment: MainAxisAlignment.center,
            children: [Text(vm.errorMessage), ElevatedButton(onPressed: vm.loadProducts, child: const Text('Retry'))])),
        ViewState.loaded => ListView.builder(itemCount: vm.products.length,
            itemBuilder: (ctx, i) => ListTile(title: Text(vm.products[i].name))),
      },
    );
  }
}
```

### 5. Dependency Injection with get_it

Your view model needs a use case. The use case needs a repository interface. The repository implementation needs data sources. Someone has to create all these objects and wire them together. That someone is your DI container.

`get_it` is the most widely used service locator in Flutter. It is simple, fast, and does not require code generation. You register dependencies at app startup and retrieve them where needed. The trade-off: dependencies are implicit when you call `sl<Type>()`. The best practice is to use `get_it` only at the composition root (startup) and constructor injection everywhere else. Never call `sl()` inside a use case or repository.

```dart
// lib/injection_container.dart
import 'package:get_it/get_it.dart';
final sl = GetIt.instance;

Future<void> initDependencies() async {
  sl.registerLazySingleton<ProductRemoteDataSource>(() => ProductRemoteDataSourceImpl(client: sl()));
  sl.registerLazySingleton<ProductLocalDataSource>(() => ProductLocalDataSourceImpl(database: sl()));
  // Register against the INTERFACE, provide the IMPLEMENTATION
  sl.registerLazySingleton<ProductRepository>(() => ProductRepositoryImpl(
      remoteDataSource: sl(), localDataSource: sl()));
  sl.registerLazySingleton(() => GetProducts(repository: sl()));
  sl.registerLazySingleton(() => AddToCart(cartRepository: sl(), productRepository: sl()));
  // Factory: new instance each time (ViewModels hold page-specific state)
  sl.registerFactory(() => ProductListViewModel(getProducts: sl(), addToCart: sl()));
}
```

### 6. Project Structure: Feature-Based vs Layer-Based

How you organize files determines how quickly someone new can navigate the codebase and how cleanly features can be developed in parallel.

**Layer-based** puts all entities together, all repositories together, all widgets together. It works well for small apps (under five features) where most features share significant domain logic. The downside: adding a feature touches six different directories.

**Feature-based** groups everything for a feature in one directory. Each feature is a self-contained mini Clean Architecture app. Shared concepts (`User`, `Money`, error types) live in `core/`. This scales better for teams because a developer working on "cart" rarely needs to touch "auth" files, and merge conflicts between feature teams become rare.

```
# Feature-based (recommended for production)
lib/
  core/               # Shared: error types, network, DI, logging
  features/
    auth/
      domain/         # entities, usecases, repository interfaces
      data/           # models, datasources, repository impls
      presentation/   # viewmodels, pages, widgets
    products/
      domain/ | data/ | presentation/
    cart/
      domain/ | data/ | presentation/
```

### 7. Error Handling Across Layers

Each layer speaks its own error language, and this is by design. The data layer throws exceptions (`ServerException`, `CacheException`) because that is the natural idiom for I/O code. The domain layer uses typed failures (`ServerFailure`, `ValidationFailure`) wrapped in `Result` because business operations should make failure a first-class value, not a side effect. The presentation layer translates domain failures into user-friendly messages. The critical rule: exceptions never cross layer boundaries -- the repository catches them at the edge.

```dart
// lib/core/error/error_mapper.dart
String mapFailureToMessage(AppFailure failure) => switch (failure) {
  ServerFailure() => 'Something went wrong. Please try again later.',
  ValidationFailure(:final message) => message,
  NotFoundFailure() => 'The item you are looking for no longer exists.',
  NetworkFailure() => 'No internet connection. Check your network.',
  UnauthorizedFailure() => 'Session expired. Please log in again.',
};
```

### 8. Service Locator vs Constructor Injection

The Flutter community debates these two approaches endlessly. Here is the practical truth.

**Constructor injection** means every class declares its dependencies in its constructor. Dependencies are visible in the signature, the class is trivially testable (pass mocks through the constructor), and it is impossible to forget a dependency -- the compiler enforces it.

**Service locator** (like `get_it`) means a class reaches into a global registry to get what it needs. Less boilerplate, but dependencies become invisible. You cannot tell what a class needs by reading its constructor. Tests must register mocks in the locator, and forgetting one causes a runtime crash, not a compile error.

The pragmatic approach: use constructor injection for all your classes (use cases, repositories, view models). Use `get_it` exclusively at the composition root where you create these objects and pass the dependencies. This gives you the best of both worlds: explicit dependencies everywhere that matters, with a single centralized wiring point.

---

## Exercises

### Exercise 1 -- Layer Separation (Basic)

**Objective**: Reorganize a flat, single-file notes feature into Clean Architecture layers.

```dart
// exercise_1_starter.dart -- everything in one file
class _NotesPageState extends State<NotesPage> {
  List<Map<String, dynamic>> notes = [];
  bool isLoading = false;
  String? error;

  Future<void> loadNotes() async {
    setState(() { isLoading = true; });
    try {
      final response = await http.get(Uri.parse('https://api.example.com/notes'));
      if (response.statusCode == 200) {
        setState(() { notes = (json.decode(response.body) as List).cast(); isLoading = false; });
      } else { setState(() { error = 'Server error'; isLoading = false; }); }
    } catch (e) { setState(() { error = e.toString(); isLoading = false; }); }
  }

  Future<void> addNote(String title, String content) async {
    await http.post(Uri.parse('https://api.example.com/notes'),
        body: json.encode({'title': title, 'content': content}));
    await loadNotes();
  }
}
```

**Tasks**: (1) Create `Note` entity with `id`, `title`, `content`, `createdAt`, `isValid` getter. (2) Create `NoteRepository` interface returning `Result` types. (3) Create `NoteModel` with `fromJson`/`toJson`/`toDomain()`. (4) Create `NoteRemoteDataSource`. (5) Create `NoteRepositoryImpl` catching exceptions into failures. (6) Create `NotesViewModel` with `ChangeNotifier`. (7) Rewrite `NotesPage` to use the view model.

**Verification**: Domain layer has zero imports from `dart:convert`, `package:http`, or `package:flutter`. Presentation does not import `data/`. Run `dart analyze` and confirm zero warnings.

**Transition**: Now that you can separate layers, the next exercise focuses on the data layer's most important responsibility: managing where data comes from and how it is cached.

---

### Exercise 2 -- Repository with Caching (Basic)

**Objective**: Implement a weather repository with remote-first, local-fallback caching strategy.

```dart
// exercise_2_starter.dart
class WeatherForecast {
  final String city;
  final double temperatureCelsius;
  final String condition;
  final DateTime fetchedAt;
  bool get isStale => DateTime.now().difference(fetchedAt) > const Duration(minutes: 30);
}

// Implement: WeatherModel, WeatherRemoteDataSource, WeatherLocalDataSource (in-memory Map),
// WeatherRepositoryImpl with strategy: remote first -> cache on success -> fallback to cache
// on failure -> return stale with warning -> return failure if no cache
```

**Tasks**: (1) `WeatherModel` with serialization. (2) Remote and local data sources. (3) `WeatherRepositoryImpl` with the four-path caching strategy. (4) Unit tests with mocked data sources.

**Verification**: Test where remote throws returns cached value. Test where both fail returns `Failure`. Test where cache is stale -- confirm a warning is logged but data is still returned.

**Transition**: With layers and caching under your belt, it is time to build a complete feature with proper use cases that enforce business rules.

---

### Exercise 3 -- Full Feature with Use Cases (Intermediate)

**Objective**: Implement a task management feature with five use cases, proper validation, and view models.

```dart
// exercise_3_domain.dart
class Task {
  final String id, title, description;
  final TaskPriority priority;
  final TaskStatus status;
  final DateTime createdAt;
  final DateTime? completedAt;
  bool get isOverdue => status != TaskStatus.done &&
      DateTime.now().difference(createdAt) > const Duration(days: 7);
}
enum TaskPriority { low, medium, high, critical }
enum TaskStatus { todo, inProgress, done }

// Implement: GetTasks (sorted by priority then date), CreateTask (validates, generates id),
// CompleteTask (marks done, sets completedAt), GetOverdueTasks, GetTaskStats
```

**Tasks**: (1) All five use cases. (2) `TaskModel` with serialization. (3) Remote and local data sources. (4) `TaskRepositoryImpl` with cache-aside. (5) `TaskListViewModel` and `TaskStatsViewModel`. (6) Widget rendering based on view model state.

**Verification**: Unit tests for every use case. `GetOverdueTasks` correctly filters on the seven-day rule. `CompleteTask` sets `completedAt` and rejects already-completed tasks.

**Transition**: You now have a complete feature with proper use cases. The next step is automating the dependency injection wiring.

---

### Exercise 4 -- Injectable and Advanced DI (Intermediate)

**Objective**: Replace manual get_it registration with `injectable` code generation and environment-specific configurations.

**Tasks**: (1) Add `injectable` and `injectable_generator` to the project. (2) Annotate all services with `@injectable`, `@lazySingleton`, or `@module`. (3) Create `dev`, `staging`, `prod` environments with different API base URLs. (4) Create a `RegisterModule` for third-party dependencies like Dio. (5) Create a mock module for testing. (6) Verify generated code compiles and wires correctly.

**Verification**: `dart run build_runner build` succeeds. The generated config file contains all registrations. Test environment injects mock implementations. Switching to `prod` environment changes the API base URL.

**Transition**: With automated DI in place, you are ready to scale to a real multi-feature application.

---

### Exercise 5 -- Multi-Feature Architecture (Advanced)

**Objective**: Design a multi-feature app with shared domain, cross-cutting concerns, and per-feature DI.

**Tasks**: (1) Shared `core/` with error types, API client with auth interceptor, logging. (2) `auth` feature end-to-end including `AuthGuard` widget. (3) `products` feature with auth token attachment. (4) `cart` feature with local persistence and network sync. (5) Wire all features in one DI container with shared singletons. (6) Cross-cutting logging: log every use case invocation with timing.

**Verification**: Full flow -- login, browse products, add to cart, logout triggers auth guard. Logs show use case timing. Unit tests per feature run independently without importing other features.

**Transition**: A multi-feature app is only as reliable as its tests. The next exercise builds the testing strategy that ensures each layer is verified in isolation.

---

### Exercise 6 -- Testing Strategy Per Layer (Advanced)

**Objective**: Build a comprehensive test suite where each layer has its own testing approach.

**Tasks**: (1) Entity tests: `canPurchase` edge cases, `Money` arithmetic, `isOverdue` boundaries. (2) Use case tests: mock repos, test success/failure/propagation. (3) Model tests: `fromJson` with missing fields, round-trip. (4) Data source tests: mock HTTP, verify URL construction, exception on non-200. (5) Repository tests: cache-aside four paths. (6) ViewModel tests: state transitions, error state. (7) One integration test with real implementations (mock HTTP server).

**Verification**: `flutter test --coverage`. Domain at 100%. Overall exceeds 85%. No cross-layer test imports.

---

### Exercise 7 -- Production E-Commerce Architecture (Insane)

**Objective**: Architect and implement a production e-commerce app as a monorepo with separate Dart packages per layer per feature.

```
packages/
  core/                  # Result, failures, UseCase, Pagination, network
  domain_auth/           # depends on: core
  domain_products/       # depends on: core
  domain_cart/           # depends on: core, domain_products
  data_auth/             # depends on: core, domain_auth
  data_products/         # depends on: core, domain_products
  data_cart/             # depends on: core, domain_cart, domain_products
  feature_auth/          # depends on: domain_auth
  feature_products/      # depends on: domain_products
  feature_cart/          # depends on: domain_cart
app/                     # depends on: all feature + data packages
```

**Tasks**: (1) `core` package with `Result<T>`, `AppFailure` hierarchy, `UseCase<T,P>`, `Pagination`. (2) `domain_auth` with `User`, `AuthToken`, login/register/logout/refresh use cases. (3) `domain_products` with `Product`, `Category`, paginated listing/search use cases. (4) `domain_cart` with `Cart` (computed `totalPrice`, `validate()` checking stock), add/remove/update use cases. (5) All three data packages with cache-aside repos, auth error handling triggering token refresh. (6) All three feature packages with view models -- `AuthGuard`, infinite scroll, real-time cart totals. (7) App DI container with dev/prod environments. (8) 40+ unit tests, 2+ integration tests for cross-feature flows.

**Verification**: `dart analyze` in every package -- zero warnings. `flutter test` in every package -- all pass. Verify that `domain_products` cannot import `data_products` (the pubspec dependency is not declared). App starts in both dev and prod environments.

**Transition**: If you have built a monorepo e-commerce app, you are ready for the final challenge: runtime module loading with feature flags.

---

### Exercise 8 -- Micro-Frontend Architecture (Insane)

**Objective**: Design a micro-frontend system where feature modules are independently developed, tested, and toggled at runtime.

```dart
// exercise_8_architecture.dart
abstract class FeatureModule {
  String get moduleId;
  List<String> get dependencies;
  Future<void> initialize(ModuleContext context);
  Future<void> dispose();
  Map<String, WidgetBuilder> get routes;
  Widget? get navigationEntry;
  void registerDependencies(GetIt container);
  bool isEnabled(FeatureFlagService flags);
}
```

**Tasks**: (1) Implement `FeatureModule`, `ModuleContext`, `ModuleRegistry`, `FeatureFlagService`. (2) Event bus with `UserLoggedIn`, `UserLoggedOut`, `CartUpdated`, `ProductViewed` -- modules subscribe without knowing publishers. (3) Auth module: on `UserLoggedOut`, other modules clear cached state. (4) Products module: publish analytics on view, publish `CartUpdated` via event bus instead of importing cart. (5) Cart module listens for `CartUpdated`. (6) `ModuleRegistry.loadModules` with topological sort, circular dependency detection. (7) Feature flags: toggle `profile` module at runtime, verify navigation updates. (8) Tests for lifecycle ordering, dependency resolution, event bus isolation, feature flag toggling, cross-module flows.

**Verification**: All modules enabled -- navigation shows all entries. Disable cart -- its routes return 404. Re-enable -- reinitializes. No module imports another module's internal types.

---

## Summary

Clean Architecture protects your domain from framework churn. The Repository Pattern provides the seam between domain and data. Dependency injection wires it all practically. MVVM gives the presentation layer its own separation of concerns. The real test: can a new developer add a feature without understanding the whole codebase? Can you write tests without standing up the entire app? Can you change a technical decision without rewriting everything?

## What's Next

In **Section 19: Flutter Performance**, you will measure, profile, and optimize Flutter apps using DevTools, understand the rendering pipeline, optimize widget rebuilds, and manage memory. The architectural layers you built here tell you exactly where to look when something is slow.

## References

- [Clean Architecture -- Robert C. Martin](https://blog.cleancoder.com/uncle-bob/2012/08/13/the-clean-architecture.html)
- [Flutter App Architecture with Riverpod -- Andrea Bizzotto](https://codewithandrea.com/articles/flutter-app-architecture-riverpod-introduction/)
- [get_it package](https://pub.dev/packages/get_it)
- [injectable package](https://pub.dev/packages/injectable)
- [Very Good Architecture](https://verygood.ventures/blog/very-good-flutter-architecture)
- [Reso Coder's Flutter Clean Architecture](https://resocoder.com/flutter-clean-architecture-tdd/)
- [Bloc Library Architecture](https://bloclibrary.dev/architecture/)
