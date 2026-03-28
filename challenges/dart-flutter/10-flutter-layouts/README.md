# Section 10: Flutter Layouts -- Row, Column, Stack, Flex & Responsive Design

## Introduction

Every visible pixel in a Flutter application is the result of a negotiation between parent and child widgets. The parent says "here are your constraints," the child responds with "here is my size," and then the parent decides where to place the child. This three-phase protocol -- constraints go down, sizes go up, parent sets position -- is the single most important mental model for Flutter layout. Every layout bug is a violation of this protocol, and every fix begins with understanding which phase went wrong.

Unlike CSS where elements influence siblings through document flow, Flutter's layout is strictly single-pass and top-down. A widget never knows where it is on screen during layout. It only knows how much space is available. This makes layouts predictable and fast, but requires a different way of thinking about visual composition.

## Prerequisites

- Completed Section 09: Flutter Setup & Widgets (StatelessWidget, StatefulWidget, widget tree basics)
- Flutter SDK installed and working (`flutter doctor` passes)
- An emulator/simulator or physical device for testing layouts at different screen sizes

## Learning Objectives

By the end of this section, you will be able to:

1. **Explain** Flutter's constraint-based layout protocol and predict how constraints propagate
2. **Apply** Row, Column, and Flex widgets with appropriate axis alignment and sizing
3. **Analyze** how Expanded, Flexible, and Spacer distribute remaining space using flex factors
4. **Construct** layered interfaces using Stack and Positioned widgets
5. **Design** responsive layouts using LayoutBuilder and MediaQuery
6. **Evaluate** layout bugs by interpreting overflow errors and unbounded constraint messages
7. **Create** custom layout solutions using Flow and CustomMultiChildLayout

---

## Core Concepts

### 1. The Box Constraints Model

Every widget receives a BoxConstraints from its parent defining the min/max width and height allowed.

```dart
// box_constraints_demo.dart
import 'package:flutter/material.dart';

class ConstraintExplorer extends StatelessWidget {
  const ConstraintExplorer({super.key});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: LayoutBuilder(
        builder: (context, constraints) {
          debugPrint('Min: ${constraints.minWidth}x${constraints.minHeight}');
          debugPrint('Max: ${constraints.maxWidth}x${constraints.maxHeight}');
          return Center(
            child: Container(width: 200, height: 200, color: Colors.blue),
          );
        },
      ),
    );
  }
}
```

Three kinds of constraints matter: **tight** (min equals max -- widget has no size choice), **loose** (min is zero, max is finite -- widget chooses freely), and **unbounded** (max is infinity -- happens inside scrollable widgets). Unbounded constraints cause most layout errors. A widget that wants to be "as big as possible" inside unbounded constraints cannot determine its size.

### 2. Row and Column

Row and Column are Flex widgets for horizontal and vertical main axes. Three properties control their behavior:

- **MainAxisAlignment**: distribution along the main axis (`start`, `center`, `spaceBetween`, `spaceEvenly`, etc.)
- **CrossAxisAlignment**: alignment on the perpendicular axis (`start`, `center`, `stretch`, `baseline`)
- **MainAxisSize**: `max` takes all space, `min` shrink-wraps to children

```dart
// row_column_basics.dart
import 'package:flutter/material.dart';

class RowColumnDemo extends StatelessWidget {
  const RowColumnDemo({super.key});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: Column(
        mainAxisAlignment: MainAxisAlignment.center,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        mainAxisSize: MainAxisSize.max,
        children: [
          Container(height: 60, color: Colors.red),
          const SizedBox(height: 16),
          Container(height: 60, color: Colors.green),
          const SizedBox(height: 16),
          Container(height: 60, color: Colors.blue),
        ],
      ),
    );
  }
}
```

### 3. Expanded, Flexible, and Spacer

After fixed-size children are laid out, remaining space is divided among flex children proportionally.

```dart
// flex_distribution.dart
import 'package:flutter/material.dart';

class FlexDemo extends StatelessWidget {
  const FlexDemo({super.key});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: Column(children: [
        // Expanded: forces child to fill allocated space (FlexFit.tight)
        Row(children: [
          Expanded(flex: 2, child: Container(height: 60, color: Colors.red)),
          Expanded(flex: 1, child: Container(height: 60, color: Colors.green)),
        ]),
        const SizedBox(height: 16),
        // Flexible(loose): child can be smaller than allocation
        Row(children: [
          Flexible(fit: FlexFit.loose, child: Container(width: 50, height: 60, color: Colors.orange)),
          Expanded(child: Container(height: 60, color: Colors.purple)),
        ]),
        const SizedBox(height: 16),
        // Spacer: Expanded with empty child, pushes siblings apart
        Row(children: [const Text('Left'), const Spacer(), const Text('Right')]),
      ]),
    );
  }
}
```

### 4. Stack and Positioned

Stack layers children on top of each other. Non-positioned children fill the Stack (or align per `alignment`). Positioned children anchor to Stack edges.

```dart
// stack_demo.dart
import 'package:flutter/material.dart';

class StackDemo extends StatelessWidget {
  const StackDemo({super.key});

  @override
  Widget build(BuildContext context) {
    return Center(
      child: SizedBox(
        width: 300, height: 300,
        child: Stack(
          clipBehavior: Clip.none,
          children: [
            Container(color: Colors.grey.shade300),
            Positioned(top: 20, left: 20,
              child: Container(width: 100, height: 100, color: Colors.red.withValues(alpha: 0.8))),
            Positioned(bottom: 20, right: 20,
              child: Container(width: 80, height: 80, color: Colors.blue.withValues(alpha: 0.8))),
            const Positioned.fill(child: Center(child: Text('Overlay'))),
          ],
        ),
      ),
    );
  }
}
```

Stack sizes itself from non-positioned children. If all children are Positioned, provide explicit dimensions.

### 5. Wrap and Flow

Wrap handles children that should flow to the next line when space runs out (tags, chips).

```dart
// wrap_demo.dart
import 'package:flutter/material.dart';

class WrapDemo extends StatelessWidget {
  const WrapDemo({super.key});

  @override
  Widget build(BuildContext context) {
    final tags = ['Flutter', 'Dart', 'Widgets', 'Layout', 'Responsive', 'Material'];
    return Padding(
      padding: const EdgeInsets.all(16),
      child: Wrap(
        spacing: 8, runSpacing: 8,
        children: tags.map((t) => Chip(label: Text(t))).toList(),
      ),
    );
  }
}
```

Flow is a lower-level alternative with a FlowDelegate giving complete positional control. Flow can reposition children without re-layout, making it efficient for animations.

### 6. LayoutBuilder and MediaQuery: Responsive Design

LayoutBuilder provides the constraints your widget receives. MediaQuery provides device-level info (screen size, safe area, text scale).

```dart
// responsive_layout.dart
import 'package:flutter/material.dart';

class ResponsiveLayout extends StatelessWidget {
  const ResponsiveLayout({super.key});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: LayoutBuilder(
        builder: (context, constraints) {
          if (constraints.maxWidth >= 1200) return _buildDesktop(context);
          if (constraints.maxWidth >= 600) return _buildTablet(context);
          return _buildMobile(context);
        },
      ),
    );
  }

  Widget _buildMobile(BuildContext context) {
    final padding = MediaQuery.of(context).padding;
    return Padding(
      padding: EdgeInsets.only(top: padding.top),
      child: Column(children: [
        Container(height: 200, color: Colors.blue, child: const Center(child: Text('Hero'))),
        Expanded(child: ListView.builder(
          itemCount: 20,
          itemBuilder: (context, i) => ListTile(title: Text('Item $i')),
        )),
      ]),
    );
  }

  Widget _buildTablet(BuildContext context) => Row(children: [
    SizedBox(width: 250, child: ListView.builder(
      itemCount: 20, itemBuilder: (context, i) => ListTile(title: Text('Item $i')))),
    const VerticalDivider(width: 1),
    const Expanded(child: Center(child: Text('Detail'))),
  ]);

  Widget _buildDesktop(BuildContext context) => Row(children: [
    SizedBox(width: 200, child: Column(children: const [
      ListTile(leading: Icon(Icons.home), title: Text('Home')),
      ListTile(leading: Icon(Icons.settings), title: Text('Settings')),
    ])),
    const VerticalDivider(width: 1),
    SizedBox(width: 300, child: ListView.builder(
      itemCount: 20, itemBuilder: (context, i) => ListTile(title: Text('Item $i')))),
    const VerticalDivider(width: 1),
    const Expanded(child: Center(child: Text('Detail'))),
  ]);
}
```

Use LayoutBuilder for local available space. Use MediaQuery for device-level info like safe area insets.

### 7. Constraint Manipulation Widgets

Flutter provides several widgets whose sole purpose is to modify the constraints passed to their children. You reach for these when the default constraint propagation does not match your design intent.

```dart
// constraint_widgets.dart
import 'package:flutter/material.dart';

class ConstraintWidgetsDemo extends StatelessWidget {
  const ConstraintWidgetsDemo({super.key});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: SingleChildScrollView(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            // ConstrainedBox: adds extra min/max bounds
            ConstrainedBox(
              constraints: const BoxConstraints(minHeight: 80, maxWidth: 200),
              child: Container(color: Colors.red, child: const Text('ConstrainedBox')),
            ),
            const SizedBox(height: 16),
            // FractionallySizedBox: sizes relative to parent
            SizedBox(
              height: 100,
              child: FractionallySizedBox(
                widthFactor: 0.5, // 50% of parent width
                child: Container(color: Colors.green),
              ),
            ),
            const SizedBox(height: 16),
            // AspectRatio: forces a width/height ratio
            SizedBox(
              width: 200,
              child: AspectRatio(
                aspectRatio: 16 / 9,
                child: Container(color: Colors.blue, child: const Center(child: Text('16:9'))),
              ),
            ),
            const SizedBox(height: 16),
            // FittedBox: scales child to fit within constraints
            SizedBox(
              width: 100, height: 50,
              child: FittedBox(
                fit: BoxFit.contain,
                child: Text('Scaled', style: TextStyle(fontSize: 40)),
              ),
            ),
            const SizedBox(height: 16),
            // IntrinsicWidth: sizes based on intrinsic dimensions (expensive)
            IntrinsicWidth(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  ElevatedButton(onPressed: () {}, child: const Text('Short')),
                  ElevatedButton(onPressed: () {}, child: const Text('Much Longer Text')),
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

A warning about IntrinsicWidth and IntrinsicHeight: they perform a speculative layout pass to determine intrinsic dimensions, meaning the child subtree is laid out twice. For simple subtrees this is fine. For deep or complex subtrees inside lists or animations, it causes performance problems. Measure before using them in hot paths.

### 8. Scrollable Layouts

Scrollable widgets provide unbounded constraints along the scroll axis. This is by design -- it lets them contain content taller (or wider) than the screen. But it also means placing a widget that wants to expand infinitely (like an unconstrained Column or another ListView) inside a scrollable causes a crash.

```dart
// scrollable_bug_and_fix.dart
import 'package:flutter/material.dart';

// BUG: ListView inside Column without bounded constraints
class UnboundedBug extends StatelessWidget {
  const UnboundedBug({super.key});
  @override
  Widget build(BuildContext context) {
    return Column(children: [
      const Text('Header'),
      // ListView tries infinite height, Column allows it -- crash
      ListView.builder(itemCount: 50, itemBuilder: (ctx, i) => Text('Item $i')),
    ]);
  }
}

// FIX: Wrap ListView in Expanded
class UnboundedFixed extends StatelessWidget {
  const UnboundedFixed({super.key});
  @override
  Widget build(BuildContext context) {
    return Column(children: [
      const Text('Header'),
      Expanded(child: ListView.builder(
        itemCount: 50, itemBuilder: (ctx, i) => Text('Item $i'))),
    ]);
  }
}
```

Use CustomScrollView with slivers for mixed scrollable content (grids, lists, headers in one scroll area). Slivers only build visible children, so they are efficient for long lists. Prefer slivers over `shrinkWrap: true`, which forces the list to lay out all children immediately.

### 9. Common Layout Patterns

```dart
// common_patterns.dart
import 'package:flutter/material.dart';

// Centered content with max width (common for web apps)
class CenteredContent extends StatelessWidget {
  final Widget child;
  final double maxWidth;
  const CenteredContent({super.key, required this.child, this.maxWidth = 800});

  @override
  Widget build(BuildContext context) => Center(
    child: ConstrainedBox(
      constraints: BoxConstraints(maxWidth: maxWidth), child: child));
}

// Adaptive card grid that calculates column count from width
class AdaptiveCardGrid extends StatelessWidget {
  final List<Widget> cards;
  const AdaptiveCardGrid({super.key, required this.cards});

  @override
  Widget build(BuildContext context) => LayoutBuilder(
    builder: (context, constraints) {
      final cols = (constraints.maxWidth / 300).floor().clamp(1, 4);
      return GridView.count(
        crossAxisCount: cols, crossAxisSpacing: 16, mainAxisSpacing: 16,
        padding: const EdgeInsets.all(16), children: cards);
    },
  );
}

// Sticky bottom action bar
class StickyBottomBar extends StatelessWidget {
  final Widget body;
  final Widget bottomBar;
  const StickyBottomBar({super.key, required this.body, required this.bottomBar});

  @override
  Widget build(BuildContext context) => Column(
    children: [Expanded(child: body), bottomBar]);
}
```

### 10. Debugging Layouts

Layout bugs in Flutter produce specific error messages that point directly at the problem once you learn to read them.

**Overflow error**: "A RenderFlex overflowed by X pixels on the right/bottom." Children are wider/taller than the parent allows. Fix: wrap in Expanded, Flexible, SingleChildScrollView, or reduce child sizes.

**Unbounded constraints**: "RenderBox was not laid out" or "Vertical viewport was given unbounded height." A widget that wants to expand is placed inside a parent that imposes no limit. Classic: ListView inside Column without Expanded.

**Missing size**: "RenderBox was not laid out: RenderConstrainedBox." Widget cannot determine its size from children or constraints. Fix: provide explicit dimensions.

```dart
// debugging_overflow.dart
import 'package:flutter/material.dart';

// BUG: Three 200px containers in a Row overflow on narrow screens
class OverflowBug extends StatelessWidget {
  const OverflowBug({super.key});
  @override
  Widget build(BuildContext context) {
    return Row(children: [
      Container(width: 200, height: 60, color: Colors.red),
      Container(width: 200, height: 60, color: Colors.green),
      Container(width: 200, height: 60, color: Colors.blue),
    ]);
  }
}

// FIX: Expanded distributes space proportionally
class OverflowFixed extends StatelessWidget {
  const OverflowFixed({super.key});
  @override
  Widget build(BuildContext context) {
    return Row(children: [
      Expanded(child: Container(height: 60, color: Colors.red)),
      Expanded(child: Container(height: 60, color: Colors.green)),
      Expanded(child: Container(height: 60, color: Colors.blue)),
    ]);
  }
}
```

Use Flutter Inspector (in DevTools) to visualize widget bounds, see constraint values at each node, and identify where layout rules break. Toggle "Show Guidelines" to see padding and margin boxes.

---

## Exercises

### Exercise 1 (Basic): Profile Card Layout

**Estimated time: 20 minutes**

Build a user profile card with a Row containing a CircleAvatar and a Column of text (name, email), a Divider, and a Row of three evenly-spaced stat columns (Posts, Followers, Following). Use MainAxisAlignment.spaceEvenly for the stats, MainAxisSize.min on the outer Column, and Expanded on the text Column to prevent overflow with long names.

```dart
// exercise_01_profile.dart
import 'package:flutter/material.dart';

class ProfileCard extends StatelessWidget {
  const ProfileCard({super.key});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: Center(
        child: Padding(
          padding: const EdgeInsets.all(16),
          child: Card(
            child: Padding(
              padding: const EdgeInsets.all(16),
              child: Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  // TODO: Header Row with CircleAvatar + text Column
                  // TODO: Divider
                  // TODO: Stats Row with three evenly-spaced stat Columns
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }
}
```

**Verification:** Card centered on screen, no overflow on resize, stats evenly spaced at any width.

---

### Exercise 2 (Basic): Expanded and Flexible Playground

**Estimated time: 25 minutes**

Build a StatefulWidget with a mode selector toggling between: (1) three equal Expanded children, (2) flex 1:2:3 distribution, (3) one fixed 100px + one Expanded + one Flexible(loose) with 50px inner Container, (4) two fixed children separated by Spacer. Each child displays its width via LayoutBuilder.

**Verification:** In Mode 3, the Flexible child should be 50px (not the full allocation). In Mode 4, children pushed to opposite ends.

---

### Exercise 3 (Intermediate): Responsive Card Grid

**Estimated time: 40 minutes**

Build a card grid using LayoutBuilder and GridView.builder. Column count: `(maxWidth / 300).floor().clamp(1, 4)`. Cards have 3:4 aspect ratio with a colored header and text area. Below 200px width, fall back to a single-column ListView.

**Verification:** 1 column on phone, 2-3 on tablet, 4 on desktop. Smooth reconfiguration on window resize.

---

### Exercise 4 (Intermediate): Drawer-to-Sidebar Navigation

**Estimated time: 50 minutes**

Build a navigation shell: Drawer on mobile (<600px), NavigationRail on tablet (600-1024px), full 250px sidebar on desktop (>1024px). Four nav items (Home, Search, Favorites, Profile). Selected index persists across layout transitions since State survives LayoutBuilder rebuilds.

**Verification:** Resize from desktop to mobile and back. Selected item preserved. Drawer closes on selection. Rail shows icons only.

---

### Exercise 5 (Advanced): Complex Dashboard with Nested Scroll Views

**Estimated time: 90 minutes**

Build a dashboard using CustomScrollView: pinned SliverAppBar, a SliverToBoxAdapter containing a 120px-tall horizontal ListView of summary cards, a SliverGrid of 8 metric widgets (responsive column count), and a SliverList of 20 activity items. Add pull-to-refresh. Handle the unbounded constraint: the horizontal ListView inside SliverToBoxAdapter needs explicit height via SizedBox.

**Verification:** Vertical scroll collapses app bar. Horizontal cards scroll independently. Grid adjusts columns on resize. Pull-to-refresh works.

---

### Exercise 6 (Advanced): Responsive Design System

**Estimated time: 90 minutes**

Build reusable responsive infrastructure: a `Breakpoints` class with mobile/tablet/desktop/wide thresholds, a `ResponsiveValue<T>` that resolves values per breakpoint, a `ResponsiveSpacing` providing adaptive padding/margins/gaps, and a `FluidTypography` system where font sizes interpolate linearly between breakpoints (not jumping). Use the formula: `min + (max - min) * ((currentWidth - minWidth) / (maxWidth - minWidth)).clamp(0, 1)`. Build a demo screen using all three systems.

**Verification:** Resize from 320px to 1600px. Spacing grows gradually, text scales smoothly, layout transitions at breakpoints, no overflow at any width.

---

### Exercise 7 (Insane): Custom Masonry Layout

**Estimated time: 3-4 hours**

Build a Pinterest-style masonry layout using CustomMultiChildLayout. Cards of varying heights are arranged in columns, filling the shortest column first.

**Requirements:**

1. Create `exercise_07_masonry.dart`
2. Implement a `MasonryDelegate` extending `MultiChildLayoutDelegate` that:
   - Accepts column count, spacing, and child count as parameters
   - In `performLayout`, computes column width from available width minus spacing
   - Lays out each child with fixed width and unconstrained height (via `layoutChild`)
   - Tracks column heights in a list, always placing the next child in the shortest column
   - Implements `shouldRelayout` comparing all parameters
3. Wrap the `CustomMultiChildLayout` in a `SingleChildScrollView` with a `SizedBox` for estimated height
4. Make column count responsive: 2 columns on mobile, 3 on tablet, 4 on desktop
5. Generate 20+ cards with deterministic varying heights using `Random(index)`

```dart
// exercise_07_masonry.dart (starter)
import 'package:flutter/material.dart';

class MasonryDelegate extends MultiChildLayoutDelegate {
  final int columnCount;
  final double spacing;
  final int childCount;

  MasonryDelegate({required this.columnCount, required this.spacing, required this.childCount});

  @override
  void performLayout(Size size) {
    // TODO: Calculate column width
    // TODO: For each child, find shortest column, layoutChild, positionChild
    // TODO: Update column height tracking
  }

  @override
  Size getSize(BoxConstraints constraints) => constraints.biggest;

  @override
  bool shouldRelayout(MasonryDelegate old) => throw UnimplementedError();
}
```

**Design considerations:**

- MultiChildLayoutDelegate gives you `layoutChild` returning the child's actual size before positioning -- this is why it suits masonry better than FlowDelegate, which only provides sizes during paint
- Each child needs a `LayoutId(id: index, child: ...)` wrapper
- The height estimation challenge: `CustomMultiChildLayout` reports `constraints.biggest` from `getSize`, which is infinite inside a scroll view. You must wrap it in a SizedBox with estimated or computed height
- Handle edge cases: zero children, single column, children taller than the viewport

**Verification:** Run with 20+ cards. You should see a masonry pattern with roughly equal column heights. Resize the window and confirm column count adapts. No overflow errors.

---

### Exercise 8 (Insane): Drag-and-Drop Layout Builder

**Estimated time: 4-5 hours**

Build an interactive layout builder where users drag widgets from a palette, drop them onto a canvas, reposition them, resize them, and persist the layout as JSON.

**Requirements:**

1. Create `exercise_08_layout_builder.dart`
2. Two-panel interface: left palette (200px wide) with Draggable widget templates, right DragTarget canvas
3. Widget templates: text block, image placeholder, colored box, spacer
4. Canvas is a Stack with Positioned children, positions stored in state
5. Dragged widgets snap to a 16px grid: `(value / 16).round() * 16`
6. Four resize handles (small circles) at corners of the selected widget
7. Serialization: each widget stores `{id, type, x, y, width, height}` as JSON
8. Undo/redo: `List<List<State>>` history stack with integer index

```dart
// exercise_08_layout_builder.dart (starter)
import 'package:flutter/material.dart';
import 'dart:convert';

class PlacedWidget {
  final String id;
  final String type;
  double x, y, width, height;
  // TODO: constructor, copy, toJson, fromJson
}

class LayoutBuilderApp extends StatefulWidget {
  // TODO: State with List<PlacedWidget>, selectedId, history stack
}
```

**Key challenges:**

- Coordinate conversion: DragTarget's `onAcceptWithDetails` gives global offsets. Convert with `renderBox.globalToLocal(details.offset)` and subtract palette width
- Resize handles: each corner handle modifies different dimensions. Bottom-right changes width/height. Top-left changes x, y, width, height (origin moves while expanding). Always clamp to minimum 40px
- History: push a deep copy of the widget list on `onPanEnd` (not `onPanUpdate` -- that would create hundreds of snapshots per drag). On new change after undo, truncate future states
- Grid painter: use CustomPaint with lines at 16px intervals for visual alignment

**Verification:** Drag widget to canvas. Move it -- snaps to grid. Resize from corner. Add several widgets. Save prints JSON. Undo reverses last action. Redo restores it. Load from JSON restores positions.

---

## Summary

In this section you covered:

- **Box constraints model**: how tight, loose, and unbounded constraints flow from parent to child, and how the three-phase layout protocol governs every pixel on screen
- **Row and Column**: the primary layout axes, controlled by MainAxisAlignment, CrossAxisAlignment, and MainAxisSize
- **Expanded, Flexible, and Spacer**: flex factor distribution of remaining space, the difference between tight and loose flex fit
- **Stack and Positioned**: layering widgets with absolute positioning, overflow control, sizing from non-positioned children
- **Wrap and Flow**: wrapping layouts for tags and chips, and lower-level Flow for custom positioning with animation performance
- **LayoutBuilder and MediaQuery**: responsive design through constraint-aware building and device-level information
- **Constraint widgets**: ConstrainedBox, FractionallySizedBox, AspectRatio, FittedBox, IntrinsicWidth/Height and their performance trade-offs
- **Scrollable layouts**: ListView, GridView, CustomScrollView with slivers, and the unbounded constraint relationship
- **Common patterns**: centered max-width content, adaptive grids, sticky bottom bars
- **Layout debugging**: reading overflow errors, diagnosing unbounded constraints, using Flutter Inspector

Key takeaways:

- Think constraints-first. Before reaching for a layout widget, ask: "What constraints does my widget receive, and what does it pass to its children?"
- Unbounded constraints inside scrollables are by design. Do not place expanding widgets inside them without explicit bounds.
- Prefer LayoutBuilder over MediaQuery for local responsiveness. MediaQuery gives screen size; your widget might not occupy the full screen.
- IntrinsicWidth/Height perform O(2n) work. Use sparingly and never in scrollable item builders.
- CustomScrollView with slivers is the correct architecture for screens mixing grids, lists, and headers in one scroll area.

## What's Next

In **Section 11: Navigation & Routing**, you will learn how to structure multi-screen applications using Navigator 2.0 and go_router. You will implement deep linking, nested navigation, route guards, and animated transitions. The layout skills from this section directly support every screen you navigate to.

## References

- [Understanding Constraints](https://docs.flutter.dev/ui/layout/constraints)
- [Flutter Layout Documentation](https://docs.flutter.dev/ui/layout)
- [Row](https://api.flutter.dev/flutter/widgets/Row-class.html) / [Column](https://api.flutter.dev/flutter/widgets/Column-class.html) / [Stack](https://api.flutter.dev/flutter/widgets/Stack-class.html)
- [LayoutBuilder](https://api.flutter.dev/flutter/widgets/LayoutBuilder-class.html) / [MediaQuery](https://api.flutter.dev/flutter/widgets/MediaQuery-class.html)
- [CustomScrollView & Slivers](https://docs.flutter.dev/ui/layout/scrolling/slivers)
- [Flow](https://api.flutter.dev/flutter/widgets/Flow-class.html) / [CustomMultiChildLayout](https://api.flutter.dev/flutter/widgets/CustomMultiChildLayout-class.html)
- [Adaptive and Responsive Design](https://docs.flutter.dev/ui/layout/responsive/adaptive-responsive)
- [Flutter Inspector (DevTools)](https://docs.flutter.dev/tools/devtools/inspector)
