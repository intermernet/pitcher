/****************************************************************************
*
* COPYRIGHT 2025 Mike Hughes <mike <AT> mikehughes <DOT> info
*
****************************************************************************/

package algos

import "math"

// ProcessPSOLA implements the Pitch-Synchronous Overlap-Add (PSOLA) pitch-shift
// algorithm. It targets minimum latency by operating on short frames and
// re-sampling grains in the time domain without any FFT.
func ProcessPSOLA(ctx *Context, output, input []byte) {
	byteDepth := ctx.BitDepth / 8
	ratio := math.Exp2(ctx.PitchShift / 12.0)
	grainSize := ctx.FFTFrameSize
	hopSize := ctx.Step // analysis hop = grainSize / oversampling

	for c := 0; c < int(ctx.Channels); c++ {
		numSamples := bytesToFloat64(ctx.F64Buf, input, ctx.Channels, ctx.BitDepth, c)
		frameIndex := ctx.FrameIndex[c]

		for i := 0; i < numSamples; i++ {
			ctx.Frame[c][frameIndex] = ctx.F64Buf[i]
			ctx.F64Buf[i] = ctx.Stack[c][frameIndex-ctx.Latency]
			frameIndex++

			if frameIndex >= grainSize {
				frameIndex = ctx.Latency

				// Apply Hanning window and pitch-shift via time-domain resampling.
				// We resample the analysis grain into a synthesis grain of a different
				// length (grainSize / ratio), then overlap-add at the synthesis hop.
				for k := 0; k < grainSize; k++ {
					ctx.Reals[k] = ctx.Frame[c][k] * ctx.Window[k]
				}

				// Length of resampled grain in the output domain
				synGrainLen := int(math.Round(float64(grainSize) / ratio))
				if synGrainLen < 1 {
					synGrainLen = 1
				}
				if synGrainLen > 2*grainSize {
					synGrainLen = 2 * grainSize
				}

				// Resample via linear interpolation and overlap-add into OutAcc.
				// Scale = 1.0: with Hanning window and hopSize = grainSize/2, the
				// overlap-add windows sum exactly to 1 at every output point, giving
				// perfect reconstruction for ratio=1 without any additional scaling.
				for k := 0; k < synGrainLen && k < len(ctx.OutAcc[c]); k++ {
					srcPos := float64(k) * float64(grainSize-1) / float64(synGrainLen-1)
					if synGrainLen == 1 {
						srcPos = 0
					}
					lo := int(srcPos)
					hi := lo + 1
					if hi >= grainSize {
						hi = grainSize - 1
					}
					frac := srcPos - float64(lo)
					sample := ctx.Reals[lo]*(1-frac) + ctx.Reals[hi]*frac
					ctx.OutAcc[c][k] += sample
				}

				// Drain hop-sized chunk into stack output buffer
				copyFloat64s(ctx.Stack[c][:hopSize], ctx.OutAcc[c][:hopSize])

				// Shift output accumulator
				copyFloat64s(ctx.OutAcc[c][:2*grainSize-hopSize], ctx.OutAcc[c][hopSize:2*grainSize])
				zeroFloat64s(ctx.OutAcc[c][2*grainSize-hopSize : 2*grainSize])

				// Slide the input frame
				copyFloat64s(ctx.Frame[c][:ctx.Latency], ctx.Frame[c][hopSize:hopSize+ctx.Latency])
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
