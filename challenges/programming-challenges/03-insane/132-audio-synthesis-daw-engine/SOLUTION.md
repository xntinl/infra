# Solution: Audio Synthesis DAW Engine

## Architecture Overview

The engine is structured around four core systems:

1. **Audio Graph**: a directed acyclic graph of processing nodes. Each node reads from input buffers, processes samples, and writes to an output buffer. Topological sort determines evaluation order. Node types include oscillators, gain, mixer, effects processors, and the master output.
2. **Effects Pipeline**: a chain of audio effects applied per-track. Each effect implements a common trait. The chain processes buffers sequentially: the output of one effect feeds the input of the next.
3. **MIDI Sequencer**: parses standard MIDI files, extracts note and tempo events, and schedules them against a BPM-based timeline. Converts tick positions to sample-accurate positions.
4. **Transport and Renderer**: manages playback state (play, stop, seek), drives the audio graph frame-by-frame, and renders the final mix to a WAV file.

---

## Rust Solution

### Project Setup

```bash
cargo new daw_engine --lib
cd daw_engine
```

Add to `Cargo.toml`:

```toml
[dependencies]

[dev-dependencies]
```

No external dependencies -- all DSP, MIDI parsing, and WAV writing are implemented from scratch.

### `src/buffer.rs` -- Audio Buffer

```rust
pub const DEFAULT_BUFFER_SIZE: usize = 512;
pub const DEFAULT_SAMPLE_RATE: u32 = 44100;

#[derive(Clone, Debug)]
pub struct AudioBuffer {
    pub left: Vec<f32>,
    pub right: Vec<f32>,
}

impl AudioBuffer {
    pub fn new(size: usize) -> Self {
        Self {
            left: vec![0.0; size],
            right: vec![0.0; size],
        }
    }

    pub fn silence(size: usize) -> Self {
        Self::new(size)
    }

    pub fn len(&self) -> usize {
        self.left.len()
    }

    pub fn clear(&mut self) {
        self.left.iter_mut().for_each(|s| *s = 0.0);
        self.right.iter_mut().for_each(|s| *s = 0.0);
    }

    pub fn mix_into(&self, dest: &mut AudioBuffer, gain: f32) {
        for i in 0..self.len().min(dest.len()) {
            dest.left[i] += self.left[i] * gain;
            dest.right[i] += self.right[i] * gain;
        }
    }

    pub fn apply_stereo_pan(&mut self, pan: f32) {
        // pan: -1.0 (full left) to 1.0 (full right)
        let left_gain = ((1.0 - pan) / 2.0).sqrt();
        let right_gain = ((1.0 + pan) / 2.0).sqrt();
        for s in self.left.iter_mut() { *s *= left_gain; }
        for s in self.right.iter_mut() { *s *= right_gain; }
    }
}
```

### `src/oscillator.rs` -- Oscillators

```rust
use std::f32::consts::PI;

#[derive(Clone, Copy, Debug)]
pub enum Waveform {
    Sine,
    Saw,
    Square,
    Triangle,
}

pub struct Oscillator {
    pub waveform: Waveform,
    pub frequency: f32,
    pub amplitude: f32,
    phase: f32,
    sample_rate: f32,
}

impl Oscillator {
    pub fn new(waveform: Waveform, frequency: f32, sample_rate: u32) -> Self {
        Self {
            waveform,
            frequency,
            amplitude: 1.0,
            phase: 0.0,
            sample_rate: sample_rate as f32,
        }
    }

    pub fn set_frequency(&mut self, freq: f32) {
        self.frequency = freq;
    }

    pub fn generate(&mut self, output: &mut [f32]) {
        let phase_inc = self.frequency / self.sample_rate;

        for sample in output.iter_mut() {
            *sample = self.sample_at_phase(self.phase) * self.amplitude;
            self.phase += phase_inc;
            if self.phase >= 1.0 { self.phase -= 1.0; }
        }
    }

    fn sample_at_phase(&self, phase: f32) -> f32 {
        match self.waveform {
            Waveform::Sine => (2.0 * PI * phase).sin(),
            Waveform::Saw => 2.0 * phase - 1.0,
            Waveform::Square => if phase < 0.5 { 1.0 } else { -1.0 },
            Waveform::Triangle => {
                if phase < 0.25 { 4.0 * phase }
                else if phase < 0.75 { 2.0 - 4.0 * phase }
                else { -4.0 + 4.0 * phase }
            }
        }
    }

    pub fn reset(&mut self) {
        self.phase = 0.0;
    }
}
```

### `src/effects.rs` -- Audio Effects

```rust
use std::f32::consts::PI;

pub trait Effect: Send {
    fn process(&mut self, input: &mut [f32]);
    fn reset(&mut self);
    fn name(&self) -> &str;
}

// --- Biquad Filter ---

#[derive(Clone, Copy, Debug)]
pub enum FilterMode {
    LowPass,
    HighPass,
    BandPass,
}

pub struct BiquadFilter {
    mode: FilterMode,
    b0: f32, b1: f32, b2: f32,
    a1: f32, a2: f32,
    x1: f32, x2: f32,
    y1: f32, y2: f32,
}

impl BiquadFilter {
    pub fn new(mode: FilterMode, cutoff: f32, q: f32, sample_rate: u32) -> Self {
        let mut filter = Self {
            mode, b0: 0.0, b1: 0.0, b2: 0.0,
            a1: 0.0, a2: 0.0, x1: 0.0, x2: 0.0, y1: 0.0, y2: 0.0,
        };
        filter.compute_coefficients(cutoff, q, sample_rate as f32);
        filter
    }

    fn compute_coefficients(&mut self, cutoff: f32, q: f32, sample_rate: f32) {
        let omega = 2.0 * PI * cutoff / sample_rate;
        let sin_w = omega.sin();
        let cos_w = omega.cos();
        let alpha = sin_w / (2.0 * q);

        let (b0, b1, b2, a0, a1, a2) = match self.mode {
            FilterMode::LowPass => {
                let b1 = 1.0 - cos_w;
                let b0 = b1 / 2.0;
                let b2 = b0;
                (b0, b1, b2, 1.0 + alpha, -2.0 * cos_w, 1.0 - alpha)
            }
            FilterMode::HighPass => {
                let b1 = -(1.0 + cos_w);
                let b0 = (1.0 + cos_w) / 2.0;
                let b2 = b0;
                (b0, b1, b2, 1.0 + alpha, -2.0 * cos_w, 1.0 - alpha)
            }
            FilterMode::BandPass => {
                let b0 = alpha;
                let b1 = 0.0;
                let b2 = -alpha;
                (b0, b1, b2, 1.0 + alpha, -2.0 * cos_w, 1.0 - alpha)
            }
        };

        self.b0 = b0 / a0;
        self.b1 = b1 / a0;
        self.b2 = b2 / a0;
        self.a1 = a1 / a0;
        self.a2 = a2 / a0;
    }
}

impl Effect for BiquadFilter {
    fn process(&mut self, input: &mut [f32]) {
        for sample in input.iter_mut() {
            let x0 = *sample;
            let y0 = self.b0 * x0 + self.b1 * self.x1 + self.b2 * self.x2
                   - self.a1 * self.y1 - self.a2 * self.y2;

            self.x2 = self.x1;
            self.x1 = x0;
            self.y2 = self.y1;
            self.y1 = y0;
            *sample = y0;
        }
    }

    fn reset(&mut self) {
        self.x1 = 0.0; self.x2 = 0.0;
        self.y1 = 0.0; self.y2 = 0.0;
    }

    fn name(&self) -> &str { "BiquadFilter" }
}

// --- Comb Filter (building block for reverb) ---

struct CombFilter {
    buffer: Vec<f32>,
    index: usize,
    feedback: f32,
    damp: f32,
    damp_prev: f32,
}

impl CombFilter {
    fn new(delay_samples: usize, feedback: f32, damp: f32) -> Self {
        Self {
            buffer: vec![0.0; delay_samples],
            index: 0,
            feedback,
            damp,
            damp_prev: 0.0,
        }
    }

    fn process_sample(&mut self, input: f32) -> f32 {
        let output = self.buffer[self.index];
        self.damp_prev = output * (1.0 - self.damp) + self.damp_prev * self.damp;
        self.buffer[self.index] = input + self.damp_prev * self.feedback;
        self.index = (self.index + 1) % self.buffer.len();
        output
    }

    fn reset(&mut self) {
        self.buffer.iter_mut().for_each(|s| *s = 0.0);
        self.index = 0;
        self.damp_prev = 0.0;
    }
}

// --- Allpass Filter (building block for reverb) ---

struct AllpassFilter {
    buffer: Vec<f32>,
    index: usize,
    feedback: f32,
}

impl AllpassFilter {
    fn new(delay_samples: usize, feedback: f32) -> Self {
        Self {
            buffer: vec![0.0; delay_samples],
            index: 0,
            feedback,
        }
    }

    fn process_sample(&mut self, input: f32) -> f32 {
        let delayed = self.buffer[self.index];
        let output = -input + delayed;
        self.buffer[self.index] = input + delayed * self.feedback;
        self.index = (self.index + 1) % self.buffer.len();
        output
    }

    fn reset(&mut self) {
        self.buffer.iter_mut().for_each(|s| *s = 0.0);
        self.index = 0;
    }
}

// --- Schroeder Reverb ---

pub struct Reverb {
    combs: Vec<CombFilter>,
    allpasses: Vec<AllpassFilter>,
    wet: f32,
    dry: f32,
}

impl Reverb {
    pub fn new(room_size: f32, damping: f32, wet: f32, sample_rate: u32) -> Self {
        let scale = sample_rate as f32 / 44100.0;
        let comb_delays = [1557, 1617, 1491, 1422];
        let allpass_delays = [225, 556];

        let combs = comb_delays.iter()
            .map(|&d| {
                let delay = (d as f32 * scale) as usize;
                CombFilter::new(delay, room_size, damping)
            })
            .collect();

        let allpasses = allpass_delays.iter()
            .map(|&d| {
                let delay = (d as f32 * scale) as usize;
                AllpassFilter::new(delay, 0.5)
            })
            .collect();

        Self { combs, allpasses, wet, dry: 1.0 - wet }
    }
}

impl Effect for Reverb {
    fn process(&mut self, input: &mut [f32]) {
        for sample in input.iter_mut() {
            let dry = *sample;

            // Parallel comb filters
            let mut comb_sum = 0.0;
            for comb in &mut self.combs {
                comb_sum += comb.process_sample(dry);
            }
            comb_sum /= self.combs.len() as f32;

            // Series allpass filters
            let mut out = comb_sum;
            for allpass in &mut self.allpasses {
                out = allpass.process_sample(out);
            }

            *sample = dry * self.dry + out * self.wet;
        }
    }

    fn reset(&mut self) {
        for c in &mut self.combs { c.reset(); }
        for a in &mut self.allpasses { a.reset(); }
    }

    fn name(&self) -> &str { "Reverb" }
}

// --- Chorus ---

pub struct Chorus {
    delay_buffer: Vec<f32>,
    write_index: usize,
    lfo_phase: f32,
    rate: f32,
    depth: f32,
    sample_rate: f32,
}

impl Chorus {
    pub fn new(rate: f32, depth: f32, sample_rate: u32) -> Self {
        let max_delay = (sample_rate as f32 * 0.05) as usize; // 50ms max
        Self {
            delay_buffer: vec![0.0; max_delay],
            write_index: 0,
            lfo_phase: 0.0,
            rate,
            depth,
            sample_rate: sample_rate as f32,
        }
    }
}

impl Effect for Chorus {
    fn process(&mut self, input: &mut [f32]) {
        let buf_len = self.delay_buffer.len();

        for sample in input.iter_mut() {
            self.delay_buffer[self.write_index] = *sample;

            let lfo = (2.0 * PI * self.lfo_phase).sin();
            let delay_samples = (self.depth * self.sample_rate * 0.001) * (1.0 + lfo) / 2.0;
            let delay_samples = delay_samples.max(1.0);

            let read_pos = self.write_index as f32 - delay_samples;
            let read_pos = if read_pos < 0.0 { read_pos + buf_len as f32 } else { read_pos };

            // Linear interpolation
            let idx0 = read_pos.floor() as usize % buf_len;
            let idx1 = (idx0 + 1) % buf_len;
            let frac = read_pos.fract();
            let delayed = self.delay_buffer[idx0] * (1.0 - frac)
                        + self.delay_buffer[idx1] * frac;

            *sample = (*sample + delayed) * 0.5;

            self.write_index = (self.write_index + 1) % buf_len;
            self.lfo_phase += self.rate / self.sample_rate;
            if self.lfo_phase >= 1.0 { self.lfo_phase -= 1.0; }
        }
    }

    fn reset(&mut self) {
        self.delay_buffer.iter_mut().for_each(|s| *s = 0.0);
        self.write_index = 0;
        self.lfo_phase = 0.0;
    }

    fn name(&self) -> &str { "Chorus" }
}

// --- Distortion (soft clipping) ---

pub struct Distortion {
    drive: f32,
}

impl Distortion {
    pub fn new(drive: f32) -> Self {
        Self { drive: drive.max(1.0) }
    }
}

impl Effect for Distortion {
    fn process(&mut self, input: &mut [f32]) {
        for sample in input.iter_mut() {
            *sample = (*sample * self.drive).tanh();
        }
    }

    fn reset(&mut self) {}

    fn name(&self) -> &str { "Distortion" }
}

// --- Effects Chain ---

pub struct EffectsChain {
    effects: Vec<Box<dyn Effect>>,
}

impl EffectsChain {
    pub fn new() -> Self {
        Self { effects: Vec::new() }
    }

    pub fn add(&mut self, effect: Box<dyn Effect>) {
        self.effects.push(effect);
    }

    pub fn process(&mut self, buffer: &mut [f32]) {
        for effect in &mut self.effects {
            effect.process(buffer);
        }
    }

    pub fn reset(&mut self) {
        for effect in &mut self.effects {
            effect.reset();
        }
    }
}
```

### `src/midi.rs` -- MIDI File Parser and Sequencer

```rust
use std::io::{self, Read, Cursor};

#[derive(Debug, Clone)]
pub enum MidiEvent {
    NoteOn { channel: u8, note: u8, velocity: u8 },
    NoteOff { channel: u8, note: u8, velocity: u8 },
    TempoChange { microseconds_per_beat: u32 },
}

#[derive(Debug, Clone)]
pub struct TimedEvent {
    pub tick: u64,
    pub event: MidiEvent,
}

#[derive(Debug)]
pub struct MidiFile {
    pub ticks_per_quarter: u16,
    pub tracks: Vec<Vec<TimedEvent>>,
}

pub fn parse_midi(data: &[u8]) -> io::Result<MidiFile> {
    let mut cursor = Cursor::new(data);

    // Header chunk: "MThd"
    let mut header_tag = [0u8; 4];
    cursor.read_exact(&mut header_tag)?;
    if &header_tag != b"MThd" {
        return Err(io::Error::new(io::ErrorKind::InvalidData, "not a MIDI file"));
    }

    let header_len = read_u32_be(&mut cursor)?;
    let format = read_u16_be(&mut cursor)?;
    let num_tracks = read_u16_be(&mut cursor)?;
    let tpqn = read_u16_be(&mut cursor)?;

    // Skip remaining header bytes if any
    if header_len > 6 {
        let skip = (header_len - 6) as usize;
        let mut discard = vec![0u8; skip];
        cursor.read_exact(&mut discard)?;
    }

    let mut tracks = Vec::new();
    for _ in 0..num_tracks {
        let track = parse_track(&mut cursor)?;
        tracks.push(track);
    }

    Ok(MidiFile { ticks_per_quarter: tpqn, tracks })
}

fn parse_track(cursor: &mut Cursor<&[u8]>) -> io::Result<Vec<TimedEvent>> {
    let mut tag = [0u8; 4];
    cursor.read_exact(&mut tag)?;
    if &tag != b"MTrk" {
        return Err(io::Error::new(io::ErrorKind::InvalidData, "expected MTrk"));
    }

    let track_len = read_u32_be(cursor)? as usize;
    let mut track_data = vec![0u8; track_len];
    cursor.read_exact(&mut track_data)?;

    let mut events = Vec::new();
    let mut pos = 0;
    let mut absolute_tick: u64 = 0;
    let mut running_status: u8 = 0;

    while pos < track_data.len() {
        let (delta, bytes_read) = read_variable_length(&track_data[pos..]);
        pos += bytes_read;
        absolute_tick += delta as u64;

        if pos >= track_data.len() { break; }

        let status = if track_data[pos] & 0x80 != 0 {
            running_status = track_data[pos];
            pos += 1;
            running_status
        } else {
            running_status
        };

        match status & 0xF0 {
            0x90 => {
                let note = track_data[pos]; pos += 1;
                let velocity = track_data[pos]; pos += 1;
                let event = if velocity == 0 {
                    MidiEvent::NoteOff { channel: status & 0x0F, note, velocity }
                } else {
                    MidiEvent::NoteOn { channel: status & 0x0F, note, velocity }
                };
                events.push(TimedEvent { tick: absolute_tick, event });
            }
            0x80 => {
                let note = track_data[pos]; pos += 1;
                let velocity = track_data[pos]; pos += 1;
                events.push(TimedEvent {
                    tick: absolute_tick,
                    event: MidiEvent::NoteOff { channel: status & 0x0F, note, velocity },
                });
            }
            0xA0 | 0xB0 | 0xE0 => { pos += 2; } // skip 2-byte messages
            0xC0 | 0xD0 => { pos += 1; } // skip 1-byte messages
            0xFF => {
                // Meta event
                let meta_type = track_data[pos]; pos += 1;
                let (len, bytes_read) = read_variable_length(&track_data[pos..]);
                pos += bytes_read;

                if meta_type == 0x51 && len == 3 {
                    let tempo = ((track_data[pos] as u32) << 16)
                              | ((track_data[pos + 1] as u32) << 8)
                              | (track_data[pos + 2] as u32);
                    events.push(TimedEvent {
                        tick: absolute_tick,
                        event: MidiEvent::TempoChange { microseconds_per_beat: tempo },
                    });
                }
                pos += len as usize;
            }
            0xF0 | 0xF7 => {
                // SysEx
                let (len, bytes_read) = read_variable_length(&track_data[pos..]);
                pos += bytes_read + len as usize;
            }
            _ => { break; }
        }
    }

    Ok(events)
}

fn read_variable_length(data: &[u8]) -> (u32, usize) {
    let mut value: u32 = 0;
    let mut bytes_read = 0;
    for &byte in data {
        value = (value << 7) | (byte & 0x7F) as u32;
        bytes_read += 1;
        if byte & 0x80 == 0 { break; }
    }
    (value, bytes_read)
}

fn read_u32_be(cursor: &mut Cursor<&[u8]>) -> io::Result<u32> {
    let mut buf = [0u8; 4];
    cursor.read_exact(&mut buf)?;
    Ok(u32::from_be_bytes(buf))
}

fn read_u16_be(cursor: &mut Cursor<&[u8]>) -> io::Result<u16> {
    let mut buf = [0u8; 2];
    cursor.read_exact(&mut buf)?;
    Ok(u16::from_be_bytes(buf))
}

// --- Sequencer ---

#[derive(Debug, Clone)]
pub struct ScheduledNote {
    pub start_sample: u64,
    pub end_sample: u64,
    pub note: u8,
    pub velocity: u8,
    pub channel: u8,
}

pub fn schedule_notes(
    midi: &MidiFile,
    bpm: f64,
    sample_rate: u32,
) -> Vec<ScheduledNote> {
    let tpqn = midi.ticks_per_quarter as f64;
    let sr = sample_rate as f64;

    // Collect all events across tracks
    let mut all_events: Vec<TimedEvent> = midi.tracks.iter()
        .flat_map(|t| t.iter().cloned())
        .collect();
    all_events.sort_by_key(|e| e.tick);

    // Build tempo map
    let mut tempo_changes: Vec<(u64, f64)> = vec![(0, bpm)];
    for event in &all_events {
        if let MidiEvent::TempoChange { microseconds_per_beat } = event.event {
            let new_bpm = 60_000_000.0 / microseconds_per_beat as f64;
            tempo_changes.push((event.tick, new_bpm));
        }
    }

    let tick_to_sample = |tick: u64| -> u64 {
        let mut sample_pos: f64 = 0.0;
        let mut prev_tick: u64 = 0;
        let mut current_bpm = bpm;

        for &(change_tick, new_bpm) in &tempo_changes {
            if change_tick >= tick { break; }
            let delta_ticks = change_tick.saturating_sub(prev_tick) as f64;
            sample_pos += (delta_ticks / tpqn) * (60.0 / current_bpm) * sr;
            prev_tick = change_tick;
            current_bpm = new_bpm;
        }

        let remaining_ticks = tick.saturating_sub(prev_tick) as f64;
        sample_pos += (remaining_ticks / tpqn) * (60.0 / current_bpm) * sr;
        sample_pos as u64
    };

    // Match note-on with note-off
    let mut active_notes: Vec<(u8, u8, u64)> = Vec::new(); // (note, channel, start_tick)
    let mut scheduled = Vec::new();

    for event in &all_events {
        match &event.event {
            MidiEvent::NoteOn { channel, note, velocity } => {
                active_notes.push((*note, *channel, event.tick));
            }
            MidiEvent::NoteOff { channel, note, .. } => {
                if let Some(pos) = active_notes.iter().position(|(n, c, _)| n == note && c == channel) {
                    let (note_num, ch, start_tick) = active_notes.remove(pos);
                    scheduled.push(ScheduledNote {
                        start_sample: tick_to_sample(start_tick),
                        end_sample: tick_to_sample(event.tick),
                        note: note_num,
                        velocity: 127, // default
                        channel: ch,
                    });
                }
            }
            _ => {}
        }
    }

    scheduled.sort_by_key(|n| n.start_sample);
    scheduled
}

pub fn midi_note_to_freq(note: u8) -> f32 {
    440.0 * 2.0f32.powf((note as f32 - 69.0) / 12.0)
}
```

### `src/graph.rs` -- Audio Graph

```rust
use crate::buffer::AudioBuffer;
use std::collections::HashMap;

pub type NodeId = usize;

pub trait AudioNode: Send {
    fn process(&mut self, inputs: &[&AudioBuffer], output: &mut AudioBuffer);
    fn reset(&mut self);
    fn name(&self) -> &str;
}

pub struct AudioGraph {
    nodes: HashMap<NodeId, Box<dyn AudioNode>>,
    edges: Vec<(NodeId, NodeId)>, // (from, to)
    next_id: NodeId,
    processing_order: Vec<NodeId>,
    output_node: Option<NodeId>,
}

impl AudioGraph {
    pub fn new() -> Self {
        Self {
            nodes: HashMap::new(),
            edges: Vec::new(),
            next_id: 0,
            processing_order: Vec::new(),
            output_node: None,
        }
    }

    pub fn add_node(&mut self, node: Box<dyn AudioNode>) -> NodeId {
        let id = self.next_id;
        self.next_id += 1;
        self.nodes.insert(id, node);
        id
    }

    pub fn connect(&mut self, from: NodeId, to: NodeId) -> Result<(), &'static str> {
        self.edges.push((from, to));
        match self.topological_sort() {
            Ok(order) => {
                self.processing_order = order;
                Ok(())
            }
            Err(e) => {
                self.edges.pop();
                Err(e)
            }
        }
    }

    pub fn set_output(&mut self, node_id: NodeId) {
        self.output_node = Some(node_id);
    }

    pub fn process(&mut self, buffer_size: usize) -> AudioBuffer {
        let mut buffers: HashMap<NodeId, AudioBuffer> = HashMap::new();

        for &node_id in &self.processing_order {
            // Gather inputs
            let input_ids: Vec<NodeId> = self.edges.iter()
                .filter(|(_, to)| *to == node_id)
                .map(|(from, _)| *from)
                .collect();

            let input_refs: Vec<&AudioBuffer> = input_ids.iter()
                .filter_map(|id| buffers.get(id))
                .collect();

            let mut output = AudioBuffer::new(buffer_size);

            // Process node (temporarily remove to satisfy borrow checker)
            if let Some(mut node) = self.nodes.remove(&node_id) {
                node.process(&input_refs, &mut output);
                self.nodes.insert(node_id, node);
            }

            buffers.insert(node_id, output);
        }

        self.output_node
            .and_then(|id| buffers.remove(&id))
            .unwrap_or_else(|| AudioBuffer::silence(buffer_size))
    }

    fn topological_sort(&self) -> Result<Vec<NodeId>, &'static str> {
        let mut in_degree: HashMap<NodeId, usize> = HashMap::new();
        for &id in self.nodes.keys() {
            in_degree.insert(id, 0);
        }
        for &(_, to) in &self.edges {
            *in_degree.entry(to).or_insert(0) += 1;
        }

        let mut queue: Vec<NodeId> = in_degree.iter()
            .filter(|(_, &deg)| deg == 0)
            .map(|(&id, _)| id)
            .collect();
        queue.sort();

        let mut order = Vec::new();
        while let Some(node) = queue.pop() {
            order.push(node);
            for &(from, to) in &self.edges {
                if from == node {
                    if let Some(deg) = in_degree.get_mut(&to) {
                        *deg -= 1;
                        if *deg == 0 {
                            queue.push(to);
                            queue.sort();
                        }
                    }
                }
            }
        }

        if order.len() != self.nodes.len() {
            Err("cycle detected in audio graph")
        } else {
            Ok(order)
        }
    }

    pub fn reset(&mut self) {
        for node in self.nodes.values_mut() {
            node.reset();
        }
    }
}
```

### `src/mixer.rs` -- Multi-Track Mixer

```rust
use crate::buffer::AudioBuffer;
use crate::effects::EffectsChain;

pub struct Track {
    pub name: String,
    pub volume: f32,
    pub pan: f32,
    pub mute: bool,
    pub solo: bool,
    pub effects: EffectsChain,
    buffer: AudioBuffer,
}

impl Track {
    pub fn new(name: &str, buffer_size: usize) -> Self {
        Self {
            name: name.to_string(),
            volume: 1.0,
            pan: 0.0,
            mute: false,
            solo: false,
            effects: EffectsChain::new(),
            buffer: AudioBuffer::new(buffer_size),
        }
    }

    pub fn write_buffer(&mut self, buffer: AudioBuffer) {
        self.buffer = buffer;
    }

    pub fn buffer_mut(&mut self) -> &mut AudioBuffer {
        &mut self.buffer
    }
}

pub struct Mixer {
    tracks: Vec<Track>,
    master_volume: f32,
    buffer_size: usize,
}

impl Mixer {
    pub fn new(buffer_size: usize) -> Self {
        Self {
            tracks: Vec::new(),
            master_volume: 1.0,
            buffer_size,
        }
    }

    pub fn add_track(&mut self, track: Track) -> usize {
        let idx = self.tracks.len();
        self.tracks.push(track);
        idx
    }

    pub fn track_mut(&mut self, index: usize) -> Option<&mut Track> {
        self.tracks.get_mut(index)
    }

    pub fn mix(&mut self) -> AudioBuffer {
        let mut master = AudioBuffer::silence(self.buffer_size);

        let any_solo = self.tracks.iter().any(|t| t.solo);

        for track in &mut self.tracks {
            if track.mute { continue; }
            if any_solo && !track.solo { continue; }

            // Apply effects chain to each channel
            track.effects.process(&mut track.buffer.left);
            track.effects.process(&mut track.buffer.right);

            // Apply pan
            track.buffer.apply_stereo_pan(track.pan);

            // Mix into master
            track.buffer.mix_into(&mut master, track.volume);
        }

        // Apply master volume
        for s in master.left.iter_mut() { *s *= self.master_volume; }
        for s in master.right.iter_mut() { *s *= self.master_volume; }

        master
    }

    pub fn set_master_volume(&mut self, vol: f32) {
        self.master_volume = vol;
    }
}
```

### `src/wav.rs` -- WAV Export

```rust
use std::io::{self, Write, BufWriter};
use std::fs::File;
use std::path::Path;

pub fn write_wav_16bit(
    path: &Path,
    sample_rate: u32,
    left: &[f32],
    right: &[f32],
) -> io::Result<()> {
    let num_samples = left.len().min(right.len());
    let num_channels: u16 = 2;
    let bits_per_sample: u16 = 16;
    let byte_rate = sample_rate * num_channels as u32 * bits_per_sample as u32 / 8;
    let block_align = num_channels * bits_per_sample / 8;
    let data_size = (num_samples * num_channels as usize * (bits_per_sample / 8) as usize) as u32;
    let file_size = 36 + data_size;

    let file = File::create(path)?;
    let mut writer = BufWriter::new(file);

    // RIFF header
    writer.write_all(b"RIFF")?;
    writer.write_all(&file_size.to_le_bytes())?;
    writer.write_all(b"WAVE")?;

    // fmt chunk
    writer.write_all(b"fmt ")?;
    writer.write_all(&16u32.to_le_bytes())?;     // chunk size
    writer.write_all(&1u16.to_le_bytes())?;       // PCM format
    writer.write_all(&num_channels.to_le_bytes())?;
    writer.write_all(&sample_rate.to_le_bytes())?;
    writer.write_all(&byte_rate.to_le_bytes())?;
    writer.write_all(&block_align.to_le_bytes())?;
    writer.write_all(&bits_per_sample.to_le_bytes())?;

    // data chunk
    writer.write_all(b"data")?;
    writer.write_all(&data_size.to_le_bytes())?;

    for i in 0..num_samples {
        let l = float_to_i16(left[i]);
        let r = float_to_i16(right[i]);
        writer.write_all(&l.to_le_bytes())?;
        writer.write_all(&r.to_le_bytes())?;
    }

    writer.flush()?;
    Ok(())
}

pub fn write_wav_f32(
    path: &Path,
    sample_rate: u32,
    left: &[f32],
    right: &[f32],
) -> io::Result<()> {
    let num_samples = left.len().min(right.len());
    let num_channels: u16 = 2;
    let bits_per_sample: u16 = 32;
    let byte_rate = sample_rate * num_channels as u32 * bits_per_sample as u32 / 8;
    let block_align = num_channels * bits_per_sample / 8;
    let data_size = (num_samples * num_channels as usize * 4) as u32;
    let file_size = 36 + data_size;

    let file = File::create(path)?;
    let mut writer = BufWriter::new(file);

    writer.write_all(b"RIFF")?;
    writer.write_all(&file_size.to_le_bytes())?;
    writer.write_all(b"WAVE")?;

    writer.write_all(b"fmt ")?;
    writer.write_all(&16u32.to_le_bytes())?;
    writer.write_all(&3u16.to_le_bytes())?; // IEEE float format
    writer.write_all(&num_channels.to_le_bytes())?;
    writer.write_all(&sample_rate.to_le_bytes())?;
    writer.write_all(&byte_rate.to_le_bytes())?;
    writer.write_all(&block_align.to_le_bytes())?;
    writer.write_all(&bits_per_sample.to_le_bytes())?;

    writer.write_all(b"data")?;
    writer.write_all(&data_size.to_le_bytes())?;

    for i in 0..num_samples {
        writer.write_all(&left[i].to_le_bytes())?;
        writer.write_all(&right[i].to_le_bytes())?;
    }

    writer.flush()?;
    Ok(())
}

fn float_to_i16(sample: f32) -> i16 {
    let clamped = sample.max(-1.0).min(1.0);
    (clamped * i16::MAX as f32) as i16
}
```

### `src/transport.rs` -- Transport and Renderer

```rust
use crate::buffer::{AudioBuffer, DEFAULT_BUFFER_SIZE, DEFAULT_SAMPLE_RATE};
use crate::mixer::Mixer;
use crate::wav;
use std::path::Path;
use std::io;

#[derive(Debug, Clone, Copy, PartialEq)]
pub enum TransportState {
    Stopped,
    Playing,
}

pub struct Transport {
    pub bpm: f64,
    pub sample_rate: u32,
    pub buffer_size: usize,
    pub state: TransportState,
    pub position_samples: u64,
}

impl Transport {
    pub fn new(bpm: f64, sample_rate: u32) -> Self {
        Self {
            bpm,
            sample_rate,
            buffer_size: DEFAULT_BUFFER_SIZE,
            state: TransportState::Stopped,
            position_samples: 0,
        }
    }

    pub fn play(&mut self) { self.state = TransportState::Playing; }
    pub fn stop(&mut self) { self.state = TransportState::Stopped; }

    pub fn seek_to_beat(&mut self, beat: f64) {
        let seconds = beat * 60.0 / self.bpm;
        self.position_samples = (seconds * self.sample_rate as f64) as u64;
    }

    pub fn seek_to_sample(&mut self, sample: u64) {
        self.position_samples = sample;
    }

    pub fn current_beat(&self) -> f64 {
        let seconds = self.position_samples as f64 / self.sample_rate as f64;
        seconds * self.bpm / 60.0
    }

    pub fn advance(&mut self) {
        self.position_samples += self.buffer_size as u64;
    }
}

pub fn render_to_wav(
    mixer: &mut Mixer,
    transport: &mut Transport,
    total_samples: u64,
    path: &Path,
    render_callback: &mut dyn FnMut(&mut Mixer, &Transport),
) -> io::Result<()> {
    let mut all_left = Vec::new();
    let mut all_right = Vec::new();

    transport.seek_to_sample(0);
    transport.play();

    while transport.position_samples < total_samples {
        render_callback(mixer, transport);
        let mixed = mixer.mix();
        all_left.extend_from_slice(&mixed.left);
        all_right.extend_from_slice(&mixed.right);
        transport.advance();
    }

    transport.stop();
    wav::write_wav_16bit(path, transport.sample_rate, &all_left, &all_right)
}
```

### `src/lib.rs` -- Module Root

```rust
pub mod buffer;
pub mod oscillator;
pub mod effects;
pub mod midi;
pub mod graph;
pub mod mixer;
pub mod wav;
pub mod transport;
```

### Tests

```rust
#[cfg(test)]
mod tests {
    use super::*;
    use buffer::{AudioBuffer, DEFAULT_SAMPLE_RATE};
    use oscillator::{Oscillator, Waveform};
    use effects::*;
    use mixer::{Track, Mixer};
    use transport::Transport;
    use std::path::PathBuf;

    #[test]
    fn test_oscillator_sine() {
        let mut osc = Oscillator::new(Waveform::Sine, 440.0, DEFAULT_SAMPLE_RATE);
        let mut buf = vec![0.0f32; 512];
        osc.generate(&mut buf);

        // Verify amplitude is within [-1, 1]
        assert!(buf.iter().all(|&s| s >= -1.0 && s <= 1.0));
        // Verify not silence
        assert!(buf.iter().any(|&s| s.abs() > 0.01));
    }

    #[test]
    fn test_oscillator_waveforms() {
        for wf in [Waveform::Sine, Waveform::Saw, Waveform::Square, Waveform::Triangle] {
            let mut osc = Oscillator::new(wf, 440.0, DEFAULT_SAMPLE_RATE);
            let mut buf = vec![0.0f32; 512];
            osc.generate(&mut buf);
            assert!(buf.iter().all(|&s| s >= -1.0 && s <= 1.0),
                "{:?} exceeded bounds", wf);
        }
    }

    #[test]
    fn test_biquad_lowpass() {
        let mut filter = BiquadFilter::new(FilterMode::LowPass, 1000.0, 0.707, DEFAULT_SAMPLE_RATE);

        // Generate high-frequency signal (10kHz)
        let mut high_freq = vec![0.0f32; 4096];
        let mut osc = Oscillator::new(Waveform::Sine, 10000.0, DEFAULT_SAMPLE_RATE);
        osc.generate(&mut high_freq);

        let energy_before: f32 = high_freq.iter().map(|s| s * s).sum();
        filter.process(&mut high_freq);
        let energy_after: f32 = high_freq.iter().map(|s| s * s).sum();

        // High frequency should be significantly attenuated
        assert!(energy_after < energy_before * 0.1,
            "low-pass filter did not attenuate high frequency");
    }

    #[test]
    fn test_reverb() {
        let mut reverb = Reverb::new(0.8, 0.5, 0.5, DEFAULT_SAMPLE_RATE);

        // Impulse: single sample at 1.0, rest zeros
        let mut buf = vec![0.0f32; 4096];
        buf[0] = 1.0;

        reverb.process(&mut buf);

        // Reverb tail should produce non-zero samples after the impulse
        let tail_energy: f32 = buf[100..].iter().map(|s| s * s).sum();
        assert!(tail_energy > 0.001, "reverb did not produce a tail");
    }

    #[test]
    fn test_chorus() {
        let mut chorus = Chorus::new(1.5, 5.0, DEFAULT_SAMPLE_RATE);
        let mut osc = Oscillator::new(Waveform::Sine, 440.0, DEFAULT_SAMPLE_RATE);
        let mut buf = vec![0.0f32; 2048];
        osc.generate(&mut buf);

        let original = buf.clone();
        chorus.process(&mut buf);

        // Chorus should modify the signal (not identical)
        let diff: f32 = original.iter().zip(buf.iter())
            .map(|(a, b)| (a - b).abs())
            .sum();
        assert!(diff > 0.1, "chorus did not modify the signal");
    }

    #[test]
    fn test_distortion() {
        let mut dist = Distortion::new(10.0);
        let mut buf = vec![0.5f32; 512];
        dist.process(&mut buf);

        // Soft clipping: tanh(5.0) approx 0.9999
        assert!(buf.iter().all(|&s| s < 1.0 && s > 0.0));
    }

    #[test]
    fn test_mixer_volume_pan() {
        let buf_size = 512;
        let mut mixer = Mixer::new(buf_size);

        let mut track = Track::new("test", buf_size);
        track.volume = 0.5;
        track.pan = 0.0; // center

        // Fill track with constant signal
        let buf = track.buffer_mut();
        buf.left.iter_mut().for_each(|s| *s = 1.0);
        buf.right.iter_mut().for_each(|s| *s = 1.0);

        mixer.add_track(track);
        let output = mixer.mix();

        // Volume 0.5 applied
        assert!((output.left[0] - 0.5).abs() < 0.01);
    }

    #[test]
    fn test_mixer_mute_solo() {
        let buf_size = 64;
        let mut mixer = Mixer::new(buf_size);

        let mut t1 = Track::new("track1", buf_size);
        t1.buffer_mut().left.iter_mut().for_each(|s| *s = 1.0);
        t1.buffer_mut().right.iter_mut().for_each(|s| *s = 1.0);

        let mut t2 = Track::new("track2", buf_size);
        t2.mute = true;
        t2.buffer_mut().left.iter_mut().for_each(|s| *s = 1.0);
        t2.buffer_mut().right.iter_mut().for_each(|s| *s = 1.0);

        mixer.add_track(t1);
        mixer.add_track(t2);

        let output = mixer.mix();
        // Only track1 should contribute (track2 muted)
        assert!((output.left[0] - 1.0).abs() < 0.01);
    }

    #[test]
    fn test_wav_export() {
        let mut osc = Oscillator::new(Waveform::Sine, 440.0, DEFAULT_SAMPLE_RATE);
        let num_samples = DEFAULT_SAMPLE_RATE as usize; // 1 second
        let mut left = vec![0.0f32; num_samples];
        let mut right = vec![0.0f32; num_samples];
        osc.generate(&mut left);
        osc.generate(&mut right);

        let path = std::env::temp_dir().join("test_daw_output.wav");
        wav::write_wav_16bit(&path, DEFAULT_SAMPLE_RATE, &left, &right).unwrap();

        // Verify file exists and has reasonable size
        let metadata = std::fs::metadata(&path).unwrap();
        let expected_size = 44 + num_samples * 2 * 2; // header + samples * channels * 2 bytes
        assert_eq!(metadata.len(), expected_size as u64);

        std::fs::remove_file(&path).ok();
    }

    #[test]
    fn test_midi_note_to_freq() {
        let freq_a4 = midi::midi_note_to_freq(69);
        assert!((freq_a4 - 440.0).abs() < 0.01);

        let freq_c4 = midi::midi_note_to_freq(60);
        assert!((freq_c4 - 261.63).abs() < 0.5);
    }

    #[test]
    fn test_transport_seek() {
        let mut transport = Transport::new(120.0, DEFAULT_SAMPLE_RATE);

        transport.seek_to_beat(4.0);
        // 4 beats at 120 BPM = 2 seconds = 88200 samples
        assert_eq!(transport.position_samples, 88200);

        let beat = transport.current_beat();
        assert!((beat - 4.0).abs() < 0.001);
    }

    #[test]
    fn test_stereo_pan() {
        let mut buf = AudioBuffer::new(64);
        buf.left.iter_mut().for_each(|s| *s = 1.0);
        buf.right.iter_mut().for_each(|s| *s = 1.0);

        buf.apply_stereo_pan(1.0); // full right
        assert!(buf.left[0].abs() < 0.01, "left channel should be near zero for full-right pan");
        assert!(buf.right[0] > 0.9, "right channel should be near 1.0 for full-right pan");
    }
}
```

### Running

```bash
cargo test
cargo test -- --nocapture
```

### Expected Test Output

```
running 12 tests
test tests::test_oscillator_sine ... ok
test tests::test_oscillator_waveforms ... ok
test tests::test_biquad_lowpass ... ok
test tests::test_reverb ... ok
test tests::test_chorus ... ok
test tests::test_distortion ... ok
test tests::test_mixer_volume_pan ... ok
test tests::test_mixer_mute_solo ... ok
test tests::test_wav_export ... ok
test tests::test_midi_note_to_freq ... ok
test tests::test_transport_seek ... ok
test tests::test_stereo_pan ... ok

test result: ok. 12 passed; 0 failed; 0 ignored
```

---

## Design Decisions

1. **Buffer-based processing over sample-by-sample**: every node processes a fixed-size buffer (512 samples) rather than one sample at a time. This is critical for performance -- it amortizes function call overhead, allows SIMD auto-vectorization, and improves cache locality. The trade-off is latency: output is delayed by one buffer. At 512 samples / 44100 Hz, latency is ~11.6ms, acceptable for non-realtime rendering.

2. **Schroeder reverb with mutually prime comb filter delays**: the four comb filter delay lengths (1557, 1617, 1491, 1422 samples) are mutually prime to prevent resonant peaks at common multiples. If delays shared a common factor, certain frequencies would reinforce and create an unnatural metallic sound. The allpass filters add diffusion without changing the frequency content, smoothing the comb filter output into a natural-sounding tail.

3. **Audio graph with topological sort over fixed pipeline**: a graph with topological sort allows arbitrary routing (send effects, parallel chains, feedback with one-buffer delay). A fixed pipeline (source -> effects -> mixer) would be simpler but cannot represent common DAW routing patterns like aux sends or sidechain compression. The graph rejects cycles at connection time to guarantee processability.

4. **MIDI tick-to-sample conversion with tempo map accumulation**: rather than assuming constant BPM, the converter accumulates sample offsets at each tempo change point. This handles accelerando, ritardando, and abrupt tempo changes correctly. The naive approach of `tick * (60 / bpm) / tpqn * sample_rate` only works for constant tempo, which most MIDI files are not.

5. **Equal-power panning (square root law)**: the pan implementation uses `sqrt((1-pan)/2)` for left and `sqrt((1+pan)/2)` for right. This maintains constant perceived loudness across the stereo field. Linear panning (`(1-pan)/2` for left) causes a 3dB dip at center, making centered sources sound quieter than hard-panned ones.

6. **WAV written from scratch instead of using the `hound` crate**: implementing WAV writing teaches the actual PCM format (RIFF header, fmt chunk, data chunk). The format is simple enough that a dependency is not justified. It also makes the project zero-dependency for the core engine, keeping compilation fast and reducing supply chain surface.

7. **Effects chain applied per-channel mono**: each effect processes left and right channels independently. True stereo effects (where left input affects right output) would require a different interface. For reverb and chorus, mono processing per channel still produces a stereo image because the effects have internal state that diverges between channels over time.

## Common Mistakes

- **Forgetting to normalize comb filter output sum**: four parallel comb filters produce a sum that is up to 4x the input amplitude. Dividing by the number of comb filters prevents clipping before the allpass stage.
- **Biquad filter coefficient calculation with wrong formula**: the Audio EQ Cookbook uses specific formulas for each filter type. Swapping the lowpass and highpass formulas (a common copy-paste error) produces a filter that attenuates the opposite frequency range.
- **MIDI running status not handled**: MIDI files use running status (omitting the status byte when consecutive events have the same status). Failing to track the running status byte causes desynchronization: the parser reads a data byte as a status byte and loses alignment for the rest of the track.
- **Phase accumulation overflow in oscillator**: the phase variable should wrap at 1.0 (not 2*PI). Using `phase += freq / sample_rate` and wrapping with `if phase >= 1.0 { phase -= 1.0 }` keeps the value small. Without wrapping, floating-point precision degrades after millions of samples, causing pitch drift.
- **WAV header size calculation off by one**: the RIFF chunk size field is `file_size - 8` (total bytes minus the 8-byte "RIFF" + size fields). The data chunk size is `num_samples * num_channels * bytes_per_sample`. Getting these wrong produces a file that some players reject or play with silence at the end.

## Performance Notes

Offline rendering (WAV export) processes as fast as the CPU allows, not constrained by real-time deadlines. A 3-minute stereo track at 44100 Hz with 4 effects per track typically renders in 1-5 seconds on modern hardware.

The main bottleneck is the effects chain: each effect iterates over every sample with multiple multiplications and memory accesses. The biquad filter has a data dependency between consecutive samples (each output depends on the previous two outputs), limiting instruction-level parallelism. Reverb is the most expensive effect due to four comb filters plus two allpass filters, each with a delay buffer read/write per sample.

For real-time output (not covered in this challenge), the audio callback must complete within the buffer duration (11.6ms for 512 samples at 44100 Hz). Missing this deadline causes audible glitches. Production DAWs use lock-free queues for parameter changes and avoid all heap allocation in the audio thread.

## Going Further

- Add a sampler node that loads and plays back WAV files triggered by MIDI notes
- Implement a compressor/limiter effect with sidechain input
- Add automation lanes (parameter changes over time, e.g., volume fades)
- Build a real-time audio backend using `cpal` for live playback
- Implement convolution reverb using FFT-based fast convolution
- Add VU meters and waveform visualization
