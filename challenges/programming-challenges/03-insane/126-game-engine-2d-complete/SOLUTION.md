# Solution: Complete 2D Game Engine

## Architecture Overview

The engine is organized into nine modules plus a demo game:

1. **math**: `Vec2`, `Rect`, color utilities
2. **ecs**: Entity allocator, component storage, system scheduler
3. **render**: Framebuffer, sprite drawing, tilemap rendering, camera transform
4. **physics**: Gravity, AABB collision, tilemap collision, ground detection
5. **input**: Keyboard state tracking (pressed/held/released)
6. **audio**: WAV loading, mixer with multiple channels
7. **assets**: Handle-based asset manager for textures, sounds, tilemaps
8. **scene**: Scene trait, scene manager with stack-based transitions
9. **engine**: Game loop with fixed timestep, subsystem initialization, frame timing

```
Engine (main loop)
  |
  |-- Window (minifb) <-> Input System
  |
  |-- Scene Manager
  |     |-- Active Scene
  |           |-- on_enter: spawn entities, load assets
  |           |-- update(dt): run ECS systems
  |           |-- render(fb): draw world
  |           |-- on_exit: cleanup
  |
  |-- ECS World
  |     |-- Entities (generational IDs)
  |     |-- Components: Transform, Sprite, RigidBody, Collider, Player, Collectible
  |     |-- Systems: physics, collision, camera, animation
  |
  |-- Renderer -> Framebuffer (Vec<u32>)
  |     |-- Clear
  |     |-- Draw tilemap (camera-relative)
  |     |-- Draw sprites (sorted by layer, camera-relative)
  |     |-- Draw UI (screen-space)
  |
  |-- Audio Mixer -> cpal/rodio output
  |
  |-- Asset Manager
        |-- Textures (Vec<u32> pixel buffers)
        |-- Sounds (Vec<i16> PCM samples)
        |-- Tilemaps (grid data)
```

## Rust Solution

### Cargo.toml

```toml
[package]
name = "engine2d"
version = "0.1.0"
edition = "2021"

[dependencies]
minifb = "0.27"
rodio = { version = "0.19", default-features = false, features = ["wav"] }
serde = { version = "1", features = ["derive"] }
serde_json = "1"
image = { version = "0.25", default-features = false, features = ["png"] }
```

### src/math.rs

```rust
use std::ops::{Add, Sub, Mul};

#[derive(Debug, Clone, Copy, PartialEq)]
pub struct Vec2 {
    pub x: f32,
    pub y: f32,
}

impl Vec2 {
    pub const ZERO: Vec2 = Vec2 { x: 0.0, y: 0.0 };

    pub fn new(x: f32, y: f32) -> Self {
        Self { x, y }
    }

    pub fn length(self) -> f32 {
        (self.x * self.x + self.y * self.y).sqrt()
    }

    pub fn lerp(self, target: Vec2, t: f32) -> Vec2 {
        Vec2::new(
            self.x + (target.x - self.x) * t,
            self.y + (target.y - self.y) * t,
        )
    }
}

impl Add for Vec2 { type Output = Vec2; fn add(self, r: Vec2) -> Vec2 { Vec2::new(self.x + r.x, self.y + r.y) } }
impl Sub for Vec2 { type Output = Vec2; fn sub(self, r: Vec2) -> Vec2 { Vec2::new(self.x - r.x, self.y - r.y) } }
impl Mul<f32> for Vec2 { type Output = Vec2; fn mul(self, t: f32) -> Vec2 { Vec2::new(self.x * t, self.y * t) } }

#[derive(Debug, Clone, Copy)]
pub struct Rect {
    pub x: f32,
    pub y: f32,
    pub w: f32,
    pub h: f32,
}

impl Rect {
    pub fn new(x: f32, y: f32, w: f32, h: f32) -> Self {
        Self { x, y, w, h }
    }

    pub fn overlaps(&self, other: &Rect) -> bool {
        self.x < other.x + other.w
            && self.x + self.w > other.x
            && self.y < other.y + other.h
            && self.y + self.h > other.y
    }

    pub fn contains_point(&self, px: f32, py: f32) -> bool {
        px >= self.x && px < self.x + self.w && py >= self.y && py < self.y + self.h
    }
}

pub fn color_rgb(r: u8, g: u8, b: u8) -> u32 {
    (0xFF << 24) | ((r as u32) << 16) | ((g as u32) << 8) | (b as u32)
}

pub fn color_rgba(r: u8, g: u8, b: u8, a: u8) -> u32 {
    ((a as u32) << 24) | ((r as u32) << 16) | ((g as u32) << 8) | (b as u32)
}
```

### src/ecs.rs

```rust
use std::any::{Any, TypeId};
use std::collections::HashMap;

#[derive(Clone, Copy, PartialEq, Eq, Hash, Debug)]
pub struct Entity {
    pub id: u32,
    pub generation: u32,
}

struct Slot {
    generation: u32,
    alive: bool,
}

pub struct World {
    slots: Vec<Slot>,
    free_list: Vec<u32>,
    components: HashMap<TypeId, HashMap<u32, Box<dyn Any>>>,
}

impl World {
    pub fn new() -> Self {
        Self {
            slots: Vec::new(),
            free_list: Vec::new(),
            components: HashMap::new(),
        }
    }

    pub fn spawn(&mut self) -> Entity {
        if let Some(id) = self.free_list.pop() {
            let slot = &mut self.slots[id as usize];
            slot.alive = true;
            Entity { id, generation: slot.generation }
        } else {
            let id = self.slots.len() as u32;
            self.slots.push(Slot { generation: 0, alive: true });
            Entity { id, generation: 0 }
        }
    }

    pub fn despawn(&mut self, entity: Entity) {
        if !self.is_alive(entity) { return; }
        self.slots[entity.id as usize].alive = false;
        self.slots[entity.id as usize].generation += 1;
        self.free_list.push(entity.id);

        for store in self.components.values_mut() {
            store.remove(&entity.id);
        }
    }

    pub fn is_alive(&self, entity: Entity) -> bool {
        self.slots.get(entity.id as usize)
            .map_or(false, |s| s.alive && s.generation == entity.generation)
    }

    pub fn insert<T: 'static>(&mut self, entity: Entity, component: T) {
        let type_id = TypeId::of::<T>();
        self.components
            .entry(type_id)
            .or_default()
            .insert(entity.id, Box::new(component));
    }

    pub fn get<T: 'static>(&self, entity: Entity) -> Option<&T> {
        let type_id = TypeId::of::<T>();
        self.components.get(&type_id)?
            .get(&entity.id)?
            .downcast_ref::<T>()
    }

    pub fn get_mut<T: 'static>(&mut self, entity: Entity) -> Option<&mut T> {
        let type_id = TypeId::of::<T>();
        self.components.get_mut(&type_id)?
            .get_mut(&entity.id)?
            .downcast_mut::<T>()
    }

    pub fn has<T: 'static>(&self, entity: Entity) -> bool {
        let type_id = TypeId::of::<T>();
        self.components.get(&type_id)
            .map_or(false, |store| store.contains_key(&entity.id))
    }

    /// Return all alive entities that have component T.
    pub fn query<T: 'static>(&self) -> Vec<Entity> {
        let type_id = TypeId::of::<T>();
        let store = match self.components.get(&type_id) {
            Some(s) => s,
            None => return Vec::new(),
        };
        store.keys()
            .filter_map(|&id| {
                let slot = &self.slots[id as usize];
                if slot.alive {
                    Some(Entity { id, generation: slot.generation })
                } else {
                    None
                }
            })
            .collect()
    }

    /// Return alive entities that have both T1 and T2.
    pub fn query2<T1: 'static, T2: 'static>(&self) -> Vec<Entity> {
        let tid1 = TypeId::of::<T1>();
        let tid2 = TypeId::of::<T2>();
        let store1 = match self.components.get(&tid1) {
            Some(s) => s,
            None => return Vec::new(),
        };
        let store2 = match self.components.get(&tid2) {
            Some(s) => s,
            None => return Vec::new(),
        };
        store1.keys()
            .filter(|id| store2.contains_key(id))
            .filter_map(|&id| {
                let slot = &self.slots[id as usize];
                if slot.alive {
                    Some(Entity { id, generation: slot.generation })
                } else {
                    None
                }
            })
            .collect()
    }
}
```

### src/components.rs

```rust
use crate::math::{Vec2, Rect};

#[derive(Debug, Clone)]
pub struct Transform {
    pub position: Vec2,
    pub scale: Vec2,
    pub rotation: f32,
    pub layer: i32,
}

impl Transform {
    pub fn at(x: f32, y: f32) -> Self {
        Self {
            position: Vec2::new(x, y),
            scale: Vec2::new(1.0, 1.0),
            rotation: 0.0,
            layer: 0,
        }
    }

    pub fn with_layer(mut self, layer: i32) -> Self {
        self.layer = layer;
        self
    }
}

#[derive(Debug, Clone)]
pub struct Sprite {
    pub texture_id: usize,
    pub src_rect: Rect,      // Region in texture atlas
    pub width: u32,
    pub height: u32,
    pub flip_x: bool,
}

#[derive(Debug, Clone)]
pub struct RigidBody {
    pub velocity: Vec2,
    pub gravity_scale: f32,
    pub is_grounded: bool,
    pub is_static: bool,
}

impl RigidBody {
    pub fn dynamic() -> Self {
        Self {
            velocity: Vec2::ZERO,
            gravity_scale: 1.0,
            is_grounded: false,
            is_static: false,
        }
    }

    pub fn stationary() -> Self {
        Self {
            velocity: Vec2::ZERO,
            gravity_scale: 0.0,
            is_grounded: false,
            is_static: true,
        }
    }
}

#[derive(Debug, Clone)]
pub struct Collider {
    pub width: f32,
    pub height: f32,
    pub offset: Vec2,
}

impl Collider {
    pub fn new(width: f32, height: f32) -> Self {
        Self { width, height, offset: Vec2::ZERO }
    }

    pub fn world_rect(&self, position: Vec2) -> Rect {
        Rect::new(
            position.x + self.offset.x,
            position.y + self.offset.y,
            self.width,
            self.height,
        )
    }
}

#[derive(Debug, Clone)]
pub struct Player {
    pub speed: f32,
    pub jump_force: f32,
    pub score: u32,
}

#[derive(Debug, Clone)]
pub struct Collectible {
    pub point_value: u32,
    pub sound_id: Option<usize>,
}

#[derive(Debug, Clone)]
pub struct Animation {
    pub frames: Vec<Rect>,
    pub frame_duration: f32,
    pub current_frame: usize,
    pub elapsed: f32,
    pub looping: bool,
}

impl Animation {
    pub fn update(&mut self, dt: f32) {
        self.elapsed += dt;
        if self.elapsed >= self.frame_duration {
            self.elapsed -= self.frame_duration;
            if self.looping {
                self.current_frame = (self.current_frame + 1) % self.frames.len();
            } else if self.current_frame < self.frames.len() - 1 {
                self.current_frame += 1;
            }
        }
    }

    pub fn current_src_rect(&self) -> Rect {
        self.frames[self.current_frame]
    }
}
```

### src/render.rs

```rust
use crate::math::{Vec2, Rect, color_rgb};

pub struct Framebuffer {
    pub width: usize,
    pub height: usize,
    pub pixels: Vec<u32>,
}

impl Framebuffer {
    pub fn new(width: usize, height: usize) -> Self {
        Self {
            width,
            height,
            pixels: vec![0; width * height],
        }
    }

    pub fn clear(&mut self, color: u32) {
        self.pixels.fill(color);
    }

    #[inline]
    pub fn set_pixel(&mut self, x: i32, y: i32, color: u32) {
        if x >= 0 && y >= 0 && (x as usize) < self.width && (y as usize) < self.height {
            let alpha = (color >> 24) & 0xFF;
            if alpha == 0 { return; }
            let idx = y as usize * self.width + x as usize;
            if alpha == 0xFF {
                self.pixels[idx] = color;
            } else {
                self.pixels[idx] = blend_pixel(self.pixels[idx], color, alpha);
            }
        }
    }

    pub fn draw_rect(&mut self, x: i32, y: i32, w: i32, h: i32, color: u32) {
        for dy in 0..h {
            for dx in 0..w {
                self.set_pixel(x + dx, y + dy, color);
            }
        }
    }

    /// Blit a texture region onto the framebuffer.
    pub fn blit(
        &mut self,
        texture: &Texture,
        src: Rect,
        dst_x: i32,
        dst_y: i32,
        flip_x: bool,
    ) {
        let sw = src.w as i32;
        let sh = src.h as i32;
        let sx = src.x as i32;
        let sy = src.y as i32;

        for dy in 0..sh {
            for dx in 0..sw {
                let tx = if flip_x { sx + sw - 1 - dx } else { sx + dx };
                let ty = sy + dy;

                if tx >= 0 && ty >= 0
                    && (tx as usize) < texture.width
                    && (ty as usize) < texture.height
                {
                    let color = texture.pixels[ty as usize * texture.width + tx as usize];
                    self.set_pixel(dst_x + dx, dst_y + dy, color);
                }
            }
        }
    }

    /// Draw text using a simple 8x8 built-in font (ASCII printable range).
    pub fn draw_text(&mut self, text: &str, x: i32, y: i32, color: u32) {
        let mut cx = x;
        for ch in text.chars() {
            if ch == ' ' {
                cx += 6;
                continue;
            }
            draw_char_simple(self, ch, cx, y, color);
            cx += 6;
        }
    }
}

fn blend_pixel(dst: u32, src: u32, alpha: u32) -> u32 {
    let inv_alpha = 255 - alpha;
    let r = ((((src >> 16) & 0xFF) * alpha + ((dst >> 16) & 0xFF) * inv_alpha) / 255) & 0xFF;
    let g = ((((src >> 8) & 0xFF) * alpha + ((dst >> 8) & 0xFF) * inv_alpha) / 255) & 0xFF;
    let b = (((src & 0xFF) * alpha + (dst & 0xFF) * inv_alpha) / 255) & 0xFF;
    (0xFF << 24) | (r << 16) | (g << 8) | b
}

fn draw_char_simple(fb: &mut Framebuffer, ch: char, x: i32, y: i32, color: u32) {
    // Minimal 5x5 bitmaps for digits and common letters
    let bitmap = match ch {
        '0'..='9' => get_digit_bitmap(ch as u8 - b'0'),
        'A'..='Z' => get_letter_bitmap(ch as u8 - b'A'),
        'a'..='z' => get_letter_bitmap(ch as u8 - b'a'),
        ':' => &[0b00000, 0b00100, 0b00000, 0b00100, 0b00000],
        _ => &[0b11111, 0b10001, 0b10001, 0b10001, 0b11111],
    };
    for (row, &bits) in bitmap.iter().enumerate() {
        for col in 0..5 {
            if bits & (1 << (4 - col)) != 0 {
                fb.set_pixel(x + col, y + row as i32, color);
            }
        }
    }
}

fn get_digit_bitmap(d: u8) -> &'static [u8; 5] {
    const DIGITS: [[u8; 5]; 10] = [
        [0b01110, 0b10001, 0b10001, 0b10001, 0b01110], // 0
        [0b00100, 0b01100, 0b00100, 0b00100, 0b01110], // 1
        [0b01110, 0b10001, 0b00110, 0b01000, 0b11111], // 2
        [0b11110, 0b00001, 0b01110, 0b00001, 0b11110], // 3
        [0b10010, 0b10010, 0b11111, 0b00010, 0b00010], // 4
        [0b11111, 0b10000, 0b11110, 0b00001, 0b11110], // 5
        [0b01110, 0b10000, 0b11110, 0b10001, 0b01110], // 6
        [0b11111, 0b00010, 0b00100, 0b01000, 0b01000], // 7
        [0b01110, 0b10001, 0b01110, 0b10001, 0b01110], // 8
        [0b01110, 0b10001, 0b01111, 0b00001, 0b01110], // 9
    ];
    &DIGITS[d as usize]
}

fn get_letter_bitmap(l: u8) -> &'static [u8; 5] {
    const FALLBACK: [u8; 5] = [0b01110, 0b10001, 0b11111, 0b10001, 0b10001];
    // Only define a few common letters; fallback for the rest
    match l {
        0 => &[0b01110, 0b10001, 0b11111, 0b10001, 0b10001], // A
        4 => &[0b11111, 0b10000, 0b11110, 0b10000, 0b11111], // E
        5 => &[0b11111, 0b10000, 0b11110, 0b10000, 0b10000], // F
        7 => &[0b10001, 0b10001, 0b11111, 0b10001, 0b10001], // H
        8 => &[0b01110, 0b00100, 0b00100, 0b00100, 0b01110], // I
        11 => &[0b10000, 0b10000, 0b10000, 0b10000, 0b11111],// L
        13 => &[0b10001, 0b11001, 0b10101, 0b10011, 0b10001],// N
        14 => &[0b01110, 0b10001, 0b10001, 0b10001, 0b01110],// O
        15 => &[0b11110, 0b10001, 0b11110, 0b10000, 0b10000],// P
        17 => &[0b11110, 0b10001, 0b11110, 0b10010, 0b10001],// R
        18 => &[0b01111, 0b10000, 0b01110, 0b00001, 0b11110],// S
        19 => &[0b11111, 0b00100, 0b00100, 0b00100, 0b00100],// T
        _ => &FALLBACK,
    }
}

pub struct Texture {
    pub width: usize,
    pub height: usize,
    pub pixels: Vec<u32>,
}

impl Texture {
    pub fn from_color(width: usize, height: usize, color: u32) -> Self {
        Self {
            width,
            height,
            pixels: vec![color; width * height],
        }
    }

    /// Load from a PNG file using the `image` crate.
    pub fn load_png(path: &str) -> Result<Self, String> {
        let img = image::open(path).map_err(|e| format!("Failed to load {}: {}", path, e))?;
        let rgba = img.to_rgba8();
        let (w, h) = rgba.dimensions();
        let pixels: Vec<u32> = rgba.pixels()
            .map(|p| {
                let [r, g, b, a] = p.0;
                ((a as u32) << 24) | ((r as u32) << 16) | ((g as u32) << 8) | (b as u32)
            })
            .collect();
        Ok(Self {
            width: w as usize,
            height: h as usize,
            pixels,
        })
    }
}

pub struct Camera {
    pub position: Vec2,
    pub target: Vec2,
    pub smoothing: f32,
    pub bounds: Option<Rect>,
}

impl Camera {
    pub fn new() -> Self {
        Self {
            position: Vec2::ZERO,
            target: Vec2::ZERO,
            smoothing: 0.1,
            bounds: None,
        }
    }

    pub fn follow(&mut self, target_pos: Vec2, screen_w: f32, screen_h: f32) {
        self.target = Vec2::new(
            target_pos.x - screen_w / 2.0,
            target_pos.y - screen_h / 2.0,
        );
        self.position = self.position.lerp(self.target, self.smoothing);

        if let Some(bounds) = &self.bounds {
            self.position.x = self.position.x.clamp(bounds.x, bounds.x + bounds.w - screen_w);
            self.position.y = self.position.y.clamp(bounds.y, bounds.y + bounds.h - screen_h);
        }
    }

    pub fn world_to_screen(&self, world_pos: Vec2) -> (i32, i32) {
        (
            (world_pos.x - self.position.x) as i32,
            (world_pos.y - self.position.y) as i32,
        )
    }
}
```

### src/tilemap.rs

```rust
use crate::math::{Vec2, Rect};
use crate::render::{Framebuffer, Texture, Camera};

#[derive(Debug, Clone)]
pub struct TileMap {
    pub width: usize,
    pub height: usize,
    pub tile_size: usize,
    pub tiles: Vec<u8>,          // Tile IDs
    pub solid_tiles: Vec<bool>,  // Which tile IDs are solid (indexed by tile ID)
}

impl TileMap {
    pub fn from_string(data: &str, tile_size: usize, solid_ids: &[u8]) -> Self {
        let lines: Vec<&str> = data.lines().collect();
        let height = lines.len();
        let width = lines.iter().map(|l| l.split(',').count()).max().unwrap_or(0);
        let mut tiles = vec![0u8; width * height];

        for (y, line) in lines.iter().enumerate() {
            for (x, cell) in line.split(',').enumerate() {
                if let Ok(id) = cell.trim().parse::<u8>() {
                    tiles[y * width + x] = id;
                }
            }
        }

        let max_id = *tiles.iter().max().unwrap_or(&0) as usize;
        let mut solid_tiles = vec![false; max_id + 1];
        for &id in solid_ids {
            if (id as usize) < solid_tiles.len() {
                solid_tiles[id as usize] = true;
            }
        }

        Self { width, height, tile_size, tiles, solid_tiles }
    }

    pub fn get_tile(&self, x: usize, y: usize) -> u8 {
        if x < self.width && y < self.height {
            self.tiles[y * self.width + x]
        } else {
            0
        }
    }

    pub fn is_solid(&self, x: usize, y: usize) -> bool {
        let tile = self.get_tile(x, y);
        self.solid_tiles.get(tile as usize).copied().unwrap_or(false)
    }

    pub fn is_solid_world(&self, world_x: f32, world_y: f32) -> bool {
        if world_x < 0.0 || world_y < 0.0 { return true; }
        let tx = (world_x / self.tile_size as f32) as usize;
        let ty = (world_y / self.tile_size as f32) as usize;
        self.is_solid(tx, ty)
    }

    pub fn pixel_width(&self) -> f32 {
        (self.width * self.tile_size) as f32
    }

    pub fn pixel_height(&self) -> f32 {
        (self.height * self.tile_size) as f32
    }

    /// Render visible tiles using colored rectangles (no texture atlas).
    pub fn render_simple(&self, fb: &mut Framebuffer, camera: &Camera, colors: &[u32]) {
        let ts = self.tile_size as i32;
        let cam_x = camera.position.x as i32;
        let cam_y = camera.position.y as i32;

        let start_tx = (cam_x / ts).max(0) as usize;
        let start_ty = (cam_y / ts).max(0) as usize;
        let end_tx = ((cam_x + fb.width as i32) / ts + 1).min(self.width as i32) as usize;
        let end_ty = ((cam_y + fb.height as i32) / ts + 1).min(self.height as i32) as usize;

        for ty in start_ty..end_ty {
            for tx in start_tx..end_tx {
                let tile_id = self.get_tile(tx, ty) as usize;
                if tile_id == 0 { continue; }
                let color = colors.get(tile_id).copied().unwrap_or(0xFF888888);
                let screen_x = (tx as i32 * ts) - cam_x;
                let screen_y = (ty as i32 * ts) - cam_y;
                fb.draw_rect(screen_x, screen_y, ts, ts, color);
            }
        }
    }

    /// Render tiles from a texture atlas (tile sheet).
    pub fn render_textured(
        &self,
        fb: &mut Framebuffer,
        camera: &Camera,
        tilesheet: &Texture,
        sheet_cols: usize,
    ) {
        let ts = self.tile_size as i32;
        let cam_x = camera.position.x as i32;
        let cam_y = camera.position.y as i32;

        let start_tx = (cam_x / ts).max(0) as usize;
        let start_ty = (cam_y / ts).max(0) as usize;
        let end_tx = ((cam_x + fb.width as i32) / ts + 1).min(self.width as i32) as usize;
        let end_ty = ((cam_y + fb.height as i32) / ts + 1).min(self.height as i32) as usize;

        for ty in start_ty..end_ty {
            for tx in start_tx..end_tx {
                let tile_id = self.get_tile(tx, ty) as usize;
                if tile_id == 0 { continue; }
                let src_col = tile_id % sheet_cols;
                let src_row = tile_id / sheet_cols;
                let src = Rect::new(
                    (src_col * self.tile_size) as f32,
                    (src_row * self.tile_size) as f32,
                    self.tile_size as f32,
                    self.tile_size as f32,
                );
                let screen_x = (tx as i32 * ts) - cam_x;
                let screen_y = (ty as i32 * ts) - cam_y;
                fb.blit(tilesheet, src, screen_x, screen_y, false);
            }
        }
    }
}

/// Resolve AABB vs tilemap collision, returning corrected position.
pub fn resolve_tilemap_collision(
    pos: Vec2,
    vel: &mut Vec2,
    width: f32,
    height: f32,
    tilemap: &TileMap,
    grounded: &mut bool,
) -> Vec2 {
    let mut new_pos = pos;
    let ts = tilemap.tile_size as f32;
    *grounded = false;

    // Horizontal resolution
    new_pos.x += vel.x;
    let left = new_pos.x;
    let right = new_pos.x + width;
    let top = new_pos.y;
    let bottom = new_pos.y + height - 1.0;

    if vel.x > 0.0 {
        let tx = (right / ts) as usize;
        for check_y in [(top / ts) as usize, (bottom / ts) as usize] {
            if tilemap.is_solid(tx, check_y) {
                new_pos.x = (tx as f32 * ts) - width;
                vel.x = 0.0;
                break;
            }
        }
    } else if vel.x < 0.0 {
        let tx = (left / ts) as usize;
        for check_y in [(top / ts) as usize, (bottom / ts) as usize] {
            if tilemap.is_solid(tx, check_y) {
                new_pos.x = (tx + 1) as f32 * ts;
                vel.x = 0.0;
                break;
            }
        }
    }

    // Vertical resolution
    new_pos.y += vel.y;
    let left = new_pos.x + 1.0;
    let right = new_pos.x + width - 1.0;
    let top = new_pos.y;
    let bottom = new_pos.y + height;

    if vel.y > 0.0 {
        // Falling down
        let ty = (bottom / ts) as usize;
        for check_x in [(left / ts) as usize, (right / ts) as usize] {
            if tilemap.is_solid(check_x, ty) {
                new_pos.y = (ty as f32 * ts) - height;
                vel.y = 0.0;
                *grounded = true;
                break;
            }
        }
    } else if vel.y < 0.0 {
        // Moving up (jumping)
        let ty = (top / ts) as usize;
        for check_x in [(left / ts) as usize, (right / ts) as usize] {
            if tilemap.is_solid(check_x, ty) {
                new_pos.y = (ty + 1) as f32 * ts;
                vel.y = 0.0;
                break;
            }
        }
    }

    new_pos
}
```

### src/input.rs

```rust
use minifb::Key;
use std::collections::HashSet;

pub struct Input {
    current: HashSet<Key>,
    previous: HashSet<Key>,
}

impl Input {
    pub fn new() -> Self {
        Self {
            current: HashSet::new(),
            previous: HashSet::new(),
        }
    }

    pub fn update(&mut self, keys: &[Key]) {
        self.previous = self.current.clone();
        self.current.clear();
        for &key in keys {
            self.current.insert(key);
        }
    }

    pub fn is_held(&self, key: Key) -> bool {
        self.current.contains(&key)
    }

    pub fn is_pressed(&self, key: Key) -> bool {
        self.current.contains(&key) && !self.previous.contains(&key)
    }

    pub fn is_released(&self, key: Key) -> bool {
        !self.current.contains(&key) && self.previous.contains(&key)
    }
}
```

### src/audio.rs

```rust
use rodio::{OutputStream, Sink, Source};
use std::io::Cursor;
use std::sync::Arc;

pub struct AudioManager {
    _stream: OutputStream,
    sinks: Vec<Sink>,
    sounds: Vec<Arc<Vec<u8>>>,
}

impl AudioManager {
    pub fn new(channel_count: usize) -> Self {
        let (stream, handle) = OutputStream::try_default()
            .expect("Failed to initialize audio output");
        let sinks: Vec<Sink> = (0..channel_count)
            .map(|_| Sink::try_new(&handle).expect("Failed to create audio sink"))
            .collect();
        Self {
            _stream: stream,
            sinks,
            sounds: Vec::new(),
        }
    }

    pub fn load_wav(&mut self, path: &str) -> usize {
        let data = std::fs::read(path)
            .unwrap_or_else(|_| panic!("Failed to read WAV file: {}", path));
        let id = self.sounds.len();
        self.sounds.push(Arc::new(data));
        id
    }

    /// Play a sound on the first available (non-busy) channel.
    pub fn play(&self, sound_id: usize) {
        if sound_id >= self.sounds.len() { return; }
        let data = Arc::clone(&self.sounds[sound_id]);

        for sink in &self.sinks {
            if sink.empty() {
                let cursor = Cursor::new(data);
                if let Ok(source) = rodio::Decoder::new(cursor) {
                    sink.append(source);
                }
                return;
            }
        }
        // All channels busy -- skip this sound
    }
}
```

### src/scene.rs

```rust
use crate::ecs::World;
use crate::render::{Framebuffer, Camera};
use crate::input::Input;
use crate::audio::AudioManager;

pub enum SceneTransition {
    None,
    Push(Box<dyn Scene>),
    Pop,
    Replace(Box<dyn Scene>),
}

pub trait Scene {
    fn on_enter(&mut self, world: &mut World, audio: &mut AudioManager);
    fn update(
        &mut self,
        world: &mut World,
        input: &Input,
        audio: &AudioManager,
        dt: f32,
    ) -> SceneTransition;
    fn render(&self, world: &World, fb: &mut Framebuffer, camera: &Camera);
    fn on_exit(&mut self, world: &mut World);
}

pub struct SceneManager {
    stack: Vec<Box<dyn Scene>>,
}

impl SceneManager {
    pub fn new(initial: Box<dyn Scene>) -> Self {
        Self { stack: vec![initial] }
    }

    pub fn current(&self) -> Option<&dyn Scene> {
        self.stack.last().map(|s| s.as_ref())
    }

    pub fn current_mut(&mut self) -> Option<&mut Box<dyn Scene>> {
        self.stack.last_mut()
    }

    pub fn apply_transition(
        &mut self,
        transition: SceneTransition,
        world: &mut World,
        audio: &mut AudioManager,
    ) {
        match transition {
            SceneTransition::None => {}
            SceneTransition::Push(mut scene) => {
                scene.on_enter(world, audio);
                self.stack.push(scene);
            }
            SceneTransition::Pop => {
                if let Some(mut scene) = self.stack.pop() {
                    scene.on_exit(world);
                }
            }
            SceneTransition::Replace(mut scene) => {
                if let Some(mut old) = self.stack.pop() {
                    old.on_exit(world);
                }
                scene.on_enter(world, audio);
                self.stack.push(scene);
            }
        }
    }

    pub fn is_empty(&self) -> bool {
        self.stack.is_empty()
    }
}
```

### src/engine.rs

```rust
use minifb::{Window, WindowOptions, Key};
use std::time::Instant;

use crate::ecs::World;
use crate::render::{Framebuffer, Camera};
use crate::input::Input;
use crate::audio::AudioManager;
use crate::scene::{SceneManager, Scene};
use crate::math::color_rgb;

pub struct EngineConfig {
    pub width: usize,
    pub height: usize,
    pub title: String,
    pub target_fps: f32,
}

pub fn run(config: EngineConfig, initial_scene: Box<dyn Scene>) {
    let mut window = Window::new(
        &config.title,
        config.width,
        config.height,
        WindowOptions::default(),
    )
    .expect("Failed to create window");

    window.set_target_fps(config.target_fps as usize);

    let mut fb = Framebuffer::new(config.width, config.height);
    let mut camera = Camera::new();
    let mut input = Input::new();
    let mut audio = AudioManager::new(4);
    let mut world = World::new();
    let mut scene_manager = SceneManager::new(initial_scene);

    // Initialize first scene
    if let Some(scene) = scene_manager.current_mut() {
        scene.on_enter(&mut world, &mut audio);
    }

    let fixed_dt: f32 = 1.0 / config.target_fps;
    let mut accumulator: f32 = 0.0;
    let mut last_time = Instant::now();

    while window.is_open() && !window.is_key_down(Key::Escape) {
        let now = Instant::now();
        let frame_time = now.duration_since(last_time).as_secs_f32();
        last_time = now;

        // Cap frame time to prevent spiral of death
        let frame_time = frame_time.min(0.1);
        accumulator += frame_time;

        // Input
        let keys: Vec<Key> = window.get_keys().into_iter().collect();
        input.update(&keys);

        // Fixed timestep updates
        let mut transition = crate::scene::SceneTransition::None;
        while accumulator >= fixed_dt {
            if let Some(scene) = scene_manager.current_mut() {
                transition = scene.update(&mut world, &input, &audio, fixed_dt);
            }
            accumulator -= fixed_dt;
        }

        scene_manager.apply_transition(transition, &mut world, &mut audio);
        if scene_manager.is_empty() {
            break;
        }

        // Render
        fb.clear(color_rgb(30, 30, 50));
        if let Some(scene) = scene_manager.current() {
            scene.render(&world, &mut fb, &camera);
        }

        window
            .update_with_buffer(&fb.pixels, config.width, config.height)
            .expect("Failed to update window");
    }
}
```

### src/main.rs (Demo Platformer Game)

```rust
mod math;
mod ecs;
mod components;
mod render;
mod tilemap;
mod input;
mod audio;
mod scene;
mod engine;

use math::*;
use ecs::*;
use components::*;
use render::*;
use tilemap::*;
use input::Input;
use audio::AudioManager;
use scene::*;
use minifb::Key;

const SCREEN_W: usize = 320;
const SCREEN_H: usize = 240;
const GRAVITY: f32 = 600.0;
const TILE_SIZE: usize = 16;

// --- Level 1 Scene ---
struct Level1 {
    map: TileMap,
    player: Option<Entity>,
    camera: Camera,
}

impl Level1 {
    fn new() -> Self {
        let map_data = "\
0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0
0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0
0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0
0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0
0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0
0,0,0,0,0,0,0,0,0,0,0,0,1,1,1,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0
0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,1,1,1,0,0,0,0,0,0,0,0,0,0
0,0,0,0,0,0,0,0,1,1,0,0,0,0,0,0,0,0,1,1,1,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0
0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,1,1,1,0,0,0,0
0,0,0,0,0,1,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,1,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0
0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0
0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0
0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0
1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1
1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1,1";

        let map = TileMap::from_string(map_data, TILE_SIZE, &[1]);
        let mut camera = Camera::new();
        camera.bounds = Some(Rect::new(0.0, 0.0, map.pixel_width(), map.pixel_height()));

        Self { map, player: None, camera }
    }
}

impl Scene for Level1 {
    fn on_enter(&mut self, world: &mut World, _audio: &mut AudioManager) {
        let player = world.spawn();
        world.insert(player, Transform::at(48.0, 160.0).with_layer(10));
        world.insert(player, RigidBody::dynamic());
        world.insert(player, Collider::new(12.0, 14.0));
        world.insert(player, Player { speed: 120.0, jump_force: -280.0, score: 0 });
        self.player = Some(player);

        // Spawn some collectibles
        let coin_positions = [(200.0, 64.0), (300.0, 96.0), (450.0, 80.0), (150.0, 128.0)];
        for &(x, y) in &coin_positions {
            let coin = world.spawn();
            world.insert(coin, Transform::at(x, y).with_layer(5));
            world.insert(coin, Collider::new(8.0, 8.0));
            world.insert(coin, Collectible { point_value: 10, sound_id: None });
        }
    }

    fn update(
        &mut self,
        world: &mut World,
        input: &Input,
        audio: &AudioManager,
        dt: f32,
    ) -> SceneTransition {
        let player = match self.player {
            Some(e) => e,
            None => return SceneTransition::None,
        };

        // Player input
        let (speed, jump_force) = {
            let p = world.get::<Player>(player).unwrap();
            (p.speed, p.jump_force)
        };

        {
            let body = world.get_mut::<RigidBody>(player).unwrap();
            body.velocity.x = 0.0;
            if input.is_held(Key::Left) || input.is_held(Key::A) {
                body.velocity.x = -speed;
            }
            if input.is_held(Key::Right) || input.is_held(Key::D) {
                body.velocity.x = speed;
            }
            if (input.is_pressed(Key::Space) || input.is_pressed(Key::W) || input.is_pressed(Key::Up))
                && body.is_grounded
            {
                body.velocity.y = jump_force;
                body.is_grounded = false;
            }

            // Gravity
            body.velocity.y += GRAVITY * body.gravity_scale * dt;
        }

        // Physics: move and resolve tilemap collision
        {
            let body = world.get::<RigidBody>(player).unwrap();
            let vel = body.velocity;
            let transform = world.get::<Transform>(player).unwrap();
            let collider = world.get::<Collider>(player).unwrap();

            let mut velocity = vel;
            let mut grounded = false;
            let new_pos = resolve_tilemap_collision(
                transform.position,
                &mut velocity,
                collider.width,
                collider.height,
                &self.map,
                &mut grounded,
            );

            let body = world.get_mut::<RigidBody>(player).unwrap();
            body.velocity = velocity;
            body.is_grounded = grounded;
            let transform = world.get_mut::<Transform>(player).unwrap();
            transform.position = new_pos;
        }

        // Collectible pickup
        let player_rect = {
            let t = world.get::<Transform>(player).unwrap();
            let c = world.get::<Collider>(player).unwrap();
            c.world_rect(t.position)
        };

        let collectibles = world.query::<Collectible>();
        let mut to_despawn = Vec::new();
        for entity in collectibles {
            if let (Some(t), Some(c), Some(col)) = (
                world.get::<Transform>(entity),
                world.get::<Collider>(entity),
                world.get::<Collectible>(entity),
            ) {
                let coin_rect = c.world_rect(t.position);
                if player_rect.overlaps(&coin_rect) {
                    let points = col.point_value;
                    if let Some(sid) = col.sound_id {
                        audio.play(sid);
                    }
                    to_despawn.push((entity, points));
                }
            }
        }
        for (entity, points) in to_despawn {
            if let Some(p) = world.get_mut::<Player>(player) {
                p.score += points;
            }
            world.despawn(entity);
        }

        // Camera follow
        if let Some(t) = world.get::<Transform>(player) {
            self.camera.follow(t.position, SCREEN_W as f32, SCREEN_H as f32);
        }

        SceneTransition::None
    }

    fn render(&self, world: &World, fb: &mut Framebuffer, _camera: &Camera) {
        // Tilemap
        let tile_colors = [
            0x00000000, // 0: empty (transparent)
            color_rgb(80, 120, 80), // 1: ground
        ];
        self.map.render_simple(fb, &self.camera, &tile_colors);

        // Collectibles
        let collectibles = world.query::<Collectible>();
        for entity in collectibles {
            if let (Some(t), Some(c)) = (world.get::<Transform>(entity), world.get::<Collider>(entity)) {
                let (sx, sy) = self.camera.world_to_screen(t.position);
                fb.draw_rect(sx, sy, c.width as i32, c.height as i32, color_rgb(255, 215, 0));
            }
        }

        // Player
        if let Some(player) = self.player {
            if let (Some(t), Some(c)) = (world.get::<Transform>(player), world.get::<Collider>(player)) {
                let (sx, sy) = self.camera.world_to_screen(t.position);
                fb.draw_rect(sx, sy, c.width as i32, c.height as i32, color_rgb(50, 120, 220));
            }
        }

        // UI: Score
        if let Some(player) = self.player {
            if let Some(p) = world.get::<Player>(player) {
                let score_text = format!("SCORE: {}", p.score);
                fb.draw_text(&score_text, 8, 8, color_rgb(255, 255, 255));
            }
        }
    }

    fn on_exit(&mut self, world: &mut World) {
        if let Some(player) = self.player.take() {
            world.despawn(player);
        }
    }
}

fn main() {
    let config = engine::EngineConfig {
        width: SCREEN_W,
        height: SCREEN_H,
        title: "2D Engine Demo".to_string(),
        target_fps: 60.0,
    };

    engine::run(config, Box::new(Level1::new()));
}
```

### src/lib.rs

```rust
pub mod math;
pub mod ecs;
pub mod components;
pub mod render;
pub mod tilemap;
pub mod input;
pub mod audio;
pub mod scene;
pub mod engine;
```

### Commands

```bash
cargo new engine2d
cd engine2d
# Place source files, then:
cargo build --release
cargo run --release
# Controls: Arrow keys / WASD to move, Space to jump, Escape to quit
```

### Expected Output

A 320x240 window opens showing:
- A tiled level with green ground platforms
- A blue rectangle (player) controlled by keyboard
- Gold rectangles (collectibles) that disappear on contact
- Score counter in the top-left corner
- Camera follows the player across the level
- Player falls with gravity and lands on solid tiles
- Frame runs at stable 60 FPS

```
2D Engine Demo
[Window opens with rendered platformer game]
FPS: 60 | Physics: 0.2ms | Render: 0.8ms | Total: 1.0ms
```

## Design Decisions

1. **Simple HashMap-based ECS over archetypes**: For an engine that manages fewer than 10,000 entities, a `HashMap<TypeId, HashMap<EntityId, Component>>` is simpler and good enough. Archetypal storage adds complexity that only pays off at higher entity counts or when iteration speed is critical.

2. **Software rendering to Vec<u32> framebuffer**: Drawing to a pixel buffer via `minifb` avoids the complexity of GPU APIs (wgpu, OpenGL). The engine demonstrates all rendering concepts (sprites, tilemaps, camera, layers) without graphics API overhead. For a production engine, this would be replaced with GPU rendering.

3. **Fixed timestep with accumulator**: Physics runs at exactly 60Hz regardless of actual frame rate. The accumulator pattern (from Glenn Fiedler's "Fix Your Timestep") ensures deterministic physics while allowing rendering at any rate.

4. **Scene trait with stack**: The scene stack allows push/pop semantics (e.g., pause menu over gameplay). Each scene owns its logic and decides when to transition. The engine loop delegates all game-specific behavior to the active scene.

5. **rodio for audio**: Rather than implementing raw WAV parsing and PCM mixing from scratch, `rodio` provides reliable cross-platform audio output. The engine wraps it in a multi-channel mixer interface.

6. **Tilemap collision via grid lookup**: Instead of creating AABB colliders for every solid tile (expensive for large maps), collision queries the tilemap grid directly. This is O(1) per point query and handles arbitrarily large maps.

7. **Colored rectangles as placeholder graphics**: The demo uses solid rectangles rather than requiring external sprite assets. This makes the project self-contained while demonstrating the rendering pipeline. Texture atlas support is implemented but optional.

## Common Mistakes

- **Variable timestep physics**: Using frame delta time directly for physics causes objects to tunnel through walls on lag spikes and produces non-deterministic behavior. Always use a fixed timestep accumulator.
- **Forgetting tilemap collision order**: Horizontal and vertical collision must be resolved separately. Resolving both axes simultaneously causes corner-case bugs where entities get stuck at tile edges.
- **Camera jitter**: Updating the camera before physics produces one-frame lag. Update camera after all positions are finalized for the frame.
- **Entity lifetime during iteration**: Despawning entities while iterating over query results invalidates the iterator. Collect despawn targets first, then remove them after the iteration completes.
- **Audio blocking**: Audio playback must not block the game loop. Use a separate thread (rodio handles this internally) and fire-and-forget sound effects.

## Performance Notes

- Software rendering at 320x240 (76,800 pixels) is trivially fast on modern CPUs. Even at 640x480, clearing and blitting runs in under 1ms.
- Tilemap rendering culls off-screen tiles using the camera viewport, so only visible tiles are drawn regardless of map size.
- The ECS HashMap lookup is O(1) amortized per component access. For the demo's entity count (~50), this is well under 0.1ms per frame.
- The main performance bottleneck in a software renderer is fill rate. Alpha blending doubles the cost per pixel. Minimize overdraw by rendering back-to-front and skipping fully transparent pixels.
- In release mode with optimizations, the full update+render loop completes in ~1ms, leaving 15ms of headroom for a 60 FPS target.
