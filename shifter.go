/****************************************************************************
*
* COPYRIGHT 2025 Mike Hughes <mike <AT> mikehughes <DOT> info
*
*****************************************************************************
*
* pitch shift algorithm based on
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

package main

import (
	"encoding/binary"
	"math"

	"github.com/runningwild/go-fftw/fftw"
)

type shifter struct {
	pitchShift                        float64
	fftFrameSize                      int
	oversampling                      int
	sampleRate                        float64
	bitDepth                          uint16
	channels                          uint16
	periods                           int
	bufferSize                        int
	exclusive                         bool
	step                              int
	latency                           int
	freqPerBin                        float64
	frameIndex                        []int
	stack, frame                      [][]float64
	fftwdata                          *fftw.Array
	forward, inverse                  *fftw.Plan
	magnitudes, frequencies           []float64
	synthMagnitudes, synthFrequencies []float64
	lastPhase, sumPhase               [][]float64
	outAcc                            [][]float64
	expected                          float64
	window, windowFactors             []float64
	// Temporary SIMD working buffers
	reals, imags []float64
	f64buf       []float64
	// Output volume
	volume float64
}

func newShifter(fftFrameSize int, oversampling int, sampleRate float64, bitDepth uint16, channels int, periods int, bufferSize int, exclusive bool) *shifter {
	s := new(shifter)
	s.pitchShift = float64(*shift)
	s.fftFrameSize = fftFrameSize
	s.oversampling = oversampling
	s.sampleRate = sampleRate
	s.bitDepth = bitDepth
	s.channels = uint16(channels)
	s.periods = periods
	s.bufferSize = bufferSize
	s.exclusive = exclusive
	s.step = fftFrameSize / oversampling
	s.latency = fftFrameSize - s.step
	s.fftwdata = fftw.NewArray(fftFrameSize)
	s.forward = fftw.NewPlan(s.fftwdata, s.fftwdata, fftw.Forward, fftw.Estimate)
	s.inverse = fftw.NewPlan(s.fftwdata, s.fftwdata, fftw.Backward, fftw.Estimate)
	s.magnitudes = make([]float64, fftFrameSize)
	s.frequencies = make([]float64, fftFrameSize)
	s.synthMagnitudes = make([]float64, fftFrameSize)
	s.synthFrequencies = make([]float64, fftFrameSize)
	s.frameIndex = make([]int, channels)
	s.stack = make([][]float64, channels)
	s.frame = make([][]float64, channels)
	s.lastPhase = make([][]float64, channels)
	s.sumPhase = make([][]float64, channels)
	s.outAcc = make([][]float64, channels)
	for ch := 0; ch < channels; ch++ {
		s.frameIndex[ch] = s.latency
		s.stack[ch] = make([]float64, fftFrameSize)
		s.frame[ch] = make([]float64, fftFrameSize)
		s.lastPhase[ch] = make([]float64, fftFrameSize/2+1)
		s.sumPhase[ch] = make([]float64, fftFrameSize/2+1)
		s.outAcc[ch] = make([]float64, 2*fftFrameSize)
	}
	s.volume = 1.0

	s.expected = 2 * math.Pi * float64(s.step) / float64(fftFrameSize)
	s.freqPerBin = sampleRate / float64(fftFrameSize)

	s.window = make([]float64, fftFrameSize)
	s.windowFactors = make([]float64, fftFrameSize)
	s.reals = make([]float64, fftFrameSize)
	s.imags = make([]float64, fftFrameSize)
	s.f64buf = make([]float64, max(fftFrameSize, 8192))
	t := 0.0
	for i := 0; i < fftFrameSize; i++ {
		// Hanning window
		w := -0.5*math.Cos(t) + 0.5
		s.window[i] = w
		s.windowFactors[i] = w * (2.0 / float64(fftFrameSize*oversampling))
		t += (math.Pi * 2.0) / float64(fftFrameSize)
	}

	return s
}

func (s *shifter) process(pOutputSample, pInputSamples []byte, framecount uint32) {
	s.processAudio(pOutputSample, pInputSamples)
}

func (s *shifter) processAudio(output, input []byte) {
	byteDepth := s.bitDepth / 8
	ratio := math.Exp2(s.pitchShift / 12.0)

	for c := 0; c < int(s.channels); c++ {
		numSamples := bytesToFloat64(s.f64buf, input, s.channels, s.bitDepth, c)
		frameIndex := s.frameIndex[c]

		for i := 0; i < numSamples; i++ {
			s.frame[c][frameIndex] = s.f64buf[i]
			s.f64buf[i] = s.stack[c][frameIndex-s.latency]
			frameIndex++

			if frameIndex >= s.fftFrameSize {
				frameIndex = s.latency

				// Windowing (SIMD multiply)
				mulFloat64s(s.reals[:s.fftFrameSize], s.frame[c], s.window)
				for k := 0; k < s.fftFrameSize; k++ {
					s.fftwdata.Elems[k] = complex(s.reals[k], 0)
				}

				// STFT
				s.forward.Execute()

				// Analysis
				halfPlus1 := s.fftFrameSize/2 + 1
				for k := 0; k < halfPlus1; k++ {
					cplx := s.fftwdata.Elems[k]
					s.reals[k] = real(cplx)
					s.imags[k] = imag(cplx)
				}

				computeMagnitudes(s.magnitudes[:halfPlus1], s.reals[:halfPlus1], s.imags[:halfPlus1])

				for k := 0; k < halfPlus1; k++ {
					phase := math.Atan2(s.imags[k], s.reals[k])
					diff := phase - s.lastPhase[c][k]
					s.lastPhase[c][k] = phase
					diff -= float64(k) * s.expected
					deltaPhase := int(diff / math.Pi)
					if deltaPhase >= 0 {
						deltaPhase += deltaPhase & 1
					} else {
						deltaPhase -= deltaPhase & 1
					}
					diff -= math.Pi * float64(deltaPhase)
					diff *= float64(s.oversampling) / (math.Pi * 2.0)
					diff = (float64(k) + diff) * s.freqPerBin
					s.frequencies[k] = diff
				}

				// Pitch shifting
				zeroFloat64s(s.synthMagnitudes[:s.fftFrameSize])
				zeroFloat64s(s.synthFrequencies[:s.fftFrameSize])
				for k := 0; k < s.fftFrameSize/2; k++ {
					l := int(float64(k) * ratio)
					if l < s.fftFrameSize/2 {
						s.synthMagnitudes[l] += s.magnitudes[k]
						s.synthFrequencies[l] = s.frequencies[k] * ratio
					}
				}

				// Synthesis
				for k := 0; k <= s.fftFrameSize/2; k++ {
					magn := s.synthMagnitudes[k]
					tmp := s.synthFrequencies[k]
					tmp -= float64(k) * s.freqPerBin
					tmp /= s.freqPerBin
					tmp *= 2 * math.Pi / float64(s.oversampling)
					tmp += float64(k) * s.expected
					s.sumPhase[c][k] += tmp
					s.fftwdata.Set(k, complex(magn*math.Cos(s.sumPhase[c][k]), magn*math.Sin(s.sumPhase[c][k])))
				}

				// Zero negative frequencies
				for k := s.fftFrameSize/2 + 1; k < s.fftFrameSize; k++ {
					s.fftwdata.Elems[k] = 0
				}

				// Inverse STFT
				s.inverse.Execute()

				// Windowing and add to output accumulator (SIMD)
				for k := 0; k < s.fftFrameSize; k++ {
					s.reals[k] = real(s.fftwdata.Elems[k])
				}
				mulAddFloat64s(s.outAcc[c][:s.fftFrameSize], s.windowFactors, s.reals[:s.fftFrameSize])
				copyFloat64s(s.stack[c][:s.step], s.outAcc[c][:s.step])

				// Shift output accumulator and buffer (SIMD)
				copyFloat64s(s.outAcc[c][:s.fftFrameSize], s.outAcc[c][s.step:s.step+s.fftFrameSize])
				copyFloat64s(s.frame[c][:s.latency], s.frame[c][s.step:s.step+s.latency])
			}
		}

		s.frameIndex[c] = frameIndex

		// Re-interleave and convert to output bytes
		stride := int(byteDepth * s.channels)
		off := c * int(byteDepth)
		for i := 0; i < numSamples; i++ {
			float64ToFloat32Bytes(output, off, s.f64buf[i]*s.volume)
			off += stride
		}
	}
}

// Helper functions to convert PCM samples to and from float64

func bytesToFloat64(dst []float64, data []byte, channels, bitRate uint16, channel int) int {
	byteDepth := int(bitRate / 8)
	stride := byteDepth * int(channels)
	n := 0
	for i := channel * byteDepth; i+byteDepth <= len(data); i += stride {
		dst[n] = float64(math.Float32frombits(binary.LittleEndian.Uint32(data[i : i+4])))
		n++
	}
	return n
}

func float64ToFloat32Bytes(d []byte, i int, float float64) {
	binary.LittleEndian.PutUint32(d[i:i+4], math.Float32bits(float32(float)))
}
