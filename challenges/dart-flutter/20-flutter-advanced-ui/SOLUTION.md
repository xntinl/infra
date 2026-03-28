# Section 20: Solutions -- Flutter Advanced UI

## How to Use This File

Attempt each exercise on your own first. When you get stuck, use the progressive hints below -- each hint reveals a bit more without giving away the full answer. Only read the complete solution after a genuine attempt. The common mistakes section at the end covers the bugs you are most likely to encounter, because knowing what goes wrong is often more valuable than knowing what goes right.

---

## Exercise 1: Shape Gallery with CustomPainter

### Progressive Hints

**Hint 1:** For the 5-pointed star, you need 10 vertices alternating between outer radius and inner radius. The angle between consecutive vertices is `pi / 5`. Start at `-pi / 2` so the first point faces upward.

**Hint 2:** For the dashed line, iterate in steps of `dashWidth + gapWidth`. At each step, draw a short segment. Canvas has no native dash API -- you compute positions manually.

**Hint 3:** `SweepGradient` creates a `Shader` via `createShader(rect)`. Assign it to `Paint.shader`. The rect should match the bounding box of your ring.

### Full Solution

```dart
// file: lib/painters/star_painter.dart

import 'dart:math';
import 'package:flutter/material.dart';

class StarPainter extends CustomPainter {
  @override
  void paint(Canvas canvas, Size size) {
    final center = Offset(size.width / 2, size.height / 2);
    final outerRadius = size.width / 2 * 0.8;
    final innerRadius = outerRadius * 0.4;
    final path = Path();

    for (int i = 0; i < 10; i++) {
      final radius = i.isEven ? outerRadius : innerRadius;
      final angle = -pi / 2 + (i * pi / 5);
      final point = Offset(
        center.dx + radius * cos(angle),
        center.dy + radius * sin(angle),
      );
      if (i == 0) {
        path.moveTo(point.dx, point.dy);
      } else {
        path.lineTo(point.dx, point.dy);
      }
    }
    path.close();

    canvas.drawPath(path, Paint()..color = Colors.amber);
  }

  @override
  bool shouldRepaint(covariant CustomPainter oldDelegate) => false;
}
```

```dart
// file: lib/painters/dashed_line_painter.dart

import 'package:flutter/material.dart';

class DashedLinePainter extends CustomPainter {
  @override
  void paint(Canvas canvas, Size size) {
    final paint = Paint()
      ..color = Colors.grey.shade700
      ..strokeWidth = 3.0
      ..strokeCap = StrokeCap.round;

    const dashWidth = 12.0;
    const gapWidth = 6.0;
    final y = size.height / 2;
    double x = 0;

    while (x < size.width) {
      final endX = (x + dashWidth).clamp(0, size.width).toDouble();
      canvas.drawLine(Offset(x, y), Offset(endX, y), paint);
      x += dashWidth + gapWidth;
    }
  }

  @override
  bool shouldRepaint(covariant CustomPainter oldDelegate) => false;
}
```

```dart
// file: lib/painters/gradient_ring_painter.dart

import 'package:flutter/material.dart';

class GradientRingPainter extends CustomPainter {
  @override
  void paint(Canvas canvas, Size size) {
    final center = Offset(size.width / 2, size.height / 2);
    final radius = size.width / 2 * 0.7;
    final rect = Rect.fromCircle(center: center, radius: radius);

    final paint = Paint()
      ..shader = const SweepGradient(
        colors: [Colors.red, Colors.orange, Colors.yellow,
                 Colors.green, Colors.blue, Colors.purple, Colors.red],
      ).createShader(rect)
      ..style = PaintingStyle.stroke
      ..strokeWidth = 12.0;

    canvas.drawCircle(center, radius, paint);
  }

  @override
  bool shouldRepaint(covariant CustomPainter oldDelegate) => false;
}
```

### Common Mistakes

**Forgetting `path.close()`.** The star or triangle will have a visible gap at the starting vertex. No error is thrown -- the shape just looks wrong.

**Using `PaintingStyle.fill` when you wanted `stroke`.** The default is `fill`. Setting `strokeWidth` without changing `style` to `PaintingStyle.stroke` silently ignores the stroke width.

---

## Exercise 2: SliverAppBar with Mixed Scroll Layout

### Progressive Hints

**Hint 1:** Wrap the horizontal `ListView` in `SliverToBoxAdapter`. The horizontal scroll is independent because it has its own scroll controller.

**Hint 2:** For `SliverPersistentHeader`, implement a delegate where `minExtent` equals `maxExtent` for a fixed-height pinned header.

**Hint 3:** `SliverFillRemaining` with `hasScrollBody: false` fills whatever vertical space remains without creating a nested scrollable.

### Full Solution

```dart
// file: lib/screens/product_catalog_screen.dart

import 'package:flutter/material.dart';

class ProductCatalogScreen extends StatelessWidget {
  const ProductCatalogScreen({super.key});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: CustomScrollView(
        slivers: [
          SliverAppBar(
            expandedHeight: 250, pinned: true,
            flexibleSpace: FlexibleSpaceBar(
              title: const Text('Product Catalog'),
              background: Container(
                decoration: const BoxDecoration(
                  gradient: LinearGradient(
                    colors: [Colors.indigo, Colors.purple],
                  ),
                ),
              ),
            ),
          ),
          SliverPersistentHeader(
            pinned: true,
            delegate: _CategoryHeaderDelegate(),
          ),
          SliverToBoxAdapter(
            child: SizedBox(
              height: 50,
              child: ListView.builder(
                scrollDirection: Axis.horizontal,
                padding: const EdgeInsets.symmetric(horizontal: 12),
                itemCount: 8,
                itemBuilder: (context, i) => Padding(
                  padding: const EdgeInsets.symmetric(horizontal: 4),
                  child: FilterChip(
                    label: Text(['All','Electronics','Clothing','Books',
                                 'Home','Sports','Toys','Food'][i]),
                    selected: i == 0, onSelected: (_) {},
                  ),
                ),
              ),
            ),
          ),
          SliverPadding(
            padding: const EdgeInsets.all(12),
            sliver: SliverGrid(
              gridDelegate: const SliverGridDelegateWithFixedCrossAxisCount(
                crossAxisCount: 2, mainAxisSpacing: 12,
                crossAxisSpacing: 12, childAspectRatio: 0.75,
              ),
              delegate: SliverChildBuilderDelegate(
                (context, i) => Card(
                  child: Column(children: [
                    Expanded(child: Container(
                      color: Colors.primaries[i % Colors.primaries.length].shade100,
                    )),
                    Padding(padding: const EdgeInsets.all(8),
                      child: Text('Product ${i + 1}')),
                  ]),
                ),
                childCount: 12,
              ),
            ),
          ),
          SliverList(
            delegate: SliverChildBuilderDelegate(
              (context, i) => ListTile(
                leading: const CircleAvatar(child: Icon(Icons.person)),
                title: Text('Review ${i + 1}'),
              ),
              childCount: 10,
            ),
          ),
          SliverFillRemaining(
            hasScrollBody: false,
            child: Center(child: Text('You have reached the end')),
          ),
        ],
      ),
    );
  }
}

class _CategoryHeaderDelegate extends SliverPersistentHeaderDelegate {
  @override double get minExtent => 52;
  @override double get maxExtent => 52;

  @override
  Widget build(BuildContext context, double shrinkOffset, bool overlapsContent) {
    return Container(
      color: Theme.of(context).colorScheme.surface,
      padding: const EdgeInsets.symmetric(horizontal: 16),
      alignment: Alignment.centerLeft,
      child: const Text('Browse Categories',
        style: TextStyle(fontWeight: FontWeight.bold, fontSize: 16)),
    );
  }

  @override
  bool shouldRebuild(covariant SliverPersistentHeaderDelegate old) => false;
}
```

### Common Mistakes

**Putting a `ListView` directly inside `CustomScrollView` without `SliverToBoxAdapter`.** Slivers and box widgets cannot mix. This causes a layout exception.

**Setting `hasScrollBody: true` on `SliverFillRemaining` with static content.** This creates a nested scrollable that fights with the outer scroll.

---

## Exercise 3: Animated Donut Chart with Touch Interaction

### Progressive Hints

**Hint 1:** To detect which segment was tapped, calculate the tap angle from center using `atan2(dy, dx)`, then walk through cumulative segment angles to find which segment the angle falls into. Also check that the distance from center falls between inner and outer radius.

**Hint 2:** For the "explode" effect, translate the segment along its bisector angle by `explodeDistance * cos(bisector)` horizontally and `sin(bisector)` vertically.

**Hint 3:** Use a single `AnimationController` (0.0 to 1.0) to multiply each segment's sweep angle for the entrance animation.

### Full Solution

```dart
// file: lib/charts/donut_chart_painter.dart

import 'dart:math';
import 'package:flutter/material.dart';

class DonutChartPainter extends CustomPainter {
  final List<({String label, double value, Color color})> segments;
  final double animationValue;
  final int? selectedIndex;

  DonutChartPainter({required this.segments, required this.animationValue,
                     this.selectedIndex});

  @override
  void paint(Canvas canvas, Size size) {
    if (segments.isEmpty) return;
    final center = Offset(size.width / 2, size.height / 2);
    final outerR = min(size.width, size.height) / 2 * 0.85;
    final innerR = outerR * 0.55;
    final total = segments.fold(0.0, (s, e) => s + e.value);
    if (total == 0) return;

    const gap = 0.03;
    double startAngle = -pi / 2;

    for (int i = 0; i < segments.length; i++) {
      final sweep = (segments[i].value / total) * 2 * pi * animationValue - gap;
      if (sweep <= 0) { startAngle += (segments[i].value / total) * 2 * pi * animationValue; continue; }

      final isSelected = i == selectedIndex;
      final bisector = startAngle + sweep / 2;
      final offset = isSelected ? 10.0 : 0.0;
      final sc = Offset(center.dx + offset * cos(bisector),
                         center.dy + offset * sin(bisector));

      final path = Path()
        ..arcTo(Rect.fromCircle(center: sc, radius: outerR), startAngle, sweep, false)
        ..arcTo(Rect.fromCircle(center: sc, radius: innerR), startAngle + sweep, -sweep, false)
        ..close();

      canvas.drawPath(path, Paint()
        ..color = isSelected ? segments[i].color : segments[i].color.withOpacity(0.85));

      if (isSelected) {
        final pct = (segments[i].value / total * 100).toStringAsFixed(1);
        final tp = TextPainter(
          text: TextSpan(text: '${segments[i].label}\n$pct%',
            style: const TextStyle(color: Colors.black87, fontSize: 13, fontWeight: FontWeight.bold)),
          textAlign: TextAlign.center, textDirection: TextDirection.ltr,
        )..layout();
        final lr = outerR + 24;
        tp.paint(canvas, Offset(sc.dx + lr * cos(bisector) - tp.width / 2,
                                sc.dy + lr * sin(bisector) - tp.height / 2));
      }
      startAngle += sweep + gap;
    }
  }

  @override
  bool shouldRepaint(DonutChartPainter old) =>
      old.animationValue != animationValue || old.selectedIndex != selectedIndex;
}
```

The host widget wraps this in a `GestureDetector` with `onTapDown`, computes the distance and angle from center, and walks through cumulative angles to determine which segment index was tapped.

### Common Mistakes

**Angle math in the wrong coordinate system.** Canvas angles increase clockwise (y-axis points down). Computing tap angles in standard math coordinates (counter-clockwise) causes the wrong segment to be selected.

**Not checking the donut hole.** Without `distance > innerRadius`, tapping the center selects a segment.

---

## Exercise 4: Platform Channel for Device Info

### Progressive Hints

**Hint 1:** On Kotlin, override `configureFlutterEngine` in `MainActivity`, register the `MethodChannel`, and return data via `result.success(mapOf(...))`.

**Hint 2:** For `EventChannel`, implement `StreamHandler` with `onListen` (store the `EventSink`, start emitting) and `onCancel` (stop and release).

**Hint 3:** Always handle `PlatformException`, `MissingPluginException`, and add `.timeout()` on the Dart side.

### Full Solution

```dart
// file: lib/services/device_info_service.dart

import 'package:flutter/services.dart';

class DeviceInfo {
  final String model;
  final String osVersion;
  final int storageBytes;
  const DeviceInfo({required this.model, required this.osVersion, required this.storageBytes});
}

class DeviceInfoService {
  static const _method = MethodChannel('com.example.app/device_info');
  static const _event = EventChannel('com.example.app/battery_stream');

  Future<DeviceInfo> getInfo() async {
    try {
      final result = await _method.invokeMethod<Map>('getDeviceInfo')
          .timeout(const Duration(seconds: 5));
      return DeviceInfo(
        model: result!['model'] as String,
        osVersion: result['osVersion'] as String,
        storageBytes: result['availableStorage'] as int,
      );
    } on PlatformException catch (e) {
      throw Exception('Platform error: ${e.message}');
    } on MissingPluginException {
      throw Exception('Plugin not available on this platform.');
    }
  }

  Stream<int> get batteryStream => _event.receiveBroadcastStream()
      .map((e) => e as int)
      .handleError((e) => throw Exception('Battery error: $e'));
}
```

```kotlin
// file: android/app/src/main/kotlin/.../MainActivity.kt (key section)

MethodChannel(flutterEngine.dartExecutor.binaryMessenger, "com.example.app/device_info")
    .setMethodCallHandler { call, result ->
        when (call.method) {
            "getDeviceInfo" -> {
                val stat = StatFs(Environment.getDataDirectory().path)
                result.success(mapOf(
                    "model" to Build.MODEL,
                    "osVersion" to "Android ${Build.VERSION.RELEASE}",
                    "availableStorage" to stat.availableBlocksLong * stat.blockSizeLong
                ))
            }
            else -> result.notImplemented()
        }
    }
```

### Common Mistakes

**Channel name mismatch.** The string must be byte-for-byte identical in Dart and native. A typo causes `MissingPluginException` with no helpful message. Always copy-paste.

**Forgetting `result.notImplemented()`.** Without this fallback, calling an unrecognized method hangs the Dart `Future` forever.

**Wrong return type.** If Dart expects `invokeMethod<Map>` but native returns a `String`, you get a runtime cast error.

---

## Exercise 5: Custom Sliver with Parallax Effect

### Progressive Hints

**Hint 1:** `SliverConstraints.scrollOffset` tells you how far the sliver has scrolled past the viewport top. Use it to compute the parallax translation.

**Hint 2:** `SliverGeometry` needs `scrollExtent` (total logical height), `paintExtent` (how much currently paints -- clamp between 0 and `remainingPaintExtent`), and `maxPaintExtent`.

**Hint 3:** In `paint`, offset the background child by `scrollOffset * (1 - parallaxFactor)`. This makes it move slower than the foreground.

### Full Solution

```dart
// file: lib/slivers/sliver_parallax.dart

import 'dart:math';
import 'package:flutter/rendering.dart';
import 'package:flutter/widgets.dart';

class SliverParallax extends SingleChildRenderObjectWidget {
  final double extent;
  final double parallaxFactor;

  const SliverParallax({super.key, required super.child,
    required this.extent, this.parallaxFactor = 0.5});

  @override
  RenderObject createRenderObject(BuildContext context) =>
      RenderSliverParallax(extent: extent, parallaxFactor: parallaxFactor);

  @override
  void updateRenderObject(BuildContext context, RenderSliverParallax renderObject) {
    renderObject..extent = extent..parallaxFactor = parallaxFactor;
  }
}

class RenderSliverParallax extends RenderSliverSingleBoxAdapter {
  double _extent;
  double _parallaxFactor;

  RenderSliverParallax({required double extent, required double parallaxFactor})
      : _extent = extent, _parallaxFactor = parallaxFactor;

  set extent(double v) { if (_extent != v) { _extent = v; markNeedsLayout(); } }
  set parallaxFactor(double v) {
    final clamped = v.clamp(0.0, 1.0);
    if (_parallaxFactor != clamped) { _parallaxFactor = clamped; markNeedsLayout(); }
  }

  @override
  void performLayout() {
    if (child == null) { geometry = SliverGeometry.zero; return; }
    final scrollOffset = constraints.scrollOffset;
    final paintExtent = max(0.0, min(_extent - scrollOffset, constraints.remainingPaintExtent));

    child!.layout(constraints.asBoxConstraints(maxExtent: _extent), parentUsesSize: true);
    geometry = SliverGeometry(
      scrollExtent: _extent, paintExtent: paintExtent,
      maxPaintExtent: _extent, layoutExtent: paintExtent,
    );
  }

  @override
  void paint(PaintingContext context, Offset offset) {
    if (child == null || geometry!.paintExtent == 0) return;
    final parallaxOffset = constraints.scrollOffset * (1 - _parallaxFactor);
    context.paintChild(child!, offset + Offset(0, -parallaxOffset));
  }
}
```

### Common Mistakes

**Negative `paintExtent`.** If `scrollOffset > extent`, the subtraction goes negative. Always clamp to zero. A negative value triggers an assertion.

**Forgetting `markNeedsLayout()` in setters.** Without it, changing `extent` or `parallaxFactor` at runtime has no visible effect because the sliver uses stale geometry.

---

## Exercise 6: Bidirectional Platform Communication with Pigeon

### Progressive Hints

**Hint 1:** `@HostApi()` = Dart calls native. `@FlutterApi()` = native calls Dart. Data classes use `late` fields.

**Hint 2:** Run: `dart run pigeon --input pigeons/background_task.dart`. Configure outputs with `@ConfigurePigeon`.

**Hint 3:** On the Dart side, implement the `@FlutterApi()` interface as a concrete class, then call the generated `setUp` method to register it. Without this, native invocations are silently lost.

### Full Solution

```dart
// file: pigeons/background_task.dart

import 'package:pigeon/pigeon.dart';

@ConfigurePigeon(PigeonOptions(
  dartOut: 'lib/src/generated/background_task_api.dart',
  kotlinOut: 'android/app/src/main/kotlin/com/example/app/BackgroundTaskApi.kt',
  swiftOut: 'ios/Runner/BackgroundTaskApi.swift',
))

class TaskConfig { late String taskId; late String taskType; late Map<String, String> parameters; }
class ProgressUpdate { late String taskId; late double progressFraction; late String statusMessage; }
class TaskResult { late String taskId; late bool success; late String? data; late String? errorMessage; }

@HostApi()
abstract class BackgroundTaskHostApi {
  void startTask(TaskConfig config);
  void cancelTask(String taskId);
}

@FlutterApi()
abstract class BackgroundTaskFlutterApi {
  void onProgress(ProgressUpdate update);
  void onComplete(TaskResult result);
}
```

```dart
// file: lib/services/background_task_service.dart

import '../src/generated/background_task_api.dart';

class BackgroundTaskService implements BackgroundTaskFlutterApi {
  final BackgroundTaskHostApi _hostApi;
  void Function(ProgressUpdate)? onProgressCallback;
  void Function(TaskResult)? onCompleteCallback;

  BackgroundTaskService({BackgroundTaskHostApi? hostApi})
      : _hostApi = hostApi ?? BackgroundTaskHostApi();

  void initialize() => BackgroundTaskFlutterApi.setUp(this);

  void startTask(String taskId, String taskType) {
    _hostApi.startTask(TaskConfig()..taskId = taskId..taskType = taskType..parameters = {});
  }

  void cancelTask(String taskId) => _hostApi.cancelTask(taskId);

  @override void onProgress(ProgressUpdate update) => onProgressCallback?.call(update);
  @override void onComplete(TaskResult result) => onCompleteCallback?.call(result);

  void dispose() {
    BackgroundTaskFlutterApi.setUp(null);
    onProgressCallback = null;
    onCompleteCallback = null;
  }
}
```

### Common Mistakes

**Forgetting to call `setUp`.** The generated static method registers your Dart callbacks. Without it, native calls into `FlutterApi` are silently dropped.

**Pigeon `late` fields not initialized by native.** If the native side skips a field, Dart throws `LateInitializationError` at runtime. Ensure native code populates every field.

---

## Exercise 7: Full Chart Library with CustomPainter

### Progressive Hints

**Hint 1 (Axis ticks):** Calculate "nice" numbers by dividing the data range by desired tick count, then rounding to the nearest 1, 2, 5, 10, 20, 50, etc. This avoids awkward values like 7.3.

**Hint 2 (Smooth lines):** For cubic bezier curves, compute control points on the tangent through the previous and next data points, at a distance proportional to segment length.

**Hint 3 (Hit testing):** Bars are `Rect.contains()`. Line chart points: expand each into a circle with radius ~20px for tap targets. Pie chart: angle + distance from center, same as Exercise 3.

**Hint 4 (Performance):** Use `foregroundPainter` for the interactive layer (crosshair, tooltips) and `painter` for the static layer. The static layer only repaints when data changes. This prevents every touch event from redrawing axes and data points.

### Key Architecture Decisions

The chart library needs three layers: a **data layer** (`ChartDataSet`, `DataSeries`, `DataPoint`, `PieEntry`), a **theme layer** (`ChartTheme` with light/dark defaults), and a **rendering layer** (one painter per chart type). Each chart widget owns an `AnimationController` for entrance animation and a `GestureDetector` for touch.

```dart
// file: lib/charts/core/chart_utils.dart

import 'dart:math';

class ChartUtils {
  static double niceNumber(double range, bool round) {
    final exp = (log(range) / ln10).floor();
    final frac = range / pow(10, exp);
    final nice = round
        ? (frac < 1.5 ? 1 : frac < 3 ? 2 : frac < 7 ? 5 : 10)
        : (frac <= 1 ? 1 : frac <= 2 ? 2 : frac <= 5 ? 5 : 10);
    return nice * pow(10, exp).toDouble();
  }

  static ({double min, double max, double step}) ticks(double lo, double hi, {int n = 5}) {
    if (lo == hi) return (min: lo - 1, max: hi + 1, step: 1);
    final range = niceNumber(hi - lo, false);
    final step = niceNumber(range / (n - 1), true);
    return (min: (lo / step).floor() * step, max: (hi / step).ceil() * step, step: step);
  }
}
```

For each chart painter, the critical contract is: check for empty data and small size early, delegate axis drawing to shared utility methods, and implement `shouldRepaint` that compares only the fields that affect visual output (animation value, selected index, data identity).

The line chart painter draws area fill by cloning the line path, adding `lineTo` down to the baseline, and filling with a vertical gradient. The bar chart handles negative values by rendering bars downward from the zero line. The pie chart skips labels for segments under 1% to avoid visual clutter.

### Common Mistakes

**Always returning `true` from `shouldRepaint`.** This repaints every frame even when idle. For a complex chart with hundreds of data points, this tanks performance. Compare the specific fields that changed.

**Not handling empty or single-point data.** Passing an empty list should render a "No data" message, not crash. A single point on a line chart should render as a dot.

---

## Exercise 8: Federated Flutter Plugin with Native View Embedding

### Progressive Hints

**Hint 1:** Start with `flutter create --template=plugin --platforms=android,ios image_filter`, then restructure into federated layout: main package, platform interface package, per-platform packages.

**Hint 2:** Register `PlatformViewFactory` on Android in `onAttachedToEngine` and `FlutterPlatformViewFactory` on iOS. The factory creates the native camera view when Flutter requests it via `AndroidView`/`UiKitView`.

**Hint 3:** Use `EventChannel` for frame streaming alongside Pigeon for request-response calls. They coexist on the same plugin.

### Key Architecture Decisions

The platform interface defines the contract. Every method either returns a `Future` (for one-shot operations like `initialize`, `captureFrame`) or a `Stream` (for continuous data like processed frames). The app-facing package delegates to `ImageFilterPlatform.instance` and adds lifecycle guards (`_assertInitialized`).

```dart
// file: image_filter_platform_interface/lib/image_filter_platform_interface.dart

abstract class ImageFilterPlatform extends PlatformInterface {
  // ...
  Future<void> initializeCamera();
  Future<void> applyFilter(FilterConfig config);
  Future<void> dispose();
  Stream<ProcessedFrame> get frameStream;
}
```

```dart
// file: image_filter/lib/image_filter.dart

class ImageFilter {
  bool _initialized = false;

  Future<void> initialize() async {
    await ImageFilterPlatform.instance.initializeCamera();
    _initialized = true;
  }

  Future<void> applyFilter(FilterConfig config) {
    _assertInitialized();
    return ImageFilterPlatform.instance.applyFilter(config);
  }

  Stream<ProcessedFrame> get frameStream {
    _assertInitialized();
    return ImageFilterPlatform.instance.frameStream;
  }

  void _assertInitialized() {
    if (!_initialized) throw StateError('Call initialize() first.');
  }
}
```

For testing, create a `MockImageFilterPlatform` that implements the interface with a `StreamController<ProcessedFrame>`. Test that `applyFilter` throws before initialization, that filters propagate correctly, and that disposal closes the stream.

```dart
// file: image_filter/test/image_filter_test.dart

class MockImageFilterPlatform extends ImageFilterPlatform {
  bool initialized = false;
  FilterConfig? lastFilter;
  final _frames = StreamController<ProcessedFrame>.broadcast();

  @override Future<void> initializeCamera() async => initialized = true;
  @override Future<void> applyFilter(FilterConfig c) async => lastFilter = c;
  @override Future<void> dispose() async => await _frames.close();
  @override Stream<ProcessedFrame> get frameStream => _frames.stream;

  void emitFrame(ProcessedFrame f) => _frames.add(f);
}

// Tests verify: initialization flag, pre-init StateError, filter propagation, stream emission, disposal
```

### Common Mistakes

**Not disposing native resources.** Navigating away without calling `dispose` leaves the camera session open, draining battery and blocking other apps. Always dispose in the widget's `dispose()` method.

**Platform view performance.** Each `AndroidView`/`UiKitView` creates a separate rendering surface. More than two or three on screen simultaneously causes frame drops. Virtualize if needed.

**Assuming the stream starts immediately.** The `EventChannel` may not emit until the camera finishes initializing. Listening before init produces no events -- not an error, but it confuses the UI if not handled.

---

## Debugging Tips

**CustomPainter does not update:** Check `shouldRepaint` -- it probably returns `false` when data changed. Second cause: you forgot `setState` to trigger a widget rebuild. The painter only checks `shouldRepaint` during rebuilds.

**Sliver assertion "geometry is not valid":** Your `SliverGeometry` violates an invariant. Common violations: `paintExtent > maxPaintExtent`, `paintExtent > remainingPaintExtent`, or negative values. Print constraints and computed values to find the mismatch.

**MissingPluginException on hot restart:** Platform channel registrations happen once during engine setup. Hot restart re-runs Dart but may not re-register native handlers. Do a full restart (stop and re-run).

**Native view shows black rectangle:** The factory creates the view but it has zero size or empty content. Verify the native view's layout parameters and that rendering logic runs in the correct lifecycle callback.

**Shader compilation jank on first frame:** Fragment shaders compile at first use. Pre-compile during your splash screen by drawing one offscreen frame with each shader.

---

## Alternatives and Trade-offs

| Need | Simple approach | Advanced approach | When to go advanced |
|------|----------------|-------------------|---------------------|
| Custom drawing | fl_chart / graphic package | CustomPainter | When no package matches your exact design |
| Complex scrolling | shrinkWrap ListViews (avoid) | Custom slivers | Always use slivers for mixed scroll layouts |
| Native calls | Raw MethodChannel | Pigeon type-safe channels | Any non-trivial channel communication |
| Native UI | Texture widget | AndroidView / UiKitView | When you need a full native widget, not just pixels |
| GPU effects | Canvas API paths and gradients | FragmentProgram shaders | Per-pixel effects (noise, distortion, color grading) |
| Custom layout | Stack + Positioned | RenderObject | Only when no widget combination achieves the layout |
