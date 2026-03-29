# Solution: WAV Audio Synthesizer

## Architecture Overview

The synthesizer is organized into five modules:

1. **wav**: WAV file writing -- RIFF header construction, PCM sample encoding, file output
2. **oscillator**: Four waveform generators (sine, square, sawtooth, triangle) with phase tracking
3. **envelope**: ADSR envelope state machine producing amplitude multipliers over time
4. **sequencer**: Note parsing, frequency calculation, timeline rendering into sample buffers
5. **effects**: Echo/delay line operating on sample buffers

```
Note Sequence (text)
     |
     v
 [Parser] --> Vec<Note> with frequency, duration, waveform, start_time
     |
     v
 [Sequencer] --> For each note:
     |            - Generate oscillator samples
     |            - Apply ADSR envelope
     |            - Place in timeline buffer
     v
 [Mixer] --> Sum all voice buffers, normalize peak to 1.0
     |
     v
 [Effects] --> Apply echo/delay
     |
     v
 [WAV Writer] --> Convert f64 -> i16, write header + data
     |
     v
 output.wav
```

## Rust Solution

### Cargo.toml

```toml
[package]
name = "wav-synth"
version = "0.1.0"
edition = "2021"

[[bin]]
name = "wav-synth"
path = "src/main.rs"
```

### src/wav.rs

```rust
use std::io::Write;

pub struct WavConfig {
    pub sample_rate: u32,
    pub channels: u16,
    pub bits_per_sample: u16,
}

impl WavConfig {
    pub fn mono_44100() -> Self {
        Self {
            sample_rate: 44100,
            channels: 1,
            bits_per_sample: 16,
        }
    }

    pub fn stereo_44100() -> Self {
        Self {
            sample_rate: 44100,
            channels: 2,
            bits_per_sample: 16,
        }
    }
}

pub fn write_wav(writer: &mut impl Write, samples: &[f64], config: &WavConfig) -> std::io::Result<()> {
    let num_samples = samples.len() as u32;
    let byte_rate = config.sample_rate * config.channels as u32 * config.bits_per_sample as u32 / 8;
    let block_align = config.channels * config.bits_per_sample / 8;
    let data_size = num_samples * config.bits_per_sample as u32 / 8;
    let file_size = 36 + data_size;

    // RIFF header
    writer.write_all(b"RIFF")?;
    writer.write_all(&file_size.to_le_bytes())?;
    writer.write_all(b"WAVE")?;

    // fmt chunk
    writer.write_all(b"fmt ")?;
    writer.write_all(&16u32.to_le_bytes())?;
    writer.write_all(&1u16.to_le_bytes())?; // PCM
    writer.write_all(&config.channels.to_le_bytes())?;
    writer.write_all(&config.sample_rate.to_le_bytes())?;
    writer.write_all(&byte_rate.to_le_bytes())?;
    writer.write_all(&block_align.to_le_bytes())?;
    writer.write_all(&config.bits_per_sample.to_le_bytes())?;

    // data chunk
    writer.write_all(b"data")?;
    writer.write_all(&data_size.to_le_bytes())?;

    for &sample in samples {
        let clamped = sample.clamp(-1.0, 1.0);
        let value = (clamped * 32767.0) as i16;
        writer.write_all(&value.to_le_bytes())?;
    }

    Ok(())
}

pub fn sample_to_i16(sample: f64) -> i16 {
    let clamped = sample.clamp(-1.0, 1.0);
    (clamped * 32767.0) as i16
}
```

### src/oscillator.rs

```rust
use std::f64::consts::PI;

#[derive(Debug, Clone, Copy, PartialEq)]
pub enum Waveform {
    Sine,
    Square,
    Sawtooth,
    Triangle,
}

impl Waveform {
    pub fn from_str(s: &str) -> Option<Self> {
        match s.to_lowercase().as_str() {
            "sine" => Some(Waveform::Sine),
            "square" => Some(Waveform::Square),
            "sawtooth" | "saw" => Some(Waveform::Sawtooth),
            "triangle" | "tri" => Some(Waveform::Triangle),
            _ => None,
        }
    }
}

pub fn oscillator(waveform: Waveform, freq: f64, t: f64, phase: f64) -> f64 {
    match waveform {
        Waveform::Sine => sine(freq, t, phase),
        Waveform::Square => square(freq, t, phase),
        Waveform::Sawtooth => sawtooth(freq, t, phase),
        Waveform::Triangle => triangle(freq, t, phase),
    }
}

fn sine(freq: f64, t: f64, phase: f64) -> f64 {
    (2.0 * PI * freq * t + phase).sin()
}

fn square(freq: f64, t: f64, phase: f64) -> f64 {
    let s = sine(freq, t, phase);
    if s >= 0.0 { 1.0 } else { -1.0 }
}

fn sawtooth(freq: f64, t: f64, _phase: f64) -> f64 {
    let p = t * freq + _phase / (2.0 * PI);
    2.0 * (p - (p + 0.5).floor())
}

fn triangle(freq: f64, t: f64, phase: f64) -> f64 {
    let saw = sawtooth(freq, t, phase);
    2.0 * saw.abs() - 1.0
}

pub fn generate_samples(
    waveform: Waveform,
    freq: f64,
    duration_secs: f64,
    sample_rate: u32,
) -> Vec<f64> {
    let num_samples = (duration_secs * sample_rate as f64) as usize;
    let mut samples = Vec::with_capacity(num_samples);

    for i in 0..num_samples {
        let t = i as f64 / sample_rate as f64;
        samples.push(oscillator(waveform, freq, t, 0.0));
    }

    samples
}
```

### src/envelope.rs

```rust
#[derive(Debug, Clone, Copy)]
pub struct AdsrEnvelope {
    pub attack: f64,
    pub decay: f64,
    pub sustain: f64,  // level, 0.0..1.0
    pub release: f64,
}

impl AdsrEnvelope {
    pub fn new(attack: f64, decay: f64, sustain: f64, release: f64) -> Self {
        Self {
            attack,
            decay,
            sustain: sustain.clamp(0.0, 1.0),
            release,
        }
    }

    pub fn default_pluck() -> Self {
        Self::new(0.01, 0.1, 0.6, 0.15)
    }

    pub fn default_pad() -> Self {
        Self::new(0.3, 0.2, 0.7, 0.5)
    }

    /// Returns amplitude multiplier for a given time within a note.
    /// `t` is time since note-on, `note_duration` is total hold time before release.
    pub fn amplitude(&self, t: f64, note_duration: f64) -> f64 {
        if t < 0.0 {
            return 0.0;
        }

        let note_off_time = note_duration;

        if t < note_off_time {
            // Note is held
            if t < self.attack {
                // Attack phase: ramp 0 -> 1
                t / self.attack
            } else if t < self.attack + self.decay {
                // Decay phase: ramp 1 -> sustain
                let decay_progress = (t - self.attack) / self.decay;
                1.0 - (1.0 - self.sustain) * decay_progress
            } else {
                // Sustain phase
                self.sustain
            }
        } else {
            // Release phase
            let release_elapsed = t - note_off_time;
            if release_elapsed >= self.release {
                return 0.0;
            }
            let level_at_release = if note_off_time < self.attack {
                note_off_time / self.attack
            } else if note_off_time < self.attack + self.decay {
                let dp = (note_off_time - self.attack) / self.decay;
                1.0 - (1.0 - self.sustain) * dp
            } else {
                self.sustain
            };
            level_at_release * (1.0 - release_elapsed / self.release)
        }
    }

    pub fn total_duration(&self, note_duration: f64) -> f64 {
        note_duration + self.release
    }
}

pub fn apply_envelope(samples: &mut [f64], envelope: &AdsrEnvelope, note_duration: f64, sample_rate: u32) {
    for (i, sample) in samples.iter_mut().enumerate() {
        let t = i as f64 / sample_rate as f64;
        *sample *= envelope.amplitude(t, note_duration);
    }
}
```

### src/sequencer.rs

```rust
use crate::envelope::{AdsrEnvelope, apply_envelope};
use crate::oscillator::{Waveform, oscillator};

#[derive(Debug, Clone)]
pub struct Note {
    pub frequency: f64,
    pub duration: f64,
    pub waveform: Waveform,
    pub start_time: f64,
    pub velocity: f64, // 0.0..1.0
}

/// Convert note name (e.g., "C4", "F#5", "Bb3") to frequency in Hz.
/// Uses A4 = 440 Hz equal temperament.
pub fn note_to_frequency(name: &str) -> Option<f64> {
    let bytes = name.as_bytes();
    if bytes.is_empty() {
        return None;
    }

    let base_note = match bytes[0] {
        b'C' => 0,
        b'D' => 2,
        b'E' => 4,
        b'F' => 5,
        b'G' => 7,
        b'A' => 9,
        b'B' => 11,
        _ => return None,
    };

    let (semitone_offset, octave_start) = if bytes.len() >= 2 && bytes[1] == b'#' {
        (base_note + 1, 2)
    } else if bytes.len() >= 2 && bytes[1] == b'b' {
        (base_note - 1, 2)
    } else {
        (base_note, 1)
    };

    let octave: i32 = name[octave_start..].parse().ok()?;

    // MIDI note number: C4 = 60, A4 = 69
    let midi_note = (octave + 1) * 12 + semitone_offset;
    let semitones_from_a4 = midi_note - 69;

    Some(440.0 * 2.0_f64.powf(semitones_from_a4 as f64 / 12.0))
}

/// Parse note string: "C4 0.5 sine" or "F#5 0.25 square"
pub fn parse_note(line: &str, start_time: f64) -> Option<Note> {
    let parts: Vec<&str> = line.trim().split_whitespace().collect();
    if parts.len() < 3 {
        return None;
    }

    let frequency = note_to_frequency(parts[0])?;
    let duration: f64 = parts[1].parse().ok()?;
    let waveform = Waveform::from_str(parts[2])?;

    Some(Note {
        frequency,
        duration,
        waveform,
        start_time,
        velocity: 1.0,
    })
}

/// Parse a sequence of notes, each on its own line.
/// Notes play sequentially unless prefixed with a start time.
pub fn parse_sequence(text: &str) -> Vec<Note> {
    let mut notes = Vec::new();
    let mut current_time = 0.0;

    for line in text.lines() {
        let trimmed = line.trim();
        if trimmed.is_empty() || trimmed.starts_with('#') {
            continue;
        }

        if let Some(note) = parse_note(trimmed, current_time) {
            current_time += note.duration;
            notes.push(note);
        }
    }

    notes
}

/// Render a sequence of notes into a sample buffer.
pub fn render_sequence(
    notes: &[Note],
    envelope: &AdsrEnvelope,
    sample_rate: u32,
) -> Vec<f64> {
    if notes.is_empty() {
        return Vec::new();
    }

    let total_duration = notes
        .iter()
        .map(|n| n.start_time + envelope.total_duration(n.duration))
        .fold(0.0_f64, f64::max);

    let total_samples = (total_duration * sample_rate as f64).ceil() as usize;
    let mut output = vec![0.0_f64; total_samples];

    for note in notes {
        let start_sample = (note.start_time * sample_rate as f64) as usize;
        let note_total = envelope.total_duration(note.duration);
        let note_samples = (note_total * sample_rate as f64).ceil() as usize;

        let mut note_buffer = Vec::with_capacity(note_samples);
        for i in 0..note_samples {
            let t = i as f64 / sample_rate as f64;
            note_buffer.push(oscillator(note.waveform, note.frequency, t, 0.0));
        }

        apply_envelope(&mut note_buffer, envelope, note.duration, sample_rate);

        for (i, &sample) in note_buffer.iter().enumerate() {
            let idx = start_sample + i;
            if idx < output.len() {
                output[idx] += sample * note.velocity;
            }
        }
    }

    output
}
```

### src/effects.rs

```rust
/// Apply echo/delay effect in-place.
/// `delay_secs`: time between echoes
/// `decay`: amplitude reduction per echo (0.0..1.0)
/// `sample_rate`: samples per second
pub fn apply_echo(samples: &mut Vec<f64>, delay_secs: f64, decay: f64, sample_rate: u32) {
    let delay_samples = (delay_secs * sample_rate as f64) as usize;
    if delay_samples == 0 || decay <= 0.0 {
        return;
    }

    // Extend buffer to accommodate echoes
    let extra = (delay_samples as f64 * (1.0 / (1.0 - decay)).ln().ceil()) as usize;
    let extra_clamped = extra.min(sample_rate as usize * 5); // max 5 seconds of tail
    samples.resize(samples.len() + extra_clamped, 0.0);

    for i in delay_samples..samples.len() {
        samples[i] += decay * samples[i - delay_samples];
    }
}

/// Normalize samples so the peak amplitude is `target` (default 1.0).
pub fn normalize(samples: &mut [f64], target: f64) {
    let peak = samples.iter().map(|s| s.abs()).fold(0.0_f64, f64::max);
    if peak < 1e-10 {
        return;
    }
    let scale = target / peak;
    for sample in samples.iter_mut() {
        *sample *= scale;
    }
}

/// Mix multiple buffers into one. Normalizes the result.
pub fn mix_voices(voices: &[Vec<f64>]) -> Vec<f64> {
    if voices.is_empty() {
        return Vec::new();
    }

    let max_len = voices.iter().map(|v| v.len()).max().unwrap_or(0);
    let mut mixed = vec![0.0_f64; max_len];

    for voice in voices {
        for (i, &sample) in voice.iter().enumerate() {
            mixed[i] += sample;
        }
    }

    normalize(&mut mixed, 0.95);
    mixed
}
```

### src/main.rs

```rust
mod wav;
mod oscillator;
mod envelope;
mod sequencer;
mod effects;

use std::fs::File;
use std::io::BufWriter;

fn main() {
    let config = wav::WavConfig::mono_44100();
    let sample_rate = config.sample_rate;
    let env = envelope::AdsrEnvelope::default_pluck();

    let melody = "\
C4 0.5 sine
E4 0.5 sine
G4 0.5 sine
C5 1.0 sine
";

    let notes = sequencer::parse_sequence(melody);
    println!("Parsed {} notes:", notes.len());
    for note in &notes {
        println!(
            "  {:.1} Hz, {:.2}s, {:?} @ t={:.2}s",
            note.frequency, note.duration, note.waveform, note.start_time
        );
    }

    let mut samples = sequencer::render_sequence(&notes, &env, sample_rate);

    effects::apply_echo(&mut samples, 0.3, 0.4, sample_rate);
    effects::normalize(&mut samples, 0.9);

    let file = File::create("output.wav").expect("Failed to create output file");
    let mut writer = BufWriter::new(file);
    wav::write_wav(&mut writer, &samples, &config).expect("Failed to write WAV");

    println!("Wrote output.wav ({} samples, {:.2}s)", samples.len(), samples.len() as f64 / sample_rate as f64);

    // Generate chord example with mixed voices
    let voice1 = oscillator::generate_samples(oscillator::Waveform::Sine, 261.63, 2.0, sample_rate);
    let voice2 = oscillator::generate_samples(oscillator::Waveform::Sine, 329.63, 2.0, sample_rate);
    let voice3 = oscillator::generate_samples(oscillator::Waveform::Sine, 392.00, 2.0, sample_rate);

    let chord = effects::mix_voices(&[voice1, voice2, voice3]);

    let file = File::create("chord.wav").expect("Failed to create chord file");
    let mut writer = BufWriter::new(file);
    wav::write_wav(&mut writer, &chord, &config).expect("Failed to write chord WAV");

    println!("Wrote chord.wav (C major chord, 2.0s)");

    // Generate all waveforms for comparison
    for wf in &[
        oscillator::Waveform::Sine,
        oscillator::Waveform::Square,
        oscillator::Waveform::Sawtooth,
        oscillator::Waveform::Triangle,
    ] {
        let samples = oscillator::generate_samples(*wf, 440.0, 1.0, sample_rate);
        let filename = format!("{:?}.wav", wf).to_lowercase();
        let file = File::create(&filename).expect("Failed to create waveform file");
        let mut writer = BufWriter::new(file);
        wav::write_wav(&mut writer, &samples, &config).expect("Failed to write waveform WAV");
        println!("Wrote {} (440 Hz, 1.0s)", filename);
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_wav_header_size() {
        let mut buf = Vec::new();
        let samples = vec![0.0_f64; 100];
        let config = wav::WavConfig::mono_44100();
        wav::write_wav(&mut buf, &samples, &config).unwrap();
        // 44 bytes header + 100 samples * 2 bytes each = 244
        assert_eq!(buf.len(), 244);
    }

    #[test]
    fn test_wav_header_riff_tag() {
        let mut buf = Vec::new();
        let config = wav::WavConfig::mono_44100();
        wav::write_wav(&mut buf, &[0.0; 10], &config).unwrap();
        assert_eq!(&buf[0..4], b"RIFF");
        assert_eq!(&buf[8..12], b"WAVE");
        assert_eq!(&buf[12..16], b"fmt ");
        assert_eq!(&buf[36..40], b"data");
    }

    #[test]
    fn test_wav_pcm_format_tag() {
        let mut buf = Vec::new();
        let config = wav::WavConfig::mono_44100();
        wav::write_wav(&mut buf, &[0.0; 10], &config).unwrap();
        let format_tag = u16::from_le_bytes([buf[20], buf[21]]);
        assert_eq!(format_tag, 1); // PCM
    }

    #[test]
    fn test_oscillator_sine_range() {
        for i in 0..44100 {
            let t = i as f64 / 44100.0;
            let s = oscillator::oscillator(oscillator::Waveform::Sine, 440.0, t, 0.0);
            assert!(s >= -1.0 && s <= 1.0, "Sine out of range: {}", s);
        }
    }

    #[test]
    fn test_oscillator_square_values() {
        for i in 0..44100 {
            let t = i as f64 / 44100.0;
            let s = oscillator::oscillator(oscillator::Waveform::Square, 440.0, t, 0.0);
            assert!(s == 1.0 || s == -1.0, "Square not binary: {}", s);
        }
    }

    #[test]
    fn test_oscillator_sawtooth_range() {
        for i in 0..44100 {
            let t = i as f64 / 44100.0;
            let s = oscillator::oscillator(oscillator::Waveform::Sawtooth, 440.0, t, 0.0);
            assert!(s >= -1.0 && s <= 1.0, "Sawtooth out of range: {}", s);
        }
    }

    #[test]
    fn test_oscillator_triangle_range() {
        for i in 0..44100 {
            let t = i as f64 / 44100.0;
            let s = oscillator::oscillator(oscillator::Waveform::Triangle, 440.0, t, 0.0);
            assert!(s >= -1.0 && s <= 1.0, "Triangle out of range: {}", s);
        }
    }

    #[test]
    fn test_note_to_frequency_a4() {
        let freq = sequencer::note_to_frequency("A4").unwrap();
        assert!((freq - 440.0).abs() < 0.01);
    }

    #[test]
    fn test_note_to_frequency_c4() {
        let freq = sequencer::note_to_frequency("C4").unwrap();
        assert!((freq - 261.63).abs() < 0.1);
    }

    #[test]
    fn test_note_to_frequency_sharps() {
        let f_sharp = sequencer::note_to_frequency("F#4").unwrap();
        assert!((f_sharp - 369.99).abs() < 0.1);
    }

    #[test]
    fn test_adsr_attack_ramp() {
        let env = envelope::AdsrEnvelope::new(0.1, 0.1, 0.5, 0.1);
        assert!((env.amplitude(0.0, 1.0) - 0.0).abs() < 0.01);
        assert!((env.amplitude(0.05, 1.0) - 0.5).abs() < 0.01);
        assert!((env.amplitude(0.1, 1.0) - 1.0).abs() < 0.01);
    }

    #[test]
    fn test_adsr_sustain_level() {
        let env = envelope::AdsrEnvelope::new(0.01, 0.01, 0.7, 0.01);
        let amp = env.amplitude(0.5, 1.0);
        assert!((amp - 0.7).abs() < 0.01);
    }

    #[test]
    fn test_adsr_release_to_zero() {
        let env = envelope::AdsrEnvelope::new(0.01, 0.01, 0.5, 0.1);
        let amp = env.amplitude(1.1, 1.0);
        assert!(amp.abs() < 0.01, "Release should reach zero: {}", amp);
    }

    #[test]
    fn test_sample_to_i16_clamping() {
        assert_eq!(wav::sample_to_i16(0.0), 0);
        assert_eq!(wav::sample_to_i16(1.0), 32767);
        assert_eq!(wav::sample_to_i16(-1.0), -32767);
        assert_eq!(wav::sample_to_i16(2.0), 32767);  // clamped
        assert_eq!(wav::sample_to_i16(-2.0), -32767); // clamped
    }

    #[test]
    fn test_parse_note() {
        let note = sequencer::parse_note("C4 0.5 sine", 0.0).unwrap();
        assert!((note.frequency - 261.63).abs() < 0.1);
        assert!((note.duration - 0.5).abs() < 0.001);
        assert_eq!(note.waveform, oscillator::Waveform::Sine);
    }

    #[test]
    fn test_parse_sequence() {
        let seq = "C4 0.5 sine\nE4 0.5 square\n";
        let notes = sequencer::parse_sequence(seq);
        assert_eq!(notes.len(), 2);
        assert!((notes[1].start_time - 0.5).abs() < 0.001);
    }

    #[test]
    fn test_normalize() {
        let mut samples = vec![0.5, -1.0, 0.3];
        effects::normalize(&mut samples, 1.0);
        let peak = samples.iter().map(|s| s.abs()).fold(0.0_f64, f64::max);
        assert!((peak - 1.0).abs() < 0.001);
    }

    #[test]
    fn test_mix_voices() {
        let v1 = vec![1.0, 0.0, -1.0];
        let v2 = vec![0.5, 0.5, 0.5];
        let mixed = effects::mix_voices(&[v1, v2]);
        assert_eq!(mixed.len(), 3);
        let peak = mixed.iter().map(|s| s.abs()).fold(0.0_f64, f64::max);
        assert!(peak <= 1.0);
    }

    #[test]
    fn test_echo_extends_buffer() {
        let original_len = 44100;
        let mut samples = vec![0.0_f64; original_len];
        samples[0] = 1.0;
        effects::apply_echo(&mut samples, 0.1, 0.5, 44100);
        assert!(samples.len() > original_len);
    }
}
```

### Build and Run

```bash
cargo new wav-synth
cd wav-synth
# Copy source files into src/
cargo build
cargo test
cargo run
```

### Expected Output

```
Parsed 4 notes:
  261.6 Hz, 0.50s, Sine @ t=0.00s
  329.6 Hz, 0.50s, Sine @ t=0.50s
  392.0 Hz, 0.50s, Sine @ t=1.00s
  523.3 Hz, 1.00s, Sine @ t=1.50s
Wrote output.wav (138915 samples, 3.15s)
Wrote chord.wav (C major chord, 2.0s)
Wrote sine.wav (440 Hz, 1.0s)
Wrote square.wav (440 Hz, 1.0s)
Wrote sawtooth.wav (440 Hz, 1.0s)
Wrote triangle.wav (440 Hz, 1.0s)
```

```
running 17 tests
test tests::test_wav_header_size ... ok
test tests::test_wav_header_riff_tag ... ok
test tests::test_wav_pcm_format_tag ... ok
test tests::test_oscillator_sine_range ... ok
test tests::test_oscillator_square_values ... ok
test tests::test_oscillator_sawtooth_range ... ok
test tests::test_oscillator_triangle_range ... ok
test tests::test_note_to_frequency_a4 ... ok
test tests::test_note_to_frequency_c4 ... ok
test tests::test_note_to_frequency_sharps ... ok
test tests::test_adsr_attack_ramp ... ok
test tests::test_adsr_sustain_level ... ok
test tests::test_adsr_release_to_zero ... ok
test tests::test_sample_to_i16_clamping ... ok
test tests::test_parse_note ... ok
test tests::test_parse_sequence ... ok
test tests::test_normalize ... ok
test tests::test_mix_voices ... ok
test tests::test_echo_extends_buffer ... ok

test result: ok. 17 passed; 0 failed
```

## Design Decisions

1. **f64 internal representation**: All audio processing uses f64 in [-1.0, 1.0]. Conversion to i16 happens only at WAV output. This avoids accumulating quantization errors during mixing and effects.

2. **Phase-based oscillators**: Oscillators take absolute time `t` rather than tracking phase state. This simplifies the sequencer (no need to carry oscillator state between calls) at the cost of not supporting smooth frequency glides. For a synthesizer with portamento, you would accumulate phase incrementally.

3. **Post-mix normalization**: Voices are summed and then normalized to prevent clipping. An alternative is to divide by the number of voices, but normalization preserves relative dynamics better when voices have different amplitudes.

4. **Echo as feedback delay**: The echo effect writes back into the same buffer (`output[i] += decay * output[i - delay]`), which creates natural-sounding decaying repetitions. The buffer is extended to accommodate the echo tail.

5. **Sequential note parsing**: The simple format places notes one after another. For polyphonic (simultaneous) notes, you would need explicit start times or a more complex format like MIDI.

## Common Mistakes

- **Wrong endianness in WAV header**: WAV uses little-endian for all integer fields. Using big-endian produces a file that players reject or interpret as noise.
- **Forgetting to clamp before i16 conversion**: Mixing multiple voices can produce values above 1.0. Without clamping, the cast to i16 wraps around, causing loud pops.
- **ADSR clicks**: If the release time is 0.0 or the envelope jumps from sustain directly to silence, you get an audible click. Always ensure a minimum release time (even 1ms).
- **Phase discontinuity in sawtooth/triangle**: Naive implementations reset phase at note boundaries, causing clicks. Using absolute time with floor/fract avoids this.
- **Incorrect note frequencies**: Off-by-one in octave calculation (C4 is MIDI 60, not 48). The formula `(octave + 1) * 12 + semitone` accounts for MIDI's convention where octave -1 starts at note 0.

## Performance Notes

- At 44100 Hz mono, one second of audio is 44,100 f64 values (344 KB). A 5-minute song is ~100 MB in memory. For longer compositions, render in chunks and stream to disk.
- The oscillator functions use `sin()` from libm, which is already optimized. For real-time use, lookup tables or polynomial approximations are faster but unnecessary for offline synthesis.
- Echo with large delay and high decay can produce very long tails. The 5-second cap on echo extension prevents runaway memory allocation.
