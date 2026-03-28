# Section 20: Flutter Advanced UI -- CustomPainter, Slivers, Platform Channels & Plugins

## Introduction

Every Flutter developer eventually reaches the boundary of what built-in widgets can do. You need a chart that does not exist in any package. A scroll effect that no `ListView` parameter supports. A sensor reading that only the native OS can provide. This section is about crossing those boundaries deliberately and safely.

We will work at three distinct levels. First, the **rendering layer**: CustomPainter and CustomClipper let you draw anything the GPU can produce -- lines, arcs, gradients, shaders -- pixel by pixel. Second, the **scroll layer**: Slivers give you granular control over how content behaves inside scrollable areas, far beyond what ListView and GridView expose. Third, the **platform layer**: Platform Channels and Plugins let your Dart code talk to native Android (Kotlin/Java) and iOS (Swift/Objective-C) code, embedding native views and consuming native APIs that Flutter cannot access on its own.

These are not everyday tools. Most screens are built perfectly well with standard widgets. But when they are not enough, these capabilities separate an app that compromises from one that delivers exactly what the design demands. This is the final section of the curriculum, and it assumes you are comfortable with everything that came before.

## Prerequisites

- **Section 09 (Flutter Setup & Widgets):** Widget lifecycle, StatefulWidget, BuildContext.
- **Section 10 (Layouts):** Constraints, RenderBox basics, the layout protocol.
- **Section 12 (State Basics):** setState, state management fundamentals for driving repaints.
- **Section 16 (Animations):** AnimationController, Tween, ticker providers -- CustomPainter animations depend on these.
- **Section 19 (Performance):** Repaint boundaries, profiling tools -- custom rendering can easily become a performance bottleneck.
- A Flutter project targeting both Android and iOS (or at minimum one platform with an emulator).

## Learning Objectives

By the end of this section you will be able to:

1. **Draw** custom shapes, paths, and gradients on a Canvas using CustomPainter and the Paint API.
2. **Implement** CustomClipper to create non-rectangular clipping regions with optional animation.
3. **Compose** complex scroll layouts using CustomScrollView with SliverList, SliverGrid, SliverAppBar, and SliverPersistentHeader.
4. **Build** a custom RenderSliver that computes its own SliverGeometry from SliverConstraints.
5. **Establish** bidirectional communication between Dart and native code using MethodChannel, EventChannel, and BasicMessageChannel.
6. **Structure** a federated Flutter plugin with platform-specific implementations for Android and iOS.
7. **Generate** type-safe platform channel interfaces using Pigeon.
8. **Embed** native views in Flutter using AndroidView, UiKitView, and PlatformViewLink.
9. **Apply** fragment shaders to widgets using FragmentProgram for GPU-accelerated visual effects.
10. **Create** custom RenderObjects that define their own layout and paint behavior at the render tree level.

---

## Core Concepts

### 1. CustomPainter -- Drawing on a Canvas

CustomPainter is your escape hatch from the widget tree into raw drawing commands. You get a `Canvas` and a `Size`, and you draw whatever you want. The reason this exists is that some visual outputs -- charts, waveforms, signature pads, game graphics -- simply cannot be expressed as a composition of rectangular widgets.

```dart
// file: lib/painters/circle_painter.dart

import 'package:flutter/material.dart';

class CirclePainter extends CustomPainter {
  final double progress;
  final Color color;

  CirclePainter({required this.progress, required this.color});

  @override
  void paint(Canvas canvas, Size size) {
    final center = Offset(size.width / 2, size.height / 2);
    final radius = size.width / 2 * 0.8;

    // Background circle
    final backgroundPaint = Paint()
      ..color = color.withOpacity(0.2)
      ..style = PaintingStyle.stroke
      ..strokeWidth = 8.0;
    canvas.drawCircle(center, radius, backgroundPaint);

    // Progress arc
    final progressPaint = Paint()
      ..color = color
      ..style = PaintingStyle.stroke
      ..strokeWidth = 8.0
      ..strokeCap = StrokeCap.round;

    final sweepAngle = 2 * 3.14159 * progress;
    canvas.drawArc(
      Rect.fromCircle(center: center, radius: radius),
      -3.14159 / 2, // Start from top
      sweepAngle,
      false,
      progressPaint,
    );
  }

  @override
  bool shouldRepaint(CirclePainter oldDelegate) {
    return oldDelegate.progress != progress || oldDelegate.color != color;
  }
}
```

**Why `shouldRepaint` matters:** Flutter calls `shouldRepaint` every time the widget rebuilds. If you return `true` when nothing changed, you waste GPU cycles. If you always return `false`, your painter never updates. Compare the fields that actually affect the visual output.

```dart
// file: lib/painters/path_painter.dart

import 'package:flutter/material.dart';

class MountainPainter extends CustomPainter {
  @override
  void paint(Canvas canvas, Size size) {
    final path = Path();
    path.moveTo(0, size.height);
    path.lineTo(size.width * 0.2, size.height * 0.4);
    path.lineTo(size.width * 0.35, size.height * 0.65);
    path.lineTo(size.width * 0.55, size.height * 0.2);
    path.lineTo(size.width * 0.75, size.height * 0.5);
    path.lineTo(size.width, size.height * 0.3);
    path.lineTo(size.width, size.height);
    path.close();

    final gradient = LinearGradient(
      begin: Alignment.topCenter,
      end: Alignment.bottomCenter,
      colors: [Colors.blue.shade700, Colors.blue.shade200],
    );

    final paint = Paint()
      ..shader = gradient.createShader(
        Rect.fromLTWH(0, 0, size.width, size.height),
      );

    canvas.drawPath(path, paint);
  }

  @override
  bool shouldRepaint(covariant CustomPainter oldDelegate) => false;
}
```

Use `CustomPaint` to place a painter in the widget tree:

```dart
// file: lib/widgets/mountain_widget.dart

CustomPaint(
  size: const Size(double.infinity, 200),
  painter: MountainPainter(),
)
```

### 2. CustomClipper -- Non-Rectangular Clipping

CustomClipper carves widgets into arbitrary shapes. Where CustomPainter draws on a blank canvas, CustomClipper cuts the visible region of an existing widget.

```dart
// file: lib/clippers/wave_clipper.dart

import 'package:flutter/material.dart';

class WaveClipper extends CustomClipper<Path> {
  final double animationValue;

  WaveClipper({this.animationValue = 0.0});

  @override
  Path getClip(Size size) {
    final path = Path();
    path.lineTo(0, size.height * 0.75);

    final controlPoint1 = Offset(
      size.width * 0.25,
      size.height * (0.75 + 0.1 * (1 + animationValue)),
    );
    final controlPoint2 = Offset(
      size.width * 0.75,
      size.height * (0.75 - 0.1 * (1 + animationValue)),
    );
    final endPoint = Offset(size.width, size.height * 0.75);

    path.cubicTo(
      controlPoint1.dx, controlPoint1.dy,
      controlPoint2.dx, controlPoint2.dy,
      endPoint.dx, endPoint.dy,
    );

    path.lineTo(size.width, 0);
    path.close();
    return path;
  }

  @override
  bool shouldReclip(WaveClipper oldClipper) {
    return oldClipper.animationValue != animationValue;
  }
}
```

### 3. Slivers -- The Scroll Building Blocks

Every scrollable widget in Flutter is built on slivers internally. A `ListView` is a convenience wrapper around a `CustomScrollView` containing a single `SliverList`. When you need to combine different scroll behaviors -- a collapsing header, then a grid, then a list, then a footer -- you work with slivers directly.

**Why not just nest ListViews?** Nesting scrollables creates competing scroll controllers and degrades performance because inner lists cannot share the viewport's lazy rendering. Slivers solve this by letting a single `CustomScrollView` coordinate all its children.

```dart
// file: lib/screens/sliver_demo_screen.dart

import 'package:flutter/material.dart';

class SliverDemoScreen extends StatelessWidget {
  const SliverDemoScreen({super.key});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: CustomScrollView(
        slivers: [
          SliverAppBar(
            expandedHeight: 200, floating: false, pinned: true,
            flexibleSpace: FlexibleSpaceBar(
              title: const Text('Sliver Demo'),
              background: Container(color: Colors.indigo),
            ),
          ),
          SliverPersistentHeader(
            pinned: true,
            delegate: _SectionHeaderDelegate('Categories'),
          ),
          SliverGrid(
            gridDelegate: const SliverGridDelegateWithFixedCrossAxisCount(
              crossAxisCount: 3, mainAxisSpacing: 8, crossAxisSpacing: 8,
            ),
            delegate: SliverChildBuilderDelegate(
              (context, index) => Card(
                color: Colors.primaries[index % Colors.primaries.length],
                child: Center(child: Text('Cat $index')),
              ),
              childCount: 9,
            ),
          ),
          SliverToBoxAdapter(
            child: Padding(
              padding: const EdgeInsets.all(16),
              child: Text('Recent Items', style: Theme.of(context).textTheme.headlineSmall),
            ),
          ),
          SliverList(
            delegate: SliverChildBuilderDelegate(
              (context, index) => ListTile(title: Text('Item $index')),
              childCount: 20,
            ),
          ),
        ],
      ),
    );
  }
}

class _SectionHeaderDelegate extends SliverPersistentHeaderDelegate {
  final String title;
  _SectionHeaderDelegate(this.title);

  @override double get minExtent => 48;
  @override double get maxExtent => 48;

  @override
  Widget build(BuildContext context, double shrinkOffset, bool overlapsContent) {
    return Container(
      color: Theme.of(context).colorScheme.surface,
      alignment: Alignment.centerLeft,
      padding: const EdgeInsets.symmetric(horizontal: 16),
      child: Text(title, style: Theme.of(context).textTheme.titleMedium),
    );
  }

  @override
  bool shouldRebuild(_SectionHeaderDelegate old) => old.title != title;
}
```

### 4. NestedScrollView and Coordinated Scrolling

When you need a scrollable body beneath a SliverAppBar that also contains a TabBarView (itself scrollable), `NestedScrollView` coordinates the two scroll positions. The classic use case is a profile screen: a collapsing header with a TabBar, where each tab has its own scrollable list.

```dart
// file: lib/screens/nested_scroll_screen.dart

NestedScrollView(
  headerSliverBuilder: (context, innerBoxIsScrolled) => [
    SliverAppBar(
      expandedHeight: 200,
      pinned: true,
      forceElevated: innerBoxIsScrolled,
      flexibleSpace: const FlexibleSpaceBar(title: Text('Profile')),
      bottom: const TabBar(tabs: [Tab(text: 'Posts'), Tab(text: 'Likes')]),
    ),
  ],
  body: TabBarView(children: [
    ListView.builder(itemCount: 50, itemBuilder: (_, i) => ListTile(title: Text('Post $i'))),
    ListView.builder(itemCount: 50, itemBuilder: (_, i) => ListTile(title: Text('Like $i'))),
  ]),
)
```

The key insight: `NestedScrollView` connects the outer scroll (the SliverAppBar collapsing) with the inner scroll (the TabBarView lists) so they feel like a single gesture. Without it, you get two competing scroll controllers.

### 5. Platform Channels -- Bridging to Native Code

Platform channels are the communication mechanism between Dart and native platform code. There are three types, each for a different communication pattern.

**MethodChannel** is for request-response calls. Dart calls a method on the native side and awaits a result. Think of it as an RPC mechanism.

```dart
// file: lib/services/battery_service.dart

import 'package:flutter/services.dart';

class BatteryService {
  static const _channel = MethodChannel('com.example.app/battery');

  Future<int> getBatteryLevel() async {
    try {
      final int level = await _channel.invokeMethod('getBatteryLevel');
      return level;
    } on PlatformException catch (e) {
      throw Exception('Failed to get battery level: ${e.message}');
    } on MissingPluginException {
      throw Exception(
        'Battery plugin not available on this platform.',
      );
    }
  }
}
```

**EventChannel** is for continuous streams of data from native to Dart. Sensor readings, location updates, Bluetooth scan results -- anything that produces values over time.

```dart
// file: lib/services/accelerometer_service.dart

import 'package:flutter/services.dart';

class AccelerometerService {
  static const _eventChannel = EventChannel('com.example.app/accelerometer');

  Stream<AccelerometerReading> get readings {
    return _eventChannel.receiveBroadcastStream().map((event) {
      final data = Map<String, double>.from(event as Map);
      return AccelerometerReading(
        x: data['x']!,
        y: data['y']!,
        z: data['z']!,
      );
    });
  }
}

class AccelerometerReading {
  final double x, y, z;
  const AccelerometerReading({required this.x, required this.y, required this.z});
}
```

### 6. Pigeon -- Type-Safe Platform Channels

Hand-writing platform channel code is error-prone. String-based method names, untyped arguments, manual serialization -- any mismatch between Dart and native code becomes a runtime crash. Pigeon generates type-safe interfaces for both sides from a single definition file.

```dart
// file: pigeons/device_info.dart

import 'package:pigeon/pigeon.dart';

class DeviceInfoResult {
  late String model;
  late String osVersion;
  late int batteryLevel;
  late bool isCharging;
}

@HostApi()
abstract class DeviceInfoApi {
  DeviceInfoResult getDeviceInfo();
  String getDeviceId();
}

@FlutterApi()
abstract class DeviceEventApi {
  void onBatteryChanged(int level, bool isCharging);
}
```

Run the Pigeon code generator, and it produces Dart, Kotlin, and Swift files with matching interfaces. No string-based method names, no manual casting.

### 7. Native View Embedding

Sometimes you need to embed a native UI component -- a Google Map, a WebView, a camera preview -- directly in the Flutter widget tree. `AndroidView` and `UiKitView` create a "hole" in the Flutter rendering surface where the native view appears.

```dart
// file: lib/widgets/native_map_view.dart

Widget build(BuildContext context) {
  final params = {'latitude': latitude, 'longitude': longitude};
  const viewType = 'com.example.app/native-map';
  const codec = StandardMessageCodec();

  if (Platform.isAndroid) {
    return AndroidView(viewType: viewType, creationParams: params, creationParamsCodec: codec);
  } else if (Platform.isIOS) {
    return UiKitView(viewType: viewType, creationParams: params, creationParamsCodec: codec);
  }
  return const Center(child: Text('Platform not supported'));
}
```

Each native view creates a separate rendering surface, which is expensive. Limit to two or three on screen simultaneously. For pixel-buffer content (video, camera), prefer the `Texture` widget instead.

### 8. Fragment Shaders

Flutter supports custom fragment shaders written in GLSL. These run on the GPU and can produce effects impossible to achieve with the Canvas API alone -- noise patterns, distortion effects, color transformations applied per-pixel.

```dart
// file: lib/painters/shader_painter.dart

import 'dart:ui' as ui;
import 'package:flutter/material.dart';

class ShaderPainter extends CustomPainter {
  final ui.FragmentShader shader;
  final double time;

  ShaderPainter({required this.shader, required this.time});

  @override
  void paint(Canvas canvas, Size size) {
    shader.setFloat(0, size.width);
    shader.setFloat(1, size.height);
    shader.setFloat(2, time);

    final paint = Paint()..shader = shader;
    canvas.drawRect(
      Rect.fromLTWH(0, 0, size.width, size.height),
      paint,
    );
  }

  @override
  bool shouldRepaint(ShaderPainter oldDelegate) {
    return oldDelegate.time != time;
  }
}
```

Load the shader from an asset:

```dart
// file: lib/screens/shader_screen.dart

Future<ui.FragmentProgram> loadShader() async {
  final program = await ui.FragmentProgram.fromAsset('shaders/ripple.frag');
  return program;
}
```

### 9. RenderObject -- The Lowest Level

When neither widgets nor CustomPainter give you enough control, you drop to RenderObject. You define how a component measures itself, positions its children, and paints. The contract: extend `MultiChildRenderObjectWidget` for the widget layer, provide a `RenderBox` subclass with `performLayout` and `paint` overrides, and use `ParentData` to store per-child positioning.

```dart
// file: lib/render_objects/diagonal_layout.dart

class DiagonalLayout extends MultiChildRenderObjectWidget {
  final double spacing;
  const DiagonalLayout({super.key, required super.children, this.spacing = 20.0});

  @override
  RenderObject createRenderObject(BuildContext context) =>
      RenderDiagonalLayout(spacing: spacing);

  @override
  void updateRenderObject(BuildContext context, RenderDiagonalLayout renderObject) {
    renderObject.spacing = spacing;
  }
}

class RenderDiagonalLayout extends RenderBox
    with ContainerRenderObjectMixin<RenderBox, ContainerBoxParentData<RenderBox>>,
         RenderBoxContainerDefaultsMixin<RenderBox, ContainerBoxParentData<RenderBox>> {
  double _spacing;
  RenderDiagonalLayout({required double spacing}) : _spacing = spacing;

  set spacing(double value) {
    if (_spacing == value) return;
    _spacing = value;
    markNeedsLayout(); // Critical -- without this, changes are invisible
  }

  @override
  void performLayout() {
    double dx = 0, dy = 0, maxW = 0, maxH = 0;
    RenderBox? child = firstChild;
    while (child != null) {
      child.layout(constraints.loosen(), parentUsesSize: true);
      final pd = child.parentData! as ContainerBoxParentData<RenderBox>;
      pd.offset = Offset(dx, dy);
      dx += _spacing; dy += _spacing;
      maxW = dx + child.size.width; maxH = dy + child.size.height;
      child = pd.nextSibling;
    }
    size = constraints.constrain(Size(maxW, maxH));
  }

  @override void paint(PaintingContext context, Offset offset) => defaultPaint(context, offset);
  @override bool hitTestChildren(BoxHitTestResult result, {required Offset position}) =>
      defaultHitTestChildren(result, position: position);
}
```

The critical detail is `markNeedsLayout()` in every setter that affects layout. Without it, property changes are invisible to the rendering pipeline.

---

## Exercises

### Exercise 1 (Basic): Shape Gallery with CustomPainter

**Goal:** Build a screen that displays six different shapes drawn with CustomPainter -- a filled circle, a stroked rectangle, a rounded triangle, a star, a dashed line, and a gradient ring.

**Instructions:**

1. Create a `ShapeGalleryScreen` with a 2x3 grid of `CustomPaint` widgets.
2. Implement a separate `CustomPainter` subclass for each shape.
3. The star painter must use `Path` with `moveTo` and `lineTo` to draw a 5-pointed star.
4. The dashed line painter must simulate dashes by drawing multiple short line segments (Canvas has no native dash API -- you must compute the dash positions).
5. The gradient ring must use `Paint.shader` with a `SweepGradient`.
6. Each painter must implement `shouldRepaint` correctly -- return `false` since these are static shapes.

**Verification:**
- All six shapes render without overflow or clipping.
- Hot reload does not cause unnecessary repaints (verify by adding a print statement in `paint` and confirming it does not fire on rebuild if `shouldRepaint` returns false).
- The star has five distinct points and the path closes cleanly (no gaps at the starting point).

```dart
// file: lib/painters/star_painter.dart
// Compute 5-point star vertices using trigonometry

// file: lib/painters/dashed_line_painter.dart
// Calculate dash segments manually

// file: lib/painters/gradient_ring_painter.dart
// Use SweepGradient as the shader for Paint

// file: lib/screens/shape_gallery_screen.dart
// 2x3 grid of CustomPaint widgets
```

---

### Exercise 2 (Basic): SliverAppBar with a Mixed Scroll Layout

**Goal:** Build a product catalog screen using `CustomScrollView` with a collapsing `SliverAppBar`, a pinned category header, a horizontal category grid, a section title, and a vertical product list.

**Instructions:**

1. The `SliverAppBar` should have `expandedHeight: 250`, `pinned: true`, and a `FlexibleSpaceBar` with a background image (use a `ColoredBox` with gradient if you prefer).
2. Below the app bar, add a `SliverPersistentHeader` with `pinned: true` that acts as a category filter bar. It should stay visible when the user scrolls down.
3. Use `SliverToBoxAdapter` to wrap a horizontally-scrolling row of category chips.
4. Add a `SliverGrid` with `crossAxisCount: 2` showing product cards (at least 12 items).
5. Below the grid, add a `SliverList` with a "Reviews" section (at least 10 review items).
6. Add a `SliverFillRemaining` at the bottom with a "You've reached the end" message that fills whatever space remains.

**Verification:**
- Scroll slowly and observe the SliverAppBar collapsing. The title should remain visible when pinned.
- The category header stays pinned below the collapsed app bar.
- The horizontal chip row scrolls independently of the vertical scroll.
- Scroll to the very bottom. The "end" message fills the remaining viewport.
- Test with different screen sizes to confirm the layout adapts.

```dart
// file: lib/screens/product_catalog_screen.dart
// CustomScrollView composing all sliver types

// file: lib/widgets/pinned_category_header.dart
// SliverPersistentHeaderDelegate implementation
```

---

### Exercise 3 (Intermediate): Animated Donut Chart with Touch Interaction

**Goal:** Build an animated donut chart using CustomPainter that responds to touch. Tapping a segment should expand it outward and display its label and value.

**Instructions:**

1. Create a `DonutChartPainter` that draws segments as arcs with gaps between them.
2. Each segment has a color, label, value, and percentage of the total.
3. Animate the chart entrance: segments should grow from 0 to their full sweep angle over 800ms using an `AnimationController`.
4. On tap, detect which segment was tapped using `GestureDetector` and hit-testing with `Path.contains`. The tapped segment should animate outward (translate along its bisector angle) by 10 pixels.
5. Display a tooltip near the tapped segment showing its label and percentage.
6. Implement `shouldRepaint` that compares both the data and the animation value.

**Verification:**
- The chart animates in smoothly on first render.
- Tapping a segment expands it and shows the tooltip. Tapping another segment collapses the first and expands the second.
- Tapping outside any segment collapses all segments.
- Resize the window (on desktop/web) and confirm the chart scales proportionally.
- Pass an empty data list and verify no crash occurs (edge case: the painter should draw nothing gracefully).

```dart
// file: lib/charts/donut_chart.dart
// Stateful widget with AnimationController

// file: lib/charts/donut_chart_painter.dart
// CustomPainter with segment data, animation value, and selected index

// file: lib/models/chart_segment.dart
// Data model for chart segments
```

---

### Exercise 4 (Intermediate): Platform Channel for Device Info

**Goal:** Implement a MethodChannel that retrieves device information (model name, OS version, available storage) from native code, and an EventChannel that streams battery level changes.

**Instructions:**

1. Define a `MethodChannel` named `com.example.app/device_info`.
2. Implement the Dart side: a `DeviceInfoService` class with a `Future<DeviceInfo> getInfo()` method.
3. On Android (Kotlin), implement the method call handler that returns the device model, Android version, and available storage in bytes.
4. On iOS (Swift), implement the equivalent using `UIDevice` and `FileManager`.
5. Define an `EventChannel` named `com.example.app/battery_stream` that emits battery level as an integer every time it changes.
6. Handle all three failure cases in Dart: `PlatformException` (native code threw an error), `MissingPluginException` (no native implementation), and timeout (wrap in `Future.timeout`).
7. Build a screen that displays the device info and a live battery indicator.

**Verification:**
- Run on Android: device info displays correctly. Run on iOS: same.
- Disconnect the battery EventChannel listener and verify no memory leak (check the debug console for "Stream was disposed" or equivalent).
- Call `getInfo()` on a platform without the native implementation (e.g., web) and verify the `MissingPluginException` is caught and a user-friendly message appears.
- Verify the channel name string matches exactly between Dart and native -- a single typo causes a silent `MissingPluginException`.

```dart
// file: lib/services/device_info_service.dart
// MethodChannel and EventChannel Dart implementation

// file: lib/models/device_info.dart
// Data class for device information

// file: android/app/src/main/kotlin/.../DeviceInfoPlugin.kt
// Kotlin MethodChannel handler

// file: ios/Runner/DeviceInfoPlugin.swift
// Swift MethodChannel handler

// file: lib/screens/device_info_screen.dart
// UI displaying results and battery stream
```

---

### Exercise 5 (Advanced): Custom Sliver with Parallax Effect

**Goal:** Build a custom `RenderSliver` that implements a parallax scrolling effect -- background images scroll at half the speed of foreground content, creating a depth illusion.

**Instructions:**

1. Create a `SliverParallax` widget that takes a `background` widget and a `foreground` widget.
2. Implement the corresponding `RenderSliverParallax` that extends `RenderSliver`.
3. In `performLayout`, calculate `SliverGeometry` based on the `SliverConstraints`. The background should paint at an offset that is half the scroll offset (parallax factor 0.5).
4. Make the parallax factor configurable (0.0 = no parallax, 1.0 = scrolls with content).
5. The foreground content should overlay the background normally.
6. Integrate it into a `CustomScrollView` with multiple parallax sections interspersed with regular `SliverList` content.

**Verification:**
- Scroll slowly and observe the background moving at half speed relative to the foreground.
- Change the parallax factor to 0.0 -- the background should remain fixed. Change to 1.0 -- it should scroll at normal speed.
- Scroll rapidly and confirm no visual tearing or layout errors.
- Test with content shorter than the viewport to ensure `SliverGeometry` reports correct values and does not cause layout assertions.
- Profile with DevTools and confirm the parallax does not cause unnecessary repaints of unrelated slivers.

```dart
// file: lib/slivers/sliver_parallax.dart
// Widget and RenderSliver implementation

// file: lib/screens/parallax_demo_screen.dart
// CustomScrollView using SliverParallax sections
```

---

### Exercise 6 (Advanced): Bidirectional Platform Communication with Pigeon

**Goal:** Build a feature where Dart sends configuration to native code, native code processes data in the background, and streams results back to Dart -- all with type-safe Pigeon-generated interfaces.

**Instructions:**

1. Define a Pigeon schema with:
   - A `@HostApi()` (Dart calls native) for starting/stopping a background task and setting configuration.
   - A `@FlutterApi()` (native calls Dart) for receiving progress updates and results.
   - Data classes for `TaskConfig`, `ProgressUpdate`, and `TaskResult`.
2. Run the Pigeon generator to produce Dart, Kotlin, and Swift code.
3. On the native side, simulate a background computation (e.g., "image processing") that periodically sends progress updates to Dart via `FlutterApi` and a final result when complete.
4. Build a Dart UI that starts the task, shows a progress bar driven by native callbacks, and displays the result.
5. Handle cancellation: calling `stopTask()` from Dart should halt the native computation and send a cancellation confirmation.
6. Handle errors: if the native computation fails, it should send a structured error through the `FlutterApi` rather than throwing an untyped exception.

**Verification:**
- Start the task and observe progress updates arriving in Dart at regular intervals.
- Cancel the task mid-progress and verify the progress stops and a cancellation message appears.
- Simulate a native error and verify Dart receives the structured error and displays it.
- Verify the generated Pigeon code compiles on both platforms without manual modifications.
- Check that method names and data class fields match exactly between the Pigeon schema and generated output.

```dart
// file: pigeons/background_task.dart
// Pigeon schema definition

// file: lib/services/background_task_service.dart
// Dart-side implementation using generated code

// file: lib/screens/background_task_screen.dart
// UI with start, progress, cancel, and result display
```

---

### Exercise 7 (Insane): Full Chart Library with CustomPainter

**Goal:** Build a reusable chart library from scratch using only CustomPainter. No external charting packages. The library must support line charts, bar charts, and pie charts with animations, touch interactions, tooltips, and responsive sizing.

**Instructions:**

1. **Data layer:**
   - Define `ChartDataSet` with typed entries for each chart type.
   - Support multiple data series on a single chart (e.g., two lines on one line chart).
   - Handle edge cases: empty data, single data point, negative values, zero values.

2. **Line chart:**
   - Draw axes with labeled ticks (auto-calculate tick intervals based on data range).
   - Plot data points as dots connected by smooth cubic bezier curves.
   - Animate the line drawing from left to right over 1 second.
   - On hover/tap, show a vertical crosshair and a tooltip with the exact value at that x-position (interpolate between data points).
   - Support area fill below the line with gradient opacity.

3. **Bar chart:**
   - Vertical bars with rounded top corners.
   - Animate bars growing from the baseline upward.
   - Grouped bars for multiple series (side by side) and stacked bars.
   - Tap a bar to highlight it and show its value.
   - Negative values render bars downward from the zero line.

4. **Pie chart:**
   - Animated entrance: segments grow from center outward.
   - Tap to "explode" a segment outward.
   - Labels with leader lines pointing to each segment.
   - Handle the case where one segment is 100% (full circle) and the case where a segment is less than 1% (skip the label to avoid clutter).

5. **Responsive sizing:**
   - All charts must adapt to their container size via `LayoutBuilder`.
   - Axis labels should scale or hide when the chart is too narrow.
   - Minimum size thresholds: below 100x100, show a "Chart too small" placeholder.

6. **Theming:**
   - Accept a `ChartTheme` object controlling colors, fonts, grid lines, and padding.
   - Provide a default light theme and a default dark theme.

**Verification:**
- Render each chart type with sample data. Animations should be smooth at 60fps.
- Tap interactions on each chart type produce correct tooltips with accurate values.
- Pass an empty data set to each chart -- no crashes, just an empty state message.
- Pass a single data point to the line chart -- it should render as a dot, not crash.
- Resize the window on desktop and confirm all charts reflow without visual artifacts.
- Apply the dark theme and verify all elements (axes, labels, grid lines) respect it.
- Profile with DevTools: `shouldRepaint` should return false when nothing changed, and only the interacted chart should repaint on touch.

```dart
// file: lib/charts/core/chart_data.dart
// Data models: ChartDataSet, DataSeries, DataPoint

// file: lib/charts/core/chart_theme.dart
// ChartTheme with light and dark defaults

// file: lib/charts/core/chart_utils.dart
// Axis calculation, tick intervals, value interpolation

// file: lib/charts/line/line_chart.dart
// Line chart widget with AnimationController

// file: lib/charts/line/line_chart_painter.dart
// CustomPainter for line chart rendering

// file: lib/charts/bar/bar_chart.dart
// file: lib/charts/bar/bar_chart_painter.dart

// file: lib/charts/pie/pie_chart.dart
// file: lib/charts/pie/pie_chart_painter.dart

// file: lib/screens/chart_showcase_screen.dart
// Screen demonstrating all three chart types
```

---

### Exercise 8 (Insane): Federated Flutter Plugin with Native View Embedding

**Goal:** Build a complete federated Flutter plugin that wraps a native image filter SDK. The plugin must use Pigeon-generated type-safe channels, embed a native camera preview, stream filtered frames back to Dart, and handle errors on both platforms.

**Instructions:**

1. **Plugin structure (federated):**
   - `image_filter` -- the app-facing package with Dart API.
   - `image_filter_platform_interface` -- abstract platform interface.
   - `image_filter_android` -- Android implementation.
   - `image_filter_ios` -- iOS implementation.
   - Follow the official federated plugin template structure.

2. **Pigeon schema:**
   - `@HostApi()`: `initializeCamera()`, `applyFilter(FilterConfig)`, `captureFrame()`, `dispose()`.
   - `@FlutterApi()`: `onFrameProcessed(ProcessedFrame)`, `onError(PluginError)`.
   - Data classes: `FilterConfig` (type, intensity, parameters), `ProcessedFrame` (timestamp, dimensions, filter applied), `PluginError` (code, message, stackTrace).

3. **Native camera preview:**
   - Embed the native camera preview using `AndroidView` (Android) and `UiKitView` (iOS).
   - The preview should render live camera output.
   - Overlay Flutter widgets (filter controls, capture button) on top of the native view.

4. **Filter pipeline:**
   - Implement at least three filters on the native side: grayscale, sepia, and blur.
   - Filters are applied in real-time to the camera preview.
   - Switching filters should be smooth (no visible flicker or frame drop).

5. **Stream-based results:**
   - Processed frame metadata streams to Dart via `EventChannel`.
   - Each frame event includes the timestamp, dimensions, and which filter was active.
   - Dart displays a real-time FPS counter and filter status.

6. **Error handling:**
   - Camera permission denied: structured error with recovery suggestion.
   - Camera hardware unavailable: graceful degradation with message.
   - Filter processing failure: skip the frame and log the error, do not crash.
   - Platform not supported: the app-facing package shows a fallback widget.

7. **Testing:**
   - Write unit tests for the Dart API using a mock platform interface.
   - Write integration test skeletons for both platforms.
   - The mock should simulate the stream of processed frames.

**Verification:**
- On Android, the camera preview renders and filter changes are visible in real-time.
- On iOS, the same behavior (you may simulate if you lack a physical device).
- Deny camera permission and verify the error message appears with a prompt to open settings.
- The FPS counter updates in real-time as frames stream in.
- Switch between filters rapidly -- no crash, no ANR, no frozen frames.
- Run the Dart unit tests -- all pass with the mock platform interface.
- The federated plugin structure matches `flutter create --template=plugin_ffi` conventions.
- Disposing the plugin releases all native resources (verify with platform memory profiler or logging).

```dart
// file: image_filter/lib/image_filter.dart
// App-facing API

// file: image_filter_platform_interface/lib/image_filter_platform_interface.dart
// Abstract interface with method signatures

// file: image_filter_platform_interface/lib/method_channel_image_filter.dart
// Default MethodChannel-based implementation

// file: image_filter_android/lib/image_filter_android.dart
// Android platform registration

// file: image_filter_ios/lib/image_filter_ios.dart
// iOS platform registration

// file: pigeons/image_filter_api.dart
// Pigeon schema

// file: image_filter/lib/src/filter_preview_widget.dart
// Widget embedding native camera view with Flutter overlay

// file: image_filter/test/image_filter_test.dart
// Unit tests with mock platform
```

---

## Summary

This section covered the three boundaries that advanced Flutter development demands you cross: the rendering boundary (CustomPainter, CustomClipper, RenderObject, shaders), the scroll boundary (Slivers, custom RenderSlivers, NestedScrollView), and the platform boundary (MethodChannel, EventChannel, Pigeon, native view embedding, federated plugins).

The pattern across all three is the same. Flutter gives you high-level abstractions that cover most cases. When they fall short, a lower-level API exists. CustomPaint sits below standard widgets. Slivers sit below ListView. Platform Channels sit below Dart-only code. Each step down gives you more power at the cost of more responsibility -- more code, more edge cases, more debugging.

The critical judgment call is knowing when to step down. If a `Container` with `BoxDecoration` can draw your shape, do not reach for CustomPainter. If `ListView.builder` handles your scroll, do not compose slivers. If a pub.dev package wraps the native API you need, do not write a plugin from scratch. Step down only when the higher-level tool genuinely cannot do what you need.

### Curriculum Complete

You have now worked through 20 sections spanning the entire Dart and Flutter landscape:

- **Sections 01-08 (Dart foundations):** Variables and types, functions and closures, control flow and collections, OOP, async programming, generics and the type system, error handling and null safety, and advanced Dart patterns like isolates, extensions, and metaprogramming.
- **Sections 09-13 (Flutter fundamentals):** Widget trees and lifecycle, layouts and constraints, navigation and routing, state management basics, and forms and input handling.
- **Sections 14-16 (Data and motion):** Networking and data persistence, advanced state management with Riverpod and Bloc, and the full animation system from implicit to explicit to custom.
- **Sections 17-19 (Production readiness):** Testing at every level (unit, widget, integration), architecture patterns (clean architecture, MVVM, modularization), and performance profiling and optimization.
- **Section 20 (This section):** The rendering layer, the scroll internals, and the bridge to native platforms.

This is a foundation, not a ceiling. Some directions to continue growing:

**Contribute to open source.** Find a Flutter package you use regularly, read its source, fix a bug, or add a feature. Reading production code written by experienced developers teaches patterns no tutorial can.

**Build and ship a production app.** The gap between tutorial code and production code is real. You will encounter app signing, CI/CD pipelines, crash reporting, analytics, accessibility, localization, and the hundred small decisions that only surface when real users interact with your work.

**Engage with the Flutter community.** Follow the Flutter GitHub repository for RFCs and design documents. Read the Flutter engine source when you want to understand how rendering actually works at the Skia/Impeller level. Join discussions on the official Discord or forums.

**Explore adjacent technologies.** Dart on the server (with `shelf` or `dart_frog`), Flutter for desktop and embedded, the Impeller rendering engine internals, and WebAssembly compilation are all active areas of development.

The best code you will ever write is the code you write next, because you now have the vocabulary and mental models to make deliberate choices at every level of the stack.

## References

- [CustomPainter Class (Flutter API)](https://api.flutter.dev/flutter/rendering/CustomPainter-class.html)
- [Canvas Class (dart:ui)](https://api.flutter.dev/flutter/dart-ui/Canvas-class.html)
- [Slivers Overview (Flutter Documentation)](https://docs.flutter.dev/ui/layout/scrolling/slivers)
- [SliverAppBar (Flutter API)](https://api.flutter.dev/flutter/material/SliverAppBar-class.html)
- [Writing Platform-Specific Code](https://docs.flutter.dev/platform-integration/platform-channels)
- [Pigeon Package](https://pub.dev/packages/pigeon)
- [Developing Packages and Plugins](https://docs.flutter.dev/packages-and-plugins/developing-packages)
- [Federated Plugins](https://docs.flutter.dev/packages-and-plugins/developing-packages#federated-plugins)
- [Hosting Native Android and iOS Views](https://docs.flutter.dev/platform-integration/android/platform-views)
- [Writing and Using Fragment Shaders](https://docs.flutter.dev/ui/design/graphics/fragment-shaders)
- [RenderObject Class (Flutter API)](https://api.flutter.dev/flutter/rendering/RenderObject-class.html)
- [Flutter Architectural Overview](https://docs.flutter.dev/resources/architectural-overview)
