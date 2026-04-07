# pitcher
A naive pitch shifting app


Based on an algorithm by [Stephan Bernsee](http://blogs.zynaptiq.com/bernsee/pitch-shifting-using-the-ft/), with further inspiration from [Patrick Stephen](https://github.com/200sc/klangsynthese).

Uses [malgo](https://github.com/gen2brain/malgo) (Based on [Miniaudio](https://miniaud.io/)) for audio IO.

[fyne](https://fyne.io/) for GUI

[fftw](https://github.com/runningwild/go-fftw/) (Go bindings for [Fastest Fourier Transform in the West](https://www.fftw.org/)) for FFT functionality

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
- Buffer copy and shift
