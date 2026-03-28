# Section 17: Flutter Testing -- Unit, Widget, Integration & Golden Tests

## Introduction

Shipping code without tests is like deploying a bridge without load analysis. It might hold today, but you have no way of knowing when it will fail or why. Testing in Flutter is not an afterthought -- it is a first-class capability baked into the SDK, with dedicated packages for every layer of the testing pyramid.

The testing pyramid has three layers. At the base, unit tests verify functions and business logic in isolation -- fast, cheap, plentiful. In the middle, widget tests render components in memory, verifying UI responses to state changes and interactions. At the top, integration tests run the complete app on a real device, exercising end-to-end flows. Golden tests sit alongside: they compare rendered widget screenshots pixel-by-pixel against reference images, catching visual regressions no assertion can detect.

Why does this matter? Confidence scales with coverage. A single untested refactor can cascade through navigation, state, and API calls in ways manual QA will miss. This section teaches you to build proof that your code works, layer by layer.

## Prerequisites

- Sections 09-16 completed: widgets, layouts, navigation, state management, forms, networking, animations
- Flutter SDK 3.10+ with `flutter test` functional from the command line
- Familiarity with Dart async patterns (`Future`, `Stream`, `async`/`await`) from Section 05
- `mocktail` and `bloc_test` packages available (`flutter pub add --dev mocktail bloc_test`)

## Learning Objectives

By the end of this section, you will be able to:

1. **Distinguish** between unit, widget, integration, and golden tests, selecting the appropriate type for each scenario
2. **Implement** unit tests using `test`, `expect`, matchers, `group`, `setUp`, and `tearDown`
3. **Apply** mocking with `mocktail` to isolate units from their dependencies
4. **Construct** widget tests that find elements, simulate interaction, and verify UI state after async operations
5. **Design** golden tests that catch visual regressions and handle cross-platform differences
6. **Evaluate** code coverage reports and establish minimum thresholds for production
7. **Synthesize** a complete testing strategy combining all test types with CI/CD integration

---

## Core Concepts

### 1. The Testing Pyramid and Cost-Value Trade-offs

Unit tests run in milliseconds, require no device, and pinpoint failures to a single function. Widget tests run in hundreds of milliseconds, render real widget trees, and catch layout and interaction bugs. Integration tests take seconds to minutes on real devices, exercising the actual user experience but are slow and flaky.

The rule of thumb: 70% unit, 20% widget, 10% integration. But match your investment to where actual risk lives -- a forms-heavy app needs more widget tests, a data library needs almost exclusively unit tests.

### 2. Unit Testing Fundamentals

Every Flutter project ships with the `test` package. Test files live in `test/` mirroring your `lib/` structure.

```dart
// test/models/cart_test.dart
import 'package:test/test.dart';
import 'package:my_app/models/cart.dart';
import 'package:my_app/models/product.dart';

void main() {
  late Cart cart;
  final sampleProduct = Product(id: '1', name: 'Widget', price: 9.99);

  setUp(() {
    cart = Cart();
  });

  tearDown(() {
    // Clean up resources if needed -- files, streams, controllers
  });

  group('Cart.addItem', () {
    test('adds a product to an empty cart', () {
      cart.addItem(sampleProduct);
      expect(cart.items, hasLength(1));
      expect(cart.items.first.product, equals(sampleProduct));
    });

    test('increments quantity when adding the same product twice', () {
      cart.addItem(sampleProduct);
      cart.addItem(sampleProduct);
      expect(cart.items, hasLength(1));
      expect(cart.items.first.quantity, equals(2));
    });

    test('calculates total correctly with multiple items', () {
      final expensive = Product(id: '2', name: 'Gadget', price: 49.99);
      cart.addItem(sampleProduct);
      cart.addItem(sampleProduct);
      cart.addItem(expensive);
      expect(cart.total, closeTo(69.97, 0.01));
    });
  });

  group('Cart.removeItem', () {
    test('throws when removing a product not in the cart', () {
      expect(
        () => cart.removeItem('nonexistent'),
        throwsA(isA<CartException>()),
      );
    });
  });
}
```

`setUp` creates a fresh instance per test so no state leaks. `group` organizes by method. Each `test` verifies one behavior. The `expect` function pairs values with matchers -- and matchers go far beyond equality:

```dart
// test/matchers_showcase_test.dart
import 'package:test/test.dart';

void main() {
  test('numeric matchers', () {
    expect(42, greaterThan(40));
    expect(3.14, closeTo(3.1, 0.1));
    expect(100, inInclusiveRange(0, 100));
  });

  test('collection matchers', () {
    expect([1, 2, 3], contains(2));
    expect([1, 2, 3], containsAll([3, 1]));
    expect({'a': 1, 'b': 2}, containsPair('a', 1));
  });

  test('exception matchers with property assertions', () {
    expect(
      () => throw ArgumentError('bad input'),
      throwsA(isA<ArgumentError>().having(
        (e) => e.message, 'message', 'bad input',
      )),
    );
  });
}
```

The `.having()` chain on `isA` lets you assert on specific properties of thrown exceptions or returned objects -- essential for verifying error messages and error codes.

### 3. Mocking with Mocktail

Real classes have real dependencies: HTTP clients, databases, platform channels. Mocking replaces them with controlled stand-ins. `mocktail` (preferred over `mockito` because it requires no code generation) uses three concepts: **mocks** (record and verify interactions), **stubs** (predetermined return values), and **fakes** (lightweight implementations).

```dart
// test/services/auth_service_test.dart
import 'package:mocktail/mocktail.dart';
import 'package:test/test.dart';
import 'package:my_app/services/auth_service.dart';
import 'package:my_app/repositories/user_repository.dart';

class MockUserRepository extends Mock implements UserRepository {}
class FakeUser extends Fake implements User {}

void main() {
  late AuthService authService;
  late MockUserRepository mockRepo;

  setUpAll(() => registerFallbackValue(FakeUser()));

  setUp(() {
    mockRepo = MockUserRepository();
    authService = AuthService(repository: mockRepo);
  });

  test('returns user on valid credentials', () async {
    final expectedUser = User(id: '1', email: 'test@example.com');
    when(() => mockRepo.findByEmail('test@example.com'))
        .thenAnswer((_) async => expectedUser);
    when(() => mockRepo.verifyPassword(any(), any()))
        .thenAnswer((_) async => true);

    final result = await authService.login('test@example.com', 'pass123');

    expect(result, equals(expectedUser));
    verify(() => mockRepo.findByEmail('test@example.com')).called(1);
  });

  test('throws AuthException on invalid credentials', () async {
    when(() => mockRepo.findByEmail(any())).thenAnswer((_) async => null);
    expect(
      () => authService.login('bad@email.com', 'wrong'),
      throwsA(isA<AuthException>()),
    );
    verifyNever(() => mockRepo.verifyPassword(any(), any()));
  });
}
```

`when` sets up what the mock returns (stubbing). `verify` checks that it was called (verification). `verifyNever` confirms a method was not called -- essential for proving that your code short-circuits after early validation failures. `registerFallbackValue` is required for `any()` with custom types.

### 4. Widget Testing

Widget tests are where Flutter's test framework shines. The `flutter_test` package gives you `testWidgets` with a `WidgetTester` -- your remote control for the widget tree.

```dart
// test/screens/login_screen_test.dart
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:my_app/screens/login_screen.dart';

void main() {
  testWidgets('shows validation errors on empty submit', (tester) async {
    await tester.pumpWidget(const MaterialApp(home: LoginScreen()));

    await tester.tap(find.byType(ElevatedButton));
    await tester.pump(); // rebuilds after setState

    expect(find.text('Email is required'), findsOneWidget);
    expect(find.text('Password is required'), findsOneWidget);
  });

  testWidgets('enters text and submits form', (tester) async {
    await tester.pumpWidget(const MaterialApp(home: LoginScreen()));

    await tester.enterText(find.byKey(const Key('email_field')), 'user@example.com');
    await tester.enterText(find.byKey(const Key('password_field')), 'secret123');
    await tester.tap(find.byType(ElevatedButton));
    await tester.pumpAndSettle();

    expect(find.text('Email is required'), findsNothing);
  });
}
```

The difference between pump methods is crucial and a source of many test failures:
- `pumpWidget` -- renders the widget tree the first time. Call once at the start.
- `pump()` -- triggers a single frame rebuild. Use after a tap that calls `setState`.
- `pump(Duration(seconds: 1))` -- advances time by the duration. Essential for animations and debounced operations.
- `pumpAndSettle()` -- keeps pumping until no pending frames. Use when animations or `Future.delayed` are involved. Warning: times out on infinite animations like loading spinners.

### 5. Finding Widgets and Simulating Interaction

The `find` object provides multiple strategies. Choose based on stability and meaning:

```dart
// test/finders_demo_test.dart
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  testWidgets('finder strategies', (tester) async {
    await tester.pumpWidget(MaterialApp(
      home: Scaffold(body: Column(children: [
        const Text('Hello'),
        ElevatedButton(
          key: const Key('submit_btn'),
          onPressed: () {},
          child: const Text('Submit'),
        ),
        const Icon(Icons.check),
      ])),
    ));

    // By text -- readable but fragile with i18n
    expect(find.text('Hello'), findsOneWidget);
    // By Key -- most stable, recommended for interactive elements
    expect(find.byKey(const Key('submit_btn')), findsOneWidget);
    // By widget type -- good for structural assertions
    expect(find.byType(ElevatedButton), findsOneWidget);
    // By icon
    expect(find.byIcon(Icons.check), findsOneWidget);
    // Scoped: descendant search within a parent
    expect(
      find.descendant(of: find.byType(ElevatedButton), matching: find.text('Submit')),
      findsOneWidget,
    );
  });
}
```

Use `Key` values for elements your tests interact with. Use `find.byType` for structural assertions. Use `find.text` sparingly -- it breaks when you localize your app.

### 6. Testing Async Code and Streams

```dart
// test/services/data_service_test.dart
import 'package:test/test.dart';
import 'package:my_app/services/data_service.dart';

void main() {
  late DataService service;
  setUp(() => service = DataService());

  test('fetchItem throws on invalid id', () async {
    await expectLater(
      service.fetchItem('invalid'),
      throwsA(isA<NotFoundException>()),
    );
  });

  test('counterStream emits exact sequence then closes', () {
    expect(
      service.counterStream(3),
      emitsInOrder([1, 2, 3, emitsDone]),
    );
  });

  test('priceStream emits error on invalid symbol', () {
    expect(
      service.priceStream('INVALID'),
      emitsError(isA<SymbolException>()),
    );
  });
}
```

Forgetting `await` on `expectLater` is a classic mistake -- the test passes vacuously without ever waiting for the assertion.

### 7. Testing Blocs with bloc_test

The `bloc_test` package provides `blocTest`, a declarative way to verify Bloc behavior:

```dart
// test/blocs/auth_bloc_test.dart
import 'package:bloc_test/bloc_test.dart';
import 'package:mocktail/mocktail.dart';
import 'package:my_app/blocs/auth/auth_bloc.dart';
import 'package:my_app/repositories/auth_repository.dart';

class MockAuthRepository extends Mock implements AuthRepository {}

void main() {
  late MockAuthRepository mockRepo;
  setUp(() => mockRepo = MockAuthRepository());

  blocTest<AuthBloc, AuthState>(
    'emits [loading, authenticated] on successful login',
    setUp: () {
      when(() => mockRepo.login(any(), any()))
          .thenAnswer((_) async => User(id: '1', name: 'Test'));
    },
    build: () => AuthBloc(repository: mockRepo),
    act: (bloc) => bloc.add(LoginRequested('user@test.com', 'pass')),
    expect: () => [AuthLoading(), isA<AuthAuthenticated>()],
    verify: (_) {
      verify(() => mockRepo.login('user@test.com', 'pass')).called(1);
    },
  );
}
```

`blocTest` handles subscription, event dispatch, and teardown. `build` creates the Bloc, `act` dispatches events, `expect` lists exact state emissions. Use `seed` to set an initial state when testing events that operate on existing data (like filtering a loaded list).

### 8. Golden Tests

Golden tests capture a widget screenshot and compare against a stored reference. Any pixel change fails the test.

```dart
// test/golden/profile_card_golden_test.dart
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:my_app/widgets/profile_card.dart';

void main() {
  testWidgets('ProfileCard matches golden', (tester) async {
    await tester.pumpWidget(MaterialApp(
      theme: ThemeData.light(),
      home: Scaffold(
        body: Center(
          child: ProfileCard(name: 'Jane Doe', email: 'jane@example.com'),
        ),
      ),
    ));

    await expectLater(
      find.byType(ProfileCard),
      matchesGoldenFile('goldens/profile_card_light.png'),
    );
  });
}
```

Generate references with `flutter test --update-goldens`. Golden files are platform-dependent -- fonts render differently across macOS, Linux, and Windows. Most teams run golden tests exclusively in CI on a specific Docker image to ensure consistency.

### 9. Integration Testing

Integration tests live in `integration_test/` (not `test/`) and run on real devices:

```dart
// integration_test/app_test.dart
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:integration_test/integration_test.dart';
import 'package:my_app/main.dart' as app;

void main() {
  IntegrationTestWidgetsFlutterBinding.ensureInitialized();

  testWidgets('user can log in and see home screen', (tester) async {
    app.main();
    await tester.pumpAndSettle();

    await tester.enterText(find.byKey(const Key('email_field')), 'test@example.com');
    await tester.enterText(find.byKey(const Key('password_field')), 'password123');
    await tester.tap(find.byKey(const Key('login_button')));
    await tester.pumpAndSettle(const Duration(seconds: 5));

    expect(find.text('Home'), findsOneWidget);
  });
}
```

Run with `flutter test integration_test/`. These tests exercise real navigation, real HTTP calls (or intercepted ones), and real platform channels. Slow, but they catch bugs no other layer can find.

### 10. Code Coverage and Test Organization

Coverage tells you what percentage of code executes during tests. It does not tell you the tests are good -- 100% coverage with no assertions is worthless. But low coverage reliably indicates risk.

```bash
# coverage_commands.sh
flutter test --coverage
genhtml coverage/lcov.info -o coverage/html
open coverage/html/index.html
```

Organize tests to mirror `lib/`. Extract shared helpers and factories to keep tests DRY:

```dart
// test/helpers/test_helpers.dart
import 'package:flutter/material.dart';
import 'package:my_app/models/product.dart';

Widget createTestableWidget(Widget child) {
  return MaterialApp(home: Scaffold(body: child));
}

Product createTestProduct({
  String id = 'test-id',
  String name = 'Test Product',
  double price = 9.99,
}) {
  return Product(id: id, name: name, price: price);
}
```

Centralizing mocks and factories means you change them in one place when interfaces evolve. This is not optional for a project with more than a handful of test files.

---

## Exercises

### Exercise 1 -- Unit Testing a Task Manager (Basic)

Write unit tests for a `TaskManager` class that manages a to-do list.

```dart
// lib/services/task_manager.dart
class TaskManager {
  final List<Task> _tasks = [];
  List<Task> get tasks => List.unmodifiable(_tasks);
  List<Task> get completedTasks => _tasks.where((t) => t.isCompleted).toList();
  List<Task> get pendingTasks => _tasks.where((t) => !t.isCompleted).toList();

  void addTask(Task task) {
    if (_tasks.any((t) => t.id == task.id)) {
      throw ArgumentError('Task with id ${task.id} already exists');
    }
    _tasks.add(task);
  }

  void completeTask(String taskId) {
    final task = _tasks.firstWhere(
      (t) => t.id == taskId,
      orElse: () => throw StateError('Task $taskId not found'),
    );
    task.isCompleted = true;
  }

  void removeTask(String taskId) { _tasks.removeWhere((t) => t.id == taskId); }

  double get completionRate {
    if (_tasks.isEmpty) return 0.0;
    return completedTasks.length / _tasks.length;
  }
}
```

**Your task:** Write `test/services/task_manager_test.dart` covering: adding tasks (success and duplicate), completing tasks (success and not-found), removing tasks, filtered lists, and `completionRate` (empty, partial, all completed). Use `setUp`, `group`, and at least one success + one failure case per method.

**Verification:** `flutter test test/services/task_manager_test.dart` -- all pass, no state leaks between tests.

---

### Exercise 2 -- Widget Testing a Counter Screen (Basic)

Write widget tests for this counter screen:

```dart
// lib/screens/counter_screen.dart
class CounterScreen extends StatefulWidget {
  const CounterScreen({super.key});
  @override State<CounterScreen> createState() => _CounterScreenState();
}

class _CounterScreenState extends State<CounterScreen> {
  int _counter = 0;

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Counter')),
      body: Center(child: Text('$_counter', key: const Key('counter_text'))),
      floatingActionButton: Row(mainAxisSize: MainAxisSize.min, children: [
        FloatingActionButton(key: const Key('decrement_btn'),
            onPressed: () => setState(() => _counter--), child: const Icon(Icons.remove)),
        const SizedBox(width: 16),
        FloatingActionButton(key: const Key('increment_btn'),
            onPressed: () => setState(() => _counter++), child: const Icon(Icons.add)),
      ]),
    );
  }
}
```

**Your task:** Write `test/screens/counter_screen_test.dart` that verifies: initial value "0", increment shows "1", decrement from 0 shows "-1", 5 increments shows "5", both FABs and title render.

**Verification:** All tests pass with proper `pump()` calls after each tap.

---

### Exercise 3 -- Mocking an API Client (Intermediate)

Test a `WeatherService` without making real network calls:

```dart
// lib/services/weather_service.dart
class WeatherService {
  final ApiClient _client;
  WeatherService({required ApiClient client}) : _client = client;

  Future<Weather> getCurrentWeather(String city) async {
    if (city.isEmpty) throw ArgumentError('City cannot be empty');
    final response = await _client.get('/weather?city=$city');
    if (response.statusCode == 404) throw CityNotFoundException(city);
    if (response.statusCode != 200) throw WeatherApiException('Failed: ${response.statusCode}');
    return Weather.fromJson(response.body);
  }
}
```

**Your task:** Write `test/services/weather_service_test.dart` using `mocktail`. Test: 200 returns `Weather`, 404 throws `CityNotFoundException`, 500 throws `WeatherApiException`, empty city throws `ArgumentError` before any network call. Use `verify` and `verifyNever`.

**Verification:** All pass, zero real HTTP calls made.

---

### Exercise 4 -- Testing a Bloc with Dependencies (Intermediate)

Test a `ProductListBloc` with events `LoadProducts`, `FilterByCategory`, `SearchProducts`. States: `Initial`, `Loading`, `Loaded(products, activeFilter)`, `Error(message)`.

**Your task:** Write `test/blocs/product_list_bloc_test.dart` using `bloc_test` and `mocktail`. Test: load emits `[loading, loaded]` on success and `[loading, error]` on failure. Use `seed` for filter/search tests. Test case-insensitive search. Verify mock interactions.

**Verification:** All `blocTest` cases pass. State classes implement `Equatable`.

---

### Exercise 5 -- Golden Tests for a Design System (Advanced)

Create golden tests for `AppButton` with variants `primary`, `secondary`, `danger`, `ghost`.

**Your task:** Write `test/golden/app_button_golden_test.dart` that generates goldens for every variant in light and dark themes, plus loading and disabled states. Use a helper function, fixed-size `SizedBox`, and `RepaintBoundary`. Create a "catalog" golden showing all variants side by side.

**Verification:** `flutter test --update-goldens` creates references. Subsequent `flutter test` passes. Intentionally change a color and confirm failure.

---

### Exercise 6 -- Testing Forms and Navigation (Advanced)

Test a multi-step registration flow: Step 1 (email/password), Step 2 (profile info), Step 3 (confirmation).

**Your task:** Write `test/screens/registration_screen_test.dart` that tests: validation errors on empty fields, step navigation with valid input, data preserved on back-press, successful submit with mocked repository navigating to `HomeScreen`, error SnackBar on failed submit. Use `MockNavigatorObserver`.

**Verification:** All pass covering success, failure, and edge cases per step.

---

### Exercise 7 -- Complete Testing Architecture (Insane)

Design a full testing strategy for an e-commerce app (product catalog, cart, auth, checkout).

**Your task:**

1. Build test infrastructure: `test/helpers/pump_app.dart` (wraps widgets with all providers), `test/fixtures/` (JSON responses), `test/factories/` (model factories with defaults and overrides), `test/matchers/` (custom matchers like `isLoadingState`, `hasErrorMessage(String)`, `containsProduct(Product)`)
2. Unit tests for cart calculations with discounts, taxes, and currency rounding
3. Widget tests for every screen (product list, detail, cart, checkout)
4. Integration tests for "browse to purchase" and "sign in to order history"
5. Golden tests for product card in all states (normal, sale, out-of-stock, wishlisted)
6. GitHub Actions YAML with coverage thresholds (90% business logic, 80% screens)

**Verification:** `flutter test --coverage` passes all suites, each file runs independently, CI config enforces thresholds.

---

### Exercise 8 -- BDD Test Framework Extension (Insane)

Build a custom test utility layer with BDD syntax and property-based testing.

**Your task:**

1. Create `test/framework/bdd.dart` with `feature()`, `scenario()`, `given()`, `when()`, `then()` that generate a human-readable report to `test/reports/`
2. Create `test/framework/property_test.dart` with a `forAll` function that tests properties over 1000 random inputs
3. Rewrite cart tests using BDD syntax:

```dart
feature('Shopping Cart', () {
  scenario('Adding items', () {
    given('an empty cart', () { /* setup */ });
    when('a product is added', () { /* action */ });
    then('the cart contains one item', () { /* assertion */ });
  });
});
```

4. Write serialization round-trip property tests: `Product.fromJson(product.toJson()) == product` with edge cases (unicode, empty strings, negative numbers, max int values)

**Verification:** BDD tests produce readable output with feature/scenario labels. Property tests catch a deliberately introduced serialization bug. Report file lists pass/fail per scenario.

---

## Summary

Unit tests give speed and precision for business logic. Widget tests verify UI responses to state and interaction. Integration tests confirm the complete app works on real devices. Golden tests guard against visual regressions. The tools -- `test`, `expect`, matchers, `mocktail`, `testWidgets`, `WidgetTester`, `blocTest`, `matchesGoldenFile`, `integration_test` -- form a complete toolkit. The real skill is knowing which type to apply where, keeping tests fast and independent, and building infrastructure that makes the next test trivial.

## What's Next

In **Section 18: Flutter Architecture**, you will structure your application into clean, maintainable layers using patterns like Clean Architecture and feature-first organization. The testing skills from this section become essential -- good architecture is defined by testability.

## References

- [Flutter Testing Documentation](https://docs.flutter.dev/testing)
- [flutter_test API](https://api.flutter.dev/flutter/flutter_test/flutter_test-library.html)
- [mocktail package](https://pub.dev/packages/mocktail)
- [bloc_test package](https://pub.dev/packages/bloc_test)
- [integration_test package](https://docs.flutter.dev/testing/integration-tests)
- [Golden Toolkit](https://pub.dev/packages/golden_toolkit)
- [Very Good Ventures Testing Guide](https://verygood.ventures/blog/guide-to-flutter-testing)
