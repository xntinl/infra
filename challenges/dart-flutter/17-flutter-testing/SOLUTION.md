# Section 17: Solutions -- Flutter Testing

## How to Use This File

Do not read solutions before attempting each exercise. Follow this progression:

1. **Attempt the exercise** using only the README and Flutter docs
2. **Read the progressive hints** if stuck after 15+ minutes
3. **Compare your solution** after you have a working version
4. **Study common mistakes** -- you will recognize several from your own attempts

---

## Exercise 1 -- Unit Testing a Task Manager

### Progressive Hints

1. Start with the simplest test: create `TaskManager`, add one `Task`, verify `tasks.length == 1`.
2. For duplicate rejection, use `throwsA(isA<ArgumentError>())`.
3. `completionRate` on an empty list should return `0.0`, not throw. Test this edge case.
4. Use `closeTo` for `completionRate` -- floating-point equality with `equals` is brittle.

### Full Solution

```dart
// test/services/task_manager_test.dart
import 'package:flutter_test/flutter_test.dart';
import 'package:my_app/models/task.dart';
import 'package:my_app/services/task_manager.dart';

void main() {
  late TaskManager manager;

  Task createTask({String? id, String title = 'Test Task'}) {
    return Task(id: id ?? DateTime.now().microsecondsSinceEpoch.toString(), title: title);
  }

  setUp(() => manager = TaskManager());

  group('addTask', () {
    test('adds a task to empty manager', () {
      manager.addTask(createTask(id: '1'));
      expect(manager.tasks, hasLength(1));
    });

    test('throws ArgumentError on duplicate id', () {
      manager.addTask(createTask(id: 'dup'));
      expect(() => manager.addTask(createTask(id: 'dup')), throwsA(isA<ArgumentError>()));
    });

    test('tasks list is unmodifiable', () {
      manager.addTask(createTask(id: '1'));
      expect(() => manager.tasks.add(createTask()), throwsUnsupportedError);
    });
  });

  group('completeTask', () {
    test('marks existing task as completed', () {
      manager.addTask(createTask(id: '1'));
      manager.completeTask('1');
      expect(manager.tasks.first.isCompleted, isTrue);
    });

    test('throws StateError for nonexistent task', () {
      expect(() => manager.completeTask('ghost'), throwsA(isA<StateError>()));
    });
  });

  group('removeTask', () {
    test('removes an existing task', () {
      manager.addTask(createTask(id: '1'));
      manager.removeTask('1');
      expect(manager.tasks, isEmpty);
    });
  });

  group('filtered lists', () {
    test('completedTasks returns only completed', () {
      manager.addTask(createTask(id: '1'));
      manager.addTask(createTask(id: '2'));
      manager.completeTask('1');
      expect(manager.completedTasks, hasLength(1));
      expect(manager.completedTasks.first.id, equals('1'));
    });

    test('pendingTasks returns only incomplete', () {
      manager.addTask(createTask(id: '1'));
      manager.addTask(createTask(id: '2'));
      manager.completeTask('1');
      expect(manager.pendingTasks, hasLength(1));
      expect(manager.pendingTasks.first.id, equals('2'));
    });
  });

  group('completionRate', () {
    test('returns 0.0 for empty manager', () {
      expect(manager.completionRate, equals(0.0));
    });

    test('returns 0.5 when half are completed', () {
      manager.addTask(createTask(id: '1'));
      manager.addTask(createTask(id: '2'));
      manager.completeTask('1');
      expect(manager.completionRate, closeTo(0.5, 0.001));
    });

    test('returns 1.0 when all completed', () {
      manager.addTask(createTask(id: '1'));
      manager.completeTask('1');
      expect(manager.completionRate, closeTo(1.0, 0.001));
    });
  });
}
```

### Common Mistakes

**Not using `setUp`.** If you create `TaskManager` once at the top, tasks from one test leak into the next. Every test must start clean.

**Forgetting the empty list edge case.** Division by zero in `completionRate` returns `NaN` or throws. The implementation guards against this -- your tests should verify it.

---

## Exercise 2 -- Widget Testing a Counter Screen

### Progressive Hints

1. Wrap `CounterScreen` in `MaterialApp` -- `Scaffold` needs it as an ancestor.
2. After tapping, call `await tester.pump()` before asserting. Without it, the tree has not rebuilt.
3. To tap 5 times, use a for loop. Each tap needs its own `pump()`.

### Full Solution

```dart
// test/screens/counter_screen_test.dart
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:my_app/screens/counter_screen.dart';

void main() {
  Future<void> pumpCounter(WidgetTester tester) async {
    await tester.pumpWidget(const MaterialApp(home: CounterScreen()));
  }

  testWidgets('renders title and both FABs', (tester) async {
    await pumpCounter(tester);
    expect(find.text('Counter'), findsOneWidget);
    expect(find.byKey(const Key('increment_btn')), findsOneWidget);
    expect(find.byKey(const Key('decrement_btn')), findsOneWidget);
  });

  testWidgets('initial counter value is 0', (tester) async {
    await pumpCounter(tester);
    final text = tester.widget<Text>(find.byKey(const Key('counter_text')));
    expect(text.data, equals('0'));
  });

  testWidgets('tapping increment shows 1', (tester) async {
    await pumpCounter(tester);
    await tester.tap(find.byKey(const Key('increment_btn')));
    await tester.pump();
    expect(find.text('1'), findsOneWidget);
  });

  testWidgets('tapping decrement from 0 shows -1', (tester) async {
    await pumpCounter(tester);
    await tester.tap(find.byKey(const Key('decrement_btn')));
    await tester.pump();
    expect(find.text('-1'), findsOneWidget);
  });

  testWidgets('tapping increment 5 times shows 5', (tester) async {
    await pumpCounter(tester);
    for (var i = 0; i < 5; i++) {
      await tester.tap(find.byKey(const Key('increment_btn')));
      await tester.pump();
    }
    expect(find.text('5'), findsOneWidget);
  });
}
```

### Common Mistakes

**Forgetting `await tester.pump()` after a tap.** The most common widget test bug. The tap triggers `setState`, but the tree does not rebuild until `pump()`. Without it, you assert against stale state and the test incorrectly passes with the old value.

**Using `pumpAndSettle` when `pump` suffices.** For a simple `setState`, one `pump()` is enough. `pumpAndSettle` adds time and can timeout if an indefinite animation exists in the tree.

---

## Exercise 3 -- Mocking an API Client

### Progressive Hints

1. Define `class MockApiClient extends Mock implements ApiClient {}`.
2. Stub different status codes for different scenarios: `thenAnswer((_) async => ApiResponse(statusCode: 200, body: {...}))`.
3. For empty city, the method throws `ArgumentError` before calling the client. Use `verifyNever(() => mockClient.get(any()))`.

### Full Solution

```dart
// test/services/weather_service_test.dart
import 'package:flutter_test/flutter_test.dart';
import 'package:mocktail/mocktail.dart';
import 'package:my_app/clients/api_client.dart';
import 'package:my_app/services/weather_service.dart';

class MockApiClient extends Mock implements ApiClient {}

void main() {
  late MockApiClient mockClient;
  late WeatherService service;

  setUp(() {
    mockClient = MockApiClient();
    service = WeatherService(client: mockClient);
  });

  group('getCurrentWeather', () {
    test('returns Weather on 200', () async {
      when(() => mockClient.get('/weather?city=London')).thenAnswer(
        (_) async => ApiResponse(statusCode: 200, body: {
          'city': 'London', 'temperature': 15.5, 'condition': 'cloudy', 'humidity': 72,
        }),
      );

      final weather = await service.getCurrentWeather('London');
      expect(weather.city, equals('London'));
      verify(() => mockClient.get('/weather?city=London')).called(1);
    });

    test('throws CityNotFoundException on 404', () async {
      when(() => mockClient.get(any()))
          .thenAnswer((_) async => ApiResponse(statusCode: 404, body: {}));

      await expectLater(service.getCurrentWeather('Atlantis'), throwsA(isA<CityNotFoundException>()));
    });

    test('throws WeatherApiException on 500', () async {
      when(() => mockClient.get(any()))
          .thenAnswer((_) async => ApiResponse(statusCode: 500, body: {}));

      await expectLater(service.getCurrentWeather('London'), throwsA(isA<WeatherApiException>()));
    });

    test('throws ArgumentError on empty city before any network call', () async {
      expect(() => service.getCurrentWeather(''), throwsA(isA<ArgumentError>()));
      verifyNever(() => mockClient.get(any()));
    });
  });

  group('getForecast', () {
    test('returns correct number of items', () async {
      when(() => mockClient.get('/forecast?city=London&days=5')).thenAnswer(
        (_) async => ApiResponse(statusCode: 200, body: {
          'forecast': List.generate(5, (i) => {
            'city': 'London', 'temperature': 15.0 + i, 'condition': 'cloudy', 'humidity': 70,
          }),
        }),
      );

      final forecast = await service.getForecast('London');
      expect(forecast, hasLength(5));
    });
  });
}
```

### Common Mistakes

**Forgetting `await` on `expectLater`.** Without `await`, the test finishes before the Future resolves. The assertion never runs, and the test passes vacuously. This is the most dangerous async testing mistake.

**Stubbing too broadly with `any()`.** If you stub all calls to return 200, you cannot verify that the correct URL was constructed. Stub the specific URL for happy-path tests.

### Deep Dive: Fake vs Mock vs Stub

- **Stub**: predetermined answers. `when(...).thenReturn(value)`. Does not care how many times it is called.
- **Mock**: records interactions for verification. `verify(...).called(1)`. Tests that your code called the dependency correctly.
- **Fake**: working implementation with shortcuts. A `FakeDatabase` using an in-memory `Map` instead of SQLite.

---

## Exercise 4 -- Testing a Bloc with Dependencies

### Progressive Hints

1. `blocTest` handles the Bloc lifecycle. Do not call `bloc.close()` manually.
2. Use `seed` to set initial state for filter/search tests on pre-loaded data.
3. State classes must implement `Equatable` or override `==` -- otherwise `expect` always fails.

### Full Solution

```dart
// test/blocs/product_list_bloc_test.dart
import 'package:bloc_test/bloc_test.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:mocktail/mocktail.dart';
import 'package:my_app/blocs/product_list/product_list_bloc.dart';
import 'package:my_app/repositories/product_repository.dart';

class MockProductRepository extends Mock implements ProductRepository {}

void main() {
  late MockProductRepository mockRepo;
  final testProducts = [
    Product(id: '1', name: 'Laptop', category: 'electronics', price: 999),
    Product(id: '2', name: 'Shirt', category: 'clothing', price: 29),
    Product(id: '3', name: 'Phone', category: 'electronics', price: 699),
  ];

  setUp(() => mockRepo = MockProductRepository());

  blocTest<ProductListBloc, ProductListState>(
    'emits [loading, loaded] on successful fetch',
    setUp: () => when(() => mockRepo.getAll()).thenAnswer((_) async => testProducts),
    build: () => ProductListBloc(repository: mockRepo),
    act: (bloc) => bloc.add(LoadProducts()),
    expect: () => [ProductListLoading(), ProductListLoaded(products: testProducts)],
    verify: (_) => verify(() => mockRepo.getAll()).called(1),
  );

  blocTest<ProductListBloc, ProductListState>(
    'emits [loading, error] when repository throws',
    setUp: () => when(() => mockRepo.getAll()).thenThrow(Exception('Network error')),
    build: () => ProductListBloc(repository: mockRepo),
    act: (bloc) => bloc.add(LoadProducts()),
    expect: () => [
      ProductListLoading(),
      isA<ProductListError>().having((s) => s.message, 'message', contains('Network error')),
    ],
  );

  blocTest<ProductListBloc, ProductListState>(
    'filters by category from loaded state',
    seed: () => ProductListLoaded(products: testProducts),
    build: () => ProductListBloc(repository: mockRepo),
    act: (bloc) => bloc.add(FilterByCategory('electronics')),
    expect: () => [
      ProductListLoaded(
        products: testProducts.where((p) => p.category == 'electronics').toList(),
        activeFilter: 'electronics',
      ),
    ],
  );

  blocTest<ProductListBloc, ProductListState>(
    'search is case-insensitive',
    seed: () => ProductListLoaded(products: testProducts),
    build: () => ProductListBloc(repository: mockRepo),
    act: (bloc) => bloc.add(SearchProducts('PHONE')),
    expect: () => [
      isA<ProductListLoaded>().having((s) => s.products.first.name, 'name', 'Phone'),
    ],
  );
}
```

### Common Mistakes

**Forgetting `seed` for filter tests.** Without it the Bloc starts in `ProductListInitial` -- there are no products to filter. Use `seed` to set `ProductListLoaded` as the starting point.

**Not implementing `Equatable`.** `blocTest` compares states by value. Two `ProductListLoaded` instances with identical products fail without `Equatable`.

---

## Exercise 5 -- Golden Tests for a Design System

### Progressive Hints

1. Wrap every golden widget in `RepaintBoundary` and a fixed-size `SizedBox` for deterministic rendering.
2. For loading state with `CircularProgressIndicator`, use `pump(Duration(...))` not `pumpAndSettle` -- infinite animations timeout.
3. Create a helper that builds the button with given theme/variant to avoid boilerplate.

### Full Solution

```dart
// test/golden/app_button_golden_test.dart
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:my_app/widgets/app_button.dart';

void main() {
  Widget buildButton({required AppButtonVariant variant, required ThemeData theme,
      VoidCallback? onPressed, bool isLoading = false}) {
    return MaterialApp(theme: theme, home: Scaffold(body: Center(
      child: RepaintBoundary(child: SizedBox(width: 200, height: 60, child: Center(
        child: AppButton(label: 'Button', variant: variant,
            onPressed: onPressed ?? () {}, isLoading: isLoading),
      ))),
    )));
  }

  for (final variant in AppButtonVariant.values) {
    testWidgets('${variant.name} light golden', (tester) async {
      await tester.pumpWidget(buildButton(variant: variant, theme: ThemeData.light()));
      await expectLater(find.byType(AppButton),
          matchesGoldenFile('goldens/app_button_${variant.name}_light.png'));
    });

    testWidgets('${variant.name} dark golden', (tester) async {
      await tester.pumpWidget(buildButton(variant: variant, theme: ThemeData.dark()));
      await expectLater(find.byType(AppButton),
          matchesGoldenFile('goldens/app_button_${variant.name}_dark.png'));
    });
  }

  testWidgets('loading state golden', (tester) async {
    await tester.pumpWidget(buildButton(
        variant: AppButtonVariant.primary, theme: ThemeData.light(), isLoading: true));
    await tester.pump(const Duration(milliseconds: 100)); // NOT pumpAndSettle
    await expectLater(find.byType(AppButton), matchesGoldenFile('goldens/app_button_loading.png'));
  });

  testWidgets('disabled state golden', (tester) async {
    await tester.pumpWidget(buildButton(
        variant: AppButtonVariant.primary, theme: ThemeData.light(), onPressed: null));
    await expectLater(find.byType(AppButton), matchesGoldenFile('goldens/app_button_disabled.png'));
  });

  testWidgets('catalog golden', (tester) async {
    await tester.pumpWidget(MaterialApp(theme: ThemeData.light(), home: Scaffold(
      body: RepaintBoundary(child: SizedBox(width: 250, child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(mainAxisSize: MainAxisSize.min, children: [
          for (final v in AppButtonVariant.values) ...[
            AppButton(label: v.name.toUpperCase(), variant: v, onPressed: () {}),
            const SizedBox(height: 12),
          ],
        ]),
      ))),
    )));
    await expectLater(find.byType(Scaffold), matchesGoldenFile('goldens/app_button_catalog.png'));
  });
}
```

### Common Mistakes

**Using `pumpAndSettle` with spinners.** `CircularProgressIndicator` animates forever. `pumpAndSettle` times out after 10 seconds.

**Running goldens on different platforms.** Font rendering differs across OSes. Pin CI to a specific platform and only update goldens from that environment.

---

## Exercise 6 -- Testing Forms and Navigation

### Progressive Hints

1. Create `MockNavigatorObserver extends Mock implements NavigatorObserver {}` and pass to `MaterialApp(navigatorObservers:)`.
2. Register `FakeRoute` as fallback for `any()` in route-based verifications.
3. For SnackBar, ensure a `Scaffold` ancestor exists. Use `pumpAndSettle` after the async error.

### Full Solution

```dart
// test/screens/registration_screen_test.dart
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:mocktail/mocktail.dart';
import 'package:my_app/repositories/auth_repository.dart';
import 'package:my_app/screens/registration_screen.dart';
import 'package:my_app/screens/home_screen.dart';

class MockAuthRepository extends Mock implements AuthRepository {}
class MockNavigatorObserver extends Mock implements NavigatorObserver {}
class FakeRoute extends Fake implements Route<dynamic> {}

void main() {
  late MockAuthRepository mockRepo;
  late MockNavigatorObserver mockNav;

  setUpAll(() => registerFallbackValue(FakeRoute()));
  setUp(() { mockRepo = MockAuthRepository(); mockNav = MockNavigatorObserver(); });

  Future<void> pumpRegistration(WidgetTester tester) async {
    await tester.pumpWidget(MaterialApp(
      home: RegistrationScreen(repository: mockRepo),
      navigatorObservers: [mockNav],
      routes: {'/home': (_) => const HomeScreen()},
    ));
  }

  testWidgets('shows validation errors on empty submit', (tester) async {
    await pumpRegistration(tester);
    await tester.tap(find.byKey(const Key('next_button')));
    await tester.pump();
    expect(find.text('Email is required'), findsOneWidget);
  });

  testWidgets('advances to step 2 with valid input', (tester) async {
    await pumpRegistration(tester);
    await tester.enterText(find.byKey(const Key('email_field')), 'a@b.com');
    await tester.enterText(find.byKey(const Key('password_field')), 'pass1!');
    await tester.tap(find.byKey(const Key('next_button')));
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('name_field')), findsOneWidget);
  });

  testWidgets('successful registration navigates to home', (tester) async {
    when(() => mockRepo.register(any(), any(), any()))
        .thenAnswer((_) async => User(id: '1', name: 'Test'));
    await pumpRegistration(tester);

    // Step 1
    await tester.enterText(find.byKey(const Key('email_field')), 'a@b.com');
    await tester.enterText(find.byKey(const Key('password_field')), 'pass1!');
    await tester.tap(find.byKey(const Key('next_button')));
    await tester.pumpAndSettle();
    // Step 2
    await tester.enterText(find.byKey(const Key('name_field')), 'Test User');
    await tester.tap(find.byKey(const Key('next_button')));
    await tester.pumpAndSettle();
    // Step 3: Submit
    await tester.tap(find.byKey(const Key('submit_button')));
    await tester.pumpAndSettle();

    verify(() => mockRepo.register('a@b.com', 'pass1!', 'Test User')).called(1);
    expect(find.byType(HomeScreen), findsOneWidget);
  });

  testWidgets('failed registration shows error SnackBar', (tester) async {
    when(() => mockRepo.register(any(), any(), any()))
        .thenThrow(Exception('Email already exists'));
    await pumpRegistration(tester);

    // Navigate through all steps (abbreviated)
    await tester.enterText(find.byKey(const Key('email_field')), 'a@b.com');
    await tester.enterText(find.byKey(const Key('password_field')), 'pass1!');
    await tester.tap(find.byKey(const Key('next_button')));
    await tester.pumpAndSettle();
    await tester.enterText(find.byKey(const Key('name_field')), 'Test');
    await tester.tap(find.byKey(const Key('next_button')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('submit_button')));
    await tester.pumpAndSettle();

    expect(find.byType(SnackBar), findsOneWidget);
    expect(find.byType(HomeScreen), findsNothing);
  });
}
```

### Common Mistakes

**Asserting navigation too early.** After tapping "Submit", the repository call is async. Assert `find.byType(HomeScreen)` only after `pumpAndSettle`.

### Debugging Tips

If a finder returns `findsNothing` unexpectedly, call `debugDumpApp()` inside the test to print the entire widget tree. If a SnackBar is not found, ensure the test widget tree has a `Scaffold` ancestor for it to attach to.

---

## Exercise 7 -- Complete Testing Architecture

### Progressive Hints

1. Start with infrastructure before writing tests. The `pump_app.dart` helper should accept optional provider parameters.
2. Factories use named parameters with defaults so you override only what matters: `createProduct(price: 0)`.
3. Custom matchers: `isA<T>().having(...)` wrapped in a named variable reads better than inline chains.
4. For CI, use `flutter test --coverage` in GitHub Actions. Parse `lcov.info` to enforce thresholds.

### Key Solution Files

```dart
// test/helpers/pump_app.dart
extension PumpApp on WidgetTester {
  Future<void> pumpApp(Widget widget, {ThemeData? theme, List<BlocProvider>? providers}) async {
    Widget app = MaterialApp(theme: theme ?? ThemeData.light(), home: Scaffold(body: widget));
    if (providers != null) app = MultiBlocProvider(providers: providers, child: app);
    await pumpWidget(app);
  }
}
```

```dart
// test/factories/product_factory.dart
class ProductFactory {
  static int _counter = 0;
  static Product create({String? id, String? name, double price = 9.99, bool inStock = true}) {
    _counter++;
    return Product(id: id ?? 'product-$_counter', name: name ?? 'Product $_counter',
        category: 'general', price: price, inStock: inStock);
  }
  static Product outOfStock() => create(inStock: false);
  static Product onSale() => create(price: 50.0);
}
```

```dart
// test/matchers/custom_matchers.dart
final isLoadingState = isA<ProductListLoading>();
Matcher hasErrorMessage(String msg) =>
    isA<ProductListError>().having((s) => s.message, 'message', contains(msg));
Matcher hasProductCount(int count) =>
    isA<ProductListLoaded>().having((s) => s.products.length, 'count', count);
```

```yaml
# .github/workflows/flutter_test.yml
name: Flutter Tests
on: [pull_request, push]
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: subosito/flutter-action@v2
        with: { flutter-version: '3.22.0' }
      - run: flutter pub get
      - run: dart analyze --fatal-infos
      - run: flutter test --coverage
      - run: |
          sudo apt-get install -y lcov
          TOTAL=$(lcov --summary coverage/lcov.info 2>&1 | grep 'lines' | grep -oP '[\d.]+(?=%)')
          if (( $(echo "$TOTAL < 80" | bc -l) )); then echo "Coverage $TOTAL% below 80%"; exit 1; fi
      - run: flutter test test/golden/
```

### Common Mistakes

**Single mock file for everything.** Group mocks by domain: `test/mocks/auth_mocks.dart`, `test/mocks/product_mocks.dart`. A single file becomes a bottleneck.

**CI golden mismatch.** Developers generate goldens on macOS, CI runs Ubuntu -- every golden test fails. Pin golden generation to the CI environment.

---

## Exercise 8 -- BDD Test Framework Extension

### Progressive Hints

1. `feature` maps to `group`. `scenario` maps to `test`. `given`/`when`/`then` register closures executed in sequence inside one `test` call.
2. For reports, use `tearDownAll` to write accumulated results to a file.
3. Property testing: use `Random` with a fixed seed for reproducibility. Test `decode(encode(x)) == x`.
4. Deliberately introduce a truncation bug (e.g., rounding price to 2 decimals in `toJson`) and confirm the property test catches it.

### Key Solution Files

```dart
// test/framework/bdd.dart
import 'package:flutter_test/flutter_test.dart';
import 'dart:io';

final _report = <_FeatureReport>[];
_FeatureReport? _currentFeature;
List<void Function()> _givens = [], _whens = [], _thens = [];

class _FeatureReport {
  final String name;
  final List<_ScenarioReport> scenarios = [];
  _FeatureReport(this.name);
}

class _ScenarioReport {
  final String name;
  bool passed = true;
  String? error;
  _ScenarioReport(this.name);
}

void feature(String description, void Function() body) {
  final report = _FeatureReport(description);
  _report.add(report);
  group('Feature: $description', () {
    _currentFeature = report;
    body();
    tearDownAll(() => _writeReport());
  });
}

void scenario(String description, void Function() body) {
  final scenarioReport = _ScenarioReport(description);
  _currentFeature?.scenarios.add(scenarioReport);
  test('Scenario: $description', () {
    _givens = []; _whens = []; _thens = [];
    body();
    try {
      for (final cb in _givens) cb();
      for (final cb in _whens) cb();
      for (final cb in _thens) cb();
    } catch (e) {
      scenarioReport.passed = false;
      scenarioReport.error = e.toString();
      rethrow;
    }
  });
}

void given(String desc, void Function() body) => _givens.add(body);
void when(String desc, void Function() body) => _whens.add(body);
void then(String desc, void Function() body) => _thens.add(body);

void _writeReport() {
  final buf = StringBuffer()..writeln('Test Report - ${DateTime.now().toIso8601String()}');
  for (final f in _report) {
    buf.writeln('\nFeature: ${f.name}');
    for (final s in f.scenarios) {
      buf.writeln('  [${s.passed ? "PASS" : "FAIL"}] Scenario: ${s.name}');
      if (s.error != null) buf.writeln('    Error: ${s.error}');
    }
  }
  final dir = Directory('test/reports');
  if (!dir.existsSync()) dir.createSync(recursive: true);
  File('test/reports/bdd_report.txt').writeAsStringSync(buf.toString());
}
```

```dart
// test/framework/property_test.dart
import 'dart:math';
import 'package:flutter_test/flutter_test.dart';

void forAll<T>({required String description, required T Function(Random) generator,
    required bool Function(T) property, int iterations = 1000, int seed = 42}) {
  test(description, () {
    final random = Random(seed);
    for (var i = 0; i < iterations; i++) {
      final input = generator(random);
      if (!property(input)) fail('Property violated on iteration $i with input: $input');
    }
  });
}

String randomString(Random r, {int maxLength = 50}) {
  final len = r.nextInt(maxLength);
  return String.fromCharCodes(List.generate(len, (_) =>
      [r.nextInt(95) + 32, r.nextInt(0x4E00 - 0x3000) + 0x3000][r.nextInt(2)]));
}
```

```dart
// test/features/cart_feature_test.dart
import 'package:flutter_test/flutter_test.dart';
import 'package:my_app/models/cart.dart';
import 'package:my_app/models/product.dart';
import '../framework/bdd.dart';

void main() {
  late Cart cart;

  feature('Shopping Cart', () {
    scenario('Adding items to cart', () {
      given('an empty cart', () => cart = Cart());
      when('a product is added', () =>
          cart.addItem(Product(id: '1', name: 'Widget', price: 25.0)));
      then('the cart contains one item', () => expect(cart.items.length, 1));
      then('the total equals the product price', () =>
          expect(cart.total, closeTo(25.0, 0.01)));
    });

    scenario('Applying a discount coupon', () {
      given('a cart with items totaling \$100', () {
        cart = Cart();
        cart.addItem(Product(id: '1', name: 'A', price: 60.0));
        cart.addItem(Product(id: '2', name: 'B', price: 40.0));
      });
      when('a 20% coupon is applied', () =>
          cart.applyCoupon(Coupon(code: 'SAVE20', discountPercent: 20)));
      then('the total is \$80', () => expect(cart.total, closeTo(80.0, 0.01)));
    });
  });
}
```

### Common Mistakes

**BDD callbacks not executing in order.** `given`/`when`/`then` register closures, they do not execute immediately. Assertions outside `then` run during registration, not execution.

**Property tests without fixed seeds.** Without a seed, failing tests cannot be reproduced. Always pass a fixed seed and print the failing input in the error message.

### Alternative Approaches

- **BDD**: the `bdd_framework` pub package provides mature given/when/then with async support
- **Property testing**: the `glados` package provides automatic shrinking (minimal failing input)
- **Visual regression**: the `alchemist` package generates scenario matrices from a single test definition

---

## General Debugging Tips

**Test passes locally, fails in CI.** Check: golden platform mismatch, timezone-dependent assertions, test execution order dependencies.

**`pumpAndSettle` times out.** Something animates forever. Replace with `pump(Duration(...))` and assert intermediate state.

**Mock returns null.** You forgot to stub the method. Unstubbed `Mock` methods return null for nullable types and throw `MissingStubError` for non-nullable.

**`find.text` returns `findsNothing`.** The text might be inside `EditableText` within a `TextField`. Use `find.byKey` instead, or `find.byWidgetPredicate` for more control.

**Coverage low despite many tests.** If everything is mocked, real implementations have zero coverage. Balance mocks with actual implementations where safe.
