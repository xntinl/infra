# 132. Audio Synthesis DAW Engine

<!--
difficulty: insane
category: audio-dsp
languages: [rust]
concepts: [audio-graph, dsp-filters, midi-sequencing, multi-track-mixing, real-time-audio, wav-export, bpm-transport, effects-chain]
estimated_time: 25-35 hours
bloom_level: create
prerequisites: [signal-processing-basics, floating-point-arithmetic, graph-traversal, file-io, midi-format, audio-fundamentals]
-->

## Languages

- Rust (1.75+ stable)

## Prerequisites

- Digital audio fundamentals (sample rate, bit depth, PCM)
- Basic DSP: convolution, frequency, amplitude, phase
- Graph data structures and topological sort
- Binary file parsing (for MIDI and WAV formats)
- Floating-point arithmetic precision concerns

## Learning Objectives

By the end of this challenge you will be able to **create** a multi-track audio engine with an effects processing pipeline, MIDI sequencer, and real-time audio graph -- capable of rendering a complete mix to a WAV file with sample-accurate timing.

## The Challenge

Build the core engine of a Digital Audio Workstation. The engine manages multiple audio tracks, each with an independent effects chain. An audio graph connects source nodes (oscillators, samplers) through processing nodes (filters, effects) to a master output bus. A MIDI sequencer drives note events with BPM-based timing. The final mix renders to a WAV file.

This is not a simple sine wave generator or a WAV file reader. You are building the mixing and processing engine that sits at the heart of a DAW: a real-time audio graph where each node processes buffers of samples, effects chain with reverb (Schroeder model using comb and allpass filters), chorus, distortion, and parametric biquad filters, all driven by a MIDI sequencer with sample-accurate event scheduling.

## Requirements

- [ ] Audio graph: nodes connected by edges, processed via topological sort. Node types: oscillator (sine, saw, square, triangle), gain, mixer, effects, output
- [ ] Multi-track mixer: N tracks with independent volume, pan, mute/solo, routed to a master bus
- [ ] Effects chain per track: ordered list of effects applied in series
- [ ] Reverb effect: Schroeder reverb using 4 parallel comb filters + 2 series allpass filters, with room size and damping parameters
- [ ] Chorus effect: modulated delay line with LFO, depth and rate parameters
- [ ] Distortion effect: soft clipping (tanh waveshaper) with drive parameter
- [ ] Biquad filter: low-pass, high-pass, band-pass modes with configurable cutoff frequency and Q factor
- [ ] MIDI parser: read standard MIDI files (.mid), extract note-on/note-off events, tempo changes, and channel assignments
- [ ] MIDI sequencer: schedule note events against a BPM-based timeline, convert ticks to sample positions
- [ ] Transport controls: play, stop, seek to position (in beats or samples)
- [ ] Sample-accurate timing: events trigger at exact sample boundaries, no drift over long sequences
- [ ] WAV export: render the full mix to 16-bit or 32-bit float WAV file at configurable sample rate (44100, 48000 Hz)
- [ ] Buffer-based processing: all nodes process fixed-size buffers (e.g., 512 samples) for cache efficiency

## Hints

1. The audio graph must be processed in dependency order. Topological sort determines which nodes compute first. Each node reads from its input edges and writes to an output buffer. Cycles in the graph are invalid -- detect and reject them at connection time.

2. For the Schroeder reverb, use four comb filters with mutually prime delay lengths (e.g., 1557, 1617, 1491, 1422 samples at 44100 Hz) feeding into two allpass filters (delays 225 and 556 samples). The comb filters run in parallel; their outputs sum and pass through the allpass filters in series.

3. MIDI timing uses "ticks per quarter note" (TPQN). To convert a tick position to a sample position: `sample = (tick / tpqn) * (60.0 / bpm) * sample_rate`. Handle tempo changes by accumulating sample offsets at each tempo change point rather than assuming constant BPM.

## Acceptance Criteria

- [ ] Audio graph processes nodes in correct dependency order via topological sort
- [ ] Multi-track mixer produces correct stereo output with volume and pan
- [ ] Reverb effect produces audible room ambience with configurable decay
- [ ] Biquad filter demonstrably attenuates frequencies above/below cutoff
- [ ] MIDI file parsing extracts note events and tempo map from a standard .mid file
- [ ] Sequencer schedules note events at sample-accurate positions
- [ ] Transport seek repositions playback correctly within the timeline
- [ ] WAV export produces a valid file playable by any audio player
- [ ] Processing a 3-minute track completes in reasonable time (under 30 seconds)
- [ ] `cargo test` passes with unit and integration tests

## Research Resources

- [The Audio Programming Book (Boulanger & Lazzarini)](https://mitpress.mit.edu/9780262014465/) -- canonical DSP reference for audio programmers
- [Julius O. Smith III: Physical Audio Signal Processing](https://ccrma.stanford.edu/~jos/pasp/) -- reverb algorithms, comb and allpass filter theory
- [MIDI File Format Specification](https://www.midi.org/specifications) -- binary structure of .mid files
- [Audio EQ Cookbook (Robert Bristow-Johnson)](https://www.w3.org/2011/audio/audio-eq-cookbook.html) -- biquad filter coefficient formulas for all filter types
- [WAV File Format](http://soundfile.sapp.org/doc/WaveFormat/) -- PCM WAV header structure
- [The Synthesis ToolKit (STK)](https://ccrma.stanford.edu/software/stk/) -- reference implementations of oscillators, filters, and effects
- [Rust `hound` crate](https://docs.rs/hound/latest/hound/) -- WAV reading/writing (study the format, then implement your own)
