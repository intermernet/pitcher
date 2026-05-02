/****************************************************************************
*
* COPYRIGHT 2025 Mike Hughes <mike <AT> mikehughes <DOT> info
*
*****************************************************************************
*
* Low Latency STFT pitch-shifting algorithm based on:
*
* N. Juillerat, B. Hirsbrunner,
* "Low Latency Audio Pitch Shifting in the Frequency Domain",
* ICALIP 2010.
*
* Core idea: bins are remapped by simple rounding (b = round(k*ratio)), and a
* per-frame phase correction is applied to maintain vertical phase coherence
* across every O-th frame, avoiding the phase accumulation of the traditional
* phase vocoder and its sensitivity to small DFT sizes.
*
*****************************************************************************/

package algos

import "math"

// llstftState holds per-channel state for the Low Latency STFT algorithm.
type llstftState struct {
	channels   []llstftChanState
	frameCount int // global frame counter (shared across channels)
}

type llstftChanState struct {
	// no per-channel persistent state needed beyond what is in Context
}

// NewLLSTFTState allocates state for the Low Latency STFT algorithm.
func NewLLSTFTState(ctx *Context) interface{} {
	return &llstftState{
		channels: make([]llstftChanState, ctx.Channels),
	}
}

// ProcessLLSTFT implements the Low Latency STFT pitch-shifting algorithm
// (Juillerat & Hirsbrunner, ICALIP 2010).
func ProcessLLSTFT(ctx *Context, output, input []byte) {
	byteDepth := ctx.BitDepth / 8
	ratio := math.Exp2(ctx.PitchShift / 12.0)
	state := ctx.AlgoState.(*llstftState)

	twoPI := 2.0 * math.Pi
	N := ctx.FFTFrameSize
	O := ctx.Oversampling
	half := N / 2

	for c := 0; c < int(ctx.Channels); c++ {
		numSamples := bytesToFloat64(ctx.F64Buf, input, ctx.Channels, ctx.BitDepth, c)
		frameIndex := ctx.FrameIndex[c]

		for i := 0; i < numSamples; i++ {
			ctx.Frame[c][frameIndex] = ctx.F64Buf[i]
			ctx.F64Buf[i] = ctx.Stack[c][frameIndex-ctx.Latency]
			frameIndex++

			if frameIndex >= N {
				frameIndex = ctx.Latency
				p := state.frameCount // frame number

				// --- Analysis window + forward FFT ---
				mulFloat64s(ctx.Reals[:N], ctx.Frame[c], ctx.Window)
				for k := 0; k < N; k++ {
					ctx.FFTWData.Elems[k] = complex(ctx.Reals[k], 0)
				}
				ctx.Forward.Execute()

				// --- Bin remapping + phase correction (eq. 1 & 2 from paper) ---
				// Save the analysis spectrum before zeroing the synthesis buffer.
				// Re-use ctx.Magnitudes/Frequencies as scratch for the real/imag parts.
				for k := 0; k < N; k++ {
					ctx.Magnitudes[k] = real(ctx.FFTWData.Elems[k])
					ctx.Frequencies[k] = imag(ctx.FFTWData.Elems[k])
				}

				// Zero the synthesis spectrum ready for accumulation.
				for k := 0; k < N; k++ {
					ctx.FFTWData.Elems[k] = 0
				}

				for a := 0; a <= half; a++ {
					b := int(float64(a)*ratio + 0.5)
					if b < 0 || b > half {
						continue
					}
					re := ctx.Magnitudes[a]
					im := ctx.Frequencies[a]

					// Phase correction angle θ = -(b-a)*p/O * 2π/N
					theta := -float64(b-a) * float64(p) / float64(O) * twoPI / float64(N)
					cosT := math.Cos(theta)
					sinT := math.Sin(theta)

					// Multiply phasor (re + i*im) by exp(iθ) = cosT + i*sinT
					newRe := re*cosT - im*sinT
					newIm := re*sinT + im*cosT

					ctx.FFTWData.Elems[b] += complex(newRe, newIm)
				}

				// Mirror conjugate for bins above Nyquist so the IFFT output is real.
				for k := 1; k < half; k++ {
					ctx.FFTWData.Elems[N-k] = complex(real(ctx.FFTWData.Elems[k]), -imag(ctx.FFTWData.Elems[k]))
				}
				ctx.FFTWData.Elems[N/2] = complex(real(ctx.FFTWData.Elems[half]), 0)

				// --- Inverse FFT ---
				ctx.Inverse.Execute()

				// --- Synthesis window + OLA ---
				// Apply synthesis Hann window and normalised OLA accumulation.
				for k := 0; k < N; k++ {
					ctx.Reals[k] = real(ctx.FFTWData.Elems[k])
				}
				mulAddFloat64s(ctx.OutAcc[c][:N], ctx.WindowFactors, ctx.Reals[:N])
				copyFloat64s(ctx.Stack[c][:ctx.Step], ctx.OutAcc[c][:ctx.Step])
				copyFloat64s(ctx.OutAcc[c][:N], ctx.OutAcc[c][ctx.Step:ctx.Step+N])
				copyFloat64s(ctx.Frame[c][:ctx.Latency], ctx.Frame[c][ctx.Step:ctx.Step+ctx.Latency])

				// Only increment the global frame counter once per frame (use channel 0).
				if c == 0 {
					state.frameCount++
				}
			}
		}

		ctx.FrameIndex[c] = frameIndex

		stride := int(byteDepth * ctx.Channels)
		off := c * int(byteDepth)
		for i := 0; i < numSamples; i++ {
			float64ToFloat32Bytes(output, off, ctx.F64Buf[i]*ctx.Volume)
			off += stride
		}
	}
}
