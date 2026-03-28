# Section 19 -- Flutter Performance: Profiling, Optimization & DevTools

## Introduction

Performance is not a feature you bolt on at the end. It is the feature that determines whether users stay or leave. A study by Google found that 53% of mobile users abandon an app if it takes longer than three seconds to load, and users perceive any frame that takes longer than 16 milliseconds as "jank" -- that stuttery, unpolished feeling that erodes trust in your product even when the logic behind it is flawless.

Flutter promises 60fps (or 120fps on high-refresh displays). That gives you roughly 16 milliseconds per frame to build your widget tree, lay out elements, paint pixels, and composite layers. Miss that budget consistently and your app feels sluggish. Miss it on specific interactions -- a scroll, a page transition, a list load -- and users notice exactly when your code struggles.

But here is the trap: performance work without measurement is guesswork. Developers routinely "optimize" code that was never slow while ignoring the actual bottleneck two layers away. The single most important lesson in this section is this: **profile first, optimize second, measure the result**. Every other technique is useless if you cannot identify where time is actually spent.

This section teaches you to think about performance systematically. You will learn Flutter's rendering pipeline so you understand *where* bottlenecks occur, master DevTools so you can *find* them, and apply targeted optimizations so you can *fix* them -- with evidence, not intuition.

## Prerequisites

You should be comfortable with:

- **Section 09**: Flutter setup, widget fundamentals, StatelessWidget, StatefulWidget
- **Section 10**: Layouts, constraints, Flex, Stack
- **Section 11**: Navigation, routing
- **Section 12**: State management basics, setState, InheritedWidget
- **Section 13**: Forms, input handling
- **Section 14**: Networking, serialization, JSON
- **Section 15**: Advanced state management (Provider, Riverpod, BLoC)
- **Section 16**: Animations, implicit and explicit
- **Section 17**: Testing (unit, widget, integration)
- **Section 18**: Architecture patterns (clean architecture, layered design)

## Learning Objectives

By the end of this section, you will be able to:

1. **Explain** Flutter's rendering pipeline (build, layout, paint, composite) and **identify** which phase causes a given performance problem
2. **Use** Flutter DevTools to profile frame rendering, CPU usage, and memory allocation in profile mode
3. **Distinguish** between debug, profile, and release mode behavior and **justify** why profiling in debug mode produces misleading results
4. **Apply** const constructors, RepaintBoundary, and key-based optimizations to reduce unnecessary rebuilds and repaints
5. **Design** memory-efficient list implementations using ListView.builder, cacheExtent tuning, and keep-alive strategies
6. **Diagnose** shader compilation jank and **implement** SkSL warmup strategies
7. **Implement** deferred imports and tree shaking to reduce application binary size and startup time
8. **Architect** a performance monitoring system that tracks frame times, memory usage, and startup metrics

---

## Core Concepts

### 1. The Flutter Rendering Pipeline

Before you can fix performance problems, you need to understand where they happen. Flutter renders each frame through four phases, and each phase has distinct failure modes.

**Build** is where your widget tree gets constructed. When setState is called, Flutter walks down from the dirty widget and calls build() on it and its descendants. Expensive build methods, deeply nested trees, and unnecessary rebuilds all hurt this phase.

**Layout** is where Flutter calculates sizes and positions. Each RenderObject receives constraints from its parent and determines its own size. Pathological layouts -- deeply nested containers each adding padding, or intrinsic height calculations that walk the tree multiple times -- slow this phase.

**Paint** is where RenderObjects draw themselves onto layers. Complex custom painters, excessive use of saveLayer (triggered by Opacity, ClipRRect, and similar widgets), and large areas that repaint when only a small part changed all impact this phase.

**Compositing** is where layers are sent to the GPU for final rendering. Too many layers, overly large layers, or complex layer trees increase GPU work.

```dart
// rendering_pipeline_demo.dart
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';

// Enable these flags during development to visualize pipeline costs.
// NEVER ship with these enabled.
void enablePipelineDiagnostics() {
  // Shows which widgets rebuild -- blue overlay flashes on rebuild
  debugProfileBuildsEnabled = true;

  // Shows repaint boundaries -- each boundary gets a colored overlay
  // that rotates color on each repaint, letting you SEE what repaints
  debugRepaintRainbowEnabled = true;

  // Shows layout boundaries -- helps identify unnecessary relayouts
  debugPrintLayouts = false; // Very verbose; enable selectively
}

// A widget that demonstrates where each phase matters
class PipelineDemoScreen extends StatefulWidget {
  const PipelineDemoScreen({super.key});

  @override
  State<PipelineDemoScreen> createState() => _PipelineDemoScreenState();
}

class _PipelineDemoScreenState extends State<PipelineDemoScreen> {
  int _counter = 0;

  @override
  Widget build(BuildContext context) {
    // BUILD PHASE: this entire method runs on every setState call.
    // If this tree is deep and complex, build time grows.
    return Scaffold(
      body: Column(
        children: [
          // This subtree rebuilds even though it never changes.
          // Making it const prevents unnecessary build work.
          const _ExpensiveHeader(),

          // This is the only part that actually depends on _counter.
          Text('Count: $_counter'),

          // PAINT PHASE: Opacity triggers saveLayer, which is expensive.
          // Prefer AnimatedOpacity or FadeTransition for animated opacity.
          Opacity(
            opacity: 0.5,
            child: Container(
              width: 200,
              height: 200,
              color: Colors.blue,
            ),
          ),
        ],
      ),
      floatingActionButton: FloatingActionButton(
        onPressed: () => setState(() => _counter++),
        child: const Icon(Icons.add),
      ),
    );
  }
}

class _ExpensiveHeader extends StatelessWidget {
  // The const constructor means Flutter can skip rebuilding this widget
  // entirely when the parent rebuilds -- it knows nothing changed.
  const _ExpensiveHeader();

  @override
  Widget build(BuildContext context) {
    return const Padding(
      padding: EdgeInsets.all(16.0),
      child: Text(
        'This header never changes',
        style: TextStyle(fontSize: 24, fontWeight: FontWeight.bold),
      ),
    );
  }
}
```

### 2. Profile Mode vs Debug Mode

This is the single most common profiling mistake: measuring performance in debug mode. Debug mode enables assertions, disables optimizations, enables hot reload infrastructure, and runs with a debug-mode Dart VM. An app that stutters at 40fps in debug mode might run at a smooth 60fps in profile mode.

**Debug mode**: For development. Assertions enabled, no compilation optimizations, hot reload support. Performance numbers are meaningless here.

**Profile mode**: For performance analysis. Compiled ahead-of-time like release, but with just enough instrumentation for DevTools to connect. This is where you profile.

**Release mode**: For production. Maximum optimization, no debugging support. Performance here is what users experience.

```dart
// profile_mode_check.dart
import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';

class PerformanceGate extends StatelessWidget {
  const PerformanceGate({super.key});

  @override
  Widget build(BuildContext context) {
    // kProfileMode is true only in profile builds
    // kReleaseMode is true only in release builds
    // kDebugMode is true only in debug builds

    if (kDebugMode) {
      return const Banner(
        message: 'DEBUG',
        location: BannerLocation.topEnd,
        child: _AppContent(),
      );
    }

    return const _AppContent();
  }
}

// To run in profile mode:
//   flutter run --profile
//
// To run in release mode:
//   flutter run --release
//
// NEVER trust frame times from debug mode.
// A "slow" widget in debug might be perfectly fine in profile mode.

class _AppContent extends StatelessWidget {
  const _AppContent();

  @override
  Widget build(BuildContext context) {
    return const Scaffold(
      body: Center(child: Text('App Content')),
    );
  }
}
```

### 3. Flutter DevTools

DevTools is your primary profiling instrument. Knowing which tab to reach for is half the battle.

**Performance tab**: Shows a frame chart. Each bar represents one frame. Green bars finished within budget (16ms). Red or orange bars are jank frames. Click any bar to see the build, layout, and paint breakdown.

**CPU Profiler**: Shows where CPU time is spent across function calls. Use this when the Performance tab tells you *that* a frame is slow but you need to know *which function* is the culprit.

**Memory tab**: Tracks heap usage over time, shows allocation rates, and lets you take heap snapshots to find objects that should have been garbage collected but were not.

**Widget Inspector**: Shows the widget tree with rebuild counts. Highlights widgets that rebuild frequently -- your first clue for unnecessary rebuilds.

```dart
// devtools_integration.dart
import 'dart:developer' as developer;

// Use Timeline events to mark sections of your code in DevTools.
// These appear as labeled blocks in the Performance tab's timeline.
void processLargeDataset(List<Map<String, dynamic>> rawData) {
  developer.Timeline.startSync('processLargeDataset');

  // Phase 1: parsing
  developer.Timeline.startSync('parsing');
  final parsed = rawData.map(_parseRecord).toList();
  developer.Timeline.finishSync();

  // Phase 2: filtering
  developer.Timeline.startSync('filtering');
  final filtered = parsed.where(_isValid).toList();
  developer.Timeline.finishSync();

  // Phase 3: sorting
  developer.Timeline.startSync('sorting');
  filtered.sort(_compareByDate);
  developer.Timeline.finishSync();

  developer.Timeline.finishSync(); // end processLargeDataset
}

// Placeholder implementations
Map<String, dynamic> _parseRecord(Map<String, dynamic> raw) => raw;
bool _isValid(Map<String, dynamic> record) => true;
int _compareByDate(Map<String, dynamic> a, Map<String, dynamic> b) => 0;

// You can also log custom events that appear in DevTools
void logPerformanceEvent(String name, Map<String, dynamic> details) {
  developer.log(
    'perf_event',
    name: name,
    error: details.toString(),
  );
  // View these in DevTools > Logging tab
}
```

### 4. Build Optimization: Reducing Unnecessary Rebuilds

The cheapest frame is the one where nothing rebuilds. Flutter provides several mechanisms to skip work.

**Const constructors** tell Flutter that a widget's configuration is compile-time constant. When the parent rebuilds, Flutter sees the same const instance and skips the child's build entirely. This is the lowest-effort, highest-impact optimization available.

**Extracting widgets** into their own StatelessWidget or StatefulWidget classes creates natural rebuild boundaries. When a parent calls setState, only children that actually depend on the changed state need to rebuild.

**shouldRebuild and didUpdateWidget** give you manual control, but const constructors and widget extraction handle most cases.

```dart
// build_optimization.dart
import 'package:flutter/material.dart';

// BAD: monolithic build method where everything rebuilds together
class BadCounter extends StatefulWidget {
  const BadCounter({super.key});

  @override
  State<BadCounter> createState() => _BadCounterState();
}

class _BadCounterState extends State<BadCounter> {
  int _count = 0;

  @override
  Widget build(BuildContext context) {
    return Column(
      children: [
        // This expensive widget rebuilds every time _count changes,
        // even though it does not depend on _count at all.
        _buildExpensiveChart(),
        Text('Count: $_count'),
        ElevatedButton(
          onPressed: () => setState(() => _count++),
          child: const Text('Increment'),
        ),
      ],
    );
  }

  Widget _buildExpensiveChart() {
    // Imagine this does expensive layout calculations
    return Container(
      height: 200,
      color: Colors.grey[200],
      child: const Center(child: Text('Expensive chart that never changes')),
    );
  }
}

// GOOD: extracted widgets with const constructors
class GoodCounter extends StatefulWidget {
  const GoodCounter({super.key});

  @override
  State<GoodCounter> createState() => _GoodCounterState();
}

class _GoodCounterState extends State<GoodCounter> {
  int _count = 0;

  @override
  Widget build(BuildContext context) {
    return Column(
      children: [
        // const means Flutter skips this entirely during rebuild
        const ExpensiveChart(),
        // Only this Text widget rebuilds -- it is the only part that changed
        Text('Count: $_count'),
        ElevatedButton(
          onPressed: () => setState(() => _count++),
          child: const Text('Increment'),
        ),
      ],
    );
  }
}

class ExpensiveChart extends StatelessWidget {
  const ExpensiveChart({super.key});

  @override
  Widget build(BuildContext context) {
    return Container(
      height: 200,
      color: Colors.grey[200],
      child: const Center(child: Text('Expensive chart that never changes')),
    );
  }
}
```

### 5. RepaintBoundary

While const constructors prevent unnecessary *builds*, RepaintBoundary prevents unnecessary *paints*. When Flutter repaints a region of the screen, it normally repaints the entire layer. RepaintBoundary creates a separate compositing layer, so changes inside the boundary do not force repaints outside it, and vice versa.

Use RepaintBoundary when you have a frequently changing widget (an animation, a clock, a live counter) next to static content. Without the boundary, the static content repaints every time the animation ticks.

But do not scatter RepaintBoundary everywhere. Each boundary creates a new compositing layer, and too many layers increase GPU memory and compositing time. Profile to confirm it helps.

```dart
// repaint_boundary_usage.dart
import 'package:flutter/material.dart';

class LiveDashboard extends StatelessWidget {
  const LiveDashboard({super.key});

  @override
  Widget build(BuildContext context) {
    return Column(
      children: [
        // Static header -- does not change
        const _DashboardHeader(),

        // This ticker updates every second. Without RepaintBoundary,
        // it would force the entire Column to repaint.
        RepaintBoundary(
          child: _LiveTicker(),
        ),

        // Static chart -- does not change
        const _StaticChart(),
      ],
    );
  }
}

class _DashboardHeader extends StatelessWidget {
  const _DashboardHeader();

  @override
  Widget build(BuildContext context) {
    return const Padding(
      padding: EdgeInsets.all(16),
      child: Text('Dashboard', style: TextStyle(fontSize: 24)),
    );
  }
}

class _LiveTicker extends StatefulWidget {
  @override
  State<_LiveTicker> createState() => _LiveTickerState();
}

class _LiveTickerState extends State<_LiveTicker> {
  late final Stream<DateTime> _timeStream;

  @override
  void initState() {
    super.initState();
    _timeStream = Stream.periodic(
      const Duration(seconds: 1),
      (_) => DateTime.now(),
    );
  }

  @override
  Widget build(BuildContext context) {
    return StreamBuilder<DateTime>(
      stream: _timeStream,
      builder: (context, snapshot) {
        final time = snapshot.data ?? DateTime.now();
        return Padding(
          padding: const EdgeInsets.all(16),
          child: Text(
            '${time.hour}:${time.minute}:${time.second}',
            style: const TextStyle(fontSize: 48, fontFamily: 'monospace'),
          ),
        );
      },
    );
  }
}

class _StaticChart extends StatelessWidget {
  const _StaticChart();

  @override
  Widget build(BuildContext context) {
    return Container(
      height: 200,
      margin: const EdgeInsets.all(16),
      color: Colors.blueGrey[50],
      child: const Center(child: Text('Static chart area')),
    );
  }
}
```

### 6. ListView Optimization

Lists are the most common source of performance problems in mobile apps. A naive approach builds all items upfront, consuming memory and CPU proportional to the total item count. A lazy approach builds only visible items and recycles them as the user scrolls.

```dart
// listview_optimization.dart
import 'package:flutter/material.dart';

// BAD: ListView with children builds ALL items immediately.
// For 10,000 items, this allocates 10,000 widgets at once.
class BadProductList extends StatelessWidget {
  final List<String> products;

  const BadProductList({super.key, required this.products});

  @override
  Widget build(BuildContext context) {
    return ListView(
      children: products
          .map((p) => ListTile(title: Text(p)))
          .toList(), // All 10,000 built right now
    );
  }
}

// GOOD: ListView.builder lazily builds only visible items.
// Memory usage stays constant regardless of list size.
class GoodProductList extends StatelessWidget {
  final List<String> products;

  const GoodProductList({super.key, required this.products});

  @override
  Widget build(BuildContext context) {
    return ListView.builder(
      // itemCount lets Flutter know the total without building them
      itemCount: products.length,

      // itemExtent: if every item is the same height, set this.
      // It lets Flutter skip measuring each item during layout,
      // which speeds up scrolling significantly for uniform lists.
      itemExtent: 56.0,

      // cacheExtent: how many pixels beyond the viewport to pre-build.
      // Default is 250. Increase for smoother scrolling at the cost of
      // memory; decrease for lower memory on constrained devices.
      cacheExtent: 300.0,

      // addAutomaticKeepAlives: default true. Keeps items alive when
      // they scroll off-screen. Set to false if your items are cheap
      // to rebuild and you want lower memory usage.
      addAutomaticKeepAlives: false,

      itemBuilder: (context, index) {
        return ListTile(
          key: ValueKey(products[index]),
          title: Text(products[index]),
        );
      },
    );
  }
}

// For mixed-height items, use ListView.builder without itemExtent
// but consider using ListView.separated for visual dividers
class SeparatedProductList extends StatelessWidget {
  final List<String> products;

  const SeparatedProductList({super.key, required this.products});

  @override
  Widget build(BuildContext context) {
    return ListView.separated(
      itemCount: products.length,
      separatorBuilder: (context, index) => const Divider(height: 1),
      itemBuilder: (context, index) {
        return ListTile(title: Text(products[index]));
      },
    );
  }
}
```

### 7. Image Optimization

Images are often the largest memory consumers in a Flutter app. A single high-resolution photo can occupy tens of megabytes in decoded form. Without careful management, scrolling through an image-heavy list will exhaust memory on low-end devices.

```dart
// image_optimization.dart
import 'package:flutter/material.dart';

class OptimizedImageGrid extends StatelessWidget {
  final List<String> imageUrls;

  const OptimizedImageGrid({super.key, required this.imageUrls});

  @override
  Widget build(BuildContext context) {
    return GridView.builder(
      gridDelegate: const SliverGridDelegateWithFixedCrossAxisCount(
        crossAxisCount: 3,
      ),
      itemCount: imageUrls.length,
      itemBuilder: (context, index) {
        return Image.network(
          imageUrls[index],
          // cacheWidth/cacheHeight tell Flutter to decode the image at
          // a reduced resolution. A 4000x3000 photo displayed in a 150px
          // tile does not need to occupy 48MB of decoded pixels.
          cacheWidth: 300,
          cacheHeight: 300,

          // frameBuilder provides a smooth fade-in as the image loads
          frameBuilder: (context, child, frame, wasSynchronouslyLoaded) {
            if (wasSynchronouslyLoaded) return child;
            return AnimatedOpacity(
              opacity: frame == null ? 0 : 1,
              duration: const Duration(milliseconds: 300),
              curve: Curves.easeOut,
              child: child,
            );
          },

          // errorBuilder handles broken URLs gracefully
          errorBuilder: (context, error, stackTrace) {
            return Container(
              color: Colors.grey[300],
              child: const Icon(Icons.broken_image),
            );
          },
        );
      },
    );
  }
}

// Precaching: load images before navigating to a screen
// so they appear instantly when the user arrives.
Future<void> precacheProductImages(
  BuildContext context,
  List<String> urls,
) async {
  for (final url in urls) {
    await precacheImage(NetworkImage(url), context);
  }
}

// Managing the image cache size
void configureImageCache() {
  // Default is 1000 images and 100MB. On low-end devices, reduce these.
  PaintingBinding.instance.imageCache.maximumSize = 200;
  PaintingBinding.instance.imageCache.maximumSizeBytes = 50 * 1024 * 1024;
}
```

### 8. Shader Compilation Jank and Warmup

The first time Flutter encounters a new combination of shaders (visual effects), the GPU must compile them. This compilation happens on the main thread and can cause noticeable jank -- sometimes hundreds of milliseconds -- on the first run of an animation or transition. Subsequent runs are smooth because the compiled shaders are cached.

This is shader compilation jank, and it is particularly frustrating because it only happens once per shader combination, making it hard to reproduce consistently.

```dart
// shader_warmup.dart
import 'package:flutter/material.dart';

// Flutter provides a mechanism to "warm up" shaders at app startup,
// before the user sees any animations. You capture shaders during
// development and bundle them with the app.

// Step 1: Capture SkSL shaders during a manual test run:
//   flutter run --profile --cache-sksl --purge-persistent-cache
//   (interact with every animation/transition in the app)
//   Press M in the terminal to save the captured shaders

// Step 2: Bundle the captured shaders in your build:
//   flutter build apk --bundle-sksl-path flutter_01.sksl.json

// Step 3: Verify by profiling the built app -- first-run jank
// should be significantly reduced.

// For custom shaders, you can warm them up programmatically:
class ShaderWarmupWidget extends StatefulWidget {
  final Widget child;

  const ShaderWarmupWidget({super.key, required this.child});

  @override
  State<ShaderWarmupWidget> createState() => _ShaderWarmupWidgetState();
}

class _ShaderWarmupWidgetState extends State<ShaderWarmupWidget> {
  bool _warmedUp = false;

  @override
  void initState() {
    super.initState();
    _warmupShaders();
  }

  Future<void> _warmupShaders() async {
    // Perform operations that trigger shader compilation
    // while showing a splash screen or loading indicator.
    // This moves the jank to a time when the user expects to wait.
    await Future.delayed(const Duration(milliseconds: 500));
    if (mounted) {
      setState(() => _warmedUp = true);
    }
  }

  @override
  Widget build(BuildContext context) {
    if (!_warmedUp) {
      return const MaterialApp(
        home: Scaffold(
          body: Center(child: CircularProgressIndicator()),
        ),
      );
    }
    return widget.child;
  }
}
```

### 9. Isolates for Compute-Heavy Work

Dart is single-threaded by default. Any CPU-intensive work on the main isolate blocks the UI. Flutter provides `compute()` for simple offloading and `Isolate.spawn()` for more complex scenarios.

```dart
// isolate_offloading.dart
import 'dart:convert';
import 'package:flutter/foundation.dart';

// BAD: parsing a large JSON payload on the main thread
// This blocks the UI for the entire parse duration.
Future<List<Product>> parseProductsBad(String jsonString) async {
  final List<dynamic> data = json.decode(jsonString);
  return data.map((item) => Product.fromJson(item)).toList();
}

// GOOD: offload parsing to a separate isolate using compute()
// The UI stays responsive while the background isolate works.
Future<List<Product>> parseProductsGood(String jsonString) async {
  return compute(_parseProducts, jsonString);
}

// This function runs in a separate isolate.
// It must be a top-level function or a static method.
List<Product> _parseProducts(String jsonString) {
  final List<dynamic> data = json.decode(jsonString);
  return data.map((item) => Product.fromJson(item)).toList();
}

class Product {
  final String id;
  final String name;
  final double price;

  const Product({required this.id, required this.name, required this.price});

  factory Product.fromJson(Map<String, dynamic> json) {
    return Product(
      id: json['id'] as String,
      name: json['name'] as String,
      price: (json['price'] as num).toDouble(),
    );
  }
}

// For ongoing background work (e.g., image processing pipeline),
// use Isolate.spawn for long-lived isolates with message passing.
// compute() creates a new isolate per call, which has overhead.
```

### 10. Startup Optimization and App Size

Every millisecond of startup time is a millisecond where the user stares at a blank screen. Reducing time to first frame requires both code-level and build-level strategies.

```dart
// startup_optimization.dart
import 'package:flutter/material.dart';

// Deferred imports: load features only when needed.
// This reduces the initial code that must be loaded at startup.
import 'heavy_feature.dart' deferred as heavyFeature;

class AppWithDeferredLoading extends StatelessWidget {
  const AppWithDeferredLoading({super.key});

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      home: const HomeScreen(),
      routes: {
        '/heavy': (context) => FutureBuilder(
              future: heavyFeature.loadLibrary(),
              builder: (context, snapshot) {
                if (snapshot.connectionState == ConnectionState.done) {
                  return heavyFeature.HeavyFeatureScreen();
                }
                return const Scaffold(
                  body: Center(child: CircularProgressIndicator()),
                );
              },
            ),
      },
    );
  }
}

class HomeScreen extends StatelessWidget {
  const HomeScreen({super.key});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Home')),
      body: Center(
        child: ElevatedButton(
          onPressed: () => Navigator.pushNamed(context, '/heavy'),
          child: const Text('Open Heavy Feature'),
        ),
      ),
    );
  }
}

// Build configuration for reducing app size:
//
// Android (build.gradle):
//   android {
//     buildTypes {
//       release {
//         shrinkResources true  // removes unused resources
//         minifyEnabled true    // enables R8 code shrinking
//       }
//     }
//   }
//
// iOS: enable bitcode in Xcode build settings
//
// Flutter:
//   flutter build apk --split-per-abi  // separate APKs per architecture
//   flutter build appbundle             // preferred for Play Store
//
// Analyze what contributes to app size:
//   flutter build apk --analyze-size
```

### 11. Memory Profiling and Leak Prevention

Memory leaks in Flutter typically come from three sources: forgotten subscriptions, uncancelled timers, and retained references in closures. The DevTools Memory tab shows you heap usage over time -- a steadily growing heap that never drops back is the signature of a leak.

```dart
// memory_management.dart
import 'dart:async';
import 'package:flutter/material.dart';

// BAD: subscriptions and timers not cleaned up
class LeakyWidget extends StatefulWidget {
  const LeakyWidget({super.key});

  @override
  State<LeakyWidget> createState() => _LeakyWidgetState();
}

class _LeakyWidgetState extends State<LeakyWidget> {
  @override
  void initState() {
    super.initState();
    // This subscription is never cancelled.
    // When the widget is disposed, the stream keeps a reference
    // to this State, preventing garbage collection.
    Stream.periodic(const Duration(seconds: 1)).listen((event) {
      // ignore: avoid_print
      print('tick');
    });
  }

  @override
  Widget build(BuildContext context) => const SizedBox();
}

// GOOD: proper cleanup in dispose
class CleanWidget extends StatefulWidget {
  const CleanWidget({super.key});

  @override
  State<CleanWidget> createState() => _CleanWidgetState();
}

class _CleanWidgetState extends State<CleanWidget> {
  StreamSubscription<int>? _subscription;
  Timer? _timer;

  @override
  void initState() {
    super.initState();
    _subscription = Stream.periodic(
      const Duration(seconds: 1),
      (i) => i,
    ).listen(_onTick);

    _timer = Timer.periodic(
      const Duration(minutes: 5),
      (_) => _refreshData(),
    );
  }

  void _onTick(int count) {
    if (!mounted) return; // Guard against updates after dispose
    setState(() {});
  }

  void _refreshData() {
    if (!mounted) return;
    // fetch fresh data
  }

  @override
  void dispose() {
    _subscription?.cancel();
    _timer?.cancel();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => const SizedBox();
}
```

---

## Exercises

### Exercise 1 -- Const Constructor Audit (Basic)

Take a widget tree full of missed const opportunities and fix every one of them, verifying the difference with the Widget Inspector.

**Why this matters**: Const constructors are the simplest performance win in Flutter, yet codebases routinely miss them. Training your eye to spot missing `const` is a foundational profiling skill because it reduces rebuild work with zero architectural cost.

**Instructions**:

1. Copy the starter code below into a runnable Flutter app
2. Run the app in debug mode and open the Widget Inspector in DevTools
3. Tap the "Increment" button and observe which widgets rebuild (the inspector shows rebuild counts)
4. Add `const` to every constructor and widget instantiation that can be made constant
5. Run again and compare rebuild counts -- the static widgets should show zero rebuilds after the initial build

**Starter code**:

```dart
// exercise_01_const_audit.dart
import 'package:flutter/material.dart';

void main() => runApp(MyApp());

class MyApp extends StatelessWidget {
  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      home: CounterPage(),
    );
  }
}

class CounterPage extends StatefulWidget {
  @override
  State<CounterPage> createState() => _CounterPageState();
}

class _CounterPageState extends State<CounterPage> {
  int _count = 0;

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: Text('Const Audit'),
      ),
      body: Column(
        children: [
          HeaderSection(),
          InfoCard(
            title: 'Welcome',
            subtitle: 'This content never changes',
          ),
          Text('Taps: $_count'),
          Padding(
            padding: EdgeInsets.all(16.0),
            child: Icon(Icons.star, size: 48, color: Colors.amber),
          ),
        ],
      ),
      floatingActionButton: FloatingActionButton(
        onPressed: () => setState(() => _count++),
        child: Icon(Icons.add),
      ),
    );
  }
}

class HeaderSection extends StatelessWidget {
  @override
  Widget build(BuildContext context) {
    return Padding(
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

  InfoCard({required this.title, required this.subtitle});

  @override
  Widget build(BuildContext context) {
    return Card(
      margin: EdgeInsets.all(12),
      child: Padding(
        padding: EdgeInsets.all(16),
        child: Column(
          children: [
            Text(title, style: TextStyle(fontSize: 18)),
            SizedBox(height: 8),
            Text(subtitle),
          ],
        ),
      ),
    );
  }
}
```

**Verification**: After your changes, tap "Increment" 10 times. Open the Widget Inspector and confirm that `HeaderSection`, `InfoCard`, `Icon`, and all static `Text` widgets show 0 rebuilds after the initial build. Only the `Text('Taps: $_count')` widget should show 10 rebuilds.

---

### Exercise 2 -- ListView.builder Migration (Basic)

Convert a naive list implementation to an optimized ListView.builder and measure the difference.

**Why this matters**: This is the most common performance problem in Flutter apps that display data. Understanding lazy construction versus eager construction is fundamental to building anything that scrolls.

**Instructions**:

1. Run the starter code below with 10,000 items and observe startup time and scroll behavior
2. Open the Performance tab in DevTools and record a scroll session -- note the frame times
3. Replace the eager `ListView` with `ListView.builder`
4. Add `itemExtent` since all items have the same height
5. Set `addAutomaticKeepAlives` to false (items are cheap to rebuild)
6. Record another scroll session and compare frame times

**Starter code**:

```dart
// exercise_02_listview_migration.dart
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
      // BAD: builds all 10,000 items at once
      body: ListView(
        children: products.map((product) {
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
              child: Text(product),
            ),
          );
        }).toList(),
      ),
    );
  }
}
```

**Verification**: Compare the DevTools Performance tab recordings before and after. The "before" recording should show long frame times during initial build and janky scrolling. The "after" should show consistent sub-16ms frames during scrolling. Memory usage (DevTools Memory tab) should also be significantly lower in the optimized version.

---

### Exercise 3 -- RepaintBoundary Placement (Intermediate)

Add RepaintBoundary to a dashboard with mixed static and dynamic content, and verify with the repaint rainbow that only the dynamic sections repaint.

**Why this matters**: RepaintBoundary is powerful but misunderstood. Too few boundaries and your entire screen repaints when a single counter ticks. Too many and you waste GPU memory on compositing layers. This exercise teaches you to use the repaint rainbow diagnostic to make evidence-based decisions.

**Instructions**:

1. Run the starter code and enable `debugRepaintRainbowEnabled = true`
2. Observe the repaint rainbow -- notice that the entire screen flashes color on every timer tick
3. Add RepaintBoundary widgets to isolate the dynamic components from the static ones
4. Run again and verify that only the dynamic sections change color in the repaint rainbow
5. Add a RepaintBoundary around every single widget (over-optimization) and observe that the compositing overlay in DevTools shows too many layers

**Starter code**:

```dart
// exercise_03_repaint_boundary.dart
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
            // Static: company info header
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
            // Dynamic: live metrics
            Row(
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
            const SizedBox(height: 16),
            // Static: navigation cards
            const Expanded(
              child: Row(
                children: [
                  Expanded(
                    child: Card(
                      child: Center(child: Text('Reports')),
                    ),
                  ),
                  SizedBox(width: 16),
                  Expanded(
                    child: Card(
                      child: Center(child: Text('Settings')),
                    ),
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

**Verification**: With the repaint rainbow enabled, only the "Active Users" and "Revenue" cards should change color each second. The header card, navigation cards, and app bar should remain static. Confirm in DevTools that the total layer count stays reasonable (under 10 layers for this layout).

---

### Exercise 4 -- DevTools Profiling Session (Intermediate)

Profile a deliberately sluggish app in both debug and profile mode, document the difference, and identify the actual bottleneck using the CPU profiler.

**Why this matters**: This exercise breaks the habit of profiling in debug mode and teaches you to use the CPU profiler to find the real culprit rather than guessing. It is the most transferable skill in this entire section.

**Instructions**:

1. Run the starter app in **debug mode** (`flutter run`) and record a 10-second profiling session in DevTools while scrolling the list
2. Note the average frame time and jank frame count
3. Stop the app and run it in **profile mode** (`flutter run --profile`)
4. Record the same 10-second scrolling session
5. Compare the numbers -- document the difference in a comment at the top of your file
6. In profile mode, click on the worst jank frame, switch to the CPU Profiler, and identify which function is the bottleneck
7. Fix the bottleneck and profile again to confirm the fix

**Starter code**:

```dart
// exercise_04_profiling_session.dart
import 'dart:math';
import 'package:flutter/material.dart';

void main() => runApp(const ProfilingApp());

class ProfilingApp extends StatelessWidget {
  const ProfilingApp({super.key});

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      // Disable debug banner for cleaner profiling
      debugShowCheckedModeBanner: false,
      home: const SlowListScreen(),
    );
  }
}

class SlowListScreen extends StatelessWidget {
  const SlowListScreen({super.key});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Profiling Target')),
      body: ListView.builder(
        itemCount: 500,
        itemBuilder: (context, index) {
          return SlowListItem(index: index);
        },
      ),
    );
  }
}

class SlowListItem extends StatelessWidget {
  final int index;

  const SlowListItem({super.key, required this.index});

  @override
  Widget build(BuildContext context) {
    // This simulates an expensive computation in the build method.
    // In real apps, this might be complex date formatting, markdown
    // parsing, or layout calculations done inline.
    final color = _expensiveColorComputation(index);

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
  }

  // Deliberately slow: performs unnecessary repeated computation
  Color _expensiveColorComputation(int seed) {
    var r = 0.0;
    var g = 0.0;
    var b = 0.0;
    final random = Random(seed);
    // Simulate expensive work -- in a real app this might be
    // unoptimized data transformation or redundant calculations
    for (var i = 0; i < 50000; i++) {
      r = random.nextDouble() * 255;
      g = random.nextDouble() * 255;
      b = random.nextDouble() * 255;
    }
    return Color.fromRGBO(r.toInt(), g.toInt(), b.toInt(), 1.0);
  }
}
```

**Verification**: Your optimized version should maintain sub-16ms frame times during scrolling in profile mode. Document the before/after numbers as a comment at the top of your solution file. The fix should move the expensive computation out of the build method (precompute, cache, or simplify).

---

### Exercise 5 -- Shader Jank Elimination (Advanced)

Capture SkSL shaders from a transition-heavy app and bundle them to eliminate first-run jank.

**Why this matters**: Shader compilation jank is invisible in development (shaders get cached after the first run) but hits every new user on their first launch. Understanding the SkSL capture workflow is essential for shipping polished apps.

**Instructions**:

1. Create an app with at least three different page transitions (slide, fade, scale) and a complex animation on one page (e.g., a staggered grid animation)
2. Run the app in profile mode with shader capture enabled: `flutter run --profile --cache-sksl --purge-persistent-cache`
3. Navigate through every screen and trigger every animation manually
4. Press `M` in the terminal to export the captured shaders to a `.sksl.json` file
5. Build the app with the bundled shaders: `flutter build apk --bundle-sksl-path <your-file>.sksl.json`
6. Profile the bundled build and compare first-run transition times against a build without bundled shaders

**Starter code**:

```dart
// exercise_05_shader_warmup.dart
import 'package:flutter/material.dart';

void main() => runApp(const ShaderDemoApp());

class ShaderDemoApp extends StatelessWidget {
  const ShaderDemoApp({super.key});

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      debugShowCheckedModeBanner: false,
      theme: ThemeData(
        pageTransitionsTheme: const PageTransitionsTheme(
          builders: {
            TargetPlatform.android: CupertinoPageTransitionsBuilder(),
            TargetPlatform.iOS: CupertinoPageTransitionsBuilder(),
          },
        ),
      ),
      home: const TransitionHome(),
    );
  }
}

class TransitionHome extends StatelessWidget {
  const TransitionHome({super.key});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Shader Warmup Lab')),
      body: ListView(
        children: [
          ListTile(
            title: const Text('Slide Transition Page'),
            onTap: () => Navigator.push(
              context,
              _createSlideRoute(const DetailPage(title: 'Slide')),
            ),
          ),
          ListTile(
            title: const Text('Fade Transition Page'),
            onTap: () => Navigator.push(
              context,
              _createFadeRoute(const DetailPage(title: 'Fade')),
            ),
          ),
          ListTile(
            title: const Text('Scale Transition Page'),
            onTap: () => Navigator.push(
              context,
              _createScaleRoute(const AnimatedGridPage()),
            ),
          ),
        ],
      ),
    );
  }

  Route _createSlideRoute(Widget page) {
    return PageRouteBuilder(
      pageBuilder: (context, animation, secondaryAnimation) => page,
      transitionsBuilder: (context, animation, secondaryAnimation, child) {
        return SlideTransition(
          position: Tween(
            begin: const Offset(1.0, 0.0),
            end: Offset.zero,
          ).animate(CurvedAnimation(
            parent: animation,
            curve: Curves.easeInOut,
          )),
          child: child,
        );
      },
    );
  }

  Route _createFadeRoute(Widget page) {
    return PageRouteBuilder(
      pageBuilder: (context, animation, secondaryAnimation) => page,
      transitionsBuilder: (context, animation, secondaryAnimation, child) {
        return FadeTransition(opacity: animation, child: child);
      },
    );
  }

  Route _createScaleRoute(Widget page) {
    return PageRouteBuilder(
      pageBuilder: (context, animation, secondaryAnimation) => page,
      transitionsBuilder: (context, animation, secondaryAnimation, child) {
        return ScaleTransition(scale: animation, child: child);
      },
    );
  }
}

class DetailPage extends StatelessWidget {
  final String title;

  const DetailPage({super.key, required this.title});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: Text('$title Transition')),
      body: const Center(child: Text('Detail content')),
    );
  }
}

class AnimatedGridPage extends StatefulWidget {
  const AnimatedGridPage({super.key});

  @override
  State<AnimatedGridPage> createState() => _AnimatedGridPageState();
}

class _AnimatedGridPageState extends State<AnimatedGridPage>
    with SingleTickerProviderStateMixin {
  late AnimationController _controller;

  @override
  void initState() {
    super.initState();
    _controller = AnimationController(
      duration: const Duration(milliseconds: 1500),
      vsync: this,
    )..forward();
  }

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Animated Grid')),
      body: GridView.builder(
        padding: const EdgeInsets.all(8),
        gridDelegate: const SliverGridDelegateWithFixedCrossAxisCount(
          crossAxisCount: 3,
          crossAxisSpacing: 8,
          mainAxisSpacing: 8,
        ),
        itemCount: 18,
        itemBuilder: (context, index) {
          final delay = index * 0.05;
          return AnimatedBuilder(
            animation: _controller,
            builder: (context, child) {
              final value = (_controller.value - delay).clamp(0.0, 1.0);
              return Opacity(
                opacity: value,
                child: Transform.scale(
                  scale: 0.5 + (value * 0.5),
                  child: child,
                ),
              );
            },
            child: Container(
              decoration: BoxDecoration(
                color: Colors.primaries[index % Colors.primaries.length],
                borderRadius: BorderRadius.circular(12),
              ),
            ),
          );
        },
      ),
    );
  }
}
```

**Verification**: Profile both builds (with and without bundled shaders) on a physical device. The first navigation to each page in the unbundled build should show jank frames. The bundled build should show smooth transitions from the first interaction.

---

### Exercise 6 -- Deferred Loading Architecture (Advanced)

Implement deferred imports for a multi-feature app where each feature loads on demand, reducing initial startup time.

**Why this matters**: Large apps often bundle hundreds of screens and features into a single binary. Deferred loading means users only pay for what they actually open, and the initial time-to-first-frame drops dramatically.

**Instructions**:

1. Structure the starter app so that each feature tab uses a deferred import
2. Show a loading indicator while each deferred library loads
3. Handle the case where the deferred load fails (network error on web, corrupted download)
4. Measure the app startup time before and after deferred loading using `Timeline` events
5. Implement a preloading strategy: after the home screen is visible and idle, begin loading the most likely next feature in the background

**Starter code**:

```dart
// exercise_06_deferred_loading.dart
import 'dart:developer' as developer;
import 'package:flutter/material.dart';

// TODO: convert these to deferred imports
// import 'feature_analytics.dart';
// import 'feature_reports.dart';
// import 'feature_settings.dart';

void main() {
  developer.Timeline.startSync('app_startup');
  runApp(const DeferredApp());
}

class DeferredApp extends StatelessWidget {
  const DeferredApp({super.key});

  @override
  Widget build(BuildContext context) {
    developer.Timeline.finishSync(); // end app_startup
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

  // TODO: implement deferred page loading with error handling
  // Each tab should show a loading indicator until its library loads
  // and an error message if loading fails

  final _pages = <Widget>[
    const Center(child: Text('Home - always loaded')),
    // AnalyticsPage(),   // from feature_analytics.dart
    // ReportsPage(),     // from feature_reports.dart
    // SettingsPage(),    // from feature_settings.dart
  ];

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: IndexedStack(index: _selectedIndex, children: _pages),
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
```

**Verification**: Use DevTools Timeline to confirm that the initial startup only loads the Home feature. Navigating to each tab should show a brief loading indicator followed by the feature content. Run `flutter build apk --analyze-size` before and after to compare initial code size.

---

### Exercise 7 -- Full Performance Audit (Insane)

Take a deliberately broken app packed with every anti-pattern from this section, profile it systematically, and optimize it to maintain 60fps on a low-end device. Document every optimization and its measured impact.

**Why this matters**: Real-world performance work is never about a single fix. It is about systematically profiling, identifying the worst bottleneck, fixing it, measuring the improvement, and repeating. This exercise simulates a real performance audit where you must apply judgment about which optimizations are worth the complexity they add.

**Instructions**:

1. Run the app below in profile mode and record a baseline profiling session (30 seconds of scrolling, navigating, and interacting)
2. Document baseline metrics: average frame time, worst frame time, jank frame percentage, memory high watermark
3. Identify all performance anti-patterns in the code (there are at least 10)
4. Fix them one at a time, profiling after each fix to measure its isolated impact
5. Create an optimization log as comments at the top of your file: each entry should list the anti-pattern, the fix, and the measured improvement
6. Target: average frame time under 8ms, zero jank frames during normal scrolling, memory stable under 150MB

**Starter code**:

```dart
// exercise_07_full_audit.dart
import 'dart:math';
import 'dart:convert';
import 'package:flutter/material.dart';

void main() => runApp(BrokenApp());

// Anti-pattern 1: No const on stateless root widget
class BrokenApp extends StatelessWidget {
  @override
  Widget build(BuildContext context) {
    // Anti-pattern 2: Theme created on every build
    return MaterialApp(
      theme: ThemeData(
        colorScheme: ColorScheme.fromSeed(seedColor: Colors.deepPurple),
      ),
      home: BrokenHome(),
    );
  }
}

class BrokenHome extends StatefulWidget {
  @override
  State<BrokenHome> createState() => _BrokenHomeState();
}

class _BrokenHomeState extends State<BrokenHome> {
  final List<Map<String, dynamic>> _items = [];
  final ScrollController _scrollController = ScrollController();
  bool _loading = false;

  @override
  void initState() {
    super.initState();
    _loadItems();
    // Anti-pattern 3: scroll listener rebuilds entire widget tree
    _scrollController.addListener(() {
      if (_scrollController.position.pixels >=
          _scrollController.position.maxScrollExtent - 200) {
        _loadItems();
      }
    });
  }

  void _loadItems() {
    if (_loading) return;
    _loading = true;
    // Anti-pattern 4: generating data synchronously in setState
    setState(() {
      for (var i = 0; i < 50; i++) {
        final id = _items.length + i;
        _items.add({
          'id': id,
          'title': 'Item $id',
          'subtitle': _generateLoremIpsum(200),
          'color': _expensiveRandomColor(id),
          'image': 'https://picsum.photos/seed/$id/400/400',
        });
      }
      _loading = false;
    });
  }

  String _generateLoremIpsum(int words) {
    const lorem = 'lorem ipsum dolor sit amet consectetur adipiscing elit '
        'sed do eiusmod tempor incididunt ut labore et dolore magna aliqua';
    final loremWords = lorem.split(' ');
    final buffer = StringBuffer();
    final random = Random();
    for (var i = 0; i < words; i++) {
      buffer.write(loremWords[random.nextInt(loremWords.length)]);
      buffer.write(' ');
    }
    return buffer.toString();
  }

  Color _expensiveRandomColor(int seed) {
    var r = 0.0, g = 0.0, b = 0.0;
    final random = Random(seed);
    // Anti-pattern 5: expensive computation in build path
    for (var i = 0; i < 10000; i++) {
      r = random.nextDouble() * 255;
      g = random.nextDouble() * 255;
      b = random.nextDouble() * 255;
    }
    return Color.fromRGBO(r.toInt(), g.toInt(), b.toInt(), 1.0);
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: Text('Performance Disaster')),
      // Anti-pattern 6: no ListView.builder, builds all items
      body: ListView(
        controller: _scrollController,
        children: _items.map((item) {
          return _buildItem(item);
        }).toList(),
      ),
    );
  }

  Widget _buildItem(Map<String, dynamic> item) {
    // Anti-pattern 7: no const constructors anywhere
    return Padding(
      padding: EdgeInsets.all(8),
      child: Card(
        // Anti-pattern 8: unnecessary Opacity widget (uses saveLayer)
        child: Opacity(
          opacity: 0.95,
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              // Anti-pattern 9: full-resolution network images with no caching config
              Image.network(
                item['image'],
                height: 200,
                width: double.infinity,
                fit: BoxFit.cover,
              ),
              Padding(
                padding: EdgeInsets.all(12),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      item['title'],
                      style: TextStyle(
                        fontSize: 18,
                        fontWeight: FontWeight.bold,
                        color: item['color'],
                      ),
                    ),
                    SizedBox(height: 8),
                    // Anti-pattern 10: rendering 200 words of text per item
                    Text(item['subtitle']),
                  ],
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }

  // Anti-pattern 11: no dispose for scroll controller
}
```

**Verification**: Create an optimization log at the top of your solution file. Each entry must include: (1) the anti-pattern identified, (2) the fix applied, (3) the before/after metric from DevTools. Final metrics in profile mode should meet the targets: average frame time under 8ms, zero jank frames during 30 seconds of scrolling, memory stable.

---

### Exercise 8 -- Performance Monitoring SDK (Insane)

Build a reusable performance monitoring system that tracks frame times, memory usage, network latency, and startup metrics, with an in-app debug overlay and a reporting mechanism.

**Why this matters**: Production apps need ongoing performance visibility, not just one-time audits. A monitoring SDK lets you detect regressions before users report them, understand performance across device tiers, and make data-driven optimization decisions.

**Instructions**:

1. Create a `PerformanceMonitor` singleton that tracks:
   - Frame render times (using `WidgetsBinding.instance.addTimingsCallback`)
   - Memory usage snapshots (periodic heap size sampling)
   - App startup duration (time from main() to first frame rendered)
   - Custom trace spans (developer-defined code sections, like "api_call_products")
2. Create a `PerformanceOverlay` widget that displays a real-time HUD:
   - Current FPS (rolling average over last 60 frames)
   - Worst frame time in the last 5 seconds
   - Current memory usage in MB
   - Jank frame count (frames exceeding 16ms)
3. Implement a `PerformanceReport` that aggregates session data:
   - P50, P90, P99 frame times
   - Total jank frame count and percentage
   - Memory high watermark
   - Startup time
   - Custom trace span averages
4. Add a mechanism to export the report (JSON serialization to the debug console or a file)
5. Integrate the SDK into a sample app and demonstrate it detecting a deliberately introduced performance regression

**Starter code**:

```dart
// exercise_08_perf_sdk.dart
import 'dart:async';
import 'dart:collection';
import 'dart:developer' as developer;
import 'dart:ui';
import 'package:flutter/material.dart';
import 'package:flutter/scheduler.dart';

// TODO: implement PerformanceMonitor singleton
class PerformanceMonitor {
  static final PerformanceMonitor instance = PerformanceMonitor._();
  PerformanceMonitor._();

  // Frame timing tracking
  // Memory sampling
  // Startup time recording
  // Custom trace spans

  void initialize() {
    // Register frame timing callback
    // Start memory sampling timer
    // Record initialization timestamp
  }

  void recordStartupComplete() {
    // Called when the first frame is rendered
  }

  TraceHandle beginTrace(String name) {
    // Start a custom trace span
    throw UnimplementedError();
  }

  PerformanceReport generateReport() {
    // Aggregate all collected data into a report
    throw UnimplementedError();
  }

  void dispose() {
    // Clean up timers and callbacks
  }
}

class TraceHandle {
  final String name;
  final int startMicros;

  TraceHandle(this.name, this.startMicros);

  void end() {
    // Calculate duration and record it
  }
}

// TODO: implement PerformanceReport
class PerformanceReport {
  // P50, P90, P99 frame times
  // Jank count and percentage
  // Memory high watermark
  // Startup time
  // Custom trace averages

  Map<String, dynamic> toJson() {
    throw UnimplementedError();
  }
}

// TODO: implement PerformanceOverlay widget
class PerformanceOverlayWidget extends StatefulWidget {
  final Widget child;

  const PerformanceOverlayWidget({super.key, required this.child});

  @override
  State<PerformanceOverlayWidget> createState() =>
      _PerformanceOverlayWidgetState();
}

class _PerformanceOverlayWidgetState extends State<PerformanceOverlayWidget> {
  @override
  Widget build(BuildContext context) {
    // Overlay showing real-time FPS, memory, jank count
    return widget.child; // TODO: add overlay
  }
}

// Sample app to demonstrate the SDK
void main() {
  final startTime = DateTime.now().microsecondsSinceEpoch;
  // TODO: initialize PerformanceMonitor
  runApp(PerformanceOverlayWidget(child: SampleApp(startTime: startTime)));
}

class SampleApp extends StatelessWidget {
  final int startTime;

  const SampleApp({super.key, required this.startTime});

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      home: Builder(
        builder: (context) {
          // TODO: call recordStartupComplete on first build
          return Scaffold(
            appBar: AppBar(title: const Text('Perf SDK Demo')),
            body: ListView.builder(
              itemCount: 200,
              itemBuilder: (context, index) {
                return ListTile(title: Text('Item $index'));
              },
            ),
          );
        },
      ),
    );
  }
}
```

**Verification**: Run the sample app with the SDK integrated. The overlay should display live FPS, memory, and jank counts. Introduce a deliberate performance regression (e.g., add an expensive computation in the list item builder) and confirm the overlay reflects the degradation. Export a PerformanceReport and verify its JSON contains all specified metrics.

---

## Summary

Performance is a discipline, not a destination. The core workflow you should internalize from this section is: **profile in profile mode, identify the bottleneck, apply a targeted fix, measure the result**. Resist the temptation to optimize based on intuition -- you will waste time on code that was never slow and miss the actual problem.

The key principles to carry forward:

- **Const constructors** are the highest-impact, lowest-cost optimization. Make them a habit, not an afterthought
- **ListView.builder** is non-negotiable for any list longer than a screenful
- **RepaintBoundary** isolates paint costs, but verify with the repaint rainbow that it actually helps
- **Profile mode** produces valid numbers; debug mode does not
- **DevTools** is your instrument -- learn the Performance tab, CPU profiler, and Memory tab as core skills
- **Isolates** keep the UI thread free for rendering when you have CPU-heavy work
- **Measure before and after** every optimization. An optimization without a measurement is just a code change

## What's Next

In **Section 20: Advanced UI**, you will build on the performance foundations from this section to create complex, polished interfaces: custom painters, slivers, platform-adaptive layouts, and advanced animation choreography -- all while keeping the frame budget you learned to measure here.

## References

- [Flutter Performance Best Practices](https://docs.flutter.dev/perf) -- official performance guide
- [Flutter DevTools](https://docs.flutter.dev/tools/devtools) -- DevTools documentation
- [Understanding Flutter's Rendering Pipeline](https://docs.flutter.dev/resources/architectural-overview#rendering-and-layout) -- architectural overview
- [Profiling Flutter Performance](https://docs.flutter.dev/perf/ui-performance) -- profiling guide
- [Reducing App Size](https://docs.flutter.dev/perf/app-size) -- tree shaking and size optimization
- [Isolates and Compute](https://docs.flutter.dev/perf/isolates) -- background processing
- [SkSL Shader Warmup](https://docs.flutter.dev/perf/shader) -- shader compilation jank
- [Dart DevTools CPU Profiler](https://docs.flutter.dev/tools/devtools/cpu-profiler) -- CPU profiling guide
