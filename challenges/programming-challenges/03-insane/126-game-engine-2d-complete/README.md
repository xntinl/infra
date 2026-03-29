# 126. Complete 2D Game Engine

<!--
difficulty: insane
category: game-development-and-graphics
languages: [rust]
concepts: [game-engine, ecs, game-loop, sprite-rendering, tile-maps, physics, input-handling, audio-playback, scene-management, asset-loading, camera-system]
estimated_time: 30-50 hours
bloom_level: create
prerequisites: [ecs-architecture, 2d-vector-math, fixed-timestep-game-loop, basic-audio-formats, image-decoding, rust-trait-objects, generics]
-->

## Languages

- Rust (stable)

## Prerequisites

- Entity Component System architecture (generational IDs, archetypal or sparse-set storage)
- 2D vector math and transformation matrices
- Fixed-timestep game loop with delta time and frame interpolation
- Basic understanding of WAV audio format (PCM samples)
- Image decoding (BMP or simple formats) and pixel buffer manipulation
- Rust trait objects, generics, and module organization for large projects

## Learning Objectives

- **Create** a complete 2D game engine with ECS, physics, rendering, input, audio, and scene management working together
- **Design** a rendering pipeline that composites sprites, tilemaps, and UI onto a framebuffer with camera transformation
- **Implement** a fixed-timestep game loop that decouples physics updates from rendering
- **Architect** an asset loading system that manages textures, sounds, and scene definitions
- **Evaluate** the integration complexity of combining multiple engine subsystems into a cohesive runtime

## The Challenge

A game engine is a runtime that orchestrates dozens of subsystems: entity management, physics, rendering, input, audio, assets, scenes. Each subsystem is manageable in isolation. The challenge is making them work together seamlessly at 60 frames per second.

Build a complete 2D game engine and a playable demo game (a platformer or top-down adventure). The engine must use an ECS for entity management, a physics system for gravity and collision, a sprite-based rendering system that draws to a raw framebuffer (via `minifb` or equivalent minimal windowing crate), keyboard input handling, basic WAV audio playback, tile map support, a camera that follows the player, and a scene manager for loading levels.

The demo game must be playable: a character moves, jumps, collides with the environment, collects items, and transitions between at least two scenes. This is not a rendering exercise -- it is a systems integration challenge.

## Requirements

1. **ECS Core**: Entity creation/destruction with generational IDs. Component storage (archetypal or sparse-set). Systems execute per frame in defined order
2. **Game Loop**: Fixed timestep for physics (e.g., 1/60s) with accumulator. Variable rendering. Delta time passed to update systems. Frame rate measurement
3. **Rendering**: Software rasterizer drawing to a `Vec<u32>` framebuffer (ARGB). Sprite rendering from texture atlases. Tile map rendering from a tile sheet. Camera transform (translation, optional zoom). Layer ordering (background, entities, foreground, UI). Present framebuffer via `minifb` window
4. **Physics**: Gravity applied to dynamic bodies. AABB collision detection between entities and tilemap solid tiles. Collision response: stop movement, ground detection for jumping. No tunneling for reasonable velocities
5. **Input**: Keyboard state tracking (pressed, held, released) via `minifb` key events. Input mapped to game actions (move left/right, jump, interact)
6. **Audio**: Load WAV files (PCM 16-bit). Playback via `rodio` or raw `cpal` output. Support simultaneous sounds (at least 2 channels). Play sound effects on events (jump, collect)
7. **Asset Loading**: Load sprite sheets from BMP/PNG files. Load tile maps from a simple text or JSON format. Load WAV sounds. Asset handle system to avoid reloading
8. **Scene Management**: Scene trait with `on_enter`, `update`, `render`, `on_exit`. Scene stack or state machine for transitions. At least 2 scenes in the demo (menu + gameplay, or two levels)
9. **Tile Map**: Grid-based level representation. Solid vs passable tiles. Render tiles from a tile sheet texture. Collision against solid tiles
10. **Camera**: Follow the player entity smoothly (lerp or deadzone). Clamp to level bounds. Transform all rendering through camera offset
11. **Demo Game**: Playable platformer or top-down game. Player movement, jumping (if platformer), collision with environment. At least one collectible type. Scene transition (e.g., door to next level or game-over screen)
12. Document all `unsafe` blocks with safety invariants

## Acceptance Criteria

- [ ] A window opens showing the game world with rendered sprites and tiles
- [ ] The player character moves in response to keyboard input with smooth animation
- [ ] Physics: the player falls with gravity and lands on solid tiles (platformer) or collides with walls (top-down)
- [ ] Collecting an item plays a sound effect and removes the item entity
- [ ] The camera follows the player, and the level extends beyond the screen
- [ ] Transitioning to a second scene loads a different level layout
- [ ] The game runs at stable 60 FPS on release build
- [ ] Frame time is logged showing physics update time, render time, and total frame time
- [ ] At least 50 entities on screen simultaneously without dropping below 60 FPS
- [ ] The engine compiles with `cargo build --release` without warnings

## Research Resources

- [Bevy Engine Architecture](https://bevyengine.org/learn/quick-start/getting-started/ecs/) -- modern Rust game engine for architectural inspiration
- [Game Programming Patterns (Robert Nystrom)](https://gameprogrammingpatterns.com/) -- game loop, component, observer, state patterns (free online)
- [Fix Your Timestep (Glenn Fiedler)](https://gafferongames.com/post/fix_your_timestep/) -- the definitive article on deterministic game loops
- [minifb crate](https://crates.io/crates/minifb) -- minimal framebuffer window for Rust
- [rodio crate](https://crates.io/crates/rodio) -- simple audio playback for Rust
- [Tile Map Collision (Metanet)](https://www.metanetsoftware.com/2016/n-tutorial-a-collision-detection-and-response) -- grid-based collision for platformers
- [Handmade Hero](https://handmadehero.org/) -- building a game engine from scratch, covers all subsystems
- [Lazy Devs Academy - PICO-8 Tutorials](https://www.youtube.com/c/LazyDevs) -- simple 2D game design patterns applicable to any engine
- [Catherine West - RustConf 2018 Closing Keynote](https://www.youtube.com/watch?v=aKLntZcp27M) -- ECS design trade-offs in Rust game engines
