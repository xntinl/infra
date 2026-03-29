<!-- difficulty: intermediate-advanced -->
<!-- category: audio-video-processing -->
<!-- languages: [rust] -->
<!-- concepts: [wav-format, pcm-encoding, oscillators, adsr-envelope, audio-mixing, waveform-synthesis, byte-level-io] -->
<!-- estimated_time: 6-10 hours -->
<!-- bloom_level: apply, analyze -->
<!-- prerequisites: [floating-point-arithmetic, bitwise-operations, file-io, trigonometry-basics, rust-structs-enums] -->

# Challenge 59: WAV Audio Synthesizer

## Languages

Rust (stable, latest edition)

## Prerequisites

- Floating-point arithmetic and trigonometric functions (`sin`, `cos`, `PI`)
- Byte-level file I/O: writing little-endian integers and raw PCM sample data
- Understanding of how digital audio works: sample rate, bit depth, amplitude
- Rust structs, enums, iterators, and `Vec<f64>` for sample buffers
- Basic music theory: frequencies of notes, octaves doubling frequency

## Learning Objectives

- **Implement** the WAV file format from scratch, writing correct RIFF headers and PCM data
- **Apply** waveform mathematics to generate sine, square, sawtooth, and triangle oscillators
- **Design** an ADSR envelope generator that shapes note dynamics over time
- **Analyze** how sample rate and bit depth affect audio quality and file size
- **Create** a note sequencer that converts simple text notation into timed audio events

## The Challenge

Digital audio is a sequence of numbers. Each number represents the air pressure at a specific instant in time. Play 44,100 of these numbers per second through a speaker and you hear sound. The WAV format is the simplest uncompressed audio container: a RIFF header describing the format, followed by raw PCM samples. No compression, no codecs -- just math turned into sound.

Build an audio synthesizer that generates playable WAV files from scratch. Implement four fundamental oscillators (sine, square, sawtooth, triangle), an ADSR envelope to shape each note's amplitude over time, a note sequencer that reads a simple text-based notation format, multi-voice mixing, and a basic echo/delay effect. The output must be a valid WAV file playable in any audio player.

The core loop is: for each sample at time `t`, compute the oscillator value, multiply by the envelope value, sum all active voices, apply effects, clamp to [-1.0, 1.0], convert to 16-bit integer, write to file. Every piece is straightforward math, but getting them to work together without clicks, pops, or distortion requires careful attention to phase continuity and amplitude management.

## Requirements

1. Write a valid WAV file: RIFF header (4 bytes "RIFF", file size, "WAVE"), fmt chunk (PCM format tag 1, channels, sample rate, byte rate, block align, bits per sample), data chunk ("data", data size, raw samples)
2. Support 16-bit PCM at 44100 Hz, mono and stereo
3. Implement four oscillators, each taking frequency and phase as inputs and returning a sample in [-1.0, 1.0]:
   - Sine: `sin(2 * PI * freq * t + phase)`
   - Square: sign of sine wave (1.0 or -1.0)
   - Sawtooth: `2.0 * (t * freq - floor(t * freq + 0.5))`
   - Triangle: `2.0 * abs(sawtooth) - 1.0`
4. Implement an ADSR envelope with attack, decay, sustain level, and release times (in seconds). The envelope returns a multiplier in [0.0, 1.0] for any given time relative to note-on and note-off events
5. Parse a simple note format: `"C4 0.5 sine"` means note C4, duration 0.5 seconds, sine waveform. Support notes A0-C8 with sharps (e.g., `C#4`, `F#5`). Convert note names to frequencies using A4 = 440 Hz and equal temperament
6. Implement a sequencer that processes a list of notes with start times and renders them into a sample buffer
7. Mix multiple voices by summing their sample buffers and normalizing to prevent clipping
8. Implement an echo effect: `output[i] = input[i] + decay * output[i - delay_samples]`
9. Convert floating-point samples [-1.0, 1.0] to 16-bit signed integers [-32768, 32767] with proper clamping
10. Write unit tests verifying: WAV header correctness, oscillator output ranges, ADSR envelope shape, note frequency calculation, round-trip sample conversion

## Hints

<details>
<summary>Hint 1: WAV header byte layout</summary>

The WAV header is exactly 44 bytes for PCM format. Write every field in little-endian except the ASCII tags:

```rust
fn write_wav_header(writer: &mut impl Write, num_samples: u32, sample_rate: u32, channels: u16) {
    let bits_per_sample: u16 = 16;
    let byte_rate = sample_rate * channels as u32 * bits_per_sample as u32 / 8;
    let block_align = channels * bits_per_sample / 8;
    let data_size = num_samples * channels as u32 * bits_per_sample as u32 / 8;
    let file_size = 36 + data_size;

    writer.write_all(b"RIFF").unwrap();
    writer.write_all(&file_size.to_le_bytes()).unwrap();
    writer.write_all(b"WAVE").unwrap();
    writer.write_all(b"fmt ").unwrap();
    writer.write_all(&16u32.to_le_bytes()).unwrap(); // fmt chunk size
    writer.write_all(&1u16.to_le_bytes()).unwrap();  // PCM format
    writer.write_all(&channels.to_le_bytes()).unwrap();
    writer.write_all(&sample_rate.to_le_bytes()).unwrap();
    writer.write_all(&byte_rate.to_le_bytes()).unwrap();
    writer.write_all(&block_align.to_le_bytes()).unwrap();
    writer.write_all(&bits_per_sample.to_le_bytes()).unwrap();
    writer.write_all(b"data").unwrap();
    writer.write_all(&data_size.to_le_bytes()).unwrap();
}
```

</details>

<details>
<summary>Hint 2: Note frequency calculation</summary>

Equal temperament: every semitone multiplies frequency by `2^(1/12)`. A4 = 440 Hz is MIDI note 69. Convert note name to semitone offset from A4, then: `freq = 440.0 * 2.0^(offset / 12.0)`. C4 is 3 semitones below A4 (offset = -9 from A4 in the same octave, but C4 is actually MIDI 60, A4 is 69, so offset = -9).

</details>

<details>
<summary>Hint 3: ADSR envelope state machine</summary>

Track the envelope as a state machine with four phases. Given `note_on_time` and `note_off_time`:
- Attack: `t` in `[0, attack]` -- ramp from 0.0 to 1.0 linearly
- Decay: `t` in `[attack, attack+decay]` -- ramp from 1.0 to sustain level
- Sustain: hold at sustain level until note off
- Release: ramp from current level to 0.0 over release time

</details>

<details>
<summary>Hint 4: Avoiding clicks at note boundaries</summary>

Clicks happen when the waveform jumps discontinuously. The ADSR envelope solves most clicks by ramping amplitude to zero on release. For the echo effect, make sure the delay buffer is initialized to zero and the feedback `decay` is less than 1.0 to prevent infinite buildup.

</details>

## Acceptance Criteria

- [ ] Output WAV file is playable in standard audio players (VLC, Audacity, system default)
- [ ] WAV header fields are correct: RIFF tag, file size, fmt chunk with PCM format tag 1, data chunk size matches sample count
- [ ] Sine oscillator at 440 Hz produces a clean A4 tone with no audible artifacts
- [ ] Square, sawtooth, and triangle waveforms are audibly distinct from sine
- [ ] ADSR envelope produces smooth attack ramp, decay to sustain, and release to silence
- [ ] Notes C4 through B4 produce correct frequencies within 0.1 Hz of equal temperament values
- [ ] Sharp notes (C#, F#, etc.) are correctly calculated
- [ ] Multiple simultaneous voices are mixed without clipping
- [ ] Echo effect produces audible repetitions that decay over time
- [ ] 16-bit sample conversion clamps correctly: no wrapping on overflow
- [ ] All tests pass with `cargo test`

## Research Resources

- [WAV File Format Specification (McGill University)](http://www-mmsp.ece.mcgill.ca/documents/audioformats/wave/wave.html) -- complete byte-level format description with all chunk types
- [WAV PCM Soundfile Format (Stanford CCRMA)](https://ccrma.stanford.edu/courses/422-winter-2014/projects/WaveFormat/) -- concise single-page reference for the 44-byte header
- [Equal Temperament -- Wikipedia](https://en.wikipedia.org/wiki/Equal_temperament) -- the math behind note frequencies and semitone ratios
- [ADSR Envelope -- Wikipedia](https://en.wikipedia.org/wiki/Envelope_(music)#ADSR) -- attack, decay, sustain, release phases explained with diagrams
- [Waveform Generation (Sound on Sound)](https://www.soundonsound.com/synthesizers/synth-secrets-part-1-whats-sound) -- how oscillators produce different timbres
- [Digital Audio Fundamentals](https://xiph.org/video/vid1.shtml) -- Monty Montgomery's video on sampling, bit depth, and digital audio theory
