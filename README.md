# pitcher
A pitch shifting app with multiple algorithms.

Uses [malgo](https://github.com/gen2brain/malgo) (based on [Miniaudio](https://miniaud.io/)) for audio IO.

[fyne](https://fyne.io/) for GUI

[fftw](https://github.com/runningwild/go-fftw/) (Go bindings for [Fastest Fourier Transform in the West](https://www.fftw.org/)) for FFT functionality

## Algorithms

Select with `--algo <shortname>` on the command line, or from the drop-down in the GUI.

| Full name | Short name | Default frame size | Default oversampling |
|---|---|---|---|
| Phase Vocoder | `phasvoc` | 512 | 4 |
| Pitch-Synchronous Overlap-Add (PSOLA) | `psola` | 256 | 2 |
| Sines/Transients/Noise (STN) | `stn` | 2048 | 4 |
| Low Latency STFT | `llstft` | 512 | 4 |
| Waveform Similarity Overlap-Add (WSOLA) | `wsola` | 512 | 2 |

**Phase Vocoder** is the default. Based on the algorithm by [Stephan Bernsee](http://blogs.zynaptiq.com/bernsee/pitch-shifting-using-the-ft/), with further inspiration from [Patrick Stephen](https://github.com/200sc/klangsynthese). Frequency-domain approach; good general quality.

**PSOLA** is a time-domain grain resampling algorithm. Lowest latency.

**STN** decomposes each frame into Sines, Transients, and Noise components using fuzzy masks (Fierro & Välimäki 2023), shifts sines and noise independently, and passes transients through unmodified. Noise component is reconstructed via Noise Morphing (Moliner et al. 2024). Based on [Polak & Erkut, DAS|DAGA 2025](https://pub.dega-akustik.de/DAS-DAGA_2025/files/upload/paper/635.pdf).

**WSOLA** searches backward by up to one synthesis hop (delta = Step) to find the analysis grain whose beginning maximises cross-correlation with the current synthesis overlap region, then resamples that grain for pitch shifting. This suppresses waveform discontinuities at grain boundaries compared to PSOLA, at the cost of one extra dot-product search per frame. Based on [Verhelst & Roelands, ICASSP 1993](https://doi.org/10.1109/ICASSP.1993.319366).

**Low Latency STFT** remaps bins by simple rounding (`b = round(a·ratio)`) and applies a per-frame phase correction to maintain vertical phase coherence — no frequency estimation is performed. This makes it significantly more robust than the phase vocoder when small frame sizes are required for low latency. Phasiness is avoided at the cost of mild transient duplication (one copy per oversampling period). Based on [Juillerat & Hirsbrunner, ICALIP 2010](https://doi.org/10.1109/ICALIP.2010.5685234).

## SIMD Acceleration

Requires Go 1.26+ and AVX CPU support. To build with SIMD-accelerated DSP loops:

```sh
GOEXPERIMENT=simd go build .
```

Without `GOEXPERIMENT=simd`, the project compiles and runs with scalar fallbacks.

SIMD-optimized operations:
- Windowing multiply
- Magnitude computation (2 × √(re² + im²))
- Array zeroing
- Output accumulator (multiply-add)
- Scalar-multiply accumulate (acc[i] += src[i] × scalar)
- Buffer copy
