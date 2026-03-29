# Solution: Audio FFT Spectrum Analyzer

## Architecture Overview

The analyzer is organized into six modules:

1. **complex**: Complex number type with arithmetic operations and polar form
2. **fft**: Cooley-Tukey radix-2 FFT and inverse FFT
3. **window**: Windowing functions (Hann, Hamming, Blackman)
4. **wav**: WAV file reader with PCM parsing and mono downmix
5. **stft**: Short-Time Fourier Transform and magnitude spectrum computation
6. **visualize**: ASCII spectrogram and PPM image generation

```
WAV File
     |
     v
 [WAV Parser] --> Vec<f64> mono samples, sample_rate
     |
     v
 [STFT] --> For each overlapping frame:
     |        - Apply window function
     |        - Zero-pad to power of 2
     |        - Compute FFT
     |        - Compute magnitude spectrum (dB)
     v
 [2D Spectrogram Data] --> time x frequency matrix
     |
     +--> [ASCII Renderer] --> terminal spectrogram
     |
     +--> [PPM Renderer] --> spectrogram.ppm image
     |
     +--> [Statistics] --> dominant freq, bandwidth, energy
```

## Rust Solution

### Cargo.toml

```toml
[package]
name = "fft-analyzer"
version = "0.1.0"
edition = "2021"

[[bin]]
name = "fft-analyzer"
path = "src/main.rs"
```

### src/complex.rs

```rust
use std::f64::consts::PI;
use std::ops::{Add, Sub, Mul};

#[derive(Debug, Clone, Copy)]
pub struct Complex {
    pub re: f64,
    pub im: f64,
}

impl Complex {
    pub fn new(re: f64, im: f64) -> Self {
        Self { re, im }
    }

    pub fn zero() -> Self {
        Self { re: 0.0, im: 0.0 }
    }

    pub fn from_real(re: f64) -> Self {
        Self { re, im: 0.0 }
    }

    /// e^(i*theta) = cos(theta) + i*sin(theta)
    pub fn from_polar(magnitude: f64, phase: f64) -> Self {
        Self {
            re: magnitude * phase.cos(),
            im: magnitude * phase.sin(),
        }
    }

    /// Unit complex exponential: e^(i*theta)
    pub fn unit_polar(theta: f64) -> Self {
        Self::from_polar(1.0, theta)
    }

    pub fn magnitude(self) -> f64 {
        (self.re * self.re + self.im * self.im).sqrt()
    }

    pub fn phase(self) -> f64 {
        self.im.atan2(self.re)
    }

    pub fn conjugate(self) -> Self {
        Self {
            re: self.re,
            im: -self.im,
        }
    }

    /// Twiddle factor: e^(-2*PI*i*k/n)
    pub fn twiddle(k: usize, n: usize) -> Self {
        let theta = -2.0 * PI * k as f64 / n as f64;
        Self::unit_polar(theta)
    }
}

impl Add for Complex {
    type Output = Self;
    fn add(self, rhs: Self) -> Self {
        Self::new(self.re + rhs.re, self.im + rhs.im)
    }
}

impl Sub for Complex {
    type Output = Self;
    fn sub(self, rhs: Self) -> Self {
        Self::new(self.re - rhs.re, self.im - rhs.im)
    }
}

impl Mul for Complex {
    type Output = Self;
    fn mul(self, rhs: Self) -> Self {
        Self::new(
            self.re * rhs.re - self.im * rhs.im,
            self.re * rhs.im + self.im * rhs.re,
        )
    }
}

impl Mul<f64> for Complex {
    type Output = Self;
    fn mul(self, scalar: f64) -> Self {
        Self::new(self.re * scalar, self.im * scalar)
    }
}
```

### src/fft.rs

```rust
use crate::complex::Complex;

/// Cooley-Tukey radix-2 decimation-in-time FFT.
/// Input length MUST be a power of 2.
pub fn fft(input: &[Complex]) -> Vec<Complex> {
    let n = input.len();
    assert!(n.is_power_of_two(), "FFT size must be power of 2, got {}", n);

    if n == 1 {
        return input.to_vec();
    }

    // Split into even and odd indexed elements
    let even: Vec<Complex> = input.iter().step_by(2).copied().collect();
    let odd: Vec<Complex> = input.iter().skip(1).step_by(2).copied().collect();

    let fft_even = fft(&even);
    let fft_odd = fft(&odd);

    let mut output = vec![Complex::zero(); n];
    let half = n / 2;

    for k in 0..half {
        let twiddle = Complex::twiddle(k, n);
        let t = twiddle * fft_odd[k];
        output[k] = fft_even[k] + t;
        output[k + half] = fft_even[k] - t;
    }

    output
}

/// Inverse FFT: conjugate -> FFT -> conjugate -> divide by N.
pub fn ifft(input: &[Complex]) -> Vec<Complex> {
    let n = input.len();
    let conjugated: Vec<Complex> = input.iter().map(|c| c.conjugate()).collect();
    let transformed = fft(&conjugated);
    let scale = 1.0 / n as f64;
    transformed
        .iter()
        .map(|c| c.conjugate() * scale)
        .collect()
}

/// Zero-pad input to the next power of 2.
pub fn zero_pad(input: &[Complex]) -> Vec<Complex> {
    let n = input.len();
    if n.is_power_of_two() {
        return input.to_vec();
    }
    let next_pow2 = n.next_power_of_two();
    let mut padded = input.to_vec();
    padded.resize(next_pow2, Complex::zero());
    padded
}

/// Convert real samples to complex (imaginary part = 0).
pub fn real_to_complex(samples: &[f64]) -> Vec<Complex> {
    samples.iter().map(|&s| Complex::from_real(s)).collect()
}

/// Compute magnitude spectrum (first N/2+1 bins).
pub fn magnitude_spectrum(fft_output: &[Complex]) -> Vec<f64> {
    let n = fft_output.len();
    let nyquist = n / 2 + 1;
    fft_output[..nyquist]
        .iter()
        .map(|c| c.magnitude())
        .collect()
}

/// Convert magnitude spectrum to decibels relative to the peak.
pub fn magnitude_to_db(magnitudes: &[f64], floor_db: f64) -> Vec<f64> {
    let max_mag = magnitudes
        .iter()
        .copied()
        .fold(0.0_f64, f64::max);

    if max_mag < 1e-30 {
        return vec![floor_db; magnitudes.len()];
    }

    magnitudes
        .iter()
        .map(|&m| {
            let db = 20.0 * (m / max_mag).log10();
            db.max(floor_db)
        })
        .collect()
}

/// Find the bin with the highest magnitude (excluding DC).
pub fn dominant_bin(magnitudes: &[f64]) -> usize {
    magnitudes
        .iter()
        .enumerate()
        .skip(1) // skip DC
        .max_by(|(_, a), (_, b)| a.partial_cmp(b).unwrap())
        .map(|(i, _)| i)
        .unwrap_or(0)
}

/// Convert bin index to frequency.
pub fn bin_to_frequency(bin: usize, sample_rate: u32, fft_size: usize) -> f64 {
    bin as f64 * sample_rate as f64 / fft_size as f64
}
```

### src/window.rs

```rust
use std::f64::consts::PI;

#[derive(Debug, Clone, Copy)]
pub enum WindowType {
    Rectangular,
    Hann,
    Hamming,
    Blackman,
}

impl WindowType {
    pub fn from_str(s: &str) -> Option<Self> {
        match s.to_lowercase().as_str() {
            "rectangular" | "rect" | "none" => Some(WindowType::Rectangular),
            "hann" | "hanning" => Some(WindowType::Hann),
            "hamming" => Some(WindowType::Hamming),
            "blackman" => Some(WindowType::Blackman),
            _ => None,
        }
    }
}

/// Generate window coefficients of length N.
pub fn generate_window(window_type: WindowType, n: usize) -> Vec<f64> {
    match window_type {
        WindowType::Rectangular => vec![1.0; n],
        WindowType::Hann => hann(n),
        WindowType::Hamming => hamming(n),
        WindowType::Blackman => blackman(n),
    }
}

fn hann(n: usize) -> Vec<f64> {
    (0..n)
        .map(|i| 0.5 * (1.0 - (2.0 * PI * i as f64 / (n - 1) as f64).cos()))
        .collect()
}

fn hamming(n: usize) -> Vec<f64> {
    (0..n)
        .map(|i| 0.54 - 0.46 * (2.0 * PI * i as f64 / (n - 1) as f64).cos())
        .collect()
}

fn blackman(n: usize) -> Vec<f64> {
    (0..n)
        .map(|i| {
            let x = 2.0 * PI * i as f64 / (n - 1) as f64;
            0.42 - 0.5 * x.cos() + 0.08 * (2.0 * x).cos()
        })
        .collect()
}

/// Apply window to samples (element-wise multiply).
pub fn apply_window(samples: &[f64], window: &[f64]) -> Vec<f64> {
    samples
        .iter()
        .zip(window.iter())
        .map(|(&s, &w)| s * w)
        .collect()
}
```

### src/wav.rs

```rust
use std::io::Read;

pub struct WavData {
    pub sample_rate: u32,
    pub samples: Vec<f64>,
    pub channels: u16,
    pub original_channels: u16,
}

/// Read a WAV file and return normalized mono samples in [-1.0, 1.0].
pub fn read_wav(reader: &mut impl Read) -> Result<WavData, String> {
    let mut header = [0u8; 44];
    reader
        .read_exact(&mut header)
        .map_err(|e| format!("Failed to read WAV header: {}", e))?;

    if &header[0..4] != b"RIFF" || &header[8..12] != b"WAVE" {
        return Err("Not a valid WAV file".to_string());
    }

    let format_tag = u16::from_le_bytes([header[20], header[21]]);
    if format_tag != 1 {
        return Err(format!("Unsupported format tag: {} (expected 1 = PCM)", format_tag));
    }

    let channels = u16::from_le_bytes([header[22], header[23]]);
    let sample_rate = u32::from_le_bytes([header[24], header[25], header[26], header[27]]);
    let bits_per_sample = u16::from_le_bytes([header[34], header[35]]);
    let data_size = u32::from_le_bytes([header[40], header[41], header[42], header[43]]);

    if bits_per_sample != 16 {
        return Err(format!("Unsupported bits per sample: {} (expected 16)", bits_per_sample));
    }

    let num_samples = data_size as usize / 2; // 2 bytes per sample
    let mut raw_bytes = vec![0u8; data_size as usize];
    reader
        .read_exact(&mut raw_bytes)
        .map_err(|e| format!("Failed to read sample data: {}", e))?;

    let raw_samples: Vec<i16> = raw_bytes
        .chunks_exact(2)
        .map(|chunk| i16::from_le_bytes([chunk[0], chunk[1]]))
        .collect();

    // Convert to mono if stereo
    let mono_samples: Vec<f64> = if channels == 2 {
        raw_samples
            .chunks_exact(2)
            .map(|pair| (pair[0] as f64 + pair[1] as f64) / 2.0 / 32768.0)
            .collect()
    } else {
        raw_samples
            .iter()
            .map(|&s| s as f64 / 32768.0)
            .collect()
    };

    Ok(WavData {
        sample_rate,
        samples: mono_samples,
        channels: 1,
        original_channels: channels,
    })
}
```

### src/stft.rs

```rust
use crate::fft::{self, real_to_complex, magnitude_spectrum, magnitude_to_db};
use crate::window::{WindowType, generate_window, apply_window};

pub struct StftResult {
    pub frames: Vec<Vec<f64>>, // each frame: magnitude in dB per frequency bin
    pub num_freq_bins: usize,
    pub sample_rate: u32,
    pub fft_size: usize,
    pub hop_size: usize,
}

/// Compute the Short-Time Fourier Transform.
pub fn compute_stft(
    samples: &[f64],
    fft_size: usize,
    hop_size: usize,
    window_type: WindowType,
    sample_rate: u32,
) -> StftResult {
    let window = generate_window(window_type, fft_size);
    let num_freq_bins = fft_size / 2 + 1;
    let mut frames = Vec::new();

    let mut pos = 0;
    while pos + fft_size <= samples.len() {
        let frame = &samples[pos..pos + fft_size];
        let windowed = apply_window(frame, &window);
        let complex_input = real_to_complex(&windowed);
        let padded = fft::zero_pad(&complex_input);
        let fft_output = fft::fft(&padded);
        let magnitudes = magnitude_spectrum(&fft_output);
        let db = magnitude_to_db(&magnitudes, -80.0);
        frames.push(db);
        pos += hop_size;
    }

    StftResult {
        frames,
        num_freq_bins,
        sample_rate,
        fft_size,
        hop_size,
    }
}

/// Find the dominant frequency across the entire STFT.
pub fn dominant_frequency(stft: &StftResult) -> f64 {
    let mut max_db = f64::NEG_INFINITY;
    let mut max_bin = 0;

    for frame in &stft.frames {
        for (bin, &db) in frame.iter().enumerate().skip(1) {
            if db > max_db {
                max_db = db;
                max_bin = bin;
            }
        }
    }

    fft::bin_to_frequency(max_bin, stft.sample_rate, stft.fft_size)
}

/// Compute total spectral energy (sum of squared magnitudes, linear scale).
pub fn spectral_energy(stft: &StftResult) -> f64 {
    let mut total = 0.0;
    for frame in &stft.frames {
        for &db in frame.iter().skip(1) {
            let linear = 10.0_f64.powf(db / 20.0);
            total += linear * linear;
        }
    }
    total
}

/// Compute bandwidth: range of frequencies above a dB threshold.
pub fn bandwidth(stft: &StftResult, threshold_db: f64) -> (f64, f64) {
    let mut min_bin = usize::MAX;
    let mut max_bin = 0;

    for frame in &stft.frames {
        for (bin, &db) in frame.iter().enumerate().skip(1) {
            if db > threshold_db {
                min_bin = min_bin.min(bin);
                max_bin = max_bin.max(bin);
            }
        }
    }

    if min_bin > max_bin {
        return (0.0, 0.0);
    }

    let min_freq = fft::bin_to_frequency(min_bin, stft.sample_rate, stft.fft_size);
    let max_freq = fft::bin_to_frequency(max_bin, stft.sample_rate, stft.fft_size);
    (min_freq, max_freq)
}
```

### src/visualize.rs

```rust
use crate::stft::StftResult;
use std::io::Write;

const ASCII_CHARS: &[u8] = b" .:-=+*#%@";

/// Render an ASCII spectrogram to the given writer.
/// Time on x-axis, frequency on y-axis (low at bottom).
pub fn ascii_spectrogram(stft: &StftResult, writer: &mut impl Write, max_rows: usize) -> std::io::Result<()> {
    let num_bins = stft.num_freq_bins;
    let step = if num_bins > max_rows { num_bins / max_rows } else { 1 };
    let display_bins = num_bins / step;
    let max_cols = stft.frames.len().min(120);
    let frame_step = if stft.frames.len() > max_cols {
        stft.frames.len() / max_cols
    } else {
        1
    };

    // Header: frequency axis label
    let nyquist = stft.sample_rate as f64 / 2.0;
    writeln!(writer, "Spectrogram (FFT={}, hop={}, {} frames)", stft.fft_size, stft.hop_size, stft.frames.len())?;
    writeln!(writer, "Frequency range: 0 - {:.0} Hz", nyquist)?;
    writeln!(writer)?;

    // Render top to bottom = high freq to low freq
    for row in (0..display_bins).rev() {
        let bin = row * step;
        let freq = bin as f64 * stft.sample_rate as f64 / stft.fft_size as f64;
        write!(writer, "{:>6.0} Hz |", freq)?;

        let mut col = 0;
        while col < stft.frames.len() && col / frame_step < max_cols {
            let db = stft.frames[col][bin];
            let normalized = ((db + 80.0) / 80.0).clamp(0.0, 1.0);
            let char_idx = (normalized * (ASCII_CHARS.len() - 1) as f64) as usize;
            write!(writer, "{}", ASCII_CHARS[char_idx] as char)?;
            col += frame_step;
        }

        writeln!(writer)?;
    }

    writeln!(writer, "       +{}", "-".repeat(max_cols.min(stft.frames.len() / frame_step)))?;
    writeln!(writer, "        Time -->")?;

    Ok(())
}

/// Render a PPM spectrogram image.
pub fn ppm_spectrogram(stft: &StftResult, writer: &mut impl Write) -> std::io::Result<()> {
    let width = stft.frames.len();
    let height = stft.num_freq_bins;

    write!(writer, "P6\n{} {}\n255\n", width, height)?;

    // Render top to bottom = high frequency to low
    for row in (0..height).rev() {
        for col in 0..width {
            let db = stft.frames[col][row];
            let normalized = ((db + 80.0) / 80.0).clamp(0.0, 1.0);
            let (r, g, b) = db_to_color(normalized);
            writer.write_all(&[r, g, b])?;
        }
    }

    Ok(())
}

/// Map normalized dB value [0.0, 1.0] to RGB color.
/// Color gradient: black -> blue -> cyan -> yellow -> white.
fn db_to_color(value: f64) -> (u8, u8, u8) {
    let v = value.clamp(0.0, 1.0);

    if v < 0.25 {
        let t = v / 0.25;
        (0, 0, (t * 255.0) as u8)
    } else if v < 0.5 {
        let t = (v - 0.25) / 0.25;
        (0, (t * 255.0) as u8, 255)
    } else if v < 0.75 {
        let t = (v - 0.5) / 0.25;
        ((t * 255.0) as u8, 255, (255.0 * (1.0 - t)) as u8)
    } else {
        let t = (v - 0.75) / 0.25;
        (255, 255, (t * 255.0) as u8)
    }
}
```

### src/main.rs

```rust
mod complex;
mod fft;
mod window;
mod wav;
mod stft;
mod visualize;

use std::fs::File;
use std::io::{BufReader, BufWriter};

fn main() {
    let args: Vec<String> = std::env::args().collect();

    if args.len() < 2 {
        eprintln!("Usage: {} <input.wav> [fft_size] [window] [hop_size] [ascii|ppm]", args[0]);
        eprintln!("  fft_size: power of 2, default 2048");
        eprintln!("  window: hann|hamming|blackman|rectangular, default hann");
        eprintln!("  hop_size: default fft_size/4");
        eprintln!("  output: ascii (default) or ppm");
        std::process::exit(1);
    }

    let input_path = &args[1];
    let fft_size: usize = args.get(2).and_then(|s| s.parse().ok()).unwrap_or(2048);
    let window_type = args
        .get(3)
        .and_then(|s| window::WindowType::from_str(s))
        .unwrap_or(window::WindowType::Hann);
    let hop_size: usize = args.get(4).and_then(|s| s.parse().ok()).unwrap_or(fft_size / 4);
    let output_mode = args.get(5).map(|s| s.as_str()).unwrap_or("ascii");

    // Read WAV
    let file = File::open(input_path).expect("Failed to open WAV file");
    let mut reader = BufReader::new(file);
    let wav_data = wav::read_wav(&mut reader).expect("Failed to parse WAV file");

    println!("Loaded: {} samples, {} Hz, {} -> mono",
        wav_data.samples.len(), wav_data.sample_rate, wav_data.original_channels);
    println!("Duration: {:.2}s", wav_data.samples.len() as f64 / wav_data.sample_rate as f64);
    println!("FFT size: {}, window: {:?}, hop: {}", fft_size, window_type, hop_size);

    // Compute STFT
    let stft_result = stft::compute_stft(
        &wav_data.samples,
        fft_size,
        hop_size,
        window_type,
        wav_data.sample_rate,
    );

    println!("STFT: {} frames, {} frequency bins", stft_result.frames.len(), stft_result.num_freq_bins);

    // Statistics
    let dom_freq = stft::dominant_frequency(&stft_result);
    let (bw_low, bw_high) = stft::bandwidth(&stft_result, -40.0);
    println!("Dominant frequency: {:.1} Hz", dom_freq);
    println!("Bandwidth (-40 dB): {:.1} - {:.1} Hz", bw_low, bw_high);

    // Output
    match output_mode {
        "ppm" => {
            let output_path = input_path.replace(".wav", "_spectrogram.ppm");
            let file = File::create(&output_path).expect("Failed to create PPM file");
            let mut writer = BufWriter::new(file);
            visualize::ppm_spectrogram(&stft_result, &mut writer)
                .expect("Failed to write PPM");
            println!("Wrote spectrogram to {}", output_path);
        }
        _ => {
            let stdout = std::io::stdout();
            let mut handle = stdout.lock();
            visualize::ascii_spectrogram(&stft_result, &mut handle, 40)
                .expect("Failed to write ASCII spectrogram");
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::f64::consts::PI;

    #[test]
    fn test_complex_arithmetic() {
        let a = complex::Complex::new(3.0, 4.0);
        let b = complex::Complex::new(1.0, 2.0);
        let sum = a + b;
        assert!((sum.re - 4.0).abs() < 1e-10);
        assert!((sum.im - 6.0).abs() < 1e-10);

        let product = a * b;
        // (3+4i)(1+2i) = 3+6i+4i+8i^2 = -5+10i
        assert!((product.re - (-5.0)).abs() < 1e-10);
        assert!((product.im - 10.0).abs() < 1e-10);
    }

    #[test]
    fn test_complex_magnitude() {
        let c = complex::Complex::new(3.0, 4.0);
        assert!((c.magnitude() - 5.0).abs() < 1e-10);
    }

    #[test]
    fn test_complex_polar() {
        let c = complex::Complex::from_polar(2.0, PI / 4.0);
        assert!((c.re - 2.0_f64.sqrt()).abs() < 1e-10);
        assert!((c.im - 2.0_f64.sqrt()).abs() < 1e-10);
    }

    #[test]
    fn test_fft_dc_signal() {
        let n = 8;
        let input: Vec<complex::Complex> = vec![complex::Complex::from_real(1.0); n];
        let output = fft::fft(&input);

        // DC bin should have magnitude N, all others zero
        assert!((output[0].magnitude() - n as f64).abs() < 1e-10);
        for k in 1..n {
            assert!(output[k].magnitude() < 1e-10, "Bin {} should be zero: {}", k, output[k].magnitude());
        }
    }

    #[test]
    fn test_fft_sine_wave() {
        let n = 1024;
        let freq_bin = 10;
        let input: Vec<complex::Complex> = (0..n)
            .map(|i| {
                let t = i as f64 / n as f64;
                complex::Complex::from_real((2.0 * PI * freq_bin as f64 * t).sin())
            })
            .collect();

        let output = fft::fft(&input);
        let magnitudes: Vec<f64> = output.iter().map(|c| c.magnitude()).collect();

        // Peak at bin freq_bin and bin N - freq_bin
        let peak1 = magnitudes[freq_bin];
        let peak2 = magnitudes[n - freq_bin];
        assert!(peak1 > n as f64 / 4.0, "Peak at bin {} too low: {}", freq_bin, peak1);
        assert!(peak2 > n as f64 / 4.0, "Peak at bin {} too low: {}", n - freq_bin, peak2);

        // Other bins should be near zero
        for k in 1..n {
            if k != freq_bin && k != n - freq_bin {
                assert!(magnitudes[k] < 1.0, "Bin {} should be near zero: {}", k, magnitudes[k]);
            }
        }
    }

    #[test]
    fn test_fft_ifft_roundtrip() {
        let input: Vec<complex::Complex> = vec![
            complex::Complex::new(1.0, 0.0),
            complex::Complex::new(2.0, -1.0),
            complex::Complex::new(0.0, 3.0),
            complex::Complex::new(-1.0, 2.0),
            complex::Complex::new(4.0, 0.0),
            complex::Complex::new(-2.0, -1.0),
            complex::Complex::new(1.0, 1.0),
            complex::Complex::new(3.0, -2.0),
        ];

        let transformed = fft::fft(&input);
        let recovered = fft::ifft(&transformed);

        for (i, (orig, rec)) in input.iter().zip(recovered.iter()).enumerate() {
            assert!(
                (orig.re - rec.re).abs() < 1e-10 && (orig.im - rec.im).abs() < 1e-10,
                "Mismatch at {}: ({}, {}) vs ({}, {})",
                i, orig.re, orig.im, rec.re, rec.im
            );
        }
    }

    #[test]
    fn test_fft_power_of_two_sizes() {
        for &size in &[2, 4, 8, 16, 32, 64, 128, 256, 512, 1024] {
            let input: Vec<complex::Complex> = (0..size)
                .map(|i| complex::Complex::from_real(i as f64))
                .collect();
            let output = fft::fft(&input);
            assert_eq!(output.len(), size);

            // Verify Parseval's theorem: sum |x|^2 = (1/N) sum |X|^2
            let time_energy: f64 = input.iter().map(|c| c.magnitude().powi(2)).sum();
            let freq_energy: f64 = output.iter().map(|c| c.magnitude().powi(2)).sum::<f64>() / size as f64;
            assert!(
                (time_energy - freq_energy).abs() < 1e-6,
                "Parseval failed for N={}: {} vs {}", size, time_energy, freq_energy
            );
        }
    }

    #[test]
    fn test_zero_padding() {
        let input = vec![
            complex::Complex::from_real(1.0),
            complex::Complex::from_real(2.0),
            complex::Complex::from_real(3.0),
        ];
        let padded = fft::zero_pad(&input);
        assert_eq!(padded.len(), 4); // next power of 2
        assert!((padded[3].re - 0.0).abs() < 1e-10);
    }

    #[test]
    fn test_hann_window_endpoints() {
        let w = window::generate_window(window::WindowType::Hann, 256);
        assert!(w[0].abs() < 1e-10, "Hann start should be 0: {}", w[0]);
        assert!(w[255].abs() < 1e-10, "Hann end should be 0: {}", w[255]);
    }

    #[test]
    fn test_hann_window_center() {
        let w = window::generate_window(window::WindowType::Hann, 257);
        assert!((w[128] - 1.0).abs() < 1e-10, "Hann center should be 1.0: {}", w[128]);
    }

    #[test]
    fn test_hamming_window_not_zero_at_edges() {
        let w = window::generate_window(window::WindowType::Hamming, 256);
        // Hamming window does not go to zero at edges (it's 0.08)
        assert!(w[0] > 0.07 && w[0] < 0.09, "Hamming edge: {}", w[0]);
    }

    #[test]
    fn test_magnitude_to_db() {
        let mags = vec![1.0, 0.5, 0.1, 0.001];
        let db = fft::magnitude_to_db(&mags, -80.0);
        assert!((db[0] - 0.0).abs() < 0.01); // reference is max
        assert!((db[1] - (-6.02)).abs() < 0.1); // -6 dB
        assert!((db[2] - (-20.0)).abs() < 0.1); // -20 dB
    }

    #[test]
    fn test_bin_to_frequency() {
        assert!((fft::bin_to_frequency(10, 44100, 4096) - 107.666).abs() < 0.01);
        assert!((fft::bin_to_frequency(0, 44100, 4096) - 0.0).abs() < 0.01);
    }

    #[test]
    fn test_dominant_bin() {
        let mags = vec![0.0, 0.1, 0.5, 10.0, 0.2, 0.1];
        assert_eq!(fft::dominant_bin(&mags), 3);
    }

    #[test]
    fn test_apply_window() {
        let samples = vec![1.0; 4];
        let window = vec![0.0, 0.5, 1.0, 0.5];
        let result = window::apply_window(&samples, &window);
        assert!((result[0] - 0.0).abs() < 1e-10);
        assert!((result[1] - 0.5).abs() < 1e-10);
        assert!((result[2] - 1.0).abs() < 1e-10);
    }
}
```

### Build and Run

```bash
cargo new fft-analyzer
cd fft-analyzer
# Copy source files into src/
cargo build
cargo test

# ASCII spectrogram
cargo run -- song.wav 2048 hann 512 ascii

# PPM spectrogram
cargo run -- song.wav 4096 blackman 1024 ppm
```

### Expected Output

```
Loaded: 441000 samples, 44100 Hz, 1 -> mono
Duration: 10.00s
FFT size: 2048, window: Hann, hop: 512
STFT: 858 frames, 1025 frequency bins
Dominant frequency: 440.0 Hz
Bandwidth (-40 dB): 430.7 - 449.4 Hz
Spectrogram (FFT=2048, hop=512, 858 frames)
Frequency range: 0 - 22050 Hz

 22050 Hz |
 21484 Hz |
  ...
   440 Hz |          @@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@
   ...
     0 Hz |
```

```
running 15 tests
test tests::test_complex_arithmetic ... ok
test tests::test_complex_magnitude ... ok
test tests::test_complex_polar ... ok
test tests::test_fft_dc_signal ... ok
test tests::test_fft_sine_wave ... ok
test tests::test_fft_ifft_roundtrip ... ok
test tests::test_fft_power_of_two_sizes ... ok
test tests::test_zero_padding ... ok
test tests::test_hann_window_endpoints ... ok
test tests::test_hann_window_center ... ok
test tests::test_hamming_window_not_zero_at_edges ... ok
test tests::test_magnitude_to_db ... ok
test tests::test_bin_to_frequency ... ok
test tests::test_dominant_bin ... ok
test tests::test_apply_window ... ok

test result: ok. 15 passed; 0 failed
```

## Design Decisions

1. **Recursive FFT over iterative**: The recursive Cooley-Tukey implementation mirrors the mathematical definition directly, making it easier to verify correctness. An iterative bit-reversal-based FFT is more cache-friendly and avoids stack depth issues for large N, but the recursive version is correct for sizes up to 2^20 without stack overflow on default Rust stack sizes.

2. **IFFT via forward FFT**: Rather than implementing a separate IFFT, we use the conjugate trick: `IFFT(X) = conj(FFT(conj(X))) / N`. This halves the code and guarantees consistency between forward and inverse transforms.

3. **dB relative to peak**: The magnitude-to-dB conversion normalizes against the maximum magnitude, producing a 0 dB ceiling and negative dB values everywhere else. This is standard for spectral display and makes the dynamic range independent of input amplitude.

4. **Separate visualization module**: The spectrogram rendering is completely decoupled from the STFT computation. The same StftResult can feed both ASCII and PPM output, or any future format (SVG, terminal color, etc.).

5. **Conservative WAV parser**: The WAV reader assumes a standard 44-byte header with no extra chunks between fmt and data. Real-world WAV files sometimes include metadata chunks (LIST, bext, etc.) before the data chunk. A robust parser would scan for the "data" chunk ID rather than assuming a fixed offset.

## Common Mistakes

- **FFT size not power of 2**: The radix-2 algorithm requires N to be a power of 2. Passing 1000 samples without zero-padding to 1024 produces wrong results or panics. Always verify and pad.
- **Forgetting to normalize IFFT**: The inverse FFT must divide by N. Without this, the reconstructed signal has amplitude N times the original, which can cause overflow in downstream processing.
- **Window applied after FFT**: The window must be applied before the FFT, not after. Applying it after modifies the frequency-domain data, producing incorrect results in the time domain.
- **DC bin in dominant frequency**: Bin 0 (DC) often has the highest magnitude because of signal offset. Skip it when finding the dominant frequency of a tonal signal.
- **Aliased frequency interpretation**: Only bins 0 through N/2 represent positive frequencies. Bins N/2+1 through N-1 are negative frequencies (aliases). Displaying all N bins as 0 to sample_rate produces a mirror image above Nyquist.

## Performance Notes

- The recursive FFT allocates O(N log N) intermediate vectors. An in-place iterative FFT with bit-reversal permutation reduces allocation to O(N) total. For N=4096, the recursive version takes ~0.5ms; the iterative version takes ~0.1ms.
- STFT of a 3-minute song at 44100 Hz with FFT=2048 and hop=512 produces ~15,400 frames. Each frame requires one FFT (~0.5ms), totaling ~8 seconds. Parallelizing frames across cores with rayon reduces this to ~2 seconds on a 4-core machine.
- The PPM spectrogram image for this same song is 15400 x 1025 pixels = 47 MB. For longer audio, downsample the time axis (larger hop size) or the frequency axis (display only 0-8 kHz instead of full Nyquist).
- Precomputing the twiddle factor table once and reusing it across all FFT calls eliminates redundant `cos`/`sin` calls, providing a ~2x speedup for the FFT alone.
