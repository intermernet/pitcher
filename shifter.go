/****************************************************************************
*
* COPYRIGHT 2023 Mike Hughes <mike <AT> mikehughes <DOT> info
*
*****************************************************************************
*
* FFT and pitch shift algorithm based on
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
*****************************************************************************/

package main

import (
	"math"
)

type shifter struct {
	pitchShift                        float64
	fftFrameSize                      int
	oversampling                      int
	sampleRate                        float64
	bitDepth                          uint16
	channels                          uint16
	step                              int
	latency                           int
	stack, frame                      []float64
	workBuffer                        []float64
	magnitudes, frequencies           []float64
	synthMagnitudes, synthFrequencies []float64
	lastPhase, sumPhase               []float64
	outAcc                            []float64
	expected                          float64
	window, windowFactors             []float64
	// Buffers
	data, out []byte
	// Output volume
	volume float64
}

func newShifter(fftFrameSize int, oversampling int, sampleRate float64, bitDepth uint16, channels int) *shifter {
	s := new(shifter)
	s.pitchShift = float64(*shift)
	s.fftFrameSize = fftFrameSize
	s.oversampling = oversampling
	s.sampleRate = sampleRate
	s.bitDepth = bitDepth
	s.channels = uint16(channels)
	s.step = fftFrameSize / oversampling
	s.latency = fftFrameSize - s.step
	s.stack = make([]float64, fftFrameSize)
	s.workBuffer = make([]float64, 2*fftFrameSize)
	s.magnitudes = make([]float64, fftFrameSize)
	s.frequencies = make([]float64, fftFrameSize)
	s.synthMagnitudes = make([]float64, fftFrameSize)
	s.synthFrequencies = make([]float64, fftFrameSize)
	s.lastPhase = make([]float64, fftFrameSize/2+1)
	s.sumPhase = make([]float64, fftFrameSize/2+1)
	s.outAcc = make([]float64, 2*fftFrameSize)
	s.volume = 1.0

	s.expected = 2 * math.Pi * float64(s.step) / float64(fftFrameSize)

	s.window = make([]float64, fftFrameSize)
	s.windowFactors = make([]float64, fftFrameSize)
	t := 0.0
	for i := 0; i < fftFrameSize; i++ {
		w := -0.5*math.Cos(t) + 0.5
		s.window[i] = w
		s.windowFactors[i] = w * (2.0 / float64(fftFrameSize*oversampling))
		t += (math.Pi * 2.0) / float64(fftFrameSize)
	}

	s.frame = make([]float64, fftFrameSize)
	return s
}

func (s *shifter) shift(pOutputSample, pInputSamples []byte, framecount uint32) {
	// Map buffers
	s.data = pInputSamples
	s.out = pOutputSample

	bitDepth := s.bitDepth
	byteDepth := bitDepth / 8

	freqPerBin := float64(s.sampleRate) / float64(s.fftFrameSize)
	frameIndex := s.latency

	// Calculate semitones to pitch shift
	ratio := math.Exp2(s.pitchShift / 12.0)

	// De-interleave multi channel PCM into floats
	for c := 0; c < int(s.channels); c++ {
		f64in := bytesToF64(s.data, s.channels, bitDepth, c)
		f64out := f64in
		// Process buffer
		for i := 0; i < len(f64in); i++ {
			s.frame[frameIndex] = f64in[i]
			f64out[i] = s.stack[frameIndex-s.latency]
			frameIndex++

			// Have a full frame
			if frameIndex >= s.fftFrameSize {
				frameIndex = s.latency

				// Interleave real / imag and do windowing
				for k := 0; k < s.fftFrameSize; k++ {
					s.workBuffer[2*k] = s.frame[k] * s.window[k]
					s.workBuffer[(2*k)+1] = 0.0
				}

				// Do transform
				stft(s.workBuffer, s.fftFrameSize, -1)

				// Analysis
				for k := 0; k <= s.fftFrameSize/2; k++ {
					// De-intterleave
					real := s.workBuffer[2*k]
					imag := s.workBuffer[(2*k)+1]

					// Compute magnitude and phase
					magn := 2 * math.Sqrt(real*real+imag*imag)
					s.magnitudes[k] = magn

					phase := math.Atan2(imag, real)

					// Compute phase difference
					diff := phase - s.lastPhase[k]
					s.lastPhase[k] = phase

					// Subtract expected phase difference
					diff -= float64(k) * s.expected

					// Map deltaphase to +/- π
					deltaPhase := int(diff / math.Pi)
					if deltaPhase >= 0 {
						deltaPhase += deltaPhase & 1
					} else {
						deltaPhase -= deltaPhase & 1
					}
					diff -= math.Pi * float64(deltaPhase)

					// Get deviation from bin freq
					diff *= float64(s.oversampling) / (math.Pi * 2.0)

					// Compute k-th partials freq
					diff = (float64(k) + diff) * freqPerBin

					// Store magnitude and frequency
					s.magnitudes[k] = magn
					s.frequencies[k] = diff
				}

				// Do the actual pitch shifting
				for k := 0; k < s.fftFrameSize; k++ {
					s.synthMagnitudes[k] = 0.0
					s.synthFrequencies[k] = 0.0
				}
				for k := 0; k < s.fftFrameSize/2; k++ {
					l := int(float64(k) * ratio)
					if l < s.fftFrameSize/2 {
						s.synthMagnitudes[l] += s.magnitudes[k]
						s.synthFrequencies[l] = s.frequencies[k] * ratio
					}
				}

				// Synthesis
				for k := 0; k <= s.fftFrameSize/2; k++ {
					// Get magnitude and true freq
					magn := s.synthMagnitudes[k]
					tmp := s.synthFrequencies[k]
					// Subtract bin mid freq
					tmp -= float64(k) * freqPerBin
					// Get bin deviation from freq deviation
					tmp /= freqPerBin
					// Include oversampling
					tmp *= 2 * math.Pi / float64(s.oversampling)
					// Add overlap phase advance
					tmp += float64(k) * s.expected
					// Accumulate delta phase
					s.sumPhase[k] += tmp
					// Re-interleave real and imag
					s.workBuffer[2*k] = magn * math.Cos(s.sumPhase[k])
					s.workBuffer[(2*k)+1] = magn * math.Sin(s.sumPhase[k])
				}

				// Zero negative frequencies
				for k := s.fftFrameSize + 2; k < 2*s.fftFrameSize; k++ {
					s.workBuffer[k] = 0.0
				}

				// Inverse STFT
				stft(s.workBuffer, s.fftFrameSize, 1)

				// Windowing and add to output accumulator
				for k := 0; k < s.fftFrameSize; k++ {
					s.outAcc[k] += s.windowFactors[k] * s.workBuffer[2*k]
				}
				for k := 0; k < s.step; k++ {
					s.stack[k] = s.outAcc[k]
				}

				// Shift output accumulator and buffer
				for k := 0; k < s.fftFrameSize; k++ {
					s.outAcc[k] = s.outAcc[k+s.step]
				}
				for k := 0; k < s.latency; k++ {
					s.frame[k] = s.frame[k+s.step]
				}
			}
		}
		// Re-interleave and convert to bytes
		for i := c * int(byteDepth); i < len(s.data); i += int(byteDepth * 2) {
			// Apply volume scaling during conversion
			setInt16_f64(s.out, i, f64in[i/int(byteDepth*2)]*s.volume)
		}
	}
}

// stft : FFT routine, (C)1996 S.M.Bernsee. Sign = -1 is FFT, 1 is iFFT (inverse)
// Fills fftBuffer[0...2*fftFrameSize-1] with the Fourier transform of the
// time domain data in fftBuffer[0...2*fftFrameSize-1]. The FFT array takes
// and returns the cosine and sine parts in an interleaved manner, ie.
// fftBuffer[0] = cosPart[0], fftBuffer[1] = sinPart[0], asf. fftFrameSize
// must be a power of 2. It expects a complex input signal (see footnote 2),
// ie. when working with 'common' audio signals our input signal has to be
// passed as {in[0],0.,in[1],0.,in[2],0.,...} asf. In that case, the transform
// of the frequencies of interest is in fftBuffer[0...fftFrameSize].
func stft(data []float64, fftFrameSize, sign int) {
	for i := 2; i < 2*(fftFrameSize-2); i += 2 {
		j := 0
		for bitm := 2; bitm < 2*fftFrameSize; bitm <<= 1 {
			if (i & bitm) != 0 {
				j++
			}
			j <<= 1
		}
		if i < j {
			data[j], data[i] = data[i], data[j]
			data[j+1], data[i+1] = data[i+1], data[j+1]
		}
	}
	max := int(math.Log(float64(fftFrameSize))/math.Log(2) + .5)
	le := 2
	for k := 0; k < max; k++ {
		le <<= 1
		le2 := le >> 1
		ur := 1.0
		ui := 0.0
		arg := math.Pi / float64(le2>>1)
		wr := math.Cos(arg)
		wi := float64(sign) * math.Sin(arg)
		for j := 0; j < le2; j += 2 {
			for i := j; i < 2*fftFrameSize; i += le {
				tr := data[i+le2]*ur - data[i+le2+1]*ui
				ti := data[i+le2]*ui + data[i+le2+1]*ur
				data[i+le2] = data[i] - tr
				data[i+le2+1] = data[i+1] - ti
				data[i] += tr
				data[i+1] += ti
			}
			tr := ur*wr - ui*wi
			ui = ur*wi + ui*wr
			ur = tr
		}
	}
}

// Helper functions to convert PCM samples to and from float64

func bytesToF64(data []byte, channels, bitRate uint16, channel int) []float64 {
	byteDepth := bitRate / 8
	out := make([]float64, (len(data)/int(byteDepth*channels))+1)
	for i := channel * int(byteDepth); i < len(data); i += int(byteDepth * channels) {
		out[i/int(byteDepth*channels)] = getFloat64(data, i, byteDepth)
	}
	return out
}

func setInt16(d []byte, i int, in int64) {
	for j := 0; j < 2; j++ {
		d[i+j] = byte(in & 255)
		in >>= 8
	}
}

func getInt16(d []byte, i int) (out int16) {
	var shift uint16
	for j := 0; j < 2; j++ {
		out += int16(d[i+j]) << shift
		shift += 8
	}
	return
}

func getFloat64(d []byte, i int, byteDepth uint16) float64 {
	switch byteDepth {
	case 1:
		return float64(int8(d[i])) / 128.0
	case 2:
		return float64(getInt16(d, i)) / 32768.0
	}
	return 0.0
}

func setInt16_f64(d []byte, i int, in float64) {
	setInt16(d, i, int64(in*32768))
}
