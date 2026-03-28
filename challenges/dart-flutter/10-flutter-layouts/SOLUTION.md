# Section 10: Solutions -- Flutter Layouts

## How to Use This File

Work through each exercise in the README first. Spend real time stuck before looking here. This file is organized per exercise: progressive hints (read one at a time), full solution, common mistakes, and deep dives.

---

## Exercise 1: Profile Card Layout

### Progressive Hints

**Hint 1:** The header Row needs CircleAvatar first, SizedBox(width: 12), then a Column with CrossAxisAlignment.start.

**Hint 2:** Wrap the text Column in Expanded so long names do not overflow the Row.

**Hint 3:** Extract a `_buildStat(String label, String value)` helper returning a Column.

### Full Solution

```dart
// exercise_01_profile.dart
import 'package:flutter/material.dart';

void main() => runApp(const MaterialApp(home: ProfileCard()));

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
                  Row(children: [
                    const CircleAvatar(radius: 30, child: Icon(Icons.person, size: 30)),
                    const SizedBox(width: 12),
                    Expanded(child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text('Jane Developer', style: Theme.of(context).textTheme.titleLarge
                            ?.copyWith(fontWeight: FontWeight.bold)),
                        const SizedBox(height: 4),
                        Text('jane@example.com', style: Theme.of(context).textTheme.bodyMedium
                            ?.copyWith(color: Colors.grey)),
                      ],
                    )),
                  ]),
                  const Divider(height: 32),
                  Row(
                    mainAxisAlignment: MainAxisAlignment.spaceEvenly,
                    children: [
                      _buildStat('Posts', '42'),
                      _buildStat('Followers', '1.2K'),
                      _buildStat('Following', '300'),
                    ],
                  ),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }

  Widget _buildStat(String label, String value) => Column(children: [
    Text(value, style: const TextStyle(fontSize: 18, fontWeight: FontWeight.bold)),
    const SizedBox(height: 4),
    Text(label, style: const TextStyle(color: Colors.grey)),
  ]);
}
```

### Common Mistakes

**Forgetting Expanded on the text Column.** Without it, a long name overflows the Row. Expanded constrains the Column to remaining space after the avatar.

**Using MainAxisSize.max on the outer Column.** The card stretches to fill the entire screen. Use MainAxisSize.min to shrink-wrap.

---

## Exercise 2: Expanded and Flexible Playground

### Progressive Hints

**Hint 1:** Use a switch expression on `_mode` to return different Row children.

**Hint 2:** Show width with LayoutBuilder inside each child: `constraints.maxWidth.toStringAsFixed(0)`.

### Full Solution

```dart
// exercise_02_flex.dart
import 'package:flutter/material.dart';

void main() => runApp(const MaterialApp(home: FlexPlayground()));

class FlexPlayground extends StatefulWidget {
  const FlexPlayground({super.key});
  @override
  State<FlexPlayground> createState() => _FlexPlaygroundState();
}

class _FlexPlaygroundState extends State<FlexPlayground> {
  int _mode = 1;

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Flex Playground')),
      body: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(children: [
          SegmentedButton<int>(
            segments: const [
              ButtonSegment(value: 1, label: Text('Equal')),
              ButtonSegment(value: 2, label: Text('1:2:3')),
              ButtonSegment(value: 3, label: Text('Mixed')),
              ButtonSegment(value: 4, label: Text('Spacer')),
            ],
            selected: {_mode},
            onSelectionChanged: (v) => setState(() => _mode = v.first),
          ),
          const SizedBox(height: 24),
          SizedBox(height: 80, child: _buildRow()),
          const SizedBox(height: 24),
          Text(_explanation(), textAlign: TextAlign.center),
        ]),
      ),
    );
  }

  Widget _buildRow() => switch (_mode) {
    1 => Row(children: [
      Expanded(child: _box('A', Colors.red)),
      Expanded(child: _box('B', Colors.green)),
      Expanded(child: _box('C', Colors.blue)),
    ]),
    2 => Row(children: [
      Expanded(flex: 1, child: _box('1x', Colors.red)),
      Expanded(flex: 2, child: _box('2x', Colors.green)),
      Expanded(flex: 3, child: _box('3x', Colors.blue)),
    ]),
    3 => Row(children: [
      SizedBox(width: 100, child: _box('Fixed', Colors.red)),
      Expanded(child: _box('Expanded', Colors.green)),
      Flexible(fit: FlexFit.loose,
        child: Container(width: 50, height: 80, color: Colors.blue,
          child: const Center(child: Text('Loose', style: TextStyle(color: Colors.white, fontSize: 10))))),
    ]),
    4 => Row(children: [_box('Left', Colors.red), const Spacer(), _box('Right', Colors.blue)]),
    _ => const SizedBox.shrink(),
  };

  Widget _box(String label, Color color) => LayoutBuilder(
    builder: (context, constraints) => Container(
      color: color,
      child: Center(child: Text('$label\n${constraints.maxWidth.toStringAsFixed(0)}px',
        textAlign: TextAlign.center, style: const TextStyle(color: Colors.white, fontSize: 12))),
    ),
  );

  String _explanation() => switch (_mode) {
    1 => 'Equal flex (1:1:1). Each child gets exactly one-third.',
    2 => 'Flex 1:2:3. Total flex 6. First=1/6, second=2/6, third=3/6.',
    3 => 'Fixed 100px + Expanded (fills rest) + Flexible(loose) only 50px wide.',
    4 => 'Spacer pushes children to opposite ends.',
    _ => '',
  };
}
```

### Common Mistakes

**Confusing Flexible and Expanded.** Expanded is `Flexible(fit: FlexFit.tight)` -- it forces the child to fill its allocation. Loose fit lets the child be smaller. In Mode 3, the Flexible child is 50px (its inner Container), not the full flex allocation.

---

## Exercise 3: Responsive Card Grid

### Progressive Hints

**Hint 1:** Column count: `(constraints.maxWidth / 300).floor().clamp(1, 4)`.

**Hint 2:** Use Expanded with flex inside the card Column to divide the colored header from the text area.

**Hint 3:** Add `clipBehavior: Clip.antiAlias` on Card so the colored header respects rounded corners.

### Full Solution

```dart
// exercise_03_grid.dart
import 'package:flutter/material.dart';

void main() => runApp(const MaterialApp(home: ResponsiveGrid()));

class ResponsiveGrid extends StatelessWidget {
  const ResponsiveGrid({super.key});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Responsive Grid')),
      body: LayoutBuilder(
        builder: (context, constraints) {
          if (constraints.maxWidth < 200) {
            return ListView.builder(
              padding: const EdgeInsets.all(16), itemCount: 12,
              itemBuilder: (context, i) => Padding(
                padding: const EdgeInsets.only(bottom: 16), child: _buildCard(i)),
            );
          }
          final cols = (constraints.maxWidth / 300).floor().clamp(1, 4);
          return GridView.builder(
            padding: const EdgeInsets.all(16),
            gridDelegate: SliverGridDelegateWithFixedCrossAxisCount(
              crossAxisCount: cols, crossAxisSpacing: 16, mainAxisSpacing: 16, childAspectRatio: 3 / 4),
            itemCount: 12, itemBuilder: (context, i) => _buildCard(i),
          );
        },
      ),
    );
  }

  Widget _buildCard(int index) {
    final colors = [Colors.blue, Colors.red, Colors.green, Colors.orange,
                     Colors.purple, Colors.teal, Colors.indigo, Colors.pink];
    return Card(
      clipBehavior: Clip.antiAlias,
      child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
        Expanded(flex: 3, child: Container(color: colors[index % colors.length],
          child: Center(child: Text('${index + 1}',
            style: const TextStyle(fontSize: 32, color: Colors.white, fontWeight: FontWeight.bold))))),
        Expanded(flex: 2, child: Padding(padding: const EdgeInsets.all(12),
          child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
            Text('Card ${index + 1}', style: const TextStyle(fontWeight: FontWeight.bold, fontSize: 16)),
            const SizedBox(height: 4),
            Text('Subtitle for card ${index + 1}',
              style: TextStyle(color: Colors.grey.shade600, fontSize: 12)),
          ]))),
      ]),
    );
  }
}
```

### Common Mistakes

**Using integer division for aspect ratio.** `3~/4` produces 0, which crashes. Use `3 / 4` (double division) to get 0.75.

**Not using Expanded inside cards.** The grid delegate fixes card dimensions, so inner Column children need flex to fill that space properly.

---

## Exercise 4: Drawer-to-Sidebar Navigation

### Progressive Hints

**Hint 1:** Return completely different widget trees per breakpoint. Do not try to animate between them.

**Hint 2:** Call `Navigator.pop(context)` in the drawer's onTap to close it after selection.

### Full Solution

```dart
// exercise_04_nav.dart
import 'package:flutter/material.dart';

void main() => runApp(const MaterialApp(home: AdaptiveNav()));

class AdaptiveNav extends StatefulWidget {
  const AdaptiveNav({super.key});
  @override
  State<AdaptiveNav> createState() => _AdaptiveNavState();
}

class _AdaptiveNavState extends State<AdaptiveNav> {
  int _selected = 0;
  static const _items = [
    (icon: Icons.home, label: 'Home'), (icon: Icons.search, label: 'Search'),
    (icon: Icons.favorite, label: 'Favorites'), (icon: Icons.person, label: 'Profile'),
  ];

  @override
  Widget build(BuildContext context) => LayoutBuilder(
    builder: (context, c) {
      if (c.maxWidth >= 1024) return _desktop();
      if (c.maxWidth >= 600) return _tablet();
      return _mobile();
    },
  );

  Widget _mobile() => Scaffold(
    appBar: AppBar(title: Text(_items[_selected].label)),
    drawer: Drawer(child: ListView(padding: EdgeInsets.zero, children: [
      const DrawerHeader(decoration: BoxDecoration(color: Colors.blue),
        child: Text('Navigation', style: TextStyle(color: Colors.white, fontSize: 24))),
      for (var i = 0; i < _items.length; i++)
        ListTile(leading: Icon(_items[i].icon), title: Text(_items[i].label),
          selected: i == _selected,
          onTap: () { setState(() => _selected = i); Navigator.pop(context); }),
    ])),
    body: _content(),
  );

  Widget _tablet() => Scaffold(body: Row(children: [
    NavigationRail(
      selectedIndex: _selected,
      onDestinationSelected: (i) => setState(() => _selected = i),
      labelType: NavigationRailLabelType.selected,
      destinations: [for (final item in _items)
        NavigationRailDestination(icon: Icon(item.icon), label: Text(item.label))],
    ),
    const VerticalDivider(width: 1),
    Expanded(child: _content()),
  ]));

  Widget _desktop() => Scaffold(body: Row(children: [
    SizedBox(width: 250, child: Material(elevation: 2, child: Column(children: [
      const SizedBox(height: 16),
      Text('My App', style: Theme.of(context).textTheme.headlineSmall),
      const SizedBox(height: 16),
      for (var i = 0; i < _items.length; i++)
        ListTile(leading: Icon(_items[i].icon), title: Text(_items[i].label),
          selected: i == _selected, onTap: () => setState(() => _selected = i)),
    ]))),
    const VerticalDivider(width: 1),
    Expanded(child: _content()),
  ]));

  Widget _content() => Center(child: Column(
    mainAxisAlignment: MainAxisAlignment.center,
    children: [
      Icon(_items[_selected].icon, size: 64, color: Colors.blue),
      const SizedBox(height: 16),
      Text(_items[_selected].label, style: const TextStyle(fontSize: 24, fontWeight: FontWeight.bold)),
    ],
  ));
}
```

### Deep Dive

The `_selected` state survives layout transitions because it lives in the State object, which persists as long as AdaptiveNav occupies the same tree position. LayoutBuilder rebuilds the subtree, but the State is not recreated.

---

## Exercise 5: Complex Dashboard

### Progressive Hints

**Hint 1:** Horizontal ListView inside SliverToBoxAdapter needs `SizedBox(height: 120)` wrapper -- without it, "Vertical viewport was given unbounded height."

**Hint 2:** Use SliverPadding instead of regular Padding inside CustomScrollView.

**Hint 3:** Wrap CustomScrollView in RefreshIndicator. The `onRefresh` must return a Future.

### Full Solution

```dart
// exercise_05_dashboard.dart
import 'package:flutter/material.dart';

void main() => runApp(const MaterialApp(home: DashboardScreen()));

class DashboardScreen extends StatelessWidget {
  const DashboardScreen({super.key});

  @override
  Widget build(BuildContext context) {
    return Scaffold(body: LayoutBuilder(
      builder: (context, constraints) {
        final gridCols = constraints.maxWidth >= 1024 ? 4 : constraints.maxWidth >= 600 ? 3 : 2;
        return RefreshIndicator(
          onRefresh: () => Future.delayed(const Duration(seconds: 1)),
          child: CustomScrollView(slivers: [
            const SliverAppBar(expandedHeight: 160, pinned: true,
              flexibleSpace: FlexibleSpaceBar(title: Text('Dashboard'))),
            SliverToBoxAdapter(child: SizedBox(height: 120,
              child: ListView.builder(
                scrollDirection: Axis.horizontal, itemCount: 5,
                padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
                itemBuilder: (context, i) {
                  final titles = ['Revenue', 'Users', 'Orders', 'Returns', 'Rating'];
                  final values = ['\$12.4K', '1,234', '567', '23', '4.8'];
                  return Container(width: 200, margin: const EdgeInsets.only(right: 12),
                    child: Card(child: Padding(padding: const EdgeInsets.all(16),
                      child: Column(crossAxisAlignment: CrossAxisAlignment.start,
                        mainAxisAlignment: MainAxisAlignment.center, children: [
                        Text(titles[i], style: TextStyle(color: Colors.grey.shade600)),
                        const SizedBox(height: 8),
                        Text(values[i], style: const TextStyle(fontSize: 24, fontWeight: FontWeight.bold)),
                      ]))));
                }))),
            SliverPadding(padding: const EdgeInsets.all(16),
              sliver: SliverGrid(
                gridDelegate: SliverGridDelegateWithFixedCrossAxisCount(
                  crossAxisCount: gridCols, mainAxisSpacing: 12, crossAxisSpacing: 12, childAspectRatio: 1.5),
                delegate: SliverChildBuilderDelegate(
                  (context, i) => Card(
                    color: Colors.primaries[i % Colors.primaries.length].withValues(alpha: 0.1),
                    child: Center(child: Text('Metric ${i + 1}', style: const TextStyle(fontWeight: FontWeight.bold)))),
                  childCount: 8))),
            SliverList(delegate: SliverChildBuilderDelegate(
              (context, i) => ListTile(
                leading: CircleAvatar(child: Text('${i + 1}')),
                title: Text('Activity ${i + 1}'), subtitle: Text('${i + 1}m ago')),
              childCount: 20)),
          ]),
        );
      },
    ));
  }
}
```

### Common Mistakes

**Nesting ScrollViews on the same axis.** If you put a vertical ListView inside a vertical CustomScrollView, one of them must have `shrinkWrap: true` or bounded height. The sliver approach (SliverList, SliverGrid) avoids this entirely.

---

## Exercise 6: Responsive Design System

### Progressive Hints

**Hint 1:** Fluid interpolation formula: `min + (max - min) * ((width - minWidth) / (maxWidth - minWidth)).clamp(0.0, 1.0)`.

**Hint 2:** Clamp the progress to prevent values outside min/max range at extreme widths.

### Full Solution

```dart
// exercise_06_design_system.dart
import 'package:flutter/material.dart';

void main() => runApp(const MaterialApp(home: DesignSystemDemo()));

enum ScreenSize { mobile, tablet, desktop, wide }

class Breakpoints {
  static const double tablet = 600, desktop = 1024, wide = 1440;
  static ScreenSize fromWidth(double w) {
    if (w >= wide) return ScreenSize.wide;
    if (w >= desktop) return ScreenSize.desktop;
    if (w >= tablet) return ScreenSize.tablet;
    return ScreenSize.mobile;
  }
}

class FluidTypography {
  final double width;
  const FluidTypography(this.width);
  double _lerp(double min, double max) {
    final t = ((width - 0) / (Breakpoints.wide - 0)).clamp(0.0, 1.0);
    return min + (max - min) * t;
  }
  double get body => _lerp(14, 18);
  double get title => _lerp(20, 32);
  double get headline => _lerp(28, 48);
}

class ResponsiveSpacing {
  final double width;
  const ResponsiveSpacing(this.width);
  double get padding => [8.0, 16.0, 24.0, 32.0][Breakpoints.fromWidth(width).index];
  double get gap => [8.0, 12.0, 16.0, 20.0][Breakpoints.fromWidth(width).index];
}

class DesignSystemDemo extends StatelessWidget {
  const DesignSystemDemo({super.key});

  @override
  Widget build(BuildContext context) => Scaffold(body: LayoutBuilder(
    builder: (context, constraints) {
      final w = constraints.maxWidth;
      final sp = ResponsiveSpacing(w);
      final ty = FluidTypography(w);
      return SingleChildScrollView(
        padding: EdgeInsets.all(sp.padding),
        child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
          Text('Design System', style: TextStyle(fontSize: ty.headline, fontWeight: FontWeight.bold)),
          SizedBox(height: sp.gap),
          Text('Width: ${w.toStringAsFixed(0)}px | ${Breakpoints.fromWidth(w).name}',
            style: TextStyle(fontSize: ty.body, color: Colors.grey)),
          SizedBox(height: sp.gap * 2),
          Text('Headline: ${ty.headline.toStringAsFixed(1)}px', style: TextStyle(fontSize: ty.headline)),
          Text('Title: ${ty.title.toStringAsFixed(1)}px', style: TextStyle(fontSize: ty.title)),
          Text('Body: ${ty.body.toStringAsFixed(1)}px', style: TextStyle(fontSize: ty.body)),
          SizedBox(height: sp.gap * 2),
          Wrap(spacing: sp.gap, runSpacing: sp.gap,
            children: List.generate(6, (i) => SizedBox(width: 200,
              child: Card(child: Padding(padding: EdgeInsets.all(sp.padding),
                child: Text('Card ${i + 1}', style: TextStyle(fontSize: ty.body))))))),
        ]),
      );
    },
  ));
}
```

### Common Mistakes

**Forgetting to clamp interpolation.** Without `.clamp(0.0, 1.0)`, extreme widths produce font sizes outside the intended range.

**Not wrapping in SingleChildScrollView.** Content may exceed screen height at certain widths when spacing scales up.

---

## Exercise 7: Custom Masonry Layout

### Progressive Hints

**Hint 1:** Use CustomMultiChildLayout, not Flow. It gives you `layoutChild` returning actual child sizes before positioning.

**Hint 2:** Each child needs `LayoutId(id: index, child: ...)`. Track column heights in a list, always place in shortest column.

**Hint 3:** Wrap in SizedBox with estimated height, since CustomMultiChildLayout defaults to `constraints.biggest`.

### Full Solution

The delegate is the core. It lays out each child with fixed width (column width) and unconstrained height, then positions in the shortest column:

```dart
// exercise_07_masonry.dart (key delegate)
class MasonryDelegate extends MultiChildLayoutDelegate {
  final int columnCount;
  final double spacing;
  final int childCount;
  MasonryDelegate({required this.columnCount, required this.spacing, required this.childCount});

  @override
  void performLayout(Size size) {
    final colWidth = (size.width - spacing * (columnCount - 1)) / columnCount;
    final colHeights = List.filled(columnCount, 0.0);
    for (var i = 0; i < childCount; i++) {
      if (!hasChild(i)) continue;
      var shortest = 0;
      for (var c = 1; c < columnCount; c++) {
        if (colHeights[c] < colHeights[shortest]) shortest = c;
      }
      final childSize = layoutChild(i, BoxConstraints(minWidth: colWidth, maxWidth: colWidth));
      positionChild(i, Offset(shortest * (colWidth + spacing), colHeights[shortest]));
      colHeights[shortest] += childSize.height + spacing;
    }
  }

  @override
  Size getSize(BoxConstraints constraints) => constraints.biggest;

  @override
  bool shouldRelayout(MasonryDelegate old) =>
    old.columnCount != columnCount || old.spacing != spacing || old.childCount != childCount;
}
```

Wrap the CustomMultiChildLayout in a SingleChildScrollView with a SizedBox whose height is conservatively estimated: `(childCount / columnCount).ceil() * averageHeight`.

### Common Mistakes

**Using Flow instead of CustomMultiChildLayout.** Flow does not let you call `layoutChild` during layout -- you only get sizes during paint. This makes it impossible to do shortest-column placement correctly.

**Not implementing shouldRelayout.** Without it, changing column count on resize does not trigger re-layout.

---

## Exercise 8: Drag-and-Drop Layout Builder

### Progressive Hints

**Hint 1:** Canvas = Stack inside DragTarget. Each placed widget = Positioned in the Stack. State holds a list of `{id, type, x, y, width, height}` objects.

**Hint 2:** Convert DragTarget's global offset to local with `renderBox.globalToLocal(details.offset)`.

**Hint 3:** Undo/redo: `List<List<PlacedWidget>>` history + int index. Push snapshot on every change. Undo = decrement index. New change after undo = truncate future states.

### Full Solution

The solution is a two-panel layout (palette + canvas) with these key components:

1. **Palette items** use `Draggable<String>` with the widget type as data
2. **Canvas** is a `DragTarget<String>` containing a `Stack` with `Positioned` children
3. **Repositioning** uses `GestureDetector.onPanUpdate` on each placed widget, snapping with `(value / 16).round() * 16`
4. **Resize handles** are four small `Positioned` containers at corners, each with its own `GestureDetector.onPanUpdate` that adjusts width/height (and x/y for left/top handles)
5. **Serialization** maps each widget to `{'id', 'type', 'x', 'y', 'width', 'height'}` as JSON
6. **History** pushes a deep copy of the widget list on every completed action (`onPanEnd`, not `onPanUpdate`)

Key implementation details:
- Minimum widget size of 40px prevents collapsing to zero during resize
- Left/top resize handles must adjust both size AND position (moving the origin while expanding)
- Grid painter uses `CustomPaint` with lines at 16px intervals
- Use `onPanEnd` (not `onPanUpdate`) to push history, avoiding hundreds of snapshots per drag

### Common Mistakes

**Pushing history on every onPanUpdate.** This creates a history entry per pixel of movement. Push only on onPanEnd for completed gestures.

**Not converting global to local coordinates.** DragTarget gives global screen coordinates. Without conversion, widgets appear offset by the app bar height and palette width.

**Resize handles not propagating gestures correctly.** If the parent widget's GestureDetector uses `HitTestBehavior.opaque`, it may steal gestures from resize handles. Use default hit test behavior and let the gesture arena resolve naturally.

---

## General Debugging Tips

### The Three Questions

When any layout breaks, ask: (1) What constraints did my widget receive? (Use LayoutBuilder to print.) (2) What size did it report? (Check DevTools Inspector.) (3) Where did the parent place it? (Check alignment, Positioned offsets, padding.)

### Reading Overflow Errors

"A RenderFlex overflowed by X pixels on the right" means Flex children exceed available space. Fix: Expanded, Flexible, SingleChildScrollView, or smaller children.

### Reading Unbounded Constraint Errors

"Vertical viewport was given unbounded height" means a scrollable got infinite constraints on its scroll axis. Classic cause: ListView inside Column without Expanded. Fix: wrap in Expanded or give explicit height.

### Performance Notes

- **IntrinsicWidth/Height**: O(2n) -- double layout pass. Avoid in lists.
- **LayoutBuilder**: rebuilds on every constraint change. Keep builder logic cheap.
- **Slivers**: only build visible children. Always prefer over shrinkWrap.
- **Flow**: repositions without re-layout. Best for animated repositioning.
- **CustomMultiChildLayout**: O(n) layout. Good for custom static arrangements.
