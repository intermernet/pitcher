/****************************************************************************
*
* COPYRIGHT 2026 Mike Hughes <mike <AT> mikehughes <DOT> info
*
*****************************************************************************
*
* Phase Vocoder pitch-shifting algorithm based on
* http://blogs.zynaptiq.com/bernsee/pitch-shifting-using-the-ft/
*
* COPYRIGHT 1999-2015 Stephan M. Bernsee <s.bernsee [AT] zynaptiq [DOT] com>
*
* 						The Wide Open License (WOL)
*
* Permission to use, copy, modify, distribute and sell this software and its
* documentation for any purpose is hereby granted without fee, provided that
* the above copyright notice and this license appear in all source copies.
* THIS SOFTWARE IS PROVIDED "AS IS" WITHOUT EXPRESS OR IMPLIED WARRANTY OF
* ANY KIND. See http://www.dspguru.com/wol.htm for more information.
*
*****************************************************************************
*
* This code is further adapted from
* https://github.com/200sc/klangsynthese/blob/master/audio/filter/pitchshift.go
*
* COPYRIGHT 2017 Patrick Stephen <patrick.d.stephen [AT] gmail [DOT] com>
*
*****************************************************************************
* Uses the go-fftw library by runningwild
*****************************************************************************/

package algos

import "math"

// ProcessPhaseVocoder implements the classic phase-vocoder pitch-shift algorithm.
func ProcessPhaseVocoder(ctx *Context, output, input []byte) {
	byteDepth := ctx.BitDepth / 8
	ratio := math.Exp2(ctx.PitchShift / 12.0)

	for c := 0; c < int(ctx.Channels); c++ {
		numSamples := bytesToFloat64(ctx.F64Buf, input, ctx.Channels, ctx.BitDepth, c)
		frameIndex := ctx.FrameIndex[c]

		for i := 0; i < numSamples; i++ {
			ctx.Frame[c][frameIndex] = ctx.F64Buf[i]
			ctx.F64Buf[i] = ctx.Stack[c][frameIndex-ctx.Latency]
			frameIndex++

			if frameIndex >= ctx.FFTFrameSize {
				frameIndex = ctx.Latency

				// Windowing (SIMD multiply)
				mulFloat64s(ctx.Reals[:ctx.FFTFrameSize], ctx.Frame[c], ctx.Window)
				for k := 0; k < ctx.FFTFrameSize; k++ {
					ctx.FFTWData.Elems[k] = complex(ctx.Reals[k], 0)
				}

				// STFT
				ctx.Forward.Execute()

				// Analysis
				halfPlus1 := ctx.FFTFrameSize/2 + 1
				for k := 0; k < halfPlus1; k++ {
					cplx := ctx.FFTWData.Elems[k]
					ctx.Reals[k] = real(cplx)
					ctx.Imags[k] = imag(cplx)
				}

				computeMagnitudes(ctx.Magnitudes[:halfPlus1], ctx.Reals[:halfPlus1], ctx.Imags[:halfPlus1])

				for k := 0; k < halfPlus1; k++ {
					phase := math.Atan2(ctx.Imags[k], ctx.Reals[k])
					diff := phase - ctx.LastPhase[c][k]
					ctx.LastPhase[c][k] = phase
					diff -= float64(k) * ctx.Expected
					deltaPhase := int(diff / math.Pi)
					if deltaPhase >= 0 {
						deltaPhase += deltaPhase & 1
					} else {
						deltaPhase -= deltaPhase & 1
					}
					diff -= math.Pi * float64(deltaPhase)
					diff *= float64(ctx.Oversampling) / (math.Pi * 2.0)
					diff = (float64(k) + diff) * ctx.FreqPerBin
					ctx.Frequencies[k] = diff
				}

				// Pitch shifting
				zeroFloat64s(ctx.SynthMagnitudes[:ctx.FFTFrameSize])
				zeroFloat64s(ctx.SynthFrequencies[:ctx.FFTFrameSize])
				for k := 0; k < ctx.FFTFrameSize/2; k++ {
					l := int(float64(k) * ratio)
					if l < ctx.FFTFrameSize/2 {
						ctx.SynthMagnitudes[l] += ctx.Magnitudes[k]
						ctx.SynthFrequencies[l] = ctx.Frequencies[k] * ratio
					}
				}

				// Synthesis
				for k := 0; k <= ctx.FFTFrameSize/2; k++ {
					magn := ctx.SynthMagnitudes[k]
					tmp := ctx.SynthFrequencies[k]
					tmp -= float64(k) * ctx.FreqPerBin
					tmp /= ctx.FreqPerBin
					tmp *= 2 * math.Pi / float64(ctx.Oversampling)
					tmp += float64(k) * ctx.Expected
					ctx.SumPhase[c][k] += tmp
					ctx.FFTWData.Set(k, complex(magn*math.Cos(ctx.SumPhase[c][k]), magn*math.Sin(ctx.SumPhase[c][k])))
				}

				// Zero negative frequencies
				for k := ctx.FFTFrameSize/2 + 1; k < ctx.FFTFrameSize; k++ {
					ctx.FFTWData.Elems[k] = 0
				}

				// Inverse STFT
				ctx.Inverse.Execute()

				// Windowing and add to output accumulator (SIMD)
				for k := 0; k < ctx.FFTFrameSize; k++ {
					ctx.Reals[k] = real(ctx.FFTWData.Elems[k])
				}
				mulAddFloat64s(ctx.OutAcc[c][:ctx.FFTFrameSize], ctx.WindowFactors, ctx.Reals[:ctx.FFTFrameSize])
				copyFloat64s(ctx.Stack[c][:ctx.Step], ctx.OutAcc[c][:ctx.Step])

				// Shift output accumulator and buffer (SIMD)
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
