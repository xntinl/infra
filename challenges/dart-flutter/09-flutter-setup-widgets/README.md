# Section 09: Flutter Setup & Widget Fundamentals

## Introduction

Everything in Flutter is a widget. A button is a widget. The padding around it is a widget. The screen itself is a widget. When you internalize this, you stop fighting the framework and start composing UIs the way Flutter was designed to work.

Flutter uses a declarative UI model. Instead of imperatively telling the framework "change this label, move it left, repaint," you declare "here is what the UI looks like given this state" and Flutter computes the minimum diff to update the screen. You describe the end state; Flutter handles the transition.

## Prerequisites

- Dart Sections 01-08 completed (classes, mixins, generics, async, null safety)
- Flutter SDK 3.19+ installed (`flutter --version` prints 3.19+)
- An IDE with Flutter support (VS Code, Android Studio, or IntelliJ)
- At least one target platform configured (Chrome, iOS simulator, or Android emulator)
- `flutter doctor` shows no critical errors

## Learning Objectives

1. **Configure** a Flutter development environment and diagnose setup issues using `flutter doctor`
2. **Explain** the relationship between the Widget tree, Element tree, and RenderObject tree
3. **Implement** StatelessWidget and StatefulWidget with correct lifecycle management
4. **Analyze** when and why widgets rebuild, and how Keys control identity across rebuilds
5. **Evaluate** widget decomposition decisions for maintainability and performance
6. **Design** custom RenderObjects that implement novel layout and painting behavior

---

## Core Concepts

### 1. Installation, Project Structure, and Hot Reload

```bash
# verify_environment.sh
flutter --version
flutter doctor -v
flutter create --org com.example my_first_app
cd my_first_app && flutter run
```

The generated project has `lib/` (your source code), `test/` (tests), `pubspec.yaml` (dependencies and assets), and `analysis_options.yaml` (lint rules). Platform directories (`android/`, `ios/`, `web/`) contain build configuration you rarely touch directly.

**Hot reload** injects updated code into the running VM without losing State. It works for `build` method changes. **Hot restart** kills the VM and restarts `main()` -- all state is lost. Use it when you change `initState`, add State fields, modify enums, or change `main()`. In the terminal: `r` for reload, `R` for restart.

### 2. The Three Trees

Flutter maintains three parallel trees. The **Widget tree** is what you write -- lightweight, immutable configuration objects. The **Element tree** is what Flutter manages -- long-lived objects that mediate between Widgets and RenderObjects. The **RenderObject tree** does layout, painting, and hit testing.

StatelessWidget and StatefulWidget produce no RenderObjects -- they only return other widgets via `build`. Only leaf widgets like `Text`, `Container`, and `Padding` create RenderObjects. When a widget rebuilds, its Element checks if the new widget has the same type and key. If so, it reuses the RenderObject. If not, it destroys and recreates.

### 3. StatelessWidget

A StatelessWidget depends only on its constructor arguments and inherited widgets in its BuildContext. All fields are `final`. Use `const` constructors to enable rebuild optimizations.

```dart
// greeting_card.dart
import 'package:flutter/material.dart';

class GreetingCard extends StatelessWidget {
  final String name;
  final String message;

  const GreetingCard({super.key, required this.name, this.message = 'Welcome!'});

  @override
  Widget build(BuildContext context) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16.0),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(name, style: Theme.of(context).textTheme.headlineSmall),
            const SizedBox(height: 8),
            Text(message),
          ],
        ),
      ),
    );
  }
}
```

### 4. StatefulWidget and the State Lifecycle

A StatefulWidget is two objects: the Widget (immutable, recreated on rebuild) and its State (mutable, persists across rebuilds). The lifecycle order: constructor -> `initState` -> `didChangeDependencies` -> `build`. On parent rebuild: `didUpdateWidget` -> `build`. On removal: `dispose`.

```dart
// tap_counter.dart
import 'package:flutter/material.dart';

class TapCounter extends StatefulWidget {
  final String label;
  const TapCounter({super.key, required this.label});

  @override
  State<TapCounter> createState() => _TapCounterState();
}

class _TapCounterState extends State<TapCounter> {
  int _count = 0;

  @override
  void initState() {
    super.initState();
    // One-time setup. Do NOT access inherited widgets here.
  }

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    // Earliest safe place for Theme.of(context), MediaQuery.of(context).
  }

  @override
  void didUpdateWidget(covariant TapCounter oldWidget) {
    super.didUpdateWidget(oldWidget);
    // Parent provided a new widget. Compare oldWidget vs widget to react.
  }

  @override
  Widget build(BuildContext context) {
    return GestureDetector(
      onTap: () => setState(() => _count++),
      child: Text('${widget.label}: $_count',
          style: Theme.of(context).textTheme.headlineMedium),
    );
  }

  @override
  void dispose() {
    // Cancel timers, dispose controllers, close streams. Call super.dispose() LAST.
    super.dispose();
  }
}
```

Common mistake: accessing `Theme.of(context)` in `initState`. The context is not fully wired yet -- use `didChangeDependencies` instead.

### 5. BuildContext

BuildContext is a reference to the widget's Element in the Element tree. `Theme.of(context)` walks up from that Element to find a `Theme` ancestor. The context you use determines which ancestors you see.

```dart
// build_context_demo.dart
import 'package:flutter/material.dart';

class ContextDemo extends StatelessWidget {
  const ContextDemo({super.key});

  @override
  Widget build(BuildContext context) {
    // context here refers to ContextDemo's Element.
    final theme = Theme.of(context); // walks UP to find ThemeData

    return Scaffold(
      body: Column(
        children: [
          Text('Width: ${MediaQuery.of(context).size.width}'),
          // Builder creates a NEW context below this widget.
          Builder(
            builder: (innerContext) {
              // innerContext sees ancestors from a different position.
              // Useful when you need a context below a Scaffold.
              return ElevatedButton(
                onPressed: () {
                  // This works because innerContext is below Scaffold.
                  ScaffoldMessenger.of(innerContext).showSnackBar(
                    const SnackBar(content: Text('Hello!')),
                  );
                },
                child: const Text('Show Snackbar'),
              );
            },
          ),
        ],
      ),
    );
  }
}
```

A classic bug: calling `ScaffoldMessenger.of(context)` where `context` is above the `Scaffold`. The fix: use a `context` from below the `Scaffold`, via `Builder` or by extracting the child into a separate widget.

### 6. Keys: Controlling Widget Identity

When Flutter rebuilds, it matches old widgets with new ones by type and key. If both match (or both keys are null), the Element and State are reused. If either differs, the old Element is destroyed and a new one is created. Keys become essential when you have lists of same-type widgets that reorder.

```dart
// keys_overview.dart
// ValueKey<T>  - unique business identifier: ValueKey(item.id)
//                Use when each item has a stable unique field.
// ObjectKey    - identity is the object itself: ObjectKey(myObject)
//                Uses == and hashCode. Good when no single field is unique.
// UniqueKey    - force a new identity every time (use sparingly)
//                Creates new identity on every build -- destroys reuse.
// GlobalKey    - access State/RenderObject from outside the tree
//                Expensive. Common use: GlobalKey<FormState> for form validation.

// Without keys in a dynamic list: reorder items and State sticks to positions.
// With ValueKey: State follows the data across reorders.
```

The most common mistake: omitting keys entirely. For static lists this is fine. For any list with sorting, filtering, reordering, or animation, missing keys cause state bugs that are subtle and hard to diagnose.

### 7. App Shell and Basic Widgets

Every Material app starts with `MaterialApp` at the root (theme, routing, localization). Inside a route, `Scaffold` provides the visual structure: app bar, body, FAB, drawers, bottom navigation, snack bars.

```dart
// app_shell.dart
import 'package:flutter/material.dart';

class HomeScreen extends StatelessWidget {
  const HomeScreen({super.key});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Home')),
      body: SingleChildScrollView(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('Styled', style: Theme.of(context).textTheme.headlineMedium),
            const SizedBox(height: 16),
            const Icon(Icons.flutter_dash, size: 48, color: Colors.blue),
            const SizedBox(height: 16),
            Container(
              width: double.infinity,
              padding: const EdgeInsets.all(16),
              decoration: BoxDecoration(
                color: Colors.indigo.shade50,
                borderRadius: BorderRadius.circular(8),
              ),
              child: const Text('Inside a Container'),
            ),
          ],
        ),
      ),
      floatingActionButton: FloatingActionButton(
        onPressed: () {},
        child: const Icon(Icons.add),
      ),
    );
  }
}
```

Prefer single-purpose widgets: `Padding` over `Container` when you only need padding, `SizedBox` for spacing, `Align` for alignment. Each single-purpose widget communicates intent clearly. Use `Container` only when you genuinely need multiple features together (color + padding + border + constraints).

### 8. Widget Composition Patterns

Extract small, focused widgets. A 200-line `build` method is not just hard to read -- it causes unnecessary rebuilds because `setState` rebuilds everything below it. Smaller widgets mean smaller rebuild scopes, independent testability, and reusable APIs.

```dart
// widget_composition.dart
// BAD: monolithic build with 5 nested levels and mixed concerns.
// GOOD: decompose into focused widgets with clear constructor APIs:

class ProfileScreen extends StatelessWidget {
  const ProfileScreen({super.key});

  @override
  Widget build(BuildContext context) {
    return const Column(children: [
      ProfileAvatar(initials: 'JD'),
      ProfileInfo(name: 'Jane', email: 'jane@example.com'),
      ProfileStats(posts: 142, followers: 1200, following: 89),
    ]);
  }
}

// Each of ProfileAvatar, ProfileInfo, ProfileStats is a separate
// StatelessWidget with const constructor, independently testable,
// with a clear API defined by its final fields.
```

Each extracted widget rebuilds independently. When `ProfileStats` rebuilds, `ProfileAvatar` does not. When you need to change stats display, you touch one widget.

---

## Exercises

### Exercise 1 (Basic): Your First Flutter App

**Estimated time: 20 minutes**

Create a new Flutter project and build a personalized greeting card using basic widgets.

**Instructions:**

1. Create a project: `flutter create --org com.example greeting_app`
2. Replace `lib/main.dart` with the scaffold below
3. Implement `GreetingCard` to display an Icon, name, and title inside a Card with proper padding
4. Run the app, change the name text, and verify hot reload preserves state

```dart
// lib/main.dart
import 'package:flutter/material.dart';

void main() => runApp(const GreetingApp());

class GreetingApp extends StatelessWidget {
  const GreetingApp({super.key});

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      theme: ThemeData(colorSchemeSeed: Colors.teal, useMaterial3: true),
      home: const Scaffold(
        body: Center(
          child: GreetingCard(name: 'Your Name', title: 'Flutter Dev', icon: Icons.code),
        ),
      ),
    );
  }
}

class GreetingCard extends StatelessWidget {
  final String name;
  final String title;
  final IconData icon;
  const GreetingCard({super.key, required this.name, required this.title, required this.icon});

  @override
  Widget build(BuildContext context) {
    // TODO: Card with Icon (size 48, primary color), name (headlineSmall),
    // title (bodyMedium, muted color), padding 16, centered Column with MainAxisSize.min
    throw UnimplementedError();
  }
}
```

**Verification:** Run the app. You should see a centered card with icon, name, and title. Change the name string, save, and confirm hot reload updates the text instantly.

---

### Exercise 2 (Basic): StatefulWidget Lifecycle Logging

**Estimated time: 25 minutes**

Build a counter with `debugPrint` in every lifecycle method to observe the exact call order.

**Instructions:**

1. Create a project: `flutter create lifecycle_counter`
2. Implement a `LifecycleCounter` StatefulWidget with increment/decrement buttons
3. Add `debugPrint` in `initState`, `didChangeDependencies`, `build`, `didUpdateWidget`, `dispose`
4. Wrap it in a parent that can toggle the label between "Counter A" and "Counter B" and show/hide the counter entirely using a bool and an `if` in the widget tree
5. Observe console output during each interaction

```dart
// lib/main.dart (parent scaffold -- implement the LifecycleCounter)
class LifecycleParent extends StatefulWidget {
  const LifecycleParent({super.key});
  @override
  State<LifecycleParent> createState() => _LifecycleParentState();
}

class _LifecycleParentState extends State<LifecycleParent> {
  String _label = 'Counter A';
  bool _showCounter = true;

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Lifecycle Demo')),
      body: Column(
        mainAxisAlignment: MainAxisAlignment.center,
        children: [
          Row(mainAxisAlignment: MainAxisAlignment.center, children: [
            ElevatedButton(
              onPressed: () => setState(() {
                _label = _label == 'Counter A' ? 'Counter B' : 'Counter A';
              }),
              child: Text('Toggle Label ($_label)'),
            ),
            const SizedBox(width: 16),
            ElevatedButton(
              onPressed: () => setState(() => _showCounter = !_showCounter),
              child: Text(_showCounter ? 'Hide' : 'Show'),
            ),
          ]),
          const SizedBox(height: 32),
          if (_showCounter) LifecycleCounter(label: _label),
        ],
      ),
    );
  }
}
```

**Verification:**

On launch, debug console shows:
```
[initState] Counter A
[didChangeDependencies] Counter A
[build] Counter A, count: 0
```

Tap increment twice: `[build]` fires each time. Toggle label: `[didUpdateWidget] Counter A -> Counter B, changed: true` then `[build]`. Hide: `[dispose] Counter B`. Show again: `[initState]` (count resets to 0 because State was destroyed and recreated).

---

### Exercise 3 (Intermediate): Multi-Screen Widget Decomposition

**Estimated time: 45 minutes**

Build a "Contact Book" with a list screen and detail screen, using at least 5 separate widget classes.

**Instructions:**

1. Define a `Contact` class with `id`, `name`, `email`, `phone`, and an `avatarInitials` getter
2. Build `ContactListScreen` with `ListView.builder` and `ContactListTile` widgets
3. Build `ContactDetailScreen` showing full info with `ContactAvatar` and `ContactInfoRow`
4. Navigate with `Navigator.push` and `MaterialPageRoute`
5. No `build` method should exceed 30 lines

```dart
// lib/models/contact.dart
class Contact {
  final String id, name, email, phone;
  const Contact({required this.id, required this.name, required this.email, required this.phone});
  String get avatarInitials {
    final p = name.split(' ');
    return p.length >= 2 ? '${p[0][0]}${p[1][0]}'.toUpperCase() : name[0].toUpperCase();
  }
}

const sampleContacts = [
  Contact(id: '1', name: 'Alice Martin', email: 'alice@example.com', phone: '+1-555-0101'),
  Contact(id: '2', name: 'Bob Chen', email: 'bob@example.com', phone: '+1-555-0102'),
  Contact(id: '3', name: 'Carol Davis', email: 'carol@example.com', phone: '+1-555-0103'),
  Contact(id: '4', name: 'David Wilson', email: 'david@example.com', phone: '+1-555-0104'),
  Contact(id: '5', name: 'Eve Johnson', email: 'eve@example.com', phone: '+1-555-0105'),
];
```

**Verification:** Scrollable list with 5 contacts and avatar initials. Tapping navigates to detail screen. Back button returns. Each widget has a focused responsibility.

---

### Exercise 4 (Intermediate): Keys and State Preservation

**Estimated time: 40 minutes**

Build a sortable list of stateful colored counters demonstrating the Keys bug and its fix.

**Instructions:**

1. Create a project: `flutter create keys_lab`
2. Build `ColoredCounter` -- a StatefulWidget that initializes a random color in `initState` and has a local tap counter
3. Create a list of 5 items in a parent widget, with buttons to shuffle, reverse, remove first, and add new
4. Add a Switch to toggle `_useKeys`. When on, pass `ValueKey(item)` to each counter. When off, pass `null`
5. First test with keys OFF: tap items differently, shuffle, and observe the bug
6. Then switch keys ON and repeat

```dart
// lib/main.dart
import 'dart:math';
import 'package:flutter/material.dart';

class ColoredCounter extends StatefulWidget {
  final String label;
  const ColoredCounter({super.key, required this.label});
  @override
  State<ColoredCounter> createState() => _ColoredCounterState();
}

class _ColoredCounterState extends State<ColoredCounter> {
  late final Color _color;
  int _count = 0;

  @override
  void initState() {
    super.initState();
    _color = Color.fromRGBO(Random().nextInt(256), Random().nextInt(256), Random().nextInt(256), 1.0);
  }

  @override
  Widget build(BuildContext context) {
    return GestureDetector(
      onTap: () => setState(() => _count++),
      child: Container(
        margin: const EdgeInsets.symmetric(vertical: 4, horizontal: 16),
        padding: const EdgeInsets.all(16),
        decoration: BoxDecoration(
          color: _color.withValues(alpha: 0.3),
          borderRadius: BorderRadius.circular(8),
          border: Border.all(color: _color),
        ),
        child: Row(children: [
          Container(width: 24, height: 24, color: _color),
          const SizedBox(width: 16),
          Text(widget.label),
          const Spacer(),
          Text('Taps: $_count'),
        ]),
      ),
    );
  }
}

// TODO: Build KeysPlayground parent with:
// - _items list, _useKeys bool, shuffle/reverse/remove/add buttons
// - For each item, render: ColoredCounter(key: _useKeys ? ValueKey(item) : null, label: item)
```

**Verification:** Keys off: tap "Item 1" three times, "Item 3" five times, then shuffle. Labels move but colors and tap counts stay in their original positions -- the bug. Keys on: repeat the same experiment. Colors and counts now follow their labels. This is the single most important widget identity concept in Flutter.

---

### Exercise 5 (Advanced): Widget Rebuild Performance Analysis

**Estimated time: 60 minutes**

Build a dashboard with a clock (updates every second), a counter, a text input, and a static panel. Measure rebuild counts, then optimize.

**Instructions:**

1. Version 1 (monolithic): All state in one widget. Timer ticks cause ALL sections to rebuild
2. Version 2 (decomposed): Extract each section into its own StatefulWidget
3. Add a rebuild counter to each section (increment `_buildCount` in `build`, display it)
4. Compare: monolithic shows all sections at ~10 rebuilds after 10 seconds. Decomposed shows clock at ~10, others at 1

**Verification:** After decomposition, tapping the counter only increments the counter section's rebuild count. The clock ticks without affecting other sections. The static panel (a `const` StatelessWidget) stays at 1 rebuild forever.

---

### Exercise 6 (Advanced): Reusable Widget Library

**Estimated time: 75 minutes**

Design three reusable widgets with clean APIs: `LabeledField`, `StatusBadge`, and `SectionCard`.

**Instructions:**

1. `StatusBadge`: colored pill displaying a status. Accept `StatusType` enum (`info`, `success`, `warning`, `error`) for automatic colors, plus optional `colorOverride`
2. `LabeledField`: label above value, configurable styles and spacing, optional `onTap`
3. `SectionCard`: Card with colored title bar, optional action widget, and body
4. All must support `const` constructors and use theme colors (not hardcoded)
5. Write at least one widget test per component
6. Create a barrel file `lib/widgets.dart` exporting all three

```dart
// test/status_badge_test.dart
testWidgets('StatusBadge displays label text', (tester) async {
  await tester.pumpWidget(
    const MaterialApp(
      home: Scaffold(body: StatusBadge(label: 'Active', type: StatusType.success)),
    ),
  );
  expect(find.text('Active'), findsOneWidget);
});
```

**Verification:** `flutter test` passes. Demo screen renders all three widgets. `const` instances produce no analyzer warnings.

---

### Exercise 7 (Insane): Custom RenderObject Widget

**Estimated time: 3-4 hours**

Build `RadialLayout` -- a widget that positions children in a circle by implementing a custom RenderObject. No `Transform` or `Stack` with `Positioned` allowed.

**Instructions:**

Create three classes:
1. `RadialLayout` extending `MultiChildRenderObjectWidget` (accepts `radius` and `startAngle`)
2. `RadialParentData` extending `ContainerBoxParentData<RenderBox>`
3. `RenderRadialLayout` extending `RenderBox` with `ContainerRenderObjectMixin` -- implement `performLayout` (position children using trigonometry), `paint`, and `hitTestChildren`

```dart
// lib/radial_layout.dart (performLayout sketch)
@override
void performLayout() {
  // 1. Lay out each child with loose constraints (maxDim = radius * 0.6)
  // 2. Compute angleStep = 2 * pi / childCount
  // 3. For each child: angle = startAngle + index * angleStep
  //    offset = Offset(center + radius*cos(angle) - child.width/2,
  //                    center + radius*sin(angle) - child.height/2)
  // 4. Store offset in RadialParentData
  // 5. size = constraints.constrain(Size(diameter, diameter))
}
```

**Consider:** Why does `child.layout()` need `parentUsesSize: true`? Why must hit testing iterate children in reverse paint order? What happens with 0 or 1 children?

**Verification:** Place 12 numbered Text widgets to create a clock face. Wrap some in GestureDetector -- taps should register on the correct child. Test with 1, 4, 12, and 0 children.

---

### Exercise 8 (Insane): JSON-Driven Dynamic Widget System

**Estimated time: 4-5 hours**

Build a system that renders a widget tree from JSON configuration with type safety, validation, and error handling.

**Instructions:**

1. `WidgetNode` -- parses JSON with `type` (required string), `properties` (optional map), `children` (optional list, recursively parsed). Throws `FormatException` on invalid structure
2. `WidgetFactory` -- maps type strings to builder functions. Supports: `text`, `column`, `row`, `container`, `sizedBox`, `padding`, `center`, `icon`, `card`, `elevatedButton`, `scaffold`
3. Action system: buttons specify `{"onTap": {"action": "snackbar", "message": "Hello"}}` which triggers a callback
4. Error recovery: unknown types render a red error widget instead of crashing
5. Property parsers: color strings ("#2196F3" -> Color), alignment enums, text styles

```json
// example_ui.json
{
  "type": "scaffold",
  "properties": {"appBarTitle": "Dynamic UI"},
  "children": [{
    "type": "column",
    "properties": {"crossAxisAlignment": "center"},
    "children": [
      {"type": "icon", "properties": {"icon": "flutter_dash", "size": 64, "color": "#2196F3"}},
      {"type": "sizedBox", "properties": {"height": 16}},
      {"type": "text", "properties": {"data": "Built from JSON", "style": {"fontSize": 24, "fontWeight": "bold"}}},
      {"type": "elevatedButton", "properties": {"onTap": {"action": "snackbar", "message": "It works!"}},
       "children": [{"type": "text", "properties": {"data": "Tap Me"}}]}
    ]
  }]
}
```

**Consider:** Security implications (resource exhaustion from deep nesting, tracking pixel URLs). Add depth limits. Make the factory extensible so consumers can register custom widget types.

**Verification:** JSON renders correctly. Button shows snackbar. Unknown type shows red error widget. Malformed JSON throws descriptive FormatException. Write 3+ tests: valid parsing, unknown type handling, missing required property.

---

## Summary

- **Environment**: `flutter doctor`, project structure (`lib/`, `test/`, `pubspec.yaml`), hot reload vs hot restart
- **Three trees**: Widget (your code), Element (bookkeeping), RenderObject (layout/paint). Widgets are cheap; Elements and RenderObjects are long-lived
- **StatelessWidget**: immutable, depends on constructor args and inherited data. Default choice
- **StatefulWidget lifecycle**: `initState` -> `didChangeDependencies` -> `build` -> `didUpdateWidget` -> `dispose`
- **BuildContext**: a reference to the Element, used to find ancestors. The context determines which ancestors you see
- **Keys**: `ValueKey`, `ObjectKey`, `UniqueKey`, `GlobalKey` -- control identity in dynamic lists
- **Composition**: small focused widgets reduce rebuild scope and improve testability
- **`const` constructors**: let Flutter skip entire subtrees during rebuilds

## What's Next

In **Section 10: Layouts**, you will learn how Flutter's constraint system works (constraints down, sizes up, parent sets position), master `Row`, `Column`, `Stack`, `Expanded`, `Flexible`, and `LayoutBuilder`, and understand why "unbounded height" errors happen.

## References

- [Flutter: Get Started](https://docs.flutter.dev/get-started/install)
- [Flutter: Introduction to Widgets](https://docs.flutter.dev/ui/widgets-intro)
- [Flutter: StatefulWidget Lifecycle](https://api.flutter.dev/flutter/widgets/State-class.html)
- [Flutter: Inside Flutter (Three Trees)](https://docs.flutter.dev/resources/inside-flutter)
- [Flutter: Keys](https://api.flutter.dev/flutter/foundation/Key-class.html)
- [Flutter: RenderObject Class](https://api.flutter.dev/flutter/rendering/RenderObject-class.html)
- [Flutter: Hot Reload](https://docs.flutter.dev/tools/hot-reload)
- [Flutter: Performance Best Practices](https://docs.flutter.dev/perf/best-practices)
