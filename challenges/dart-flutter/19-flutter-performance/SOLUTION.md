# Section 19 -- Solutions: Flutter Performance

## How to Use This File

Do not jump straight to the full solutions. Performance optimization is a skill built through practice, and copying an answer teaches you nothing about finding bottlenecks in your own code.

For each exercise:
1. Attempt it yourself first, using DevTools to guide your decisions
2. If stuck, read the **Progressive Hints** -- each hint reveals a little more without giving away the solution
3. Check **Common Mistakes** to see if you fell into a known trap
4. Only then read the **Full Solution** and compare your approach
5. Read the **Deep Dive** to understand the underlying mechanics

---

## Exercise 1 -- Const Constructor Audit

### Progressive Hints

**Hint 1**: Start from the bottom of the widget tree and work up. Leaf widgets (those with no children that depend on state) are the easiest candidates for `const`.

**Hint 2**: A widget can only be `const` if its constructor is marked `const` AND all arguments passed to it are compile-time constants. `EdgeInsets.all(16.0)` is const-eligible. A variable like `_count` is not.

**Hint 3**: The `MyApp`, `HeaderSection`, and `InfoCard` classes all need `const` constructors added to their class definitions before you can instantiate them with `const`.

**Hint 4**: Do not forget `super.key` in the const constructors. Also look at `TextStyle` -- it has a const constructor that most developers miss.

### Full Solution

```dart
// exercise_01_const_audit_solution.dart
import 'package:flutter/material.dart';

void main() => runApp(const MyApp());

class MyApp extends StatelessWidget {
  const MyApp({super.key});

  @override
  Widget build(BuildContext context) {
    return const MaterialApp(
      home: CounterPage(),
    );
  }
}

class CounterPage extends StatefulWidget {
  const CounterPage({super.key});

  @override
  State<CounterPage> createState() => _CounterPageState();
}

class _CounterPageState extends State<CounterPage> {
  int _count = 0;

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Const Audit'),
      ),
      body: Column(
        children: [
          const HeaderSection(),
          const InfoCard(
            title: 'Welcome',
            subtitle: 'This content never changes',
          ),
          // This is the ONLY widget that cannot be const -- it depends on _count
          Text('Taps: $_count'),
          const Padding(
            padding: EdgeInsets.all(16.0),
            child: Icon(Icons.star, size: 48, color: Colors.amber),
          ),
        ],
      ),
      floatingActionButton: FloatingActionButton(
        onPressed: () => setState(() => _count++),
        child: const Icon(Icons.add),
      ),
    );
  }
}

class HeaderSection extends StatelessWidget {
  const HeaderSection({super.key});

  @override
  Widget build(BuildContext context) {
    return const Padding(
      padding: EdgeInsets.all(20),
      child: Text(
        'Performance Lab',
        style: TextStyle(fontSize: 28, fontWeight: FontWeight.bold),
      ),
    );
  }
}

class InfoCard extends StatelessWidget {
  final String title;
  final String subtitle;

  const InfoCard({super.key, required this.title, required this.subtitle});

  @override
  Widget build(BuildContext context) {
    return Card(
      margin: const EdgeInsets.all(12),
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          children: [
            Text(title, style: const TextStyle(fontSize: 18)),
            const SizedBox(height: 8),
            Text(subtitle),
          ],
        ),
      ),
    );
  }
}
```

### Common Mistakes

**Mistake 1: Adding const to the `Text('Taps: $_count')` widget.** String interpolation with a variable is not a compile-time constant. This will cause a compile error, not a runtime error -- Dart catches it for you.

**Mistake 2: Forgetting to add `const` to the constructor declaration.** Writing `const HeaderSection()` at the call site does nothing if the `HeaderSection` class does not have a `const` constructor. You need both: `const HeaderSection({super.key})` in the class AND `const HeaderSection()` at the instantiation site.

**Mistake 3: Missing const on `EdgeInsets`, `TextStyle`, and `SizedBox`.** These are commonly overlooked because they feel like "infrastructure" rather than "widgets," but they are objects that get recreated on every build if not const.

### Deep Dive

When Flutter encounters a `const` widget during rebuild, it performs an identity check (`identical()`) against the previous widget. If they are the same object in memory -- which const guarantees -- Flutter skips the entire subtree. No `build()` call, no element update, no render object reconfiguration. This is why const is so powerful: it turns an O(n) tree walk into an O(1) identity check.

The Widget Inspector's rebuild count confirms this visually. Widgets marked const show zero rebuilds after initial construction, while non-const siblings increment their count on every `setState`.

---

## Exercise 2 -- ListView.builder Migration

### Progressive Hints

**Hint 1**: The core change is replacing `ListView(children: [...])` with `ListView.builder(itemBuilder: ...)`. The builder callback receives an index and returns a single widget -- it is only called for visible items.

**Hint 2**: Since every item is `Container(height: 60, ...)`, you know the exact item height. Set `itemExtent: 60` to skip per-item measurement during layout.

**Hint 3**: For items that are cheap to rebuild (just a Container with Text), `addAutomaticKeepAlives: false` saves memory by letting items be garbage collected when they scroll off-screen.

### Full Solution

```dart
// exercise_02_listview_migration_solution.dart
import 'package:flutter/material.dart';

void main() => runApp(const ListApp());

class ListApp extends StatelessWidget {
  const ListApp({super.key});

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      home: ProductListScreen(
        products: List.generate(
          10000,
          (i) => 'Product ${i + 1} - \$${(i * 1.5 + 0.99).toStringAsFixed(2)}',
        ),
      ),
    );
  }
}

class ProductListScreen extends StatelessWidget {
  final List<String> products;

  const ProductListScreen({super.key, required this.products});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Products')),
      body: ListView.builder(
        itemCount: products.length,
        // Every item is exactly 60 pixels tall.
        // This lets Flutter calculate scroll position without measuring
        // each item, which is a significant win for 10,000 items.
        itemExtent: 60,
        // Items are cheap to rebuild (just a Container with Text).
        // No need to keep them alive in memory when off-screen.
        addAutomaticKeepAlives: false,
        itemBuilder: (context, index) {
          return Container(
            height: 60,
            padding: const EdgeInsets.symmetric(horizontal: 16),
            decoration: BoxDecoration(
              border: Border(
                bottom: BorderSide(color: Colors.grey[300]!),
              ),
            ),
            child: Align(
              alignment: Alignment.centerLeft,
              child: Text(products[index]),
            ),
          );
        },
      ),
    );
  }
}
```

### Common Mistakes

**Mistake 1: Forgetting `itemCount`.** Without it, ListView.builder does not know the total number of items and cannot display a proper scrollbar or handle scroll-to-end correctly.

**Mistake 2: Setting `itemExtent` when items have variable heights.** If your items are not all the same height, `itemExtent` forces them to that height, clipping or adding empty space. Only use it for uniform lists.

**Mistake 3: Leaving `addAutomaticKeepAlives: true` (the default) for simple items.** KeepAlive wraps each item in an AutomaticKeepAlive widget that prevents disposal when scrolling off-screen. For expensive items (items with network images, video players), this avoids re-fetching. For cheap items (text), it just wastes memory.

### Deep Dive

The naive `ListView(children: [...])` calls `.toList()` on all 10,000 items before the first frame renders. This means 10,000 `Container` widgets, 10,000 `Text` widgets, and all their render objects are allocated simultaneously. On a low-end device with 2GB of RAM, this can trigger garbage collection pauses and even out-of-memory crashes.

`ListView.builder` creates a `SliverList` backed by a `SliverChildBuilderDelegate`. This delegate builds items lazily -- only when they enter the viewport plus the `cacheExtent` region. Typically, only 10-15 items exist in memory at any time. When items scroll out of the cache region, their elements are deactivated and their widgets become eligible for garbage collection.

The `itemExtent` optimization matters because without it, Flutter must perform a binary search to find which item corresponds to a given scroll offset. With a known item extent, it is simple division: `index = offset / itemExtent`. This makes jump-to-index, scrollbar dragging, and scroll position estimation all O(1).

---

## Exercise 3 -- RepaintBoundary Placement

### Progressive Hints

**Hint 1**: The dynamic content is the Row containing the "Active Users" and "Revenue" cards. The static content is the header card and the navigation row. You need a RepaintBoundary between them.

**Hint 2**: Wrapping the dynamic Row in a RepaintBoundary isolates its repaints. But also consider: should each metric card be its own RepaintBoundary? Only if they update independently.

**Hint 3**: For step 5 (over-optimization), wrap every single Card in its own RepaintBoundary and check the layer count in DevTools. You should see a jump from 3-4 layers to 8+, with no visual improvement.

### Full Solution

```dart
// exercise_03_repaint_boundary_solution.dart
import 'dart:async';
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';

void main() {
  debugRepaintRainbowEnabled = true;
  runApp(const DashboardApp());
}

class DashboardApp extends StatelessWidget {
  const DashboardApp({super.key});

  @override
  Widget build(BuildContext context) {
    return const MaterialApp(home: DashboardScreen());
  }
}

class DashboardScreen extends StatefulWidget {
  const DashboardScreen({super.key});

  @override
  State<DashboardScreen> createState() => _DashboardScreenState();
}

class _DashboardScreenState extends State<DashboardScreen> {
  int _activeUsers = 142;
  double _revenue = 8943.50;
  Timer? _timer;

  @override
  void initState() {
    super.initState();
    _timer = Timer.periodic(const Duration(seconds: 1), (_) {
      setState(() {
        _activeUsers += (DateTime.now().second % 3) - 1;
        _revenue += 12.50;
      });
    });
  }

  @override
  void dispose() {
    _timer?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Live Dashboard')),
      body: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            // Static content: no RepaintBoundary needed here because
            // the boundary below isolates it from the dynamic section.
            const Card(
              child: Padding(
                padding: EdgeInsets.all(16),
                child: Column(
                  children: [
                    Text('Acme Corp', style: TextStyle(fontSize: 24)),
                    Text('Q1 2026 Dashboard'),
                  ],
                ),
              ),
            ),
            const SizedBox(height: 16),

            // RepaintBoundary isolates the dynamic metrics row.
            // Without this, the timer-driven setState causes the
            // ENTIRE Column to repaint every second.
            RepaintBoundary(
              child: Row(
                children: [
                  Expanded(
                    child: Card(
                      color: Colors.blue[50],
                      child: Padding(
                        padding: const EdgeInsets.all(16),
                        child: Column(
                          children: [
                            const Text('Active Users'),
                            Text(
                              '$_activeUsers',
                              style: const TextStyle(fontSize: 32),
                            ),
                          ],
                        ),
                      ),
                    ),
                  ),
                  const SizedBox(width: 16),
                  Expanded(
                    child: Card(
                      color: Colors.green[50],
                      child: Padding(
                        padding: const EdgeInsets.all(16),
                        child: Column(
                          children: [
                            const Text('Revenue'),
                            Text(
                              '\$${_revenue.toStringAsFixed(2)}',
                              style: const TextStyle(fontSize: 32),
                            ),
                          ],
                        ),
                      ),
                    ),
                  ),
                ],
              ),
            ),
            const SizedBox(height: 16),

            // Static navigation cards -- isolated from repaints above
            const Expanded(
              child: Row(
                children: [
                  Expanded(child: Card(child: Center(child: Text('Reports')))),
                  SizedBox(width: 16),
                  Expanded(child: Card(child: Center(child: Text('Settings')))),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}
```

### Common Mistakes

**Mistake 1: Wrapping every widget in RepaintBoundary.** Each RepaintBoundary creates a new compositing layer. Layers consume GPU memory and increase compositing time. On this simple dashboard, you need exactly one boundary -- around the dynamic section. Adding five more buys nothing and costs resources.

**Mistake 2: Putting the RepaintBoundary around the static content instead of the dynamic content.** Either placement works for isolation, but wrapping the smaller dynamic region is more efficient: the new layer is smaller and cheaper to composite.

**Mistake 3: Not verifying with the repaint rainbow.** The entire point of this exercise is evidence-based optimization. If you add RepaintBoundary and do not check the rainbow, you are guessing. Enable `debugRepaintRainbowEnabled = true` and visually confirm that only the metrics row changes color each second.

### Debugging Tip

If the repaint rainbow shows that a section is still repainting when you expected it to be isolated, check for `Opacity` or `ClipRRect` widgets between your RepaintBoundary and the static content. These widgets create their own layers and can interfere with boundary isolation. Replace `Opacity` with `AnimatedOpacity` or use `FadeTransition` for animated opacity.

---

## Exercise 4 -- DevTools Profiling Session

### Progressive Hints

**Hint 1**: The bottleneck is `_expensiveColorComputation`. It runs 50,000 iterations of `Random.nextDouble()` inside `build()` -- meaning it runs every time the item scrolls into view.

**Hint 2**: The fix is to precompute the colors. You can either compute them once at startup, or cache the result so each index is only computed once.

**Hint 3**: For the debug vs profile comparison, you should see roughly 2-5x faster frame times in profile mode. Debug mode adds assertion checks, disables tree shaking, and runs interpreted Dart. Document the exact numbers.

### Full Solution

```dart
// exercise_04_profiling_session_solution.dart
//
// Optimization Log:
// ---------------------------------------------------------------
// BEFORE (profile mode):
//   Average frame time: ~35ms
//   Worst frame time: ~80ms
//   Jank frames: ~60% during scroll
//
// Debug mode was approximately 3x slower (not meaningful for perf work).
//
// Bottleneck: _expensiveColorComputation called during build() for every
// visible item on every frame. CPU profiler showed 92% of frame time
// spent in Random.nextDouble() calls inside this method.
//
// Fix: precompute all colors once at startup. Color computation moved
// from O(n * 50000) per frame to O(n * 50000) total, once.
//
// AFTER (profile mode):
//   Average frame time: ~4ms
//   Worst frame time: ~8ms
//   Jank frames: 0% during scroll
// ---------------------------------------------------------------

import 'dart:math';
import 'package:flutter/material.dart';

void main() => runApp(const ProfilingApp());

class ProfilingApp extends StatelessWidget {
  const ProfilingApp({super.key});

  @override
  Widget build(BuildContext context) {
    return const MaterialApp(
      debugShowCheckedModeBanner: false,
      home: OptimizedListScreen(),
    );
  }
}

class OptimizedListScreen extends StatefulWidget {
  const OptimizedListScreen({super.key});

  @override
  State<OptimizedListScreen> createState() => _OptimizedListScreenState();
}

class _OptimizedListScreenState extends State<OptimizedListScreen> {
  // Precompute all 500 colors ONCE during initState.
  // This moves the expensive work from "every frame" to "app startup."
  late final List<Color> _precomputedColors;

  @override
  void initState() {
    super.initState();
    _precomputedColors = List.generate(500, _computeColor);
  }

  static Color _computeColor(int seed) {
    // Simplified: we only need the final random values, not 50,000 of them.
    // The original loop was pure waste -- it overwrote r, g, b on every
    // iteration and only used the last values.
    final random = Random(seed);
    final r = (random.nextDouble() * 255).toInt();
    final g = (random.nextDouble() * 255).toInt();
    final b = (random.nextDouble() * 255).toInt();
    return Color.fromRGBO(r, g, b, 1.0);
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Profiling Target (Optimized)')),
      body: ListView.builder(
        itemCount: 500,
        // All items are 80px + 8px margin = 88px total
        itemExtent: 88,
        itemBuilder: (context, index) {
          final color = _precomputedColors[index];
          return Container(
            height: 80,
            margin: const EdgeInsets.all(4),
            decoration: BoxDecoration(
              color: color,
              borderRadius: BorderRadius.circular(8),
              boxShadow: [
                BoxShadow(
                  color: color.withValues(alpha: 0.3),
                  blurRadius: 8,
                  offset: const Offset(0, 2),
                ),
              ],
            ),
            child: Center(
              child: Text(
                'Item $index',
                style: const TextStyle(
                  color: Colors.white,
                  fontSize: 18,
                  fontWeight: FontWeight.bold,
                ),
              ),
            ),
          );
        },
      ),
    );
  }
}
```

### Common Mistakes

**Mistake 1: Profiling in debug mode and concluding the app is slow.** Debug mode frame times are 2-5x slower than profile mode. If you optimize based on debug numbers, you might "fix" something that was never a problem in production. Always switch to `flutter run --profile` before measuring.

**Mistake 2: Caching colors in a Map inside build().** Creating a cache inside `build()` means a new Map is allocated on every rebuild. The cache must live in State (via `initState`) or be static.

**Mistake 3: Not recognizing that the 50,000-iteration loop was wasteful by design.** The loop overwrites r, g, b on every iteration. Only the final iteration's values matter. The fix is not just "move it out of build" but "realize you only need 3 random numbers, not 150,000."

### Deep Dive

The CPU Profiler in DevTools shows a call tree or bottom-up view. For this exercise, the bottom-up view immediately reveals that `Random.nextDouble()` consumes over 90% of frame CPU time. The call tree shows it is called from `_expensiveColorComputation`, called from `build()` of `SlowListItem`.

This is the profiling workflow you should internalize:
1. Performance tab shows *which frames* are slow (red bars)
2. Click a slow frame and switch to CPU Profiler to see *what function* is slow
3. Bottom-up view tells you the most expensive leaf functions
4. Call tree tells you the path from the framework down to the bottleneck

---

## Exercise 5 -- Shader Jank Elimination

### Progressive Hints

**Hint 1**: The key command is `flutter run --profile --cache-sksl --purge-persistent-cache`. The `--purge-persistent-cache` flag ensures you start from a clean slate so all shaders get captured fresh.

**Hint 2**: You must manually trigger every animation and transition. Navigate to each page, go back, navigate again. The capture only records shaders that actually compile during the session.

**Hint 3**: After pressing `M`, the terminal tells you where the `.sksl.json` file was saved. Use that exact path in the build command.

### Full Solution

This exercise is primarily a workflow exercise rather than a code change. The starter code is already the final code -- the solution is the process:

```bash
# Step 1: Run with shader capture enabled
flutter run --profile --cache-sksl --purge-persistent-cache

# Step 2: In the running app, trigger every visual effect:
#   - Tap "Slide Transition Page", go back
#   - Tap "Fade Transition Page", go back
#   - Tap "Scale Transition Page" (triggers animated grid), go back
#   - Repeat each navigation to ensure all shader variants are captured

# Step 3: In the terminal, press M to save captured shaders
#   Output: "Written SkSL data to flutter_01.sksl.json"

# Step 4: Build with bundled shaders
flutter build apk --bundle-sksl-path flutter_01.sksl.json

# Step 5: Install and profile the built APK
flutter install
flutter run --profile  # connect DevTools to the installed build
```

### Common Mistakes

**Mistake 1: Capturing shaders in debug mode.** Debug mode uses a different rendering path. Shaders captured in debug may not match what release/profile mode needs. Always capture in profile mode.

**Mistake 2: Not triggering all animations.** If you skip a transition during capture, its shaders will not be in the bundle, and users will still experience jank on that specific transition.

**Mistake 3: Expecting shader warmup to fix ALL first-frame jank.** SkSL caching only addresses shader compilation jank. If your first frame is slow because of expensive build methods or large image decoding, shader warmup will not help. Profile to distinguish shader jank (shows as GPU thread spikes) from framework jank (shows as UI thread spikes).

### Debugging Tip

To confirm that shader compilation is the source of jank and not something else, look at the Performance tab in DevTools. Shader compilation jank appears as spikes in the **GPU thread** (Raster thread), not the UI thread. If you see the UI thread spiking instead, the problem is in your Dart code, not shaders.

---

## Exercise 6 -- Deferred Loading Architecture

### Progressive Hints

**Hint 1**: Each feature file needs a deferred import: `import 'feature_analytics.dart' deferred as analytics;`. Then call `analytics.loadLibrary()` before accessing any symbol from that library.

**Hint 2**: Use a `FutureBuilder` to show a loading indicator while `loadLibrary()` completes and an error widget if it fails.

**Hint 3**: For the preloading strategy, use `WidgetsBinding.instance.addPostFrameCallback` to start loading the most popular feature after the home screen's first frame renders.

### Full Solution

```dart
// exercise_06_deferred_loading_solution.dart
import 'dart:developer' as developer;
import 'package:flutter/material.dart';

// Deferred imports -- each feature loads independently
import 'feature_analytics.dart' deferred as analytics;
import 'feature_reports.dart' deferred as reports;
import 'feature_settings.dart' deferred as settings;

void main() {
  developer.Timeline.startSync('app_startup');
  runApp(const DeferredApp());
}

class DeferredApp extends StatelessWidget {
  const DeferredApp({super.key});

  @override
  Widget build(BuildContext context) {
    developer.Timeline.finishSync();
    return const MaterialApp(home: MainShell());
  }
}

class MainShell extends StatefulWidget {
  const MainShell({super.key});

  @override
  State<MainShell> createState() => _MainShellState();
}

class _MainShellState extends State<MainShell> {
  int _selectedIndex = 0;

  @override
  void initState() {
    super.initState();
    // Preload the most likely next feature after home renders.
    // This runs after the first frame so it does not block startup.
    WidgetsBinding.instance.addPostFrameCallback((_) {
      analytics.loadLibrary().catchError((_) {});
    });
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: IndexedStack(
        index: _selectedIndex,
        children: [
          const Center(child: Text('Home - always loaded')),
          _DeferredPage(
            loader: analytics.loadLibrary,
            builder: () => analytics.AnalyticsPage(),
            label: 'Analytics',
          ),
          _DeferredPage(
            loader: reports.loadLibrary,
            builder: () => reports.ReportsPage(),
            label: 'Reports',
          ),
          _DeferredPage(
            loader: settings.loadLibrary,
            builder: () => settings.SettingsPage(),
            label: 'Settings',
          ),
        ],
      ),
      bottomNavigationBar: NavigationBar(
        selectedIndex: _selectedIndex,
        onDestinationSelected: (index) {
          setState(() => _selectedIndex = index);
        },
        destinations: const [
          NavigationDestination(icon: Icon(Icons.home), label: 'Home'),
          NavigationDestination(icon: Icon(Icons.analytics), label: 'Analytics'),
          NavigationDestination(icon: Icon(Icons.description), label: 'Reports'),
          NavigationDestination(icon: Icon(Icons.settings), label: 'Settings'),
        ],
      ),
    );
  }
}

// Reusable widget for loading deferred libraries with error handling
class _DeferredPage extends StatefulWidget {
  final Future<dynamic> Function() loader;
  final Widget Function() builder;
  final String label;

  const _DeferredPage({
    required this.loader,
    required this.builder,
    required this.label,
  });

  @override
  State<_DeferredPage> createState() => _DeferredPageState();
}

class _DeferredPageState extends State<_DeferredPage> {
  late Future<void> _loadFuture;

  @override
  void initState() {
    super.initState();
    _loadFuture = widget.loader();
  }

  void _retry() {
    setState(() {
      _loadFuture = widget.loader();
    });
  }

  @override
  Widget build(BuildContext context) {
    return FutureBuilder(
      future: _loadFuture,
      builder: (context, snapshot) {
        if (snapshot.connectionState == ConnectionState.waiting) {
          return Center(
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                const CircularProgressIndicator(),
                const SizedBox(height: 16),
                Text('Loading ${widget.label}...'),
              ],
            ),
          );
        }

        if (snapshot.hasError) {
          return Center(
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                const Icon(Icons.error_outline, size: 48, color: Colors.red),
                const SizedBox(height: 16),
                Text('Failed to load ${widget.label}'),
                const SizedBox(height: 8),
                Text(
                  snapshot.error.toString(),
                  style: Theme.of(context).textTheme.bodySmall,
                  textAlign: TextAlign.center,
                ),
                const SizedBox(height: 16),
                ElevatedButton(
                  onPressed: _retry,
                  child: const Text('Retry'),
                ),
              ],
            ),
          );
        }

        return widget.builder();
      },
    );
  }
}
```

### Common Mistakes

**Mistake 1: Calling `loadLibrary()` inside `build()`.** This triggers a new future on every rebuild, potentially re-downloading the library. Load once in `initState` and store the future.

**Mistake 2: Not handling the error case.** On Flutter web, deferred libraries are loaded over the network. Network failures are common. Without error handling and a retry mechanism, the user sees a permanent loading spinner.

**Mistake 3: Using `IndexedStack` with all deferred pages.** IndexedStack builds all its children, even hidden ones. This means all deferred loads trigger immediately on first build. For true on-demand loading, consider replacing IndexedStack with a single child that switches based on the index. The IndexedStack approach works here because the `_DeferredPage` widget handles its own loading -- it builds immediately but shows a loader until the library arrives.

### Alternative Approach

Instead of `_DeferredPage` as a widget, you could use a route-based approach where each deferred feature is a separate route. This integrates with Flutter's navigation system and works well with `GoRouter` or `AutoRoute`:

```dart
// Alternative: route-based deferred loading
GoRoute(
  path: '/analytics',
  builder: (context, state) => FutureBuilder(
    future: analytics.loadLibrary(),
    builder: (context, snapshot) {
      if (snapshot.connectionState == ConnectionState.done) {
        return analytics.AnalyticsPage();
      }
      return const LoadingScreen();
    },
  ),
),
```

---

## Exercise 7 -- Full Performance Audit

### Progressive Hints

**Hint 1**: Start by listing all anti-patterns before fixing anything. There are at least 11 in the starter code. Number them in your optimization log.

**Hint 2**: Fix them one at a time and profile after each fix. Some fixes have massive impact (switching to ListView.builder) and some have minor impact (adding const to EdgeInsets). Document the delta.

**Hint 3**: The expensive color computation is doubly wasteful: the loop runs 10,000 iterations but only uses the last value, AND it runs inside a setState callback that blocks the UI thread.

**Hint 4**: The Opacity widget with 0.95 opacity triggers a saveLayer call for every single list item. Replace it with nothing (0.95 is visually indistinguishable from 1.0) or use `color.withValues(alpha: 0.95)` on individual elements if transparency is required.

### Full Solution

```dart
// exercise_07_full_audit_solution.dart
//
// OPTIMIZATION LOG
// ================================================================
//
// Baseline (profile mode, 30-second scroll):
//   Average frame time: ~45ms
//   Worst frame time: ~200ms
//   Jank frame %: ~75%
//   Memory high watermark: ~320MB
//
// Fix 1: Add const constructors to BrokenApp, AppBar title, EdgeInsets
//   Impact: negligible (< 1ms) -- these are not in the hot path
//   Cumulative avg frame time: ~44ms
//
// Fix 2: Extract ThemeData to a static const (avoid recreation on build)
//   Impact: negligible (< 0.5ms) -- ThemeData is cached by MaterialApp anyway
//   Cumulative avg frame time: ~44ms
//
// Fix 3: Switch from ListView to ListView.builder
//   Impact: MAJOR -- initial build dropped from ~800ms to ~20ms
//   Scroll jank reduced from 75% to ~40%
//   Cumulative avg frame time: ~22ms
//
// Fix 4: Simplify _expensiveRandomColor (3 random calls instead of 30,000)
//   Impact: MAJOR -- per-item build time dropped from ~8ms to < 0.1ms
//   Cumulative avg frame time: ~8ms
//
// Fix 5: Remove Opacity(0.95) wrapper (eliminates saveLayer per item)
//   Impact: significant -- GPU raster time dropped ~40%
//   Cumulative avg frame time: ~5ms
//
// Fix 6: Add cacheWidth/cacheHeight to Image.network
//   Impact: moderate -- memory dropped from ~320MB to ~180MB
//   Cumulative avg frame time: ~5ms (memory improvement, not frame time)
//
// Fix 7: Truncate subtitle text to 2 lines with overflow ellipsis
//   Impact: moderate -- layout phase shortened for each item
//   Cumulative avg frame time: ~4ms
//
// Fix 8: Add itemExtent to ListView.builder
//   Impact: minor -- scroll position calculation faster
//   Cumulative avg frame time: ~4ms
//
// Fix 9: Dispose ScrollController in dispose()
//   Impact: prevents memory leak (no frame time impact)
//
// Fix 10: Precompute item data outside of setState
//   Impact: setState call dropped from ~50ms to < 1ms
//   Cumulative avg frame time: ~3.5ms
//
// Fix 11: Add const to all eligible widget constructors
//   Impact: minor but good practice
//   Cumulative avg frame time: ~3.5ms
//
// FINAL METRICS (profile mode, 30-second scroll):
//   Average frame time: ~3.5ms
//   Worst frame time: ~9ms
//   Jank frame %: 0%
//   Memory high watermark: ~140MB
// ================================================================

import 'dart:math';
import 'package:flutter/material.dart';

void main() => runApp(const OptimizedApp());

class OptimizedApp extends StatelessWidget {
  const OptimizedApp({super.key});

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      theme: ThemeData(
        colorScheme: ColorScheme.fromSeed(seedColor: Colors.deepPurple),
      ),
      home: const OptimizedHome(),
    );
  }
}

class OptimizedHome extends StatefulWidget {
  const OptimizedHome({super.key});

  @override
  State<OptimizedHome> createState() => _OptimizedHomeState();
}

class _OptimizedHomeState extends State<OptimizedHome> {
  final List<_ItemData> _items = [];
  final ScrollController _scrollController = ScrollController();
  bool _loading = false;

  @override
  void initState() {
    super.initState();
    _loadItems();
    _scrollController.addListener(_onScroll);
  }

  void _onScroll() {
    if (_scrollController.position.pixels >=
        _scrollController.position.maxScrollExtent - 200) {
      _loadItems();
    }
  }

  void _loadItems() {
    if (_loading) return;
    _loading = true;

    // Precompute data BEFORE calling setState.
    // setState should only flip the flag -- not do heavy work.
    final newItems = List.generate(50, (i) {
      final id = _items.length + i;
      return _ItemData(
        id: id,
        title: 'Item $id',
        subtitle: _generateLoremIpsum(20), // 20 words, not 200
        color: _quickColor(id),
        imageUrl: 'https://picsum.photos/seed/$id/400/400',
      );
    });

    setState(() {
      _items.addAll(newItems);
      _loading = false;
    });
  }

  static String _generateLoremIpsum(int words) {
    const lorem = 'lorem ipsum dolor sit amet consectetur adipiscing elit '
        'sed do eiusmod tempor incididunt ut labore et dolore magna aliqua';
    final loremWords = lorem.split(' ');
    final buffer = StringBuffer();
    final random = Random();
    for (var i = 0; i < words; i++) {
      if (i > 0) buffer.write(' ');
      buffer.write(loremWords[random.nextInt(loremWords.length)]);
    }
    return buffer.toString();
  }

  static Color _quickColor(int seed) {
    // 3 random calls instead of 30,000. Same visual result.
    final random = Random(seed);
    return Color.fromRGBO(
      (random.nextDouble() * 255).toInt(),
      (random.nextDouble() * 255).toInt(),
      (random.nextDouble() * 255).toInt(),
      1.0,
    );
  }

  @override
  void dispose() {
    _scrollController.removeListener(_onScroll);
    _scrollController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Optimized Feed')),
      body: ListView.builder(
        controller: _scrollController,
        itemCount: _items.length,
        // Approximate fixed height: image 200 + padding + text ~100 = ~300
        // Using itemExtent only if truly fixed; otherwise omit it.
        // In this case heights vary slightly, so we omit itemExtent.
        cacheExtent: 500,
        itemBuilder: (context, index) {
          return _OptimizedItem(data: _items[index]);
        },
      ),
    );
  }
}

class _ItemData {
  final int id;
  final String title;
  final String subtitle;
  final Color color;
  final String imageUrl;

  const _ItemData({
    required this.id,
    required this.title,
    required this.subtitle,
    required this.color,
    required this.imageUrl,
  });
}

class _OptimizedItem extends StatelessWidget {
  final _ItemData data;

  const _OptimizedItem({super.key, required this.data});

  @override
  Widget build(BuildContext context) {
    // No Opacity wrapper -- 0.95 is visually identical to 1.0
    // and Opacity triggers an expensive saveLayer call.
    return Padding(
      padding: const EdgeInsets.all(8),
      child: Card(
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Image.network(
              data.imageUrl,
              height: 200,
              width: double.infinity,
              fit: BoxFit.cover,
              // Decode at display size, not original 400x400
              cacheWidth: 400,
              cacheHeight: 200,
              errorBuilder: (context, error, stack) => Container(
                height: 200,
                color: Colors.grey[200],
                child: const Center(child: Icon(Icons.broken_image)),
              ),
            ),
            Padding(
              padding: const EdgeInsets.all(12),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    data.title,
                    style: TextStyle(
                      fontSize: 18,
                      fontWeight: FontWeight.bold,
                      color: data.color,
                    ),
                  ),
                  const SizedBox(height: 8),
                  Text(
                    data.subtitle,
                    maxLines: 2,
                    overflow: TextOverflow.ellipsis,
                  ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}
```

### Common Mistakes

**Mistake 1: Fixing all anti-patterns at once and measuring only the final result.** This teaches you nothing about which fix had which impact. The discipline of fix-one-measure-repeat is the core skill of performance work.

**Mistake 2: Premature const optimization.** Adding const to EdgeInsets and TextStyle is good practice, but it contributes less than 1ms per frame in this scenario. If you spend an hour on const constructors while ignoring the 50,000-iteration loop, you have optimized the wrong thing.

**Mistake 3: Keeping the 200-word subtitle and just adding `maxLines`.** The text is still generated and stored in memory even if only 2 lines display. Generating 20 words instead of 200 saves both CPU (generation time) and memory (string allocation). Always fix the source, not just the symptom.

---

## Exercise 8 -- Performance Monitoring SDK

### Progressive Hints

**Hint 1**: `SchedulerBinding.instance.addTimingsCallback` gives you a list of `FrameTiming` objects for each batch of rendered frames. Each `FrameTiming` has `buildDuration` and `rasterDuration`.

**Hint 2**: For percentile calculations, keep a sorted list of frame times. P50 is the value at index `length * 0.5`, P90 at `length * 0.9`, P99 at `length * 0.99`.

**Hint 3**: The overlay should use a `Timer.periodic` to refresh its display, not rebuild on every frame. Refreshing the overlay at 1Hz (once per second) is sufficient and avoids the overlay itself causing performance overhead.

**Hint 4**: For memory tracking, `ProcessInfo.currentRss` from `dart:io` gives you the resident set size, but it is not available on web. Use conditional imports or a try-catch.

### Full Solution

```dart
// exercise_08_perf_sdk_solution.dart
import 'dart:async';
import 'dart:collection';
import 'dart:developer' as developer;
import 'dart:ui';
import 'package:flutter/material.dart';
import 'package:flutter/scheduler.dart';

class PerformanceMonitor {
  static final PerformanceMonitor instance = PerformanceMonitor._();
  PerformanceMonitor._();

  final List<double> _frameTimes = [];
  final Map<String, List<int>> _traceSpans = {};
  Timer? _memorySampler;
  int _startupTimeMicros = 0;
  int _firstFrameTimeMicros = 0;
  double _memoryHighWatermarkMB = 0;
  double _currentMemoryMB = 0;
  int _jankFrameCount = 0;
  bool _initialized = false;

  // Rolling window for the overlay display
  final Queue<double> _recentFrameTimes = Queue();
  static const int _recentWindowSize = 60;

  double get currentFps {
    if (_recentFrameTimes.isEmpty) return 0;
    final avgMs = _recentFrameTimes.reduce((a, b) => a + b) /
        _recentFrameTimes.length;
    return avgMs > 0 ? 1000.0 / avgMs : 0;
  }

  double get worstRecentFrameMs {
    if (_recentFrameTimes.isEmpty) return 0;
    return _recentFrameTimes.reduce((a, b) => a > b ? a : b);
  }

  int get jankFrameCount => _jankFrameCount;
  double get currentMemoryMB => _currentMemoryMB;

  void initialize(int appStartMicros) {
    if (_initialized) return;
    _initialized = true;
    _startupTimeMicros = appStartMicros;

    SchedulerBinding.instance.addTimingsCallback(_onFrameTimings);

    // Sample memory every 2 seconds
    _memorySampler = Timer.periodic(
      const Duration(seconds: 2),
      (_) => _sampleMemory(),
    );
  }

  void recordStartupComplete() {
    _firstFrameTimeMicros = DateTime.now().microsecondsSinceEpoch;
    developer.log(
      'Startup time: ${startupDurationMs.toStringAsFixed(1)}ms',
      name: 'PerformanceMonitor',
    );
  }

  double get startupDurationMs {
    if (_firstFrameTimeMicros == 0 || _startupTimeMicros == 0) return 0;
    return (_firstFrameTimeMicros - _startupTimeMicros) / 1000.0;
  }

  void _onFrameTimings(List<FrameTiming> timings) {
    for (final timing in timings) {
      final totalMs =
          (timing.totalSpan.inMicroseconds) / 1000.0;
      _frameTimes.add(totalMs);

      _recentFrameTimes.addLast(totalMs);
      while (_recentFrameTimes.length > _recentWindowSize) {
        _recentFrameTimes.removeFirst();
      }

      if (totalMs > 16.0) {
        _jankFrameCount++;
      }
    }
  }

  void _sampleMemory() {
    // ProcessInfo.currentRss is not available on all platforms.
    // In a production SDK, use platform channels or conditional imports.
    // For this exercise, we estimate from the image cache.
    final imageCache = PaintingBinding.instance.imageCache;
    final cacheSizeMB = imageCache.currentSizeBytes / (1024 * 1024);
    _currentMemoryMB = cacheSizeMB;
    if (_currentMemoryMB > _memoryHighWatermarkMB) {
      _memoryHighWatermarkMB = _currentMemoryMB;
    }
  }

  TraceHandle beginTrace(String name) {
    final startMicros = DateTime.now().microsecondsSinceEpoch;
    developer.Timeline.startSync(name);
    return TraceHandle._(name, startMicros, this);
  }

  void _recordTrace(String name, int durationMicros) {
    _traceSpans.putIfAbsent(name, () => []);
    _traceSpans[name]!.add(durationMicros);
  }

  PerformanceReport generateReport() {
    final sortedFrameTimes = List<double>.from(_frameTimes)..sort();
    final totalFrames = sortedFrameTimes.length;

    return PerformanceReport(
      p50FrameTimeMs: totalFrames > 0
          ? sortedFrameTimes[(totalFrames * 0.5).floor()]
          : 0,
      p90FrameTimeMs: totalFrames > 0
          ? sortedFrameTimes[(totalFrames * 0.9).floor()]
          : 0,
      p99FrameTimeMs: totalFrames > 0
          ? sortedFrameTimes[(totalFrames * 0.99).floor()]
          : 0,
      totalFrames: totalFrames,
      jankFrameCount: _jankFrameCount,
      jankPercentage: totalFrames > 0
          ? (_jankFrameCount / totalFrames) * 100
          : 0,
      memoryHighWatermarkMB: _memoryHighWatermarkMB,
      startupTimeMs: startupDurationMs,
      traceAverages: _traceSpans.map((name, durations) {
        final avg = durations.reduce((a, b) => a + b) / durations.length;
        return MapEntry(name, avg / 1000.0); // convert to ms
      }),
    );
  }

  void dispose() {
    _memorySampler?.cancel();
    // Note: there is no removeTimingsCallback in the public API.
    // In a production SDK, use a flag to ignore callbacks after dispose.
    _initialized = false;
  }
}

class TraceHandle {
  final String name;
  final int _startMicros;
  final PerformanceMonitor _monitor;

  TraceHandle._(this.name, this._startMicros, this._monitor);

  void end() {
    developer.Timeline.finishSync();
    final durationMicros =
        DateTime.now().microsecondsSinceEpoch - _startMicros;
    _monitor._recordTrace(name, durationMicros);
  }
}

class PerformanceReport {
  final double p50FrameTimeMs;
  final double p90FrameTimeMs;
  final double p99FrameTimeMs;
  final int totalFrames;
  final int jankFrameCount;
  final double jankPercentage;
  final double memoryHighWatermarkMB;
  final double startupTimeMs;
  final Map<String, double> traceAverages;

  const PerformanceReport({
    required this.p50FrameTimeMs,
    required this.p90FrameTimeMs,
    required this.p99FrameTimeMs,
    required this.totalFrames,
    required this.jankFrameCount,
    required this.jankPercentage,
    required this.memoryHighWatermarkMB,
    required this.startupTimeMs,
    required this.traceAverages,
  });

  Map<String, dynamic> toJson() {
    return {
      'frame_times': {
        'p50_ms': p50FrameTimeMs,
        'p90_ms': p90FrameTimeMs,
        'p99_ms': p99FrameTimeMs,
        'total_frames': totalFrames,
      },
      'jank': {
        'count': jankFrameCount,
        'percentage': jankPercentage,
      },
      'memory': {
        'high_watermark_mb': memoryHighWatermarkMB,
      },
      'startup': {
        'time_ms': startupTimeMs,
      },
      'traces': traceAverages,
    };
  }

  @override
  String toString() {
    final buffer = StringBuffer()
      ..writeln('=== Performance Report ===')
      ..writeln('Frame times: P50=${p50FrameTimeMs.toStringAsFixed(1)}ms '
          'P90=${p90FrameTimeMs.toStringAsFixed(1)}ms '
          'P99=${p99FrameTimeMs.toStringAsFixed(1)}ms')
      ..writeln('Total frames: $totalFrames')
      ..writeln('Jank: $jankFrameCount frames '
          '(${jankPercentage.toStringAsFixed(1)}%)')
      ..writeln('Memory high watermark: '
          '${memoryHighWatermarkMB.toStringAsFixed(1)}MB')
      ..writeln('Startup time: ${startupTimeMs.toStringAsFixed(1)}ms');

    if (traceAverages.isNotEmpty) {
      buffer.writeln('Custom traces:');
      for (final entry in traceAverages.entries) {
        buffer.writeln(
            '  ${entry.key}: ${entry.value.toStringAsFixed(1)}ms avg');
      }
    }

    return buffer.toString();
  }
}

class PerformanceOverlayWidget extends StatefulWidget {
  final Widget child;

  const PerformanceOverlayWidget({super.key, required this.child});

  @override
  State<PerformanceOverlayWidget> createState() =>
      _PerformanceOverlayWidgetState();
}

class _PerformanceOverlayWidgetState extends State<PerformanceOverlayWidget> {
  Timer? _refreshTimer;

  @override
  void initState() {
    super.initState();
    // Refresh overlay at 1Hz -- fast enough for humans, cheap enough
    // to avoid the overlay itself causing perf issues.
    _refreshTimer = Timer.periodic(
      const Duration(seconds: 1),
      (_) {
        if (mounted) setState(() {});
      },
    );
  }

  @override
  void dispose() {
    _refreshTimer?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final monitor = PerformanceMonitor.instance;

    return Directionality(
      textDirection: TextDirection.ltr,
      child: Stack(
        children: [
          widget.child,
          Positioned(
            top: 50,
            right: 8,
            child: IgnorePointer(
              child: Container(
                padding: const EdgeInsets.all(8),
                decoration: BoxDecoration(
                  color: Colors.black87,
                  borderRadius: BorderRadius.circular(8),
                ),
                child: DefaultTextStyle(
                  style: const TextStyle(
                    color: Colors.white,
                    fontSize: 11,
                    fontFamily: 'monospace',
                    decoration: TextDecoration.none,
                  ),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      Text(_fpsLabel(monitor.currentFps)),
                      Text(
                          'Worst: ${monitor.worstRecentFrameMs.toStringAsFixed(1)}ms'),
                      Text(
                          'Mem: ${monitor.currentMemoryMB.toStringAsFixed(1)}MB'),
                      Text('Jank: ${monitor.jankFrameCount}'),
                    ],
                  ),
                ),
              ),
            ),
          ),
        ],
      ),
    );
  }

  String _fpsLabel(double fps) {
    final color = fps >= 55
        ? 'ok'
        : fps >= 30
            ? 'warn'
            : 'bad';
    return 'FPS: ${fps.toStringAsFixed(0)} [$color]';
  }
}

// --- Sample app demonstrating the SDK ---

void main() {
  final startTime = DateTime.now().microsecondsSinceEpoch;
  WidgetsFlutterBinding.ensureInitialized();
  PerformanceMonitor.instance.initialize(startTime);

  runApp(PerformanceOverlayWidget(
    child: SampleApp(startTime: startTime),
  ));
}

class SampleApp extends StatefulWidget {
  final int startTime;

  const SampleApp({super.key, required this.startTime});

  @override
  State<SampleApp> createState() => _SampleAppState();
}

class _SampleAppState extends State<SampleApp> {
  bool _startupRecorded = false;

  @override
  Widget build(BuildContext context) {
    if (!_startupRecorded) {
      _startupRecorded = true;
      WidgetsBinding.instance.addPostFrameCallback((_) {
        PerformanceMonitor.instance.recordStartupComplete();
      });
    }

    return MaterialApp(
      home: Scaffold(
        appBar: AppBar(title: const Text('Perf SDK Demo')),
        body: ListView.builder(
          itemCount: 200,
          itemBuilder: (context, index) {
            return ListTile(title: Text('Item $index'));
          },
        ),
        floatingActionButton: FloatingActionButton(
          onPressed: _printReport,
          child: const Icon(Icons.assessment),
        ),
      ),
    );
  }

  void _printReport() {
    final report = PerformanceMonitor.instance.generateReport();
    debugPrint(report.toString());
    debugPrint('JSON: ${report.toJson()}');
  }
}
```

### Common Mistakes

**Mistake 1: Updating the overlay on every frame timing callback.** If you call `setState` on the overlay every time a frame timing arrives, the overlay itself triggers a rebuild that triggers a new frame timing. This feedback loop can degrade performance. Update at 1Hz instead.

**Mistake 2: Storing every frame time forever.** In a production SDK, you need a bounded buffer or periodic aggregation. An app running at 60fps generates 216,000 frame times per hour. Use a circular buffer or aggregate into percentile buckets periodically.

**Mistake 3: Using `DateTime.now()` for high-precision timing.** `DateTime.now()` has millisecond precision on some platforms. For sub-millisecond trace spans, use `Stopwatch` or `Timeline.now` (from `dart:developer`). The `FrameTiming` objects from the framework already use high-precision timestamps.

**Mistake 4: Forgetting to guard against disposal.** If the widget hosting the overlay is disposed while the timer is still running, the `setState` call will throw. Always check `mounted` before calling `setState` in timer callbacks.

### Alternative Approach

For production use, consider a more structured architecture:

1. **Separate collection from reporting.** The monitor collects raw data. A separate `PerformanceReporter` handles aggregation, formatting, and transmission.
2. **Use Isolates for aggregation.** If you are tracking thousands of custom traces, percentile calculation should happen off the main thread.
3. **Integrate with platform tools.** On Android, expose metrics via `PerformanceOverlayLayer`. On iOS, integrate with MetricKit. On web, use the Performance Observer API.
4. **Add sampling.** In production, you do not need every frame time from every user. Sample 10% of sessions to reduce overhead.

### Debugging Tip

To verify the SDK is detecting regressions, add a deliberate bottleneck to the list item builder:

```dart
itemBuilder: (context, index) {
  // Simulate a regression: expensive work in build
  var sum = 0;
  for (var i = 0; i < 100000; i++) sum += i;
  return ListTile(title: Text('Item $index (sum: $sum)'));
},
```

The overlay should immediately show FPS dropping below 30 and the jank counter climbing. The report's P90 and P99 frame times should spike. Remove the bottleneck and verify the metrics return to normal. This confirms your SDK is sensitive enough to detect real regressions.
