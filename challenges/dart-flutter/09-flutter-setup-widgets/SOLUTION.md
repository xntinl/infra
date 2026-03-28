# Section 09: Solutions -- Flutter Setup & Widget Fundamentals

## How to Use This File

Do not read the full solution until you have spent real time struggling. Use this in stages:
1. Try the exercise for at least the estimated time
2. Read the **progressive hints** first -- they nudge without giving the answer
3. Check **common mistakes** if stuck on a specific error
4. Read the **full solution** only after you have a working attempt
5. Read the **deep dive** after your solution works

---

## Exercise 1: Your First Flutter App

### Progressive Hints

1. Return a `Card` from `build`. Inside: `Padding` -> `Column` with `MainAxisSize.min`.
2. Get the primary color with `Theme.of(context).colorScheme.primary`.
3. Use `headlineSmall` for the name, `bodyMedium` with `copyWith(color: theme.colorScheme.onSurfaceVariant)` for the muted title.

### Full Solution

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
    final theme = Theme.of(context);
    return Card(
      elevation: 2,
      child: Padding(
        padding: const EdgeInsets.all(16.0),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(icon, size: 48, color: theme.colorScheme.primary),
            const SizedBox(height: 12),
            Text(name, style: theme.textTheme.headlineSmall),
            const SizedBox(height: 4),
            Text(title, style: theme.textTheme.bodyMedium?.copyWith(
              color: theme.colorScheme.onSurfaceVariant,
            )),
          ],
        ),
      ),
    );
  }
}
```

### Common Mistakes

**Forgetting `MainAxisSize.min`:** Without it, the Column fills all available vertical space and the Card becomes full-height. Always use `min` when a Column should wrap its content.

**Hardcoding colors:** `Colors.grey` ignores the theme and breaks in dark mode. Use `theme.colorScheme.onSurfaceVariant` for muted text -- it adapts automatically.

**Not using `const` constructor:** `const` lets Flutter short-circuit rebuilds when the parent provides identical arguments.

---

## Exercise 2: StatefulWidget Lifecycle Logging

### Progressive Hints

1. Override `initState`, `didChangeDependencies`, `didUpdateWidget`, and `dispose` alongside the existing `build`.
2. In `didUpdateWidget`, compare `oldWidget.label != widget.label` to detect changes.
3. Call `super.initState()` first, `super.dispose()` last. Other supers go first too.

### Full Solution

```dart
// Add these overrides to _LifecycleCounterState
@override
void initState() {
  super.initState();
  debugPrint('[initState] ${widget.label}');
}

@override
void didChangeDependencies() {
  super.didChangeDependencies();
  debugPrint('[didChangeDependencies] ${widget.label}');
}

@override
void didUpdateWidget(covariant LifecycleCounter oldWidget) {
  super.didUpdateWidget(oldWidget);
  debugPrint('[didUpdateWidget] ${oldWidget.label} -> ${widget.label}, '
      'changed: ${oldWidget.label != widget.label}');
}

@override
void dispose() {
  debugPrint('[dispose] ${widget.label}');
  super.dispose();
}
```

### Common Mistakes

**Accessing inherited widgets in `initState`:** `Theme.of(context)` may seem to work but is officially unsafe. Use `didChangeDependencies` -- it fires right after `initState` with a fully wired context.

**Expecting state to survive hide/show:** Removing a widget from the tree calls `dispose` and destroys the State. Showing it again creates fresh State (count resets to 0). This is by design -- lift state up if it must survive removal.

**Calling `super.dispose()` before cleanup:** After `super.dispose()`, the State is dead. Access to `widget` or `context` may throw. Clean up first, then call super.

### Deep Dive: Why `didUpdateWidget` Exists

When the parent rebuilds and provides a new widget instance (same type, same position), Flutter reuses the existing State. It calls `didUpdateWidget` with the old widget for comparison. This is how you detect configuration changes (e.g., a URL changed) and react (cancel old request, start new one) without losing accumulated state.

---

## Exercise 3: Multi-Screen Widget Decomposition

### Progressive Hints

1. `ContactListScreen`: Scaffold + AppBar + `ListView.builder` with `sampleContacts.length`.
2. Navigation: `Navigator.of(context).push(MaterialPageRoute(builder: (_) => ContactDetailScreen(contact: contact)))`.
3. Extract `ContactAvatar` (wraps CircleAvatar with configurable radius) and `ContactInfoRow` (icon + label + value).

### Full Solution

```dart
// lib/widgets/contact_avatar.dart
class ContactAvatar extends StatelessWidget {
  final String initials;
  final double radius;
  const ContactAvatar({super.key, required this.initials, this.radius = 20});

  @override
  Widget build(BuildContext context) {
    return CircleAvatar(radius: radius, child: Text(initials, style: TextStyle(fontSize: radius * 0.5)));
  }
}

// lib/widgets/contact_list_tile.dart
class ContactListTile extends StatelessWidget {
  final Contact contact;
  final VoidCallback onTap;
  const ContactListTile({super.key, required this.contact, required this.onTap});

  @override
  Widget build(BuildContext context) {
    return ListTile(
      leading: ContactAvatar(initials: contact.avatarInitials),
      title: Text(contact.name),
      subtitle: Text(contact.email),
      trailing: const Icon(Icons.chevron_right),
      onTap: onTap,
    );
  }
}

// lib/widgets/contact_info_row.dart
class ContactInfoRow extends StatelessWidget {
  final IconData icon;
  final String label;
  final String value;
  const ContactInfoRow({super.key, required this.icon, required this.label, required this.value});

  @override
  Widget build(BuildContext context) {
    return Row(children: [
      Icon(icon, size: 24, color: Theme.of(context).colorScheme.primary),
      const SizedBox(width: 16),
      Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
        Text(label, style: Theme.of(context).textTheme.labelSmall),
        Text(value, style: Theme.of(context).textTheme.bodyLarge),
      ]),
    ]);
  }
}

// lib/screens/contact_detail_screen.dart
class ContactDetailScreen extends StatelessWidget {
  final Contact contact;
  const ContactDetailScreen({super.key, required this.contact});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: Text(contact.name)),
      body: Padding(
        padding: const EdgeInsets.all(24),
        child: Column(children: [
          ContactAvatar(initials: contact.avatarInitials, radius: 48),
          const SizedBox(height: 16),
          Text(contact.name, style: Theme.of(context).textTheme.headlineSmall),
          const SizedBox(height: 24),
          ContactInfoRow(icon: Icons.email_outlined, label: 'Email', value: contact.email),
          const SizedBox(height: 12),
          ContactInfoRow(icon: Icons.phone_outlined, label: 'Phone', value: contact.phone),
        ]),
      ),
    );
  }
}
```

### Common Mistakes

**Passing the whole contact list to the detail screen:** Only pass the specific `Contact`. The detail screen has no business knowing about other contacts.

**Navigation with wrong context:** If your context is above `MaterialApp`, `Navigator.of(context)` fails. The context inside `ListView.builder`'s `itemBuilder` is below `MaterialApp` and works correctly.

---

## Exercise 4: Keys and State Preservation

### Progressive Hints

1. Shuffle: `setState(() { _items = List.of(_items)..shuffle(); })` -- create a new list, do not mutate in place.
2. The key toggle: `key: _useKeys ? ValueKey(item) : null`.
3. Tap items different numbers of times before shuffling so you can visually track which state belongs to which item.

### Full Solution

```dart
// Key part of the parent build method
Expanded(
  child: ListView(
    children: [
      for (final item in _items)
        ColoredCounter(
          key: _useKeys ? ValueKey(item) : null,
          label: item,
        ),
    ],
  ),
),
```

Action methods:
```dart
void _shuffle() => setState(() { _items = List.of(_items)..shuffle(); });
void _reverse() => setState(() { _items = _items.reversed.toList(); });
void _removeFirst() { if (_items.isNotEmpty) setState(() { _items = List.of(_items)..removeAt(0); }); }
void _addItem() => setState(() { _items = [..._items, 'Item $_nextId']; _nextId++; });
```

### Common Mistakes

**Using `UniqueKey()` instead of `ValueKey`:** `UniqueKey()` creates new identity every build, destroying and recreating all Elements every frame. The opposite of what you want.

**Using index as key (`ValueKey(index)`):** This is equivalent to no key. When items shuffle, indices 0-4 still exist unchanged -- the key must be tied to data identity, not position.

**Mutating the list in place:** `_items.shuffle()` modifies the list without changing its reference. Create a new list to make the change explicit to Flutter.

### Deep Dive: The Reconciliation Algorithm

Flutter walks old and new children simultaneously. For each pair, it checks: same `runtimeType` AND same `key`? If yes, reuse the Element. If no, build a map of remaining keyed old children, then match new children against it. Without keys, position-only matching means position 0 always reuses position 0's State, regardless of data changes. With keys, the match follows the key.

---

## Exercise 5: Widget Rebuild Performance Analysis

### Progressive Hints

1. Monolithic: put `_time`, `_count`, `_text` in one State with a `Timer.periodic` calling `setState`.
2. Decomposed: `ClockSection` owns the Timer. `CounterSection` owns the count. `TextInputSection` owns the text. `DashboardScreen` becomes a StatelessWidget.
3. Track rebuilds by incrementing `_buildCount++` at the top of each `build` method.

### Full Solution

The key insight: in the monolithic version, the Timer's `setState` triggers a full rebuild of everything. In the decomposed version, each section's `setState` only rebuilds that section.

```dart
// Decomposed ClockSection (owns its own Timer)
class ClockSection extends StatefulWidget {
  const ClockSection({super.key});
  @override
  State<ClockSection> createState() => _ClockSectionState();
}

class _ClockSectionState extends State<ClockSection> {
  late Timer _timer;
  DateTime _now = DateTime.now();
  int _buildCount = 0;

  @override
  void initState() {
    super.initState();
    _timer = Timer.periodic(const Duration(seconds: 1), (_) => setState(() => _now = DateTime.now()));
  }

  @override
  void dispose() { _timer.cancel(); super.dispose(); }

  @override
  Widget build(BuildContext context) {
    _buildCount++;
    return Card(child: Padding(
      padding: const EdgeInsets.all(16),
      child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
        Text('Clock rebuilds: $_buildCount', style: const TextStyle(fontSize: 10, color: Colors.grey)),
        Text('${_now.hour.toString().padLeft(2, '0')}:${_now.minute.toString().padLeft(2, '0')}:${_now.second.toString().padLeft(2, '0')}',
            style: Theme.of(context).textTheme.headlineMedium),
      ]),
    ));
  }
}
```

### Common Mistakes

**Forgetting to cancel the Timer in dispose:** Causes "setState() called after dispose()" errors after navigation.

**Using StatefulWidget for the static panel:** If it never changes, use a `const` StatelessWidget. It communicates zero-mutation intent and Flutter skips it entirely during parent rebuilds.

### Deep Dive: How `const` Prevents Rebuilds

A `const` widget is a compile-time constant. Flutter sees the same identical instance (by reference, not value) on every build and short-circuits the entire subtree comparison. `const SizedBox(height: 16)` is a single allocation reused forever. `SizedBox(height: 16)` allocates a new object every build.

---

## Exercise 6: Reusable Widget Library

### Progressive Hints

1. `StatusBadge`: private method `_resolveColors` returns `(Color bg, Color fg)` based on `StatusType` or `colorOverride`.
2. `LabeledField`: Column with label Text and value Text. Wrap in `InkWell` when `onTap != null`.
3. `SectionCard`: Card with `Clip.antiAlias`, title bar as a colored Container inside a Column.

### Full Solution

```dart
// lib/src/status_badge.dart
class StatusBadge extends StatelessWidget {
  final String label;
  final StatusType type;
  final Color? colorOverride;
  const StatusBadge({super.key, required this.label, this.type = StatusType.info, this.colorOverride});

  @override
  Widget build(BuildContext context) {
    final (bg, fg) = colorOverride != null
        ? (colorOverride!, ThemeData.estimateBrightnessForColor(colorOverride!) == Brightness.dark ? Colors.white : Colors.black)
        : switch (type) {
            StatusType.info => (Colors.blue.shade100, Colors.blue.shade900),
            StatusType.success => (Colors.green.shade100, Colors.green.shade900),
            StatusType.warning => (Colors.orange.shade100, Colors.orange.shade900),
            StatusType.error => (Colors.red.shade100, Colors.red.shade900),
          };
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 4),
      decoration: BoxDecoration(color: bg, borderRadius: BorderRadius.circular(12)),
      child: Text(label, style: TextStyle(color: fg, fontSize: 12, fontWeight: FontWeight.w600)),
    );
  }
}
```

### Common Mistakes

**Missing `Clip.antiAlias` on SectionCard:** Without it, the title bar's background bleeds past the Card's rounded corners.

**Hardcoded colors:** Use `theme.colorScheme` for adaptability across light/dark themes.

**Missing MaterialApp in tests:** Widgets using `Theme.of(context)` crash without a `MaterialApp` ancestor. Always wrap: `MaterialApp(home: Scaffold(body: yourWidget))`.

---

## Exercise 7: Custom RenderObject Widget

### Progressive Hints

1. In `performLayout`, iterate children twice: first to `layout()` each with loose constraints (`parentUsesSize: true`), then to compute positions via `center + radius * cos/sin(angle) - childSize/2`.
2. Store offsets in `(child.parentData as RadialParentData).offset`.
3. `paint`: call `defaultPaint(context, offset)`. `hitTestChildren`: call `defaultHitTestChildren(result, position: position)`.
4. Set own size: `size = constraints.constrain(Size(diameter, diameter))`.

### Full Solution

```dart
@override
void performLayout() {
  int childCount = 0;
  double maxChildDim = 0;

  RenderBox? child = firstChild;
  while (child != null) {
    child.layout(BoxConstraints.loose(Size(_radius * 0.6, _radius * 0.6)), parentUsesSize: true);
    maxChildDim = max(maxChildDim, max(child.size.width, child.size.height));
    childCount++;
    child = childAfter(child);
  }

  final diameter = _radius * 2 + maxChildDim;
  size = constraints.constrain(Size(diameter, diameter));
  if (childCount == 0) return;

  final cx = size.width / 2, cy = size.height / 2;
  final step = 2 * pi / childCount;
  int i = 0;
  child = firstChild;
  while (child != null) {
    final angle = _startAngle + i * step;
    (child.parentData as RadialParentData).offset = Offset(
      cx + _radius * cos(angle) - child.size.width / 2,
      cy + _radius * sin(angle) - child.size.height / 2,
    );
    i++;
    child = childAfter(child);
  }
}

@override
void paint(PaintingContext context, Offset offset) => defaultPaint(context, offset);

@override
bool hitTestChildren(BoxHitTestResult result, {required Offset position}) =>
    defaultHitTestChildren(result, position: position);
```

### Common Mistakes

**Forgetting `parentUsesSize: true`:** Without it, reading `child.size` after layout throws an assertion error.

**Using `Offset.zero` as center:** The center is `(size.width/2, size.height/2)`, not `(0,0)`. Painting at the wrong origin shifts the entire layout.

**Forgetting `offset` in paint:** The `offset` parameter positions this widget within its parent. Ignoring it causes children to render at incorrect screen positions.

### Debugging Tip

Enable `debugPaintSizeEnabled = true` in `main()` to visualize RenderObject boundaries with blue outlines -- invaluable for custom layout debugging.

---

## Exercise 8: JSON-Driven Dynamic Widget System

### Progressive Hints

1. `WidgetNode.fromJson`: validate `type` is a non-empty String, `properties` is a Map or null, `children` is a List of Maps or null. Recurse on children.
2. Color parsing: strip "#", `int.parse(hex, radix: 16)`, add `0xFF000000` for 6-digit hex.
3. Start the factory with `text` builder, then add one type at a time.
4. Actions: button builder reads `onTap` property, wraps `onPressed` to call `onAction` callback.

### Full Solution

```dart
// lib/json_widget/widget_schema.dart
factory WidgetNode.fromJson(Map<String, dynamic> json) {
  final type = json['type'];
  if (type is! String || type.isEmpty) {
    throw FormatException('Requires non-empty "type", got: $type');
  }
  final rawProps = json['properties'];
  final props = rawProps == null ? const <String, dynamic>{} :
    rawProps is Map<String, dynamic> ? rawProps :
    throw FormatException('"properties" must be Map, got: ${rawProps.runtimeType}');
  final rawChildren = json['children'];
  final children = rawChildren == null ? const <WidgetNode>[] :
    rawChildren is List ? rawChildren.map((c) {
      if (c is! Map<String, dynamic>) throw FormatException('Child must be Map');
      return WidgetNode.fromJson(c);
    }).toList() :
    throw FormatException('"children" must be List, got: ${rawChildren.runtimeType}');
  return WidgetNode(type: type, properties: props, children: children);
}
```

```dart
// lib/json_widget/widget_factory.dart (selected builders)
_builders['text'] = (node, children, ctx) {
  final data = node.properties['data'] as String? ?? '';
  return Text(data, style: parseTextStyle(node.properties['style']));
};

_builders['elevatedButton'] = (node, children, ctx) {
  final tapConfig = node.properties['onTap'];
  VoidCallback? onPressed;
  if (tapConfig is Map<String, dynamic> && onAction != null) {
    final action = tapConfig['action'] as String? ?? 'unknown';
    final params = Map<String, dynamic>.from(tapConfig)..remove('action');
    onPressed = () => onAction!(action, params);
  }
  return ElevatedButton(
    onPressed: onPressed,
    child: children.isNotEmpty ? children.first : const Text('Button'),
  );
};
```

### Common Mistakes

**Not handling recursion termination:** If children parsing does not validate each child is a Map, a malformed JSON with `"children": [42]` crashes instead of producing a useful error.

**Missing try-catch in the factory's build method:** If a builder throws, the entire app crashes. Wrap each call and substitute a red error widget to keep the rest of the UI functional.

**No depth limit:** Deeply nested JSON (1000+ levels) causes stack overflow. In production, track depth and reject configs beyond a threshold (e.g., 50 levels).

### Deep Dive: Security

Server-driven UI introduces attack surface: resource exhaustion from huge trees, tracking pixels from image URLs, action handlers exploited for phishing navigation. Always validate depth, node count, and whitelist allowed actions. Consider protobuf instead of JSON for stricter schemas and smaller payloads.

---

## General Debugging Tips

**Flutter DevTools:** Widget Inspector (tree visualization, rebuild counts), Layout Explorer (constraints and sizes), Performance Overlay (frame timing), Memory Tab (leak detection). Access via `flutter run` then press `d`.

**Common errors:**
- "RenderFlex overflowed": Row/Column too small. Use `Flexible`, `Expanded`, or wrap in `SingleChildScrollView`.
- "setState() called after dispose()": async operation completed after navigation. Cancel in `dispose`.
- "No Material widget found": used Material widget without `Scaffold` or `Material` ancestor.
- "Looking up a deactivated widget's ancestor": stored a BuildContext and used it after widget removal. Never store contexts.
