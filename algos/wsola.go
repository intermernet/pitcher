/****************************************************************************
*
* COPYRIGHT 2025 Mike Hughes <mike <AT> mikehughes <DOT> info
*
*****************************************************************************
*
* Waveform Similarity Overlap-Add (WSOLA) pitch-shifting algorithm.
*
* Based on:
*   W. Verhelst, M. Roelands,
*   "An Overlap-Add Technique Based on Waveform Similarity (WSOLA)
*    for High Quality Time-Scale Modification of Speech",
*   IEEE ICASSP, 1993.
*
* The key difference from basic PSOLA: instead of always taking the analysis
* grain from the ideal position, WSOLA searches backward by up to delta =
* Step samples to find the starting point whose beginning most closely
* matches the current synthesis overlap region (maximum cross-correlation).
* This suppresses discontinuities at grain boundaries and produces noticeably
* cleaner output on voiced speech and tonal instruments.
*
* For pitch shifting, the best-matching grain is then resampled to the target
* ratio via linear interpolation — identical to the PSOLA resampling step.
*
* Buffer layout at each frame:
*
*   searchBuf:  [ prevDelta (delta samples) | Frame[c] (N samples) ]
*                 t-(N+delta-1)  …  t-N      t-(N-1)  …  t
*
*   Grain at offset d (d ∈ [0, delta]):
*     d = delta → Frame[c] exactly  (ideal, no backward shift)
*     d = 0     → delta samples before Frame[c] start  (maximum backward shift)
*
*   CC(d) = Σ_k  prevOut[k] · searchBuf[d + k],  k ∈ [0, Step)
*
*****************************************************************************/

package algos

import "math"

// wsolaState holds shared WSOLA algorithm state.
type wsolaState struct {
	ch        []wsolaChanState
	delta     int       // search radius = Step (set by NewWSOLAState)
	searchBuf []float64 // scratch: [prevDelta | Frame[c]], length = delta + N
}

// wsolaChanState holds per-channel WSOLA state.
type wsolaChanState struct {
	// delta oldest samples from the previous frame fire; forms the backward
	// extension of searchBuf beyond Frame[c][0].
	prevDelta []float64
	// hopSize OLA output samples saved after the current grain is added.
	// Used as the waveform-similarity reference for the next frame.
	prevOut []float64
	hasRef  bool
}

// NewWSOLAState allocates WSOLA state for the given Context.
func NewWSOLAState(ctx *Context) interface{} {
	delta := ctx.Step
	st := &wsolaState{
		ch:        make([]wsolaChanState, ctx.Channels),
		delta:     delta,
		searchBuf: make([]float64, ctx.FFTFrameSize+delta),
	}
	for c := range st.ch {
		st.ch[c] = wsolaChanState{
			prevDelta: make([]float64, delta),
			prevOut:   make([]float64, ctx.Step),
		}
	}
	return st
}

// ProcessWSOLA implements Waveform Similarity Overlap-Add pitch shifting
// (Verhelst & Roelands, ICASSP 1993).
func ProcessWSOLA(ctx *Context, output, input []byte) {
	byteDepth := ctx.BitDepth / 8
	ratio := math.Exp2(ctx.PitchShift / 12.0)
	st := ctx.AlgoState.(*wsolaState)
	grainSize := ctx.FFTFrameSize
	hopSize := ctx.Step
	delta := st.delta

	for c := 0; c < int(ctx.Channels); c++ {
		ch := &st.ch[c]
		numSamples := bytesToFloat64(ctx.F64Buf, input, ctx.Channels, ctx.BitDepth, c)
		frameIndex := ctx.FrameIndex[c]

		for i := 0; i < numSamples; i++ {
			ctx.Frame[c][frameIndex] = ctx.F64Buf[i]
			ctx.F64Buf[i] = ctx.Stack[c][frameIndex-ctx.Latency]
			frameIndex++

			if frameIndex >= grainSize {
				frameIndex = ctx.Latency

				// Build search buffer:
				//   searchBuf[0:delta]          = prevDelta  (older samples)
				//   searchBuf[delta:delta+N]    = Frame[c]   (current frame)
				copyFloat64s(st.searchBuf[:delta], ch.prevDelta)
				copyFloat64s(st.searchBuf[delta:delta+grainSize], ctx.Frame[c][:grainSize])

				// Search for the best backward offset d ∈ [0, delta].
				// d = delta → ideal grain (= Frame[c], no shift).
				// d < delta → grain starts earlier in time (better waveform match).
				//
				// CC(d) = Σ_k prevOut[k] · searchBuf[d+k], maximised over the
				// overlap region (hopSize samples) per Verhelst & Roelands §2.
				bestD := delta
				if ch.hasRef {
					var bestVal float64
					for d := 0; d <= delta; d++ {
						var cc float64
						for k := 0; k < hopSize; k++ {
							cc += ch.prevOut[k] * st.searchBuf[d+k]
						}
						if absCC := math.Abs(cc); d == 0 || absCC > bestVal {
							bestVal = absCC
							bestD = d
						}
					}
				}

				// Window the chosen grain into ctx.Reals.
				mulFloat64s(ctx.Reals[:grainSize], st.searchBuf[bestD:bestD+grainSize], ctx.Window)

				// Resample to the synthesis grain length (same as PSOLA).
				// Scale = 1.0: Hanning OLA at 50% overlap gives perfect
				// reconstruction for ratio = 1 without any additional scaling.
				synGrainLen := int(math.Round(float64(grainSize) / ratio))
				if synGrainLen < 1 {
					synGrainLen = 1
				}
				if synGrainLen > 2*grainSize {
					synGrainLen = 2 * grainSize
				}
				for k := 0; k < synGrainLen && k < len(ctx.OutAcc[c]); k++ {
					var srcPos float64
					if synGrainLen > 1 {
						srcPos = float64(k) * float64(grainSize-1) / float64(synGrainLen-1)
					}
					lo := int(srcPos)
					hi := lo + 1
					if hi >= grainSize {
						hi = grainSize - 1
					}
					frac := srcPos - float64(lo)
					ctx.OutAcc[c][k] += ctx.Reals[lo]*(1-frac) + ctx.Reals[hi]*frac
				}

				// Save the OLA overlap region as the CC reference for the next
				// frame. This must happen after adding the current grain but
				// before draining, so it reflects the overlap the next grain
				// will be joined to.
				copyFloat64s(ch.prevOut[:hopSize], ctx.OutAcc[c][:hopSize])
				ch.hasRef = true

				// Save the delta oldest samples of Frame[c] before the input
				// frame shift overwrites them. These become the backward
				// extension of searchBuf in the next frame fire.
				copyFloat64s(ch.prevDelta, ctx.Frame[c][:delta])

				// Drain, shift output accumulator, slide input frame.
				copyFloat64s(ctx.Stack[c][:hopSize], ctx.OutAcc[c][:hopSize])
				copyFloat64s(ctx.OutAcc[c][:2*grainSize-hopSize], ctx.OutAcc[c][hopSize:2*grainSize])
				zeroFloat64s(ctx.OutAcc[c][2*grainSize-hopSize : 2*grainSize])
				copyFloat64s(ctx.Frame[c][:ctx.Latency], ctx.Frame[c][hopSize:hopSize+ctx.Latency])
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
