/****************************************************************************
*
* COPYRIGHT 2026 Mike Hughes <mike <AT> mikehughes <DOT> info
*
*****************************************************************************
*
* "Based on Signalsmith Stretch" pitch-shifting algorithm.
*
* Inspired by and based on the design described in:
*   G. Luff, "The Design of Signalsmith Stretch", 2023.
*   https://signalsmith-audio.co.uk/writing/2023/stretch-design/
*
* Reference C++ implementation (MIT licence):
*   https://github.com/Signalsmith-Audio/signalsmith-stretch
*
* Core idea: assemble each output STFT spectrum using a weighted blend of
* multiple complex-valued phase predictions.  Predictions are formed by
* multiplying a known output complex value by the observed phase change
* ("twist") between the corresponding points in the input spectrum.  This
* naturally weights predictions by the signal energy at the relevant
* input positions, so strong tones dominate the blend and impose correct
* phase relationships on nearby bins.
*
* Two-pass design per frame:
*
*   Pass 1 â€“ horizontal prediction (like a standard phase vocoder):
*     For each output bin b, measure the phase change at the mapped input
*     position between the previous and current frames, and apply it to the
*     previous output bin.  This gives a horizontally-coherent first estimate.
*
*   Pass 2 â€“ vertical prediction (assembles from neighbouring bins):
*     Iterate upwards from DC.  For each bin b combine four vertical predictors:
*       â€¢ short upward:   from pass-2 result at bâˆ’1, using input twist from
*                         (inBinâˆ’1) to inBin
*       â€¢ long  upward:   from pass-2 result at bâˆ’L, using input twist from
*                         (inBinâˆ’L) to inBin
*       â€¢ short downward: from pass-1 result at b+1, using the reverse of
*                         b+1's own upward twist
*       â€¢ long  downward: from pass-1 result at b+L, using the reverse of
*                         b+L's own upward twist
*     All vertical twists are measured as fixed 1- or L-step offsets in the
*     input bin space (not in output bin space), consistent with the reference
*     implementation.  The final magnitude is set to |curInput[inBin]|.
*
* Omissions vs. the reference library:
*   â€¢ No non-linear frequency map / peak detection
*   â€¢ No formant preservation
*   â€¢ No time-stretch (pitch-shift only, time factor = 1)
*   â€¢ Single-resolution (no adaptive block size)
*
*****************************************************************************/

package algos

import "math"

// sssState holds per-channel state for the SSS algorithm.
type sssState struct {
	prevInput  [][]complex128 // [ch][bins]: input spectrum from the previous frame
	prevOutput [][]complex128 // [ch][bins]: pass-2 output from the previous frame
	pass1Out   [][]complex128 // [ch][bins]: pass-1 (horizontal) output for this frame
	curInput   [][]complex128 // [ch][bins]: current frame input spectrum (scratch)
	longStep   int            // long vertical step in bins = round(N / step) = oversampling
}

// NewSSSState allocates state for the SSS algorithm.
func NewSSSState(ctx *Context) interface{} {
	bins := ctx.FFTFrameSize/2 + 1
	nCh := int(ctx.Channels)
	longStep := ctx.Oversampling
	if longStep < 1 {
		longStep = 1
	}
	st := &sssState{
		prevInput:  make([][]complex128, nCh),
		prevOutput: make([][]complex128, nCh),
		pass1Out:   make([][]complex128, nCh),
		curInput:   make([][]complex128, nCh),
		longStep:   longStep,
	}
	for c := 0; c < nCh; c++ {
		st.prevInput[c] = make([]complex128, bins)
		st.prevOutput[c] = make([]complex128, bins)
		st.pass1Out[c] = make([]complex128, bins)
		st.curInput[c] = make([]complex128, bins)
	}
	return st
}

// sssInterp linearly interpolates a complex spectrum at fractional bin f.
// Returns 0 for positions outside [0, len(spec)).
func sssInterp(spec []complex128, f float64) complex128 {
	n := len(spec)
	if f < 0 || f >= float64(n) {
		return 0
	}
	lo := int(f)
	if lo >= n-1 {
		return spec[n-1]
	}
	frac := f - float64(lo)
	return complex(
		real(spec[lo])*(1-frac)+real(spec[lo+1])*frac,
		imag(spec[lo])*(1-frac)+imag(spec[lo+1])*frac,
	)
}

// sssConjMul returns a * conj(b).
func sssConjMul(a, b complex128) complex128 {
	ar, ai := real(a), imag(a)
	br, bi := real(b), imag(b)
	return complex(ar*br+ai*bi, ai*br-ar*bi)
}

// sssSetMag returns a complex value with the phase of pred and magnitude |ref|.
// Falls back to ref itself when pred is near-zero (no prediction signal).
func sssSetMag(pred, ref complex128) complex128 {
	pr, pi := real(pred), imag(pred)
	pm2 := pr*pr + pi*pi
	rr, ri := real(ref), imag(ref)
	mag := math.Sqrt(rr*rr + ri*ri)
	if pm2 < 1e-30 {
		return ref // no prediction: preserve input phase
	}
	scale := mag / math.Sqrt(pm2)
	return complex(pr*scale, pi*scale)
}

// ProcessSSS implements the "Based on Signalsmith Stretch" pitch-shifting
// algorithm (Luff 2023 / Signalsmith-Audio/signalsmith-stretch).
func ProcessSSS(ctx *Context, output, input []byte) {
	byteDepth := ctx.BitDepth / 8
	ratio := math.Exp2(ctx.PitchShift / 12.0)
	st := ctx.AlgoState.(*sssState)
	N := ctx.FFTFrameSize
	bins := N/2 + 1
	longStep := st.longStep

	for c := 0; c < int(ctx.Channels); c++ {
		numSamples := bytesToFloat64(ctx.F64Buf, input, ctx.Channels, ctx.BitDepth, c)
		frameIndex := ctx.FrameIndex[c]

		for i := 0; i < numSamples; i++ {
			ctx.Frame[c][frameIndex] = ctx.F64Buf[i]
			ctx.F64Buf[i] = ctx.Stack[c][frameIndex-ctx.Latency]
			frameIndex++

			if frameIndex >= N {
				frameIndex = ctx.Latency

				// â”€â”€ Window + forward FFT â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
				mulFloat64s(ctx.Reals[:N], ctx.Frame[c], ctx.Window)
				for k := 0; k < N; k++ {
					ctx.FFTData[k] = complex(ctx.Reals[k], 0)
				}
				ctx.Forward.Execute(ctx.FFTData, ctx.FFTData)

				// Save the current half-spectrum before either pass overwrites
				// FFTWData (pass 2 writes to FFTWData bin by bin).
				for k := 0; k < bins; k++ {
					st.curInput[c][k] = ctx.FFTData[k]
				}

				// â”€â”€ Pass 1: horizontal (phase-vocoder) prediction â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
				//
				// For each output bin b, measure the phase change at the
				// mapped input position between prevInput and curInput; apply
				// it to the previous output value at bin b.  Vertical
				// coherence is NOT yet enforced.
				for b := 0; b < bins; b++ {
					inBin := float64(b) / ratio
					inC := sssInterp(st.curInput[c], inBin)
					prevC := sssInterp(st.prevInput[c], inBin)
					// twist = curInput[inBin] * conj(prevInput[inBin])
					pred := st.prevOutput[c][b] * sssConjMul(inC, prevC)
					st.pass1Out[c][b] = sssSetMag(pred, inC)
				}

				// â”€â”€ Pass 2: vertical predictions â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
				//
				// Iterates upward (b = 0, 1, â€¦, binsâˆ’1).  Four predictors:
				//
				//   Upward short/long: use the pass-2 result already written
				//     at bâˆ’1 / bâˆ’longStep (bins below, already stable).
				//     Twist is measured from input position (inBin âˆ’ offset)
				//     to inBin â€” a fixed 1- or L-step shift in input space,
				//     matching the reference implementation.
				//
				//   Downward short/long: use the pass-1 result at b+1 /
				//     b+longStep (bins above, pass-1 is still intact).
				//     The twist at the upper bin is computed from its own
				//     inBin, and then its conjugate is applied to predict
				//     downward.
				//
				// The combined prediction is normalised to |curInput[inBin]|.
				for b := 0; b < bins; b++ {
					inBin := float64(b) / ratio
					inC := sssInterp(st.curInput[c], inBin)

					var phase complex128

					// Upward short (from pass-2 bin bâˆ’1, already written)
					if b > 0 {
						inCBelow := sssInterp(st.curInput[c], inBin-1)
						phase += ctx.FFTData[b-1] * sssConjMul(inC, inCBelow)
					}

					// Upward long (from pass-2 bin bâˆ’longStep)
					if b >= longStep {
						inCBelowL := sssInterp(st.curInput[c], inBin-float64(longStep))
						phase += ctx.FFTData[b-longStep] * sssConjMul(inC, inCBelowL)
					}

					// Downward short (from pass-1 bin b+1)
					if b < bins-1 {
						inBin1 := float64(b+1) / ratio
						inC1 := sssInterp(st.curInput[c], inBin1)
						inC1Below := sssInterp(st.curInput[c], inBin1-1)
						twistUp1 := sssConjMul(inC1, inC1Below)
						phase += sssConjMul(st.pass1Out[c][b+1], twistUp1)
					}

					// Downward long (from pass-1 bin b+longStep)
					if b+longStep < bins {
						inBinL := float64(b+longStep) / ratio
						inCL := sssInterp(st.curInput[c], inBinL)
						inCLBelow := sssInterp(st.curInput[c], inBinL-float64(longStep))
						twistUpL := sssConjMul(inCL, inCLBelow)
						phase += sssConjMul(st.pass1Out[c][b+longStep], twistUpL)
					}

					ctx.FFTData[b] = sssSetMag(phase, inC)
				}

				// Save pass-2 output and current input for the next frame.
				for k := 0; k < bins; k++ {
					st.prevOutput[c][k] = ctx.FFTData[k]
					st.prevInput[c][k] = st.curInput[c][k]
				}

				// â”€â”€ Mirror conjugate + inverse FFT + OLA â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€â”€
				for k := 1; k < N/2; k++ {
					ctx.FFTData[N-k] = complex(
						real(ctx.FFTData[k]),
						-imag(ctx.FFTData[k]),
					)
				}
				ctx.FFTData[N/2] = complex(real(ctx.FFTData[N/2]), 0)

				ctx.Inverse.Execute(ctx.FFTData, ctx.FFTData)

				for k := 0; k < N; k++ {
					ctx.Reals[k] = real(ctx.FFTData[k])
				}
				mulAddFloat64s(ctx.OutAcc[c][:N], ctx.WindowFactors, ctx.Reals[:N])
				copyFloat64s(ctx.Stack[c][:ctx.Step], ctx.OutAcc[c][:ctx.Step])
				copyFloat64s(ctx.OutAcc[c][:N], ctx.OutAcc[c][ctx.Step:ctx.Step+N])
				copyFloat64s(ctx.Frame[c][:ctx.Latency], ctx.Frame[c][ctx.Step:ctx.Step+ctx.Latency])
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
