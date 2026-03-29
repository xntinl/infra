<!-- difficulty: advanced -->
<!-- category: audio-video-processing -->
<!-- languages: [rust] -->
<!-- concepts: [fft, cooley-tukey, windowing-functions, spectrum-analysis, stft, spectrogram, wav-parsing, frequency-domain, dsp] -->
<!-- estimated_time: 12-18 hours -->
<!-- bloom_level: apply, analyze, evaluate -->
<!-- prerequisites: [complex-numbers, trigonometry, floating-point-arithmetic, file-io, bitwise-operations, rust-iterators] -->

# Challenge 106: Audio FFT Spectrum Analyzer

## Languages

Rust (stable, latest edition)

## Prerequisites

- Complex number arithmetic: addition, multiplication, magnitude, phase
- Trigonometric functions and Euler's formula: `e^(ix) = cos(x) + i*sin(x)`
- Understanding of frequency vs. time domain: a signal is a sum of sinusoids at different frequencies
- Floating-point arithmetic and numerical precision
- File I/O for reading WAV files (little-endian binary parsing)
- Rust iterators, Vec operations, and slice manipulation

## Learning Objectives

- **Implement** the Cooley-Tukey radix-2 FFT algorithm, understanding the divide-and-conquer decomposition of the DFT
- **Apply** windowing functions (Hann, Hamming, Blackman) to understand spectral leakage and its mitigation
- **Analyze** the relationship between FFT size, sample rate, frequency resolution, and time resolution
- **Design** an STFT pipeline that decomposes a time-domain signal into a time-frequency representation
- **Evaluate** the spectral content of real audio signals by identifying dominant frequencies and harmonics
- **Create** visual representations of frequency data as ASCII spectrograms and PPM spectral images

## The Challenge

The Fast Fourier Transform converts a time-domain signal into its frequency-domain representation. Instead of seeing amplitude over time, you see amplitude over frequency -- which frequencies are present in the signal and how strong each one is. The FFT is the most important algorithm in digital signal processing, underlying everything from audio compression (MP3, AAC) to image processing (JPEG) to wireless communications (OFDM).

The Cooley-Tukey radix-2 FFT computes the DFT of N samples in O(N log N) operations instead of the naive O(N^2). It works by recursively splitting the N-point DFT into two N/2-point DFTs (even-indexed and odd-indexed samples), computing each recursively, then combining the results using "twiddle factors" -- complex exponentials that rotate the odd DFT's output before adding it to the even DFT's output.

Build a spectrum analyzer that reads WAV audio files, applies windowing functions, computes the FFT, and produces visualizations. Implement the Short-Time Fourier Transform (STFT) to analyze how the frequency content changes over time, and generate spectrograms that show time on one axis, frequency on the other, and amplitude as brightness or ASCII characters. The FFT implementation must be from scratch -- no external FFT libraries.

## Requirements

1. Implement a `Complex` type with: addition, subtraction, multiplication, magnitude (absolute value), phase (angle), and construction from polar form `r * e^(i*theta)`
2. Implement the Cooley-Tukey radix-2 decimation-in-time FFT. Input must be a power of 2 in length. If the input is shorter, zero-pad to the next power of 2
3. Implement the inverse FFT using the forward FFT: conjugate input, apply FFT, conjugate output, divide by N
4. Verify the FFT with known signals: a pure sine wave at frequency f should produce peaks at bins f and N-f. A DC signal should produce energy only in bin 0
5. Implement three windowing functions, each returning N coefficients that multiply the input samples before FFT:
   - Hann: `w[n] = 0.5 * (1 - cos(2*PI*n/(N-1)))`
   - Hamming: `w[n] = 0.54 - 0.46 * cos(2*PI*n/(N-1))`
   - Blackman: `w[n] = 0.42 - 0.5 * cos(2*PI*n/(N-1)) + 0.08 * cos(4*PI*n/(N-1))`
6. Parse WAV files (PCM 16-bit, mono or stereo). Read the RIFF header, fmt chunk, and data chunk. For stereo, average left and right channels into mono. Normalize samples to [-1.0, 1.0]
7. Compute the magnitude spectrum: for each FFT output bin k, compute `|X[k]|`. Convert to decibels: `dB = 20 * log10(|X[k]| / max_magnitude)`. Only the first N/2+1 bins are meaningful (Nyquist)
8. Map FFT bins to frequencies: bin k corresponds to frequency `k * sample_rate / N` Hz
9. Implement STFT: divide the signal into overlapping frames (e.g., frame size 2048, hop size 512), apply a window to each frame, compute FFT, store the magnitude spectrum. The result is a 2D array: time (frame index) vs. frequency (bin index)
10. Generate an ASCII spectrogram: map magnitude to characters (e.g., ` .:-=+*#%@`), with time on the horizontal axis and frequency on the vertical axis (low frequencies at bottom)
11. Generate a PPM image spectrogram: map magnitude (dB) to a color gradient (black -> blue -> cyan -> yellow -> white), with time on the x-axis and frequency on the y-axis
12. Accept command-line arguments: input WAV path, FFT size, window type, hop size, output format (ascii/ppm)
13. Print summary statistics: dominant frequency, total spectral energy, bandwidth (range of frequencies above a threshold)

## Hints

1. The Cooley-Tukey butterfly operation at each stage combines pairs of values:
   ```
   X_even = FFT(x[0], x[2], x[4], ...)
   X_odd  = FFT(x[1], x[3], x[5], ...)
   For k = 0..N/2-1:
       twiddle = e^(-2*PI*i*k/N)
       X[k]       = X_even[k] + twiddle * X_odd[k]
       X[k + N/2] = X_even[k] - twiddle * X_odd[k]
   ```
   The recursion bottoms out at N=1, where the FFT of a single element is itself.

2. Windowing is essential for STFT. Without a window, each frame has sharp edges that create
   artificial high-frequency content (spectral leakage). The window function tapers the frame
   to zero at the edges. Hann is the most common choice -- it offers a good balance between
   frequency resolution and leakage suppression. Apply the window by element-wise multiplication
   before the FFT.

3. The hop size controls the time overlap between consecutive STFT frames. A hop of N/4 (75%
   overlap) gives smooth temporal resolution. A hop of N (no overlap) gives coarse resolution.
   Smaller hops produce more frames and a smoother spectrogram, at the cost of more FFT
   computations.

4. For the dB conversion, use a reference of the maximum magnitude across all bins to normalize
   the range. A floor of -80 dB (or -96 dB for 16-bit audio) prevents log10(0) and keeps the
   dynamic range manageable. The formula is:
   `dB = 20 * log10(magnitude / reference).max(-80.0)`.

5. The PPM spectrogram image should have width = number of STFT frames and height = N/2
   (number of frequency bins up to Nyquist). Render frequency bottom-to-top (row 0 = Nyquist,
   last row = 0 Hz) or top-to-bottom consistently. A good color map for spectrograms is the
   "inferno" or "viridis" style: dark for quiet, bright for loud.

## Acceptance Criteria

- [ ] FFT of a 440 Hz sine wave (sampled at 44100 Hz, N=4096) shows a peak at the bin closest to 440 Hz and negligible energy elsewhere
- [ ] FFT of a DC signal (all samples = 1.0) shows energy only in bin 0
- [ ] Inverse FFT recovers the original signal: `IFFT(FFT(x))` equals `x` within f64 precision (max error < 1e-10)
- [ ] FFT correctly handles all power-of-2 sizes from 2 to at least 65536
- [ ] Zero-padding a non-power-of-2 input produces correct results (verified against known DFT)
- [ ] Hann window values at endpoints are 0.0 and at the center are 1.0
- [ ] Applying a Hann window before FFT reduces spectral leakage compared to rectangular (no) window
- [ ] WAV files (PCM 16-bit, 44100 Hz) are parsed correctly: sample count matches file size, values are in [-1.0, 1.0]
- [ ] Stereo WAV files are correctly downmixed to mono
- [ ] Magnitude spectrum in dB has a meaningful dynamic range (at least 60 dB for 16-bit audio)
- [ ] STFT with hop_size < frame_size produces overlapping frames with smooth temporal transitions
- [ ] ASCII spectrogram of a frequency sweep shows a clear diagonal line from low to high frequency
- [ ] PPM spectrogram is a valid image file viewable in standard image viewers
- [ ] Dominant frequency detection identifies the fundamental frequency of a simple tone within one bin width
- [ ] No external FFT or DSP libraries -- all algorithms implemented from scratch
- [ ] All tests pass with `cargo test`

## Research Resources

- [The Fast Fourier Transform (FFT) -- Cooley-Tukey Algorithm (Wikipedia)](https://en.wikipedia.org/wiki/Cooley%E2%80%93Tukey_FFT_algorithm) -- mathematical derivation of the radix-2 butterfly decomposition
- [An Interactive Introduction to Fourier Transforms (Jez Swanson)](https://www.jezzamon.com/fourier/) -- visual, interactive explanation of what the Fourier transform does
- [The Scientist and Engineer's Guide to DSP, Chapter 12 (Steven Smith)](https://www.dspguide.com/ch12.htm) -- the FFT explained with code and diagrams, freely available online
- [Understanding the FFT Algorithm (Jake VanderPlas)](https://jakevdp.github.io/blog/2013/08/28/understanding-the-fft/) -- Python-based walkthrough of the Cooley-Tukey algorithm with step-by-step derivations
- [Window Functions for Spectral Analysis (Julius O. Smith III)](https://ccrma.stanford.edu/~jos/sasp/Spectrum_Analysis_Windows.html) -- comprehensive reference on Hann, Hamming, Blackman, and other windows
- [Short-Time Fourier Transform (Wikipedia)](https://en.wikipedia.org/wiki/Short-time_Fourier_transform) -- STFT definition, overlap, and the time-frequency resolution trade-off
- [WAV File Format (Stanford CCRMA)](https://ccrma.stanford.edu/courses/422-winter-2014/projects/WaveFormat/) -- PCM WAV header byte layout
- [3Blue1Brown: But what is the Fourier Transform?](https://www.youtube.com/watch?v=spUNpyF58BY) -- intuitive visual explanation of the Fourier transform as "winding" a signal around a circle
- [Spectrogram -- Wikipedia](https://en.wikipedia.org/wiki/Spectrogram) -- how spectrograms represent time-frequency energy distributions
