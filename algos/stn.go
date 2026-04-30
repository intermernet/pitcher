/****************************************************************************
*
* COPYRIGHT 2025 Mike Hughes <mike <AT> mikehughes <DOT> info
*
*****************************************************************************
*
* Sines/Transients/Noise (STN) pitch-shifting algorithm.
*
* Based on:
*   Polak & Erkut, "Low-latency Pitch-Shifting with STN decomposition",
*   DAS|DAGA 2025, Copenhagen.
*
* STN decomposition method:
*   Fierro & Välimäki, "Enhanced fuzzy decomposition of sound into sines,
*   transients, and noise", J. Audio Eng. Soc. 71 (2023), 468-480.
*
* Noise Morphing method:
*   Moliner et al., "Noise morphing for audio time stretching",
*   IEEE Signal Process. Lett. 31 (2024), 1144-1148.
*
*****************************************************************************/

package algos

import (
	"math"
	"time"
)

// stnChanState holds per-channel state for the STN algorithm.
type stnChanState struct {
	// PV phase state for the sines component.
	pvLastPhase []float64 // [bins] last analysis phase
	pvSumPhase  []float64 // [bins] accumulated synthesis phase

	// Causal ring buffer of past STFT magnitude frames for the horizontal
	// median filter. Shape: [lh][bins].
	magHistory [][]float64
	histIdx    int // next write position in magHistory
}

// stnState holds all STN algorithm state shared across the processing loop.
type stnState struct {
	ch []*stnChanState

	// Filter parameters
	lh           int     // horizontal (time) median filter length (frames, causal)
	lv           int     // vertical (frequency) median filter length (bins)
	betaL, betaU float64 // fuzzy mask thresholds (Fierro & Välimäki 2023)

	// Shared scratch buffers (one channel processed at a time, so no races)
	colBuf  []float64 // [lh]   column of ring buffer for horizontal median
	sortBuf []float64 // [max(lh,lv)+1] insertion-sort scratch
	xhBuf   []float64 // [bins] horizontal-filtered magnitudes Xh
	xvBuf   []float64 // [bins] vertical-filtered magnitudes Xv
	sinMask []float64 // [bins] sines fuzzy mask S
	traMask []float64 // [bins] transients fuzzy mask T
	noiMask []float64 // [bins] noise fuzzy mask N

	// Pitch-remapped synthesis buffers (per frame, overwritten each hop)
	synSinMag  []float64 // [FFTFrameSize] magnitude at output bin for sines
	synSinFreq []float64 // [FFTFrameSize] true frequency at output bin for sines
	synNoiMag  []float64 // [FFTFrameSize] magnitude at output bin for noise

	// Fast RNG state (xorshift64) for noise phase randomisation
	rngState uint64
}

// NewSTNState allocates and initialises STN-specific state for the given Context.
func NewSTNState(ctx *Context) interface{} {
	lh := 9  // 9 past STFT frames (~96 ms at 48 kHz / frameSize=2048 / OS=4)
	lv := 21 // 21 adjacent frequency bins for vertical filter

	bins := ctx.FFTFrameSize/2 + 1
	nCh := int(ctx.Channels)
	sortLen := lh + lv + 2

	st := &stnState{
		ch:         make([]*stnChanState, nCh),
		lh:         lh,
		lv:         lv,
		betaL:      0.55,
		betaU:      0.95,
		colBuf:     make([]float64, lh),
		sortBuf:    make([]float64, sortLen),
		xhBuf:      make([]float64, bins),
		xvBuf:      make([]float64, bins),
		sinMask:    make([]float64, bins),
		traMask:    make([]float64, bins),
		noiMask:    make([]float64, bins),
		synSinMag:  make([]float64, ctx.FFTFrameSize),
		synSinFreq: make([]float64, ctx.FFTFrameSize),
		synNoiMag:  make([]float64, ctx.FFTFrameSize),
		rngState:   uint64(time.Now().UnixNano()) | 1,
	}

	for c := 0; c < nCh; c++ {
		ch := &stnChanState{
			pvLastPhase: make([]float64, bins),
			pvSumPhase:  make([]float64, bins),
			magHistory:  make([][]float64, lh),
		}
		for i := 0; i < lh; i++ {
			ch.magHistory[i] = make([]float64, bins)
		}
		st.ch[c] = ch
	}

	return st
}

// stnFuzzy evaluates the fuzzy membership function from eq (2) of
// Fierro & Välimäki 2023:
//
//	f(x) = 1                                     if x ≥ βU
//	        sin²(π/2 · (x−βL)/(βU−βL))          if βL ≤ x ≤ βU
//	        0                                     otherwise
func stnFuzzy(x, betaL, betaU float64) float64 {
	if x >= betaU {
		return 1
	}
	if x <= betaL {
		return 0
	}
	t := math.Pi / 2 * (x - betaL) / (betaU - betaL)
	s := math.Sin(t)
	return s * s
}

// stnInsertionSort sorts buf in place. Fast for small slices (lh ≤ 21).
func stnInsertionSort(buf []float64) {
	for i := 1; i < len(buf); i++ {
		key := buf[i]
		j := i - 1
		for j >= 0 && buf[j] > key {
			buf[j+1] = buf[j]
			j--
		}
		buf[j+1] = key
	}
}

// stnMedian returns the median of buf, sorting it in place.
func stnMedian(buf []float64) float64 {
	stnInsertionSort(buf)
	n := len(buf)
	if n%2 == 0 {
		return (buf[n/2-1] + buf[n/2]) / 2
	}
	return buf[n/2]
}

// stnRandPhase returns a uniform random phase in [−π, π] via xorshift64.
func stnRandPhase(state *uint64) float64 {
	x := *state
	x ^= x << 13
	x ^= x >> 7
	x ^= x << 17
	*state = x
	// Top 53 bits → [0, 1) → [−π, π]
	return float64(x>>11)*(2.0*math.Pi/(1<<53)) - math.Pi
}

// ProcessSTN implements the Sines/Transients/Noise (STN) pitch-shift algorithm
// described in Polak & Erkut (DAS|DAGA 2025).
//
// Each STFT frame is decomposed into three components using fuzzy STN masks
// (Fierro & Välimäki 2023), then each component is pitch-shifted independently:
//
//   - Sines (S):      Phase Vocoder with frequency tracking. Transients have
//     been masked out, which removes the main source of PV smearing.
//   - Transients (T): Passed through without pitch modification. Transients
//     carry no tonal pitch information and should be preserved as-is.
//   - Noise (N):      Magnitude-preserving random-phase synthesis (Noise
//     Morphing, Moliner et al. 2024): pitch-shifted magnitudes with uniformly
//     random phases, which avoids the coherent phase artifacts of PV on noise.
//
// The three reconstructed components are summed in the frequency domain before
// a single IFFT and overlap-add step.
func ProcessSTN(ctx *Context, output, input []byte) {
	byteDepth := ctx.BitDepth / 8
	ratio := math.Exp2(ctx.PitchShift / 12.0)
	st := ctx.AlgoState.(*stnState)
	bins := ctx.FFTFrameSize/2 + 1
	halfLV := st.lv / 2

	for c := 0; c < int(ctx.Channels); c++ {
		ch := st.ch[c]
		numSamples := bytesToFloat64(ctx.F64Buf, input, ctx.Channels, ctx.BitDepth, c)
		frameIndex := ctx.FrameIndex[c]

		for i := 0; i < numSamples; i++ {
			ctx.Frame[c][frameIndex] = ctx.F64Buf[i]
			ctx.F64Buf[i] = ctx.Stack[c][frameIndex-ctx.Latency]
			frameIndex++

			if frameIndex >= ctx.FFTFrameSize {
				frameIndex = ctx.Latency

				// ── Window and forward FFT ─────────────────────────────────
				mulFloat64s(ctx.Reals[:ctx.FFTFrameSize], ctx.Frame[c], ctx.Window)
				for k := 0; k < ctx.FFTFrameSize; k++ {
					ctx.FFTWData.Elems[k] = complex(ctx.Reals[k], 0)
				}
				ctx.Forward.Execute()

				// Extract magnitudes and phases into scratch buffers.
				// ctx.Reals / ctx.Imags are kept intact through the analysis and
				// mask stages so they can be used for the transient contribution
				// in the reconstruction step below.
				for k := 0; k < bins; k++ {
					cplx := ctx.FFTWData.Elems[k]
					ctx.Reals[k] = real(cplx)
					ctx.Imags[k] = imag(cplx)
					ctx.Magnitudes[k] = 2 * math.Sqrt(ctx.Reals[k]*ctx.Reals[k]+ctx.Imags[k]*ctx.Imags[k])
				}

				// ── STN Decomposition ──────────────────────────────────────
				//
				// Step 1 – Update causal magnitude history ring buffer.
				copy(ch.magHistory[ch.histIdx][:bins], ctx.Magnitudes[:bins])
				ch.histIdx = (ch.histIdx + 1) % st.lh

				// Step 2 – Horizontal (time) median filter → Xh.
				// Causal: only the lh most-recent frames are considered. This
				// is the real-time adaptation of the centred filter from the
				// offline algorithm (Fierro & Välimäki fig. 2b).
				// Xh is large where the spectrum is constant over time → sines.
				for k := 0; k < bins; k++ {
					for j := 0; j < st.lh; j++ {
						// Reverse-walk through the ring buffer: j=0 is the
						// frame just written, j=lh-1 is the oldest.
						idx := (ch.histIdx - 1 - j + st.lh*2) % st.lh
						st.colBuf[j] = ch.magHistory[idx][k]
					}
					copy(st.sortBuf[:st.lh], st.colBuf[:st.lh])
					st.xhBuf[k] = stnMedian(st.sortBuf[:st.lh])
				}

				// Step 3 – Vertical (frequency) median filter → Xv.
				// Xv is large where the spectrum is wideband within a frame
				// → transients / impulse-like events.
				for k := 0; k < bins; k++ {
					start := k - halfLV
					end := k + halfLV + 1
					if start < 0 {
						start = 0
					}
					if end > bins {
						end = bins
					}
					n := end - start
					copy(st.sortBuf[:n], ctx.Magnitudes[start:end])
					st.xvBuf[k] = stnMedian(st.sortBuf[:n])
				}

				// Step 4 – Compute fuzzy masks S, T, N (eq 1 & 2 of Fierro & Välimäki).
				//
				//   Rs = Xh / (Xh + Xv)   tonal-ness
				//   Rt = Xv / (Xh + Xv)   transient-ness
				//   S  = f(Rs)
				//   T  = f(Rt)
				//   N  = max(0, 1 − S − T)
				//
				// βL = 0.55, βU = 0.95 as proposed in the original work.
				for k := 0; k < bins; k++ {
					xh := st.xhBuf[k]
					xv := st.xvBuf[k]
					total := xh + xv
					var rs float64
					if total > 1e-30 {
						rs = xh / total
					} else {
						rs = 0.5 // silent bin: treat as equally ambiguous
					}
					rt := 1 - rs
					sk := stnFuzzy(rs, st.betaL, st.betaU)
					tk := stnFuzzy(rt, st.betaL, st.betaU)
					nk := 1 - sk - tk
					if nk < 0 {
						nk = 0
					}
					st.sinMask[k] = sk
					st.traMask[k] = tk
					st.noiMask[k] = nk
				}

				// ── PV frequency analysis for the Sines component ─────────
				//
				// Standard phase-vocoder frequency tracking (same as ProcessPhaseVocoder)
				// using dedicated phase accumulators from stnChanState so they
				// are independent of the Phase Vocoder algorithm's state.
				for k := 0; k < bins; k++ {
					phase := math.Atan2(ctx.Imags[k], ctx.Reals[k])
					diff := phase - ch.pvLastPhase[k]
					ch.pvLastPhase[k] = phase
					diff -= float64(k) * ctx.Expected
					deltaPhase := int(diff / math.Pi)
					if deltaPhase >= 0 {
						deltaPhase += deltaPhase & 1
					} else {
						deltaPhase -= deltaPhase & 1
					}
					diff -= math.Pi * float64(deltaPhase)
					diff *= float64(ctx.Oversampling) / (math.Pi * 2.0)
					ctx.Frequencies[k] = (float64(k) + diff) * ctx.FreqPerBin
				}

				// ── Pitch remapping ────────────────────────────────────────
				//
				// Input bin k maps to output bin l = floor(k * ratio).
				// Sines energy and its tracked frequency are accumulated into
				// synSinMag / synSinFreq. Noise energy goes into synNoiMag.
				// Transients remain at their original bins (no pitch shift).
				zeroFloat64s(st.synSinMag[:ctx.FFTFrameSize])
				zeroFloat64s(st.synSinFreq[:ctx.FFTFrameSize])
				zeroFloat64s(st.synNoiMag[:ctx.FFTFrameSize])
				for k := 0; k < ctx.FFTFrameSize/2; k++ {
					l := int(float64(k) * ratio)
					if l < ctx.FFTFrameSize/2 {
						st.synSinMag[l] += st.sinMask[k] * ctx.Magnitudes[k]
						st.synSinFreq[l] = ctx.Frequencies[k] * ratio
						st.synNoiMag[l] += st.noiMask[k] * ctx.Magnitudes[k]
					}
				}

				// ── Sines: accumulate synthesis phase (PV) ─────────────────
				for k := 0; k <= ctx.FFTFrameSize/2; k++ {
					tmp := st.synSinFreq[k]
					tmp -= float64(k) * ctx.FreqPerBin
					tmp /= ctx.FreqPerBin
					tmp *= 2 * math.Pi / float64(ctx.Oversampling)
					tmp += float64(k) * ctx.Expected
					ch.pvSumPhase[k] += tmp
				}

				// ── Reconstruct spectrum: Sines + Transients + Noise ───────
				//
				// All three contributions use the same magnitude scale as the
				// existing Phase Vocoder (Magnitudes[k] = 2·|X[k]|), so the
				// OLA WindowFactors normalisation is consistent.
				//
				//   Sines     – pitch-shifted via PV phase accumulation
				//   Transients – original STFT complex values, T-masked, ×2 to
				//                match the scale of sines/noise magnitudes
				//   Noise     – pitch-shifted magnitude, uniformly random phase
				//                (Noise Morphing: Moliner et al. 2024 eq 5)
				for k := 0; k <= ctx.FFTFrameSize/2; k++ {
					// Sines
					sinR := st.synSinMag[k] * math.Cos(ch.pvSumPhase[k])
					sinI := st.synSinMag[k] * math.Sin(ch.pvSumPhase[k])

					// Transients (×2 to match the 2·|X[k]| scale of sines/noise)
					trR := 2 * st.traMask[k] * ctx.Reals[k]
					trI := 2 * st.traMask[k] * ctx.Imags[k]

					// Noise – random phase, pitch-shifted magnitude
					noisePh := stnRandPhase(&st.rngState)
					noR := st.synNoiMag[k] * math.Cos(noisePh)
					noI := st.synNoiMag[k] * math.Sin(noisePh)

					ctx.FFTWData.Set(k, complex(sinR+trR+noR, sinI+trI+noI))
				}

				// Zero negative frequencies (one-sided spectrum → real output)
				for k := ctx.FFTFrameSize/2 + 1; k < ctx.FFTFrameSize; k++ {
					ctx.FFTWData.Elems[k] = 0
				}

				// ── Inverse FFT and overlap-add ────────────────────────────
				ctx.Inverse.Execute()

				for k := 0; k < ctx.FFTFrameSize; k++ {
					ctx.Reals[k] = real(ctx.FFTWData.Elems[k])
				}
				mulAddFloat64s(ctx.OutAcc[c][:ctx.FFTFrameSize], ctx.WindowFactors, ctx.Reals[:ctx.FFTFrameSize])
				copyFloat64s(ctx.Stack[c][:ctx.Step], ctx.OutAcc[c][:ctx.Step])
				copyFloat64s(ctx.OutAcc[c][:ctx.FFTFrameSize], ctx.OutAcc[c][ctx.Step:ctx.Step+ctx.FFTFrameSize])
				copyFloat64s(ctx.Frame[c][:ctx.Latency], ctx.Frame[c][ctx.Step:ctx.Step+ctx.Latency])
			}
		}

		ctx.FrameIndex[c] = frameIndex

		// Re-interleave and convert to output bytes
		stride := int(byteDepth * ctx.Channels)
		off := c * int(byteDepth)
		for i := 0; i < numSamples; i++ {
			float64ToFloat32Bytes(output, off, ctx.F64Buf[i]*ctx.Volume)
			off += stride
		}
	}
}
