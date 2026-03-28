# Section 16 -- Solutions: Flutter Animations

## How to Use This File

Do not read solutions before attempting. Attempt from scratch first. If stuck 15+ minutes, read hints one at a time. If still stuck, check common mistakes. Only then read the full solution. After reading, close this file and reimplement from memory.

---

## Exercise 1: Animated Settings Panel

### Progressive Hints

**Hint 1:** Each toggle is a boolean in State. Implicit widgets react to changes. No controllers needed.

**Hint 2:** AnimatedSwitcher needs a ValueKey to distinguish children of the same type. Without it, no animation fires.

**Hint 3:** Combine SlideTransition + FadeTransition in transitionBuilder. Offset(0, 0.3) = 30% of child height.

### Common Mistakes

**AnimatedSwitcher does not animate.** Add `key: ValueKey<bool>(value)` to each child.

**Colors jump.** Use AnimatedContainer, not regular Container. Only Animated* variants interpolate.

**Padding fights container.** Do not nest AnimatedPadding inside AnimatedContainer if both affect the same dimensions.

### Full Solution

```dart
// file: lib/animated_settings.dart
import 'package:flutter/material.dart';

class AnimatedSettingsPanel extends StatefulWidget {
  const AnimatedSettingsPanel({super.key});
  @override
  State<AnimatedSettingsPanel> createState() => _AnimatedSettingsPanelState();
}

class _AnimatedSettingsPanelState extends State<AnimatedSettingsPanel> {
  bool _darkMode = false, _notifications = false, _compact = false;

  @override
  Widget build(BuildContext context) {
    final bg = _darkMode ? const Color(0xFF1E1E1E) : Colors.white;
    final fg = _darkMode ? Colors.white : Colors.black87;

    return AnimatedContainer(
      duration: const Duration(milliseconds: 500),
      curve: Curves.easeInOut,
      decoration: BoxDecoration(
          color: bg, borderRadius: BorderRadius.circular(_darkMode ? 24 : 12)),
      child: AnimatedPadding(
        duration: const Duration(milliseconds: 300),
        padding: EdgeInsets.all(_compact ? 8 : 24),
        child: Column(mainAxisSize: MainAxisSize.min, children: [
          _row('Dark Mode', _darkMode, fg, (v) => setState(() => _darkMode = v)),
          const SizedBox(height: 12),
          _row('Notifications', _notifications, fg,
              (v) => setState(() => _notifications = v),
              trailing: AnimatedOpacity(
                opacity: _notifications ? 1.0 : 0.0,
                duration: const Duration(milliseconds: 200),
                child: const Icon(Icons.notifications_active, color: Colors.amber, size: 20),
              )),
          const SizedBox(height: 12),
          _row('Compact', _compact, fg, (v) => setState(() => _compact = v)),
        ]),
      ),
    );
  }

  Widget _row(String label, bool value, Color fg, ValueChanged<bool> onChanged,
      {Widget? trailing}) {
    return Row(mainAxisAlignment: MainAxisAlignment.spaceBetween, children: [
      Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
        Text(label, style: TextStyle(color: fg, fontSize: 16)),
        AnimatedSwitcher(
          duration: const Duration(milliseconds: 300),
          transitionBuilder: (child, anim) => FadeTransition(opacity: anim,
            child: SlideTransition(
              position: Tween(begin: const Offset(0, 0.3), end: Offset.zero).animate(anim),
              child: child)),
          child: Text(value ? 'Enabled' : 'Disabled', key: ValueKey(value),
              style: TextStyle(color: fg.withOpacity(0.6), fontSize: 12))),
      ]),
      Row(children: [if (trailing != null) trailing, Switch(value: value, onChanged: onChanged)]),
    ]);
  }
}
```

---

## Exercise 2: Curve Explorer

### Progressive Hints

**Hint 1:** Store "animated" as a boolean. Each AnimatedContainer reads it for margin. The curve differentiates them.

**Hint 2:** For SineCurve, `transformInternal(1.0)` must return 1.0. Blend: `sin(oscillations * 2 * pi * t) * 0.15 * (1 - t) + t`.

### Common Mistakes

**Custom curve ends at 0.** Blend with `+ t` to ensure convergence to 1.0.

**Duration slider has no effect.** Duration applies on the next property change, not retroactively.

### Full Solution

```dart
// file: lib/curve_explorer.dart
import 'dart:math';
import 'package:flutter/material.dart';

class SineCurve extends Curve {
  final int oscillations;
  const SineCurve({this.oscillations = 3});

  @override
  double transformInternal(double t) =>
      t + sin(oscillations * 2 * pi * t) * 0.15 * (1 - t);
}

class CurveExplorer extends StatefulWidget {
  const CurveExplorer({super.key});
  @override
  State<CurveExplorer> createState() => _CurveExplorerState();
}

class _CurveExplorerState extends State<CurveExplorer> {
  bool _moved = false;
  double _durationMs = 1000;

  static final _curves = <String, Curve>{
    'linear': Curves.linear, 'easeIn': Curves.easeIn,
    'easeOut': Curves.easeOut, 'easeInOut': Curves.easeInOut,
    'bounceOut': Curves.bounceOut, 'elasticOut': Curves.elasticOut,
    'decelerate': Curves.decelerate, 'sineCurve': const SineCurve(),
  };

  @override
  Widget build(BuildContext context) {
    final dur = Duration(milliseconds: _durationMs.round());
    return Padding(padding: const EdgeInsets.all(16), child: Column(children: [
      Row(children: [
        ElevatedButton(onPressed: () => setState(() => _moved = !_moved),
            child: const Text('Animate')),
        const SizedBox(width: 12),
        OutlinedButton(onPressed: () => setState(() => _moved = false),
            child: const Text('Reset')),
      ]),
      Row(children: [
        const Text('Duration: '),
        Expanded(child: Slider(min: 200, max: 3000, value: _durationMs,
            divisions: 28, onChanged: (v) => setState(() => _durationMs = v))),
        Text('${_durationMs.round()}ms'),
      ]),
      Expanded(child: ListView(
        children: _curves.entries.map((e) => Padding(
          padding: const EdgeInsets.symmetric(vertical: 6),
          child: Row(children: [
            SizedBox(width: 100, child: Text(e.key, style: const TextStyle(fontSize: 12))),
            Expanded(child: AnimatedContainer(
              duration: dur, curve: e.value,
              alignment: _moved ? Alignment.centerRight : Alignment.centerLeft,
              child: Container(width: 40, height: 40,
                  decoration: BoxDecoration(color: Colors.blue.shade600,
                      borderRadius: BorderRadius.circular(8))),
            )),
          ]),
        )).toList(),
      )),
    ]));
  }
}
```

---

## Exercise 3: Staggered Card Loading

### Progressive Hints

**Hint 1:** One controller, many Intervals. Start = `index * (100ms / 1500ms)`. End = start + 0.4.

**Hint 2:** Pulse on completion: status listener starts a TweenSequence (1.0 -> 1.02 -> 1.0).

### Common Mistakes

**All cards animate at once.** Intervals are identical -- verify offset per index. **Cards flicker on replay.** Use `controller.forward(from: 0.0)`. **Pulse leak.** Second controller must be disposed.

### Full Solution

```dart
// file: lib/staggered_cards.dart
import 'package:flutter/material.dart';

class StaggeredCardGrid extends StatefulWidget {
  const StaggeredCardGrid({super.key});
  @override
  State<StaggeredCardGrid> createState() => _StaggeredCardGridState();
}

class _StaggeredCardGridState extends State<StaggeredCardGrid>
    with TickerProviderStateMixin {
  late final AnimationController _stagger;
  late final AnimationController _pulse;

  @override
  void initState() {
    super.initState();
    _stagger = AnimationController(vsync: this,
        duration: const Duration(milliseconds: 1500));
    _pulse = AnimationController(vsync: this,
        duration: const Duration(milliseconds: 300));
    _stagger.addStatusListener((s) {
      if (s == AnimationStatus.completed) _pulse.forward(from: 0);
    });
    _stagger.forward();
  }

  @override
  void dispose() { _stagger.dispose(); _pulse.dispose(); super.dispose(); }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: Padding(padding: const EdgeInsets.all(16),
        child: GridView.builder(
          gridDelegate: const SliverGridDelegateWithFixedCrossAxisCount(
              crossAxisCount: 2, mainAxisSpacing: 12, crossAxisSpacing: 12),
          itemCount: 8,
          itemBuilder: (_, i) => _Card(index: i, stagger: _stagger, pulse: _pulse),
        )),
      floatingActionButton: FloatingActionButton(
        onPressed: () { _pulse.reset(); _stagger.forward(from: 0); },
        child: const Icon(Icons.replay)),
    );
  }
}

class _Card extends StatelessWidget {
  final int index;
  final AnimationController stagger, pulse;
  const _Card({required this.index, required this.stagger, required this.pulse});

  @override
  Widget build(BuildContext context) {
    final start = (index * 0.067).clamp(0.0, 0.6);
    final end = (start + 0.4).clamp(0.0, 1.0);
    final interval = Interval(start, end, curve: Curves.easeOut);

    final opacity = Tween(begin: 0.0, end: 1.0)
        .animate(CurvedAnimation(parent: stagger, curve: interval));
    final slide = Tween(begin: 50.0, end: 0.0)
        .animate(CurvedAnimation(parent: stagger, curve: interval));
    final scale = Tween(begin: 0.8, end: 1.0)
        .animate(CurvedAnimation(parent: stagger, curve: interval));
    final pulseScale = TweenSequence<double>([
      TweenSequenceItem(tween: Tween(begin: 1.0, end: 1.02), weight: 50),
      TweenSequenceItem(tween: Tween(begin: 1.02, end: 1.0), weight: 50),
    ]).animate(CurvedAnimation(parent: pulse, curve: Curves.easeInOut));

    return AnimatedBuilder(
      animation: Listenable.merge([stagger, pulse]),
      builder: (_, child) => Transform.translate(
        offset: Offset(0, slide.value),
        child: Transform.scale(scale: scale.value * pulseScale.value,
          child: Opacity(opacity: opacity.value, child: child))),
      child: Card(elevation: 4,
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
        child: Center(child: Text('Card $index'))),
    );
  }
}
```

### Deep Dive

Interval is a Curve mapping a sub-range of 0.0-1.0 to a full 0.0-1.0 output, clamping outside. TweenSequence chains multiple tweens by weight, powering the pulse without a separate controller.

---

## Exercise 4: Hero Gallery with Custom Transitions

### Progressive Hints

**Hint 1:** Hero tags must be identical on both pages. Use the item's unique ID, not its index.

**Hint 2:** flightShuttleBuilder receives the animation and both hero contexts. Return an AnimatedBuilder that animates Material elevation during flight.

**Hint 3:** For drag-to-dismiss, track vertical drag offset and use it to translate the page down while reducing background opacity. Pop on threshold.

### Common Mistakes

**Hero does not animate.** Tags do not match. Print both to verify. Also check both Heroes are visible when transition starts.

**Drag dismiss completes but Hero does not animate back.** The Hero widget must still be mounted when Navigator.pop is called.

### Full Solution

```dart
// file: lib/hero_gallery.dart
import 'package:flutter/material.dart';

class HeroGallery extends StatelessWidget {
  const HeroGallery({super.key});
  static const _colors = [Colors.red, Colors.blue, Colors.green, Colors.orange,
    Colors.purple, Colors.teal, Colors.pink, Colors.indigo];

  @override
  Widget build(BuildContext context) {
    return Scaffold(appBar: AppBar(title: const Text('Gallery')),
      body: GridView.builder(
        padding: const EdgeInsets.all(12),
        gridDelegate: const SliverGridDelegateWithFixedCrossAxisCount(
            crossAxisCount: 3, mainAxisSpacing: 12, crossAxisSpacing: 12),
        itemCount: _colors.length,
        itemBuilder: (ctx, i) {
          final tag = 'gallery-$i';
          return GestureDetector(
            onTap: () => Navigator.push(ctx, PageRouteBuilder(
              transitionDuration: const Duration(milliseconds: 500),
              pageBuilder: (_, __, ___) => _Detail(tag: tag, color: _colors[i], index: i),
              transitionsBuilder: (_, anim, __, child) => FadeTransition(
                opacity: CurvedAnimation(parent: anim, curve: const Interval(0.3, 1.0)),
                child: child),
            )),
            child: Hero(tag: tag,
              flightShuttleBuilder: (_, anim, dir, from, to) => AnimatedBuilder(
                animation: anim,
                builder: (_, child) => Material(
                  elevation: dir == HeroFlightDirection.push
                      ? 12 * anim.value : 12 * (1 - anim.value),
                  borderRadius: BorderRadius.circular(12),
                  clipBehavior: Clip.antiAlias, child: child),
                child: Container(color: _colors[i],
                    child: const Center(child: Icon(Icons.image, color: Colors.white70))),
              ),
              child: Container(
                decoration: BoxDecoration(color: _colors[i],
                    borderRadius: BorderRadius.circular(12)),
                child: const Center(child: Icon(Icons.image, color: Colors.white70))),
            ),
          );
        },
      ));
  }
}

class _Detail extends StatefulWidget {
  final String tag; final Color color; final int index;
  const _Detail({required this.tag, required this.color, required this.index});
  @override
  State<_Detail> createState() => _DetailState();
}

class _DetailState extends State<_Detail> {
  double _dragOffset = 0;

  @override
  Widget build(BuildContext context) {
    final progress = (_dragOffset / 300).clamp(0.0, 1.0);
    return GestureDetector(
      onVerticalDragUpdate: (d) =>
          setState(() => _dragOffset = (_dragOffset + d.delta.dy).clamp(0, 300)),
      onVerticalDragEnd: (d) {
        if (_dragOffset > 100 || d.velocity.pixelsPerSecond.dy > 500)
          Navigator.pop(context);
        else setState(() => _dragOffset = 0);
      },
      child: Scaffold(
        backgroundColor: Colors.black.withOpacity(1 - progress * 0.5),
        body: Transform.translate(offset: Offset(0, _dragOffset),
          child: Transform.scale(scale: 1 - progress * 0.1,
            child: Center(child: Hero(tag: widget.tag,
              child: Container(width: 300, height: 400,
                decoration: BoxDecoration(color: widget.color,
                    borderRadius: BorderRadius.circular(12)),
                child: Center(child: Text('Item ${widget.index}',
                    style: const TextStyle(color: Colors.white, fontSize: 24))))))))),
    );
  }
}
```

---

## Exercise 5: Physics-Based Drag Interaction Library

### Progressive Hints

**Hint 1:** Use AnimationController without fixed duration -- drive with `animateWith(simulation)`.

**Hint 2:** For snap after fling, predict final position: current position + velocity * friction factor. Find nearest snap to that prediction.

**Hint 3:** Smooth rotation with exponential moving average: `_smoothVel = _smoothVel * 0.7 + delta.dx * 0.3`.

### Common Mistakes

**Card teleports on release.** Spring starts from position 0 instead of current drag position. Begin from current Alignment.

**Spring vibrates endlessly.** Damping too low. For UI: 10-30 gives snappy feel without visible oscillation.

**"setState after dispose" error.** Controller listener fires after widget removal. Dispose cancels the controller.

### Full Solution

```dart
// file: lib/physics_card.dart
import 'package:flutter/material.dart';
import 'package:flutter/physics.dart';

class PhysicsDraggableCard extends StatefulWidget {
  final double springStiffness, springDamping, springMass, flingFriction;
  final List<Alignment> snapPoints;
  final ValueChanged<Alignment>? onSnap;
  final VoidCallback? onDragStart, onDragEnd;
  final Widget child;

  const PhysicsDraggableCard({super.key, this.springStiffness = 300,
    this.springDamping = 15, this.springMass = 1, this.flingFriction = 0.5,
    this.snapPoints = const [Alignment.center], this.onSnap,
    this.onDragStart, this.onDragEnd, required this.child});

  @override
  State<PhysicsDraggableCard> createState() => _State();
}

class _State extends State<PhysicsDraggableCard> with TickerProviderStateMixin {
  late final AnimationController _spring, _scaleCtrl;
  late Animation<Alignment> _springAnim;
  late final Animation<double> _scaleAnim;
  Alignment _current = Alignment.center;
  late Alignment _target;
  double _smoothVelX = 0;

  @override
  void initState() {
    super.initState();
    _target = widget.snapPoints.first;
    _current = _target;
    _spring = AnimationController(vsync: this)
      ..addListener(() => setState(() => _current = _springAnim.value));
    _scaleCtrl = AnimationController(vsync: this,
        duration: const Duration(milliseconds: 150));
    _scaleAnim = Tween(begin: 1.0, end: 1.05)
        .animate(CurvedAnimation(parent: _scaleCtrl, curve: Curves.easeOut));
  }

  @override
  void dispose() { _spring.dispose(); _scaleCtrl.dispose(); super.dispose(); }

  Alignment _nearestSnap(Alignment pos) {
    Alignment nearest = widget.snapPoints.first;
    double minDist = double.infinity;
    for (final s in widget.snapPoints) {
      final d = (pos.x - s.x) * (pos.x - s.x) + (pos.y - s.y) * (pos.y - s.y);
      if (d < minDist) { minDist = d; nearest = s; }
    }
    return nearest;
  }

  void _snapTo(Alignment target, {double velocity = 0}) {
    _target = target;
    _springAnim = _spring.drive(AlignmentTween(begin: _current, end: target));
    final sim = SpringSimulation(SpringDescription(mass: widget.springMass,
        stiffness: widget.springStiffness, damping: widget.springDamping),
        0, 1, -velocity);
    _spring.animateWith(sim);
    widget.onSnap?.call(target);
  }

  @override
  Widget build(BuildContext context) {
    final size = MediaQuery.of(context).size;
    final disp = (_current - _target);
    final dist = (disp.x.abs() + disp.y.abs()).clamp(0.0, 2.0);

    return GestureDetector(
      onPanStart: (_) { _spring.stop(); _scaleCtrl.forward(); widget.onDragStart?.call(); },
      onPanUpdate: (d) => setState(() {
        _current += Alignment(d.delta.dx / (size.width / 2), d.delta.dy / (size.height / 2));
        _smoothVelX = _smoothVelX * 0.7 + d.delta.dx * 0.3;
      }),
      onPanEnd: (d) {
        _scaleCtrl.reverse(); _smoothVelX = 0; widget.onDragEnd?.call();
        final vel = d.velocity.pixelsPerSecond;
        Alignment snap;
        if (vel.distance > 500) {
          final px = _current.x + vel.dx / (size.width / 2) * widget.flingFriction;
          final py = _current.y + vel.dy / (size.height / 2) * widget.flingFriction;
          snap = _nearestSnap(Alignment(px, py));
        } else { snap = _nearestSnap(_current); }
        _snapTo(snap, velocity: vel.distance / size.width);
      },
      child: Align(alignment: _current,
        child: AnimatedBuilder(animation: _scaleAnim, builder: (_, child) =>
          Transform.scale(scale: _scaleAnim.value,
            child: Transform.rotate(angle: _smoothVelX * 0.003,
              child: Container(
                decoration: BoxDecoration(borderRadius: BorderRadius.circular(16),
                  boxShadow: [BoxShadow(color: Colors.black26,
                      blurRadius: 8 + dist * 8, offset: Offset(0, 4 + dist * 4))]),
                child: child))),
          child: widget.child)),
    );
  }
}
```

### Deep Dive

`animateWith(Simulation)` runs until `isDone(time)` returns true. SpringDescription parameters: mass 1, stiffness 100-500, damping 10-30 covers most UI needs. Lower stiffness + lower damping = lazy bounce. Higher stiffness + higher damping = snappy precision.

---

## Exercise 6: Animated Component Library

### Progressive Hints

**Hint 1:** SpringConfig wraps SpringDescription. Every component converts it to a simulation via `animateWith`.

**Hint 2:** Card flip: when rotation passes pi/2, the front becomes invisible. Switch to back face and counter-rotate by pi so text reads correctly.

**Hint 3:** AnimatedNumber uses a Tween between old and new values, formatting intermediate doubles during transition.

### Common Mistakes

**Card flip shows mirrored text on back.** Apply `Matrix4.identity()..rotateY(pi)` to the back face to un-mirror it.

**Spring never settles.** Damping too low. For card flip: stiffness 300, damping 20.

### Full Solution

```dart
// file: lib/anim_components/spring_config.dart
import 'package:flutter/physics.dart';

class SpringConfig {
  final double mass, stiffness, damping;
  const SpringConfig({required this.mass, required this.stiffness, required this.damping});
  factory SpringConfig.gentle() => const SpringConfig(mass: 1, stiffness: 120, damping: 14);
  factory SpringConfig.snappy() => const SpringConfig(mass: 1, stiffness: 500, damping: 25);
  factory SpringConfig.bouncy() => const SpringConfig(mass: 1, stiffness: 250, damping: 10);
  SpringDescription toDescription() =>
      SpringDescription(mass: mass, stiffness: stiffness, damping: damping);
}
```

```dart
// file: lib/anim_components/animated_card_flip.dart
import 'dart:math';
import 'package:flutter/material.dart';
import 'package:flutter/physics.dart';
import 'spring_config.dart';

class AnimatedCardFlip extends StatefulWidget {
  final SpringConfig config;
  final Widget front, back;
  const AnimatedCardFlip({super.key, required this.config,
      required this.front, required this.back});
  @override
  State<AnimatedCardFlip> createState() => _FlipState();
}

class _FlipState extends State<AnimatedCardFlip>
    with SingleTickerProviderStateMixin {
  late final AnimationController _ctrl;
  bool _showFront = true;

  @override
  void initState() {
    super.initState();
    _ctrl = AnimationController(vsync: this)
      ..addListener(() {
        final front = _ctrl.value < 0.5;
        if (front != _showFront) setState(() => _showFront = front);
      });
  }

  @override
  void dispose() { _ctrl.dispose(); super.dispose(); }

  void _flip() {
    final target = _showFront ? 1.0 : 0.0;
    _ctrl.animateWith(SpringSimulation(
        widget.config.toDescription(), _ctrl.value, target, 0));
  }

  @override
  Widget build(BuildContext context) {
    return GestureDetector(onTap: _flip,
      child: AnimatedBuilder(animation: _ctrl, builder: (_, __) {
        final angle = _ctrl.value * pi;
        final front = _ctrl.value < 0.5;
        return Transform(alignment: Alignment.center,
          transform: Matrix4.identity()..setEntry(3, 2, 0.001)..rotateY(angle),
          child: front ? widget.front
              : Transform(alignment: Alignment.center,
                  transform: Matrix4.identity()..rotateY(pi),
                  child: widget.back));
      }));
  }
}
```

```dart
// file: lib/anim_components/animated_number.dart
import 'package:flutter/material.dart';
import 'package:flutter/physics.dart';
import 'spring_config.dart';

class AnimatedNumber extends StatefulWidget {
  final SpringConfig config;
  final double value;
  final String prefix;
  final int decimalPlaces;
  const AnimatedNumber({super.key, required this.config, required this.value,
      this.prefix = '', this.decimalPlaces = 2});
  @override
  State<AnimatedNumber> createState() => _NumState();
}

class _NumState extends State<AnimatedNumber>
    with SingleTickerProviderStateMixin {
  late final AnimationController _ctrl;
  double _display = 0, _prev = 0;

  @override
  void initState() {
    super.initState();
    _display = widget.value; _prev = widget.value;
    _ctrl = AnimationController(vsync: this)
      ..addListener(() => setState(() =>
          _display = _prev + _ctrl.value * (widget.value - _prev)));
  }

  @override
  void didUpdateWidget(AnimatedNumber old) {
    super.didUpdateWidget(old);
    if (old.value != widget.value) {
      _prev = _display;
      _ctrl.animateWith(SpringSimulation(
          widget.config.toDescription(), 0, 1, 0));
    }
  }

  @override
  void dispose() { _ctrl.dispose(); super.dispose(); }

  @override
  Widget build(BuildContext context) => Text(
      '${widget.prefix}${_display.toStringAsFixed(widget.decimalPlaces)}',
      style: const TextStyle(fontSize: 32));
}
```

---

## Exercise 7: Particle System

### Progressive Hints

**Hint 1:** Fixed-size pool with `alive` flag. Emitter scans for dead particles and reinitializes -- zero allocation.

**Hint 2:** Physics per frame: gravity/wind -> velocity, velocity * friction, velocity -> position, age += dt. Kill when age >= lifetime.

**Hint 3:** Pass controller as CustomPainter repaint listenable -- repaints without setState.

**Hint 4:** Use Listener (not GestureDetector) for multi-touch. Track emitters by pointer ID in a Map.

### Common Mistakes

**FPS drops.** Use repaint listenable, not setState. Reuse one Paint object in the paint loop. **Teleporting particles.** Clamp dt: `dt.clamp(0.001, 0.05)`. **Pool grows.** Pool is fixed-size, allocated once -- recycle, do not add.

### Full Solution

The particle system spans multiple files. The critical architectural pieces are the pooled Particle, the Emitter, and the canvas that ties everything together. Below shows the canvas (the hardest part) -- the Particle and Emitter are straightforward data classes.

```dart
// file: lib/particle_system/particle.dart -- Mutable data class with reset() method
// Fields: x, y, vx, vy, size, lifetime, age, color, alive (bool)
// reset() reinitializes all fields -- this IS the pooling mechanism

// file: lib/particle_system/emitter.dart -- Accumulator-based spawner
// emit(dt, pool): accumulates dt * rate; for each unit, scans pool for dead
// particle, calls p.reset() with random angle/speed/color within configured ranges
```

```dart
// file: lib/particle_system/particle_canvas.dart
import 'package:flutter/material.dart';

class ParticleCanvas extends StatefulWidget {
  final int maxParticles;
  final Offset gravity, wind;
  final bool showDebugOverlay;
  const ParticleCanvas({super.key, this.maxParticles = 500,
    this.gravity = const Offset(0, 98), this.wind = Offset.zero,
    this.showDebugOverlay = false});
  @override
  State<ParticleCanvas> createState() => _CanvasState();
}

class _CanvasState extends State<ParticleCanvas>
    with SingleTickerProviderStateMixin {
  late final AnimationController _ctrl;
  late final List<Particle> _pool; // Fixed-size, allocated once
  final Map<int, Emitter> _emitters = {}; // Keyed by pointer ID
  double _lastTime = 0;
  int _active = 0;

  @override
  void initState() {
    super.initState();
    _pool = List.generate(widget.maxParticles, (_) => Particle());
    _ctrl = AnimationController(vsync: this,
        duration: const Duration(seconds: 1))..repeat()..addListener(_tick);
  }

  void _tick() {
    final now = _ctrl.lastElapsedDuration?.inMicroseconds ?? 0;
    final dt = (_lastTime == 0 ? 0.016 : (now - _lastTime) / 1e6)
        .clamp(0.001, 0.05); // Clamp prevents physics explosion on resume
    _lastTime = now.toDouble();

    for (final e in _emitters.values) e.emit(dt, _pool);
    for (final p in _pool) {
      if (!p.alive) continue;
      p.vx += (widget.gravity.dx + widget.wind.dx) * dt;
      p.vy += (widget.gravity.dy + widget.wind.dy) * dt;
      p.vx *= 0.99; p.vy *= 0.99;
      p.x += p.vx * dt; p.y += p.vy * dt;
      p.age += dt;
      if (p.age >= p.lifetime) p.alive = false;
    }
    _active = _pool.where((p) => p.alive).length;
  }

  @override
  void dispose() { _ctrl.dispose(); super.dispose(); }

  @override
  Widget build(BuildContext context) {
    return LayoutBuilder(builder: (_, box) => Stack(children: [
      Listener( // Listener, not GestureDetector -- supports multiple pointers
        onPointerDown: (e) => _emitters[e.pointer] =
            Emitter(x: e.localPosition.dx, y: e.localPosition.dy),
        onPointerMove: (e) { _emitters[e.pointer]?.x = e.localPosition.dx;
            _emitters[e.pointer]?.y = e.localPosition.dy; },
        onPointerUp: (e) { _emitters[e.pointer]?.active = false;
            _emitters.remove(e.pointer); },
        child: CustomPaint(
          painter: _Painter(particles: _pool, repaint: _ctrl), // repaint via listenable
          size: Size(box.maxWidth, box.maxHeight)),
      ),
      if (widget.showDebugOverlay) /* debug overlay with _active count */
    ]));
  }
}

class _Painter extends CustomPainter {
  final List<Particle> particles;
  _Painter({required this.particles, required Listenable repaint})
      : super(repaint: repaint);

  @override
  void paint(Canvas canvas, Size size) {
    final paint = Paint()..style = PaintingStyle.fill; // Reuse one Paint object
    for (final p in particles) {
      if (!p.alive) continue;
      final life = 1 - (p.age / p.lifetime).clamp(0.0, 1.0);
      paint.color = p.color.withOpacity(life * 0.8);
      canvas.drawCircle(Offset(p.x, p.y), p.size * life, paint);
    }
  }

  @override
  bool shouldRepaint(_Painter old) => true;
}
```

### Deep Dive

The repaint listenable pattern is the key performance insight. The controller ticks, the painter repaints, but build() never runs. No widget tree reconciliation happens during animation. Combine this with particle pooling (zero allocation in the hot path) and you maintain 60fps with 500 particles.

---

## Exercise 8: Gesture-Driven Animated Navigation System

### Progressive Hints

**Hint 1:** The nav controller manages a route stack where each entry has a progress double (0=offscreen, 1=visible). Push animates 0->1. Pop animates 1->0. Gesture drives the value directly.

**Hint 2:** Shared element tracking: each page registers widgets via GlobalKey. During transition, read RenderBox for global position/size, animate between rects in an Overlay.

**Hint 3:** Interrupt handling: capture current animated values, stop animation, start new animation from captured values. Must happen synchronously in one frame.

**Hint 4:** Back swipe: only start when drag begins within 20px of left edge (matches iOS). Previous page translates at 0.3x rate for parallax depth.

### Common Mistakes

**Shared elements jump at transition start.** Read positions after layout with `addPostFrameCallback`, not during build.

**Pages stack wrong after rapid push/pop.** Only modify the stack when animations complete, not when they start.

**Interrupt shows duplicate shared elements.** Remove old overlay entries before creating new ones.

### Full Solution

This system spans four files. The core challenge is the gesture handler (interrupt handling + physics) and the nav host (parallax rendering). The controller and registry are straightforward.

```dart
// file: lib/nav_system/animated_nav_controller.dart
// RouteEntry: page, id, progress (0=offscreen, 1=visible), isPopping
// AnimatedNavController extends ChangeNotifier: push, completePop, cancelPop, updateProgress
// Stack is only modified when animations complete, never mid-flight

// file: lib/nav_system/shared_element_registry.dart
// SharedElementData: GlobalKey + tag + pageId, getRect() reads RenderBox position
// SharedElementRegistry: register/unregister by tag+pageId, getTransitionRects returns
// source/target Rect pairs. CRITICAL: read positions via addPostFrameCallback, not in build
```

```dart
// file: lib/nav_system/gesture_handler.dart
import 'package:flutter/material.dart';
import 'package:flutter/physics.dart';

class _GestureState extends State<NavGestureHandler>
    with SingleTickerProviderStateMixin {
  late final AnimationController _anim;
  bool _dragging = false;

  @override
  void initState() {
    super.initState();
    _anim = AnimationController(vsync: this)
      ..addListener(() => widget.controller.updateProgress(_anim.value));
  }

  // onHorizontalDragStart: only if dx < 20 (edge) and stack.length > 1
  // onHorizontalDragUpdate: progress = 1 - (dragDistance / screenWidth)
  // onHorizontalDragEnd -- THE CRITICAL PART:
  void _onDragEnd(DragEndDetails d) {
    final vel = d.velocity.pixelsPerSecond.dx;
    final cur = widget.controller.current?.progress ?? 1;
    final spring = SpringDescription(mass: 1, stiffness: 300, damping: 25);

    // INTERRUPT HANDLING: _anim.value = cur captures current position
    // before starting new animation. No jumps.
    if (vel > 500 || cur < 0.5) {
      _anim.value = cur; // Capture interrupted position
      _anim.animateWith(SpringSimulation(spring, cur, 0, -vel / 1000))
          .then((_) => widget.controller.completePop());
    } else {
      _anim.value = cur; // Capture interrupted position
      _anim.animateWith(SpringSimulation(spring, cur, 1, -vel / 1000))
          .then((_) => widget.controller.cancelPop());
    }
  }
}
```

```dart
// file: lib/nav_system/animated_nav_page.dart
class AnimatedNavHost extends StatelessWidget {
  // Stack renders all route entries
  // Top page: offset = (1 - progress) * screenWidth (follows gesture 1:1)
  // Previous page: offset = -(1 - topProgress) * screenWidth * 0.3 (parallax)

  Widget _buildLayer(RouteEntry entry, bool isTop) {
    final currentProgress = _ctrl.current?.progress ?? 1;
    final offset = isTop
        ? (1 - entry.progress) * 400       // Current page: full translation
        : -(1 - currentProgress) * 120;     // Previous page: 0.3x parallax
    return Transform.translate(offset: Offset(offset, 0), child: entry.page);
  }
}
```

### Deep Dive

The interrupt pattern is the hardest lesson: `_anim.value = cur` before `animateWith` captures the interrupted position as the new starting point. This must be synchronous within one frame. The parallax (previous page at 0.3x) simulates depth, matching iOS back-swipe navigation.

---

## Debugging Tips for All Exercises

**"setState called after dispose"**: Controller listener fires after widget removal. Always dispose controllers. Check `mounted` before async setState.

**Jank / dropped frames**: Use `flutter run --profile` + DevTools. Causes: full tree rebuild per frame (use AnimatedBuilder child), object creation in build, heavy paint without RepaintBoundary.

**Animation does not play**: Verify forward()/repeat() is called. Check widget visibility. Verify mixin is on the State class.

**Animation plays but nothing moves**: Tween has begin == end, or builder does not use the animation value.

**Physics explosion**: Spring constants too extreme. Start with defaults. Clamp delta time.
