# Section 16 -- Flutter Animations

## Introduction

Animation is the difference between software that feels mechanical and software that feels alive. When a user taps a button and a card slides into place with a subtle bounce, they are not thinking about spring constants -- they are thinking "this feels right." That intuitive reaction is what good animation delivers.

Flutter renders at 60fps (or 120fps on ProMotion displays). That gives you roughly 16 milliseconds per frame to build, lay out, and paint your widget tree. Every animation you add competes for that budget. Animation is not decoration -- it is an engineering decision with real performance consequences.

When should you animate? When motion communicates something: a state change, a spatial relationship, a response to user input. When should you not? When the animation delays the user, distracts from content, or exists purely because you can.

## Prerequisites

You must be comfortable with:

- **Section 09** -- Flutter setup, widget tree, StatelessWidget, StatefulWidget
- **Section 10** -- Layout system, constraints, Flex, Stack
- **Section 11** -- Navigation, routes, Navigator 2.0
- **Section 12** -- setState, lifting state, InheritedWidget
- **Section 13** -- Forms, gesture detection, user input handling
- **Section 14** -- HTTP, JSON serialization, async data loading
- **Section 15** -- Advanced state management (Provider, Riverpod, Bloc)

## Learning Objectives

After completing this section you will be able to:

1. **Apply** implicit animation widgets to create smooth transitions without managing controllers
2. **Select** appropriate animation curves for different interaction contexts
3. **Construct** explicit animations using AnimationController, Tween, and AnimatedBuilder
4. **Manage** animation lifecycle including forward, reverse, repeat, and status listeners
5. **Orchestrate** staggered animations with Interval timing and multiple controllers
6. **Implement** Hero animations and custom page route transitions
7. **Design** physics-based animations using spring, friction, and gravity simulations
8. **Build** animated lists with insert and remove transitions
9. **Optimize** animation performance using RepaintBoundary and targeted rebuilds
10. **Create** custom animated effects using CustomPainter driven by AnimationController

---

## Core Concepts

### 1. Implicit Animations: Let Flutter Do the Math

Implicit animations are the simplest way to add motion. You declare the target state, and Flutter interpolates from the current state to the new one over a duration you specify. No controller to manage, no ticker to dispose.

Why start here? Because most animations in a production app should be implicit. They are harder to break and impossible to forget to dispose.

```dart
// file: lib/implicit_basics.dart
import 'package:flutter/material.dart';

class AnimatedBoxDemo extends StatefulWidget {
  const AnimatedBoxDemo({super.key});
  @override
  State<AnimatedBoxDemo> createState() => _AnimatedBoxDemoState();
}

class _AnimatedBoxDemoState extends State<AnimatedBoxDemo> {
  bool _expanded = false;

  @override
  Widget build(BuildContext context) {
    return GestureDetector(
      onTap: () => setState(() => _expanded = !_expanded),
      child: AnimatedContainer(
        duration: const Duration(milliseconds: 400),
        curve: Curves.easeInOut,
        width: _expanded ? 300 : 150,
        height: _expanded ? 300 : 150,
        decoration: BoxDecoration(
          color: _expanded ? Colors.indigo : Colors.teal,
          borderRadius: BorderRadius.circular(_expanded ? 24 : 8),
        ),
        child: Center(
          child: AnimatedOpacity(
            opacity: _expanded ? 1.0 : 0.5,
            duration: const Duration(milliseconds: 300),
            child: const Text('Tap me', style: TextStyle(color: Colors.white)),
          ),
        ),
      ),
    );
  }
}
```

AnimatedContainer interpolates between any two BoxDecoration, alignment, padding, width, height, or constraint values. AnimatedOpacity handles fade transitions. AnimatedPadding adjusts insets. They all follow the same pattern: set the property, set the duration, Flutter handles the rest.

### 2. AnimatedSwitcher and TweenAnimationBuilder

AnimatedSwitcher replaces one child widget with another using a configurable transition. The key insight: it identifies "different" children by their `key` or `runtimeType`. If neither changes, no animation fires.

```dart
// file: lib/animated_switcher_demo.dart
import 'package:flutter/material.dart';

class CounterSwitcher extends StatefulWidget {
  const CounterSwitcher({super.key});
  @override
  State<CounterSwitcher> createState() => _CounterSwitcherState();
}

class _CounterSwitcherState extends State<CounterSwitcher> {
  int _count = 0;

  @override
  Widget build(BuildContext context) {
    return Column(
      mainAxisAlignment: MainAxisAlignment.center,
      children: [
        AnimatedSwitcher(
          duration: const Duration(milliseconds: 300),
          transitionBuilder: (child, animation) => FadeTransition(
            opacity: animation,
            child: SlideTransition(
              position: Tween<Offset>(begin: const Offset(0, 0.5), end: Offset.zero)
                  .animate(animation),
              child: child,
            ),
          ),
          child: Text('$_count', key: ValueKey<int>(_count),
              style: const TextStyle(fontSize: 48)),
        ),
        ElevatedButton(
          onPressed: () => setState(() => _count++),
          child: const Text('Increment'),
        ),
      ],
    );
  }
}
```

TweenAnimationBuilder fills the gap between implicit widgets and explicit controllers. It animates any value type without a controller, but with more flexibility than built-in Animated* widgets.

### 3. Curves and Custom Curves

A curve maps a linear 0.0-to-1.0 progression to a non-linear one. Curves.linear feels robotic. Curves.easeInOut feels natural. Curves.bounceOut feels playful. Curves.elasticOut feels springy. You can define custom curves by extending Curve:

```dart
// file: lib/custom_curve.dart
import 'dart:math';
import 'package:flutter/animation.dart';

class SineCurve extends Curve {
  final int oscillations;
  const SineCurve({this.oscillations = 3});

  @override
  double transformInternal(double t) {
    return sin(oscillations * 2 * pi * t) * (1 - t);
  }
}
```

The Curve contract requires `transform(0.0) == 0.0` and `transform(1.0) == 1.0`. Values outside 0-1 are allowed mid-animation -- this is how overshoot effects work.

### 4. Explicit Animations: Full Control

When you need to loop, reverse on a condition, synchronize multiple properties, or drive an animation from a gesture, you need AnimationController. It produces values from 0.0 to 1.0 over a duration and requires a TickerProvider (SingleTickerProviderStateMixin or TickerProviderStateMixin for multiple controllers). You must dispose it. Forgetting this is one of the most common Flutter bugs.

```dart
// file: lib/explicit_animation.dart
import 'package:flutter/material.dart';

class RotatingCard extends StatefulWidget {
  const RotatingCard({super.key});
  @override
  State<RotatingCard> createState() => _RotatingCardState();
}

class _RotatingCardState extends State<RotatingCard>
    with SingleTickerProviderStateMixin {
  late final AnimationController _controller;
  late final Animation<double> _rotation;

  @override
  void initState() {
    super.initState();
    _controller = AnimationController(
        vsync: this, duration: const Duration(seconds: 2));
    _rotation = Tween<double>(begin: 0, end: 2 * 3.14159)
        .animate(CurvedAnimation(parent: _controller, curve: Curves.easeInOut));
    _controller.addStatusListener((status) {
      if (status == AnimationStatus.completed) _controller.reverse();
      else if (status == AnimationStatus.dismissed) _controller.forward();
    });
  }

  @override
  void dispose() { _controller.dispose(); super.dispose(); }

  @override
  Widget build(BuildContext context) {
    return GestureDetector(
      onTap: () => _controller.isAnimating
          ? _controller.stop() : _controller.forward(),
      child: AnimatedBuilder(
        animation: _controller,
        builder: (context, child) {
          return Transform.rotate(angle: _rotation.value, child: Container(
            width: 150, height: 200,
            decoration: BoxDecoration(
              color: Colors.blue, borderRadius: BorderRadius.circular(16)),
            child: child,
          ));
        },
        child: const Center(
            child: Text('Card', style: TextStyle(color: Colors.white, fontSize: 24))),
      ),
    );
  }
}
```

The `child` parameter in AnimatedBuilder is critical. The child is built once and reused on every frame. Anything that does not depend on the animation value should be passed as child to avoid unnecessary rebuilds. This is not optional -- it is required practice for smooth 60fps.

### 5. Staggered Animations with Interval

Staggered animations run multiple animations with overlapping timing using a single controller. Interval maps a subset of the controller's 0.0-to-1.0 range to a full 0.0-to-1.0 range for a specific property.

```dart
// file: lib/staggered_animation.dart
// Each item calculates its own Interval based on its index
final startInterval = (index * 0.1).clamp(0.0, 1.0);
final endInterval = (startInterval + 0.4).clamp(0.0, 1.0);

final opacity = Tween<double>(begin: 0, end: 1).animate(
  CurvedAnimation(
    parent: controller,
    curve: Interval(startInterval, endInterval, curve: Curves.easeOut),
  ),
);
```

The controller runs once from 0.0 to 1.0, but each item's animation starts and ends at different points within that range. This creates the cascade effect.

### 6. Hero Animations and Page Transitions

Hero animations create visual continuity between routes. Flutter animates a widget from its position on one page to its position on another, provided both share the same `tag`. For custom flight behavior, use `flightShuttleBuilder`. For custom page transitions, use PageRouteBuilder:

```dart
// file: lib/hero_transition.dart
Navigator.push(context, PageRouteBuilder(
  transitionDuration: const Duration(milliseconds: 500),
  pageBuilder: (_, __, ___) => DetailPage(tag: tag),
  transitionsBuilder: (_, animation, __, child) {
    return FadeTransition(opacity: animation, child: child);
  },
));
```

### 7. Physics-Based Animations

Not all motion should follow a fixed duration. A flung list should decelerate based on velocity. A dragged card should spring back with tension proportional to displacement. `animateWith(Simulation)` drives the controller until the simulation reports done:

```dart
// file: lib/spring_animation.dart
import 'package:flutter/physics.dart';

const spring = SpringDescription(mass: 30, stiffness: 1, damping: 1);
final simulation = SpringSimulation(spring, 0, 1, -unitVelocity);
_controller.animateWith(simulation);
```

SpringDescription takes mass, stiffness, and damping. High stiffness means snappy return. High damping means less oscillation. Low damping with moderate stiffness creates the bouncy feel common in iOS interfaces. FrictionSimulation models deceleration. GravitySimulation models falling objects.

### 8. AnimatedList

AnimatedList animates insertions and removals. It requires a GlobalKey to trigger animations programmatically:

```dart
// file: lib/animated_list_demo.dart
final _listKey = GlobalKey<AnimatedListState>();

void _addItem() {
  _items.add('New task');
  _listKey.currentState?.insertItem(_items.length - 1,
      duration: const Duration(milliseconds: 400));
}

void _removeItem(int index) {
  final removed = _items.removeAt(index);
  _listKey.currentState?.removeItem(index,
      (context, animation) => SizeTransition(
        sizeFactor: animation,
        child: FadeTransition(opacity: animation, child: Card(child: Text(removed))),
      ),
      duration: const Duration(milliseconds: 300));
}
```

### 9. Transform and Perspective

Transform applies matrix transformations at paint time without triggering relayout -- cheaper than changing layout properties. The `setEntry(3, 2, 0.001)` call adds perspective. Without it, rotations look flat:

```dart
// file: lib/transform_demo.dart
Transform(
  alignment: Alignment.center,
  transform: Matrix4.identity()
    ..setEntry(3, 2, 0.001) // perspective
    ..rotateX(_rotateX)
    ..rotateY(_rotateY),
  child: myWidget,
)
```

### 10. Performance: Keeping 60fps

Animation performance problems come from three sources: rebuilding too much per frame, triggering relayout when only repaint is needed, or allocating objects during animation.

- **RepaintBoundary**: Isolates repaints so animating regions do not force sibling repaints
- **AnimatedBuilder child**: Pass static subtrees as `child` -- built once, not every frame
- **Offstage**: Keeps a widget in the tree but removes it from painting and hit testing
- **No allocations in build**: Tweens and decorations created in `build()` create garbage every frame, triggering GC jank

### 11. Rive and Lottie

For animations impractical to build in code -- character animations, complex iconography -- Rive and Lottie bridge design tools to Flutter. Lottie renders After Effects animations exported as JSON. Rive provides a runtime with state machines responding to user input. Use code for UI transitions and micro-interactions. Use assets for illustration-grade animations.

---

## Exercises

### Exercise 1 (Basic): Animated Settings Panel

**Objective:** Build a settings panel where toggling options triggers smooth implicit animations on multiple properties simultaneously.

**Instructions:**

1. Create `lib/animated_settings.dart` with a StatefulWidget called `AnimatedSettingsPanel`
2. Implement three toggle settings (dark mode, notifications, compact layout), each stored as a boolean
3. When dark mode toggles, use AnimatedContainer to transition background color, text color, and border radius over 500ms with Curves.easeInOut
4. When compact layout toggles, use AnimatedPadding to transition padding between EdgeInsets.all(24) and EdgeInsets.all(8) over 300ms
5. Use AnimatedSwitcher on each toggle's label text so "Enabled"/"Disabled" transitions with a fade and slight vertical slide
6. Add AnimatedOpacity to show/hide a notification badge icon based on the notifications toggle

**Verification:**

```dart
// file: bin/exercise1_test.dart
import 'package:flutter/material.dart';
import 'package:your_app/animated_settings.dart';

void main() {
  runApp(const MaterialApp(home: Scaffold(body: AnimatedSettingsPanel())));
  // Toggle dark mode: background animates from white to dark grey over 500ms
  // Toggle compact: padding shrinks smoothly over 300ms
  // Toggle notifications: badge fades in/out over 200ms
  // Label text cross-fades between "Enabled" and "Disabled"
}
```

---

### Exercise 2 (Basic): Curve Explorer

**Objective:** Build an interactive curve comparison tool that visualizes how different curves affect animation feel.

**Instructions:**

1. Create `lib/curve_explorer.dart` with a StatefulWidget called `CurveExplorer`
2. Display at least 8 curves: linear, easeIn, easeOut, easeInOut, bounceOut, elasticOut, decelerate, and a custom SineCurve
3. Each row shows the curve name and a colored box that slides horizontally on tap of a global "Animate" button
4. All boxes animate with the same 1000ms duration but use their respective curves
5. Implement SineCurve by extending Curve -- override transformInternal to produce oscillating motion
6. Add a slider for animation duration (200ms-3000ms) and a reset button that snaps all boxes back

**Verification:**

```dart
// file: bin/exercise2_test.dart
import 'package:flutter/material.dart';
import 'package:your_app/curve_explorer.dart';

void main() {
  runApp(const MaterialApp(home: Scaffold(body: CurveExplorer())));
  // Tap "Animate": all 8 boxes slide right with visibly different motion profiles
  // The bounceOut box bounces at the end
  // The custom SineCurve box oscillates before settling
}
```

---

### Exercise 3 (Intermediate): Staggered Card Loading

**Objective:** Implement a staggered animation that loads a grid of cards with overlapping fade-in and slide-up animations.

**Instructions:**

1. Create `lib/staggered_cards.dart` with a StatefulWidget called `StaggeredCardGrid`
2. Use a single AnimationController with 1500ms duration
3. Display a 2-column grid of 8 cards. Each card's animation starts 100ms after the previous one (using Interval)
4. Each card animates opacity (0 to 1), vertical offset (50px to 0), and scale (0.8 to 1.0) within its interval
5. Add a FloatingActionButton that resets and replays the animation
6. When the stagger completes, each card pulses briefly (scale to 1.02 and back) using a separate animation sequence

**Verification:**

```dart
// file: bin/exercise3_test.dart
import 'package:flutter/material.dart';
import 'package:your_app/staggered_cards.dart';

void main() {
  runApp(const MaterialApp(home: StaggeredCardGrid()));
  // Cards appear in a cascading wave from top-left to bottom-right
  // Each card fades in, slides up, and scales up within its own time window
  // After all cards appear, each pulses briefly
}
```

---

### Exercise 4 (Intermediate): Hero Gallery with Custom Transitions

**Objective:** Build a photo gallery with Hero animations and custom page route transitions, including a custom flight shuttle builder that adds a shadow during flight.

**Instructions:**

1. Create `lib/hero_gallery.dart` with a grid of colored containers with icons as image placeholders
2. Create `lib/hero_detail.dart` with a detail page showing the enlarged item
3. Tapping a grid item navigates to detail using Hero with matching tags
4. Implement a custom PageRouteBuilder combining fade and vertical slide for page content
5. Add a flightShuttleBuilder that wraps the flying widget in Material with animating elevation
6. Implement drag-to-dismiss: dragging down dismisses with Hero return, background becomes transparent during drag

**Verification:**

```dart
// file: bin/exercise4_test.dart
import 'package:flutter/material.dart';
import 'package:your_app/hero_gallery.dart';

void main() {
  runApp(const MaterialApp(home: HeroGallery()));
  // Tap item: flies to detail position with growing shadow
  // Dragging down on detail dismisses with Hero return animation
  // Background transparency follows drag progress
}
```

---

### Exercise 5 (Advanced): Physics-Based Drag Interaction Library

**Objective:** Design a reusable DraggableCard widget that uses spring physics for snap-back, friction physics for fling, and exposes configuration parameters.

**Instructions:**

1. Create `lib/physics_card.dart` with a configurable `PhysicsDraggableCard`
2. Accept parameters: `springMass`, `springStiffness`, `springDamping`, `flingFriction`, and `snapPoints` (list of Alignment positions)
3. On release, determine if velocity constitutes a "fling." If so, use FrictionSimulation to predict final position, then spring to nearest snap point. Otherwise spring directly to nearest snap
4. Add tilt rotation proportional to horizontal velocity during drag
5. Scale up slightly (1.05) when grabbed, spring back on release
6. Expose callbacks: `onSnap(Alignment)`, `onDragStart()`, `onDragEnd()`
7. Shadow responds to displacement from rest position

**Verification:**

```dart
// file: bin/exercise5_test.dart
import 'package:flutter/material.dart';
import 'package:your_app/physics_card.dart';

void main() {
  runApp(MaterialApp(home: Scaffold(body: PhysicsDraggableCard(
    springStiffness: 300, springDamping: 15, springMass: 1, flingFriction: 0.5,
    snapPoints: [Alignment.center, Alignment.centerLeft, Alignment.centerRight],
    onSnap: (point) => debugPrint('Snapped to $point'),
    child: Container(width: 200, height: 280, color: Colors.white,
        child: const Center(child: Text('Drag me'))),
  ))));
  // Drag: follows finger with tilt and scale-up
  // Gentle release: springs to nearest snap point
  // Fling: slides with friction then springs to predicted snap
}
```

---

### Exercise 6 (Advanced): Animated Component Library

**Objective:** Build a library of reusable animated components with a consistent spring-based animation system.

**Instructions:**

1. Create `lib/anim_components/spring_config.dart` -- SpringConfig class with presets (gentle, snappy, bouncy) and factory constructors
2. Create `lib/anim_components/animated_button.dart` -- scales to 0.95 on press, springs back on release
3. Create `lib/anim_components/animated_card_flip.dart` -- 3D rotation flip between front/back using spring physics
4. Create `lib/anim_components/animated_progress.dart` -- circular progress that spring-animates between values with color tween
5. Create `lib/anim_components/animated_number.dart` -- animates between numeric values with formatted display during transition
6. All components accept SpringConfig for consistent feel

**Verification:**

```dart
// file: bin/exercise6_test.dart
import 'package:flutter/material.dart';
import 'package:your_app/anim_components/spring_config.dart';
import 'package:your_app/anim_components/animated_button.dart';
import 'package:your_app/anim_components/animated_card_flip.dart';

void main() {
  final config = SpringConfig.bouncy();
  runApp(MaterialApp(home: Scaffold(body: Column(
    mainAxisAlignment: MainAxisAlignment.spaceEvenly,
    children: [
      SpringAnimatedButton(config: config, onPressed: () {}, child: const Text('Press')),
      AnimatedCardFlip(config: config,
          front: const Card(child: Center(child: Text('Front'))),
          back: const Card(child: Center(child: Text('Back')))),
    ],
  ))));
}
```

---

### Exercise 7 (Insane): Particle System with Touch Interaction

**Objective:** Create a particle system using CustomPainter driven by AnimationController that spawns particles at touch points, applies physics forces, handles particle lifecycle, and maintains 60fps with hundreds of particles.

**Instructions:**

1. Create `lib/particle_system/particle.dart` -- Particle class with position, velocity, color, size, lifetime, age, and alive flag
2. Create `lib/particle_system/emitter.dart` -- spawns particles with configurable rate, spread angle, velocity range, color range, size range
3. Create `lib/particle_system/physics_engine.dart` -- applies gravity, wind, friction, and bounding box collisions per frame
4. Create `lib/particle_system/particle_painter.dart` -- CustomPainter rendering particles as circles with opacity from age/lifetime ratio
5. Create `lib/particle_system/particle_canvas.dart` -- AnimationController in repeat mode drives simulation; touch creates emitters, move updates position, up stops emitter
6. Support multiple simultaneous touch points using Listener (not GestureDetector)
7. Implement particle pooling: pre-allocate fixed pool (500), recycle dead particles to avoid GC pressure
8. Debug overlay showing active count, frame time, emitter count

**Verification:**

```dart
// file: bin/exercise7_test.dart
import 'package:flutter/material.dart';
import 'package:your_app/particle_system/particle_canvas.dart';

void main() {
  runApp(const MaterialApp(home: Scaffold(body: ParticleCanvas(
    maxParticles: 500, gravity: Offset(0, 98),
    wind: Offset(20, 0), showDebugOverlay: true,
  ))));
  // Touch and hold: particles spray from touch point
  // Multiple fingers: independent emitters
  // Debug overlay shows particle count near 500 at peak, 60fps maintained
}
```

---

### Exercise 8 (Insane): Gesture-Driven Animated Navigation System

**Objective:** Implement a fully custom navigation system with shared element transitions, momentum-based physics, interrupt handling, and gesture-driven page transitions.

**Instructions:**

1. Create `lib/nav_system/animated_nav_controller.dart` -- route stack with animation state, push/pop/swipe-to-go-back
2. Create `lib/nav_system/shared_element_registry.dart` -- tracks widgets by key across pages, calculates global positions, provides source/target rects
3. Create `lib/nav_system/page_transition_overlay.dart` -- Overlay rendering shared elements during transition with spring-animated position/size
4. Create `lib/nav_system/gesture_handler.dart` -- horizontal swipe for back navigation with parallax (previous page at 0.3x rate), physics-based completion/cancellation
5. Implement interrupt handling: new gesture during animation captures current values as starting point, no visual jumps
6. Create `lib/nav_system/animated_nav_page.dart` -- page wrapper registering shared elements with lifecycle callbacks
7. Demo app with three pages: list, detail, fullscreen viewer -- shared elements transition between all

**Verification:**

```dart
// file: bin/exercise8_test.dart
import 'package:flutter/material.dart';
import 'package:your_app/nav_system/animated_nav_controller.dart';
import 'package:your_app/nav_system/animated_nav_page.dart';

void main() {
  runApp(MaterialApp(home: AnimatedNavHost(
    initialPage: const ListPage(),
    sharedElementRegistry: SharedElementRegistry(),
  )));
  // Tap list item: shared elements fly to detail positions
  // Swipe right: page follows finger, previous page peeks with parallax
  // Fling: momentum carries page away with physics
  // Mid-swipe direction reversal: interrupts smoothly, no jumps
}
```

---

## Summary

Animation in Flutter exists on a spectrum of control versus convenience. Implicit animations handle most cases with minimal code. Explicit animations give frame-by-frame control for synchronization, looping, or gesture-driven motion. Physics simulations produce natural-feeling motion that responds to user velocity.

The performance contract is non-negotiable: 16ms per frame. Every optimization -- RepaintBoundary, the AnimatedBuilder child parameter, particle pooling -- exists to honor that contract. When you break it, users feel it as jank.

The pattern to internalize: start implicit. Move to explicit when implicit cannot express what you need. Reach for physics when fixed durations feel artificial. Use CustomPainter when you need to render things the widget tree was not designed for. And always dispose your controllers.

## What's Next

**Section 17: Flutter Testing** covers unit tests, widget tests, integration tests, golden tests, and mocking. You will learn how to test animations -- verifying controllers are disposed, widgets reach target states, and transitions complete without errors. Testing animated code requires specific patterns (pumping frames, controlling the ticker) that Section 17 addresses directly.

## References

- [Flutter Animation Documentation](https://docs.flutter.dev/ui/animations)
- [Implicit Animations](https://docs.flutter.dev/ui/animations/implicit-animations)
- [Hero Animations](https://docs.flutter.dev/ui/animations/hero-animations)
- [Staggered Animations](https://docs.flutter.dev/ui/animations/staggered-animations)
- [AnimationController API](https://api.flutter.dev/flutter/animation/AnimationController-class.html)
- [Curves Class Reference](https://api.flutter.dev/flutter/animation/Curves-class.html)
- [Physics-based Animations](https://docs.flutter.dev/cookbook/animation/physics-simulation)
- [CustomPainter API](https://api.flutter.dev/flutter/rendering/CustomPainter-class.html)
- [Flutter Performance Best Practices](https://docs.flutter.dev/perf/best-practices)
- [Lottie for Flutter](https://pub.dev/packages/lottie)
- [Rive for Flutter](https://pub.dev/packages/rive)
