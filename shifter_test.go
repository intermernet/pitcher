package main

import (
	"encoding/binary"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/intermernet/pitcher/algos"
)

const (
	testFFTFrameSize = 512
	testOversampling = 32
	testSampleRate   = 48000.0
	testBitDepth     = 32
	testChannels     = 2
)

// initShift ensures the global shift pointer is set for newShifter.
func initShift(semitones int) {
	shift = &semitones
}

// generateSineFrame generates a stereo float32 PCM frame containing a sine wave.
func generateSineFrame(freq float64, numSamples int, sampleRate float64, phase float64) ([]byte, float64) {
	bytesPerSample := 4 // float32
	buf := make([]byte, numSamples*testChannels*bytesPerSample)
	p := phase
	step := 2 * math.Pi * freq / sampleRate
	for i := 0; i < numSamples; i++ {
		sample := float32(math.Sin(p))
		bits := math.Float32bits(sample)
		offset := i * testChannels * bytesPerSample
		// Write same sample to both channels
		binary.LittleEndian.PutUint32(buf[offset:], bits)
		binary.LittleEndian.PutUint32(buf[offset+bytesPerSample:], bits)
		p += step
	}
	return buf, p
}

// newTestShifter creates a shifter wired for testing (no audio hardware).
func newTestShifter(semitones int) *shifter {
	initShift(semitones)
	return newShifter(testFFTFrameSize, testOversampling, testSampleRate, testBitDepth, testChannels, 2, testFFTFrameSize/4, false, algos.Default())
}

func TestShiftPassthrough(t *testing.T) {
	s := newTestShifter(0) // 0 semitones = passthrough
	defer s.Forward.Destroy()
	defer s.Inverse.Destroy()

	samplesPerFrame := testFFTFrameSize

	// Feed several frames of 440 Hz sine to fill the pipeline
	numFrames := 10
	phase := 0.0
	var allOutput []byte
	for i := 0; i < numFrames; i++ {
		input, p := generateSineFrame(440, samplesPerFrame, testSampleRate, phase)
		phase = p
		output := make([]byte, len(input))
		s.processAudio(output, input)
		allOutput = append(allOutput, output...)
	}

	if len(allOutput) == 0 {
		t.Fatal("expected output data, got none")
	}

	// Verify output is non-silent: at least some samples should be non-zero
	bytesPerSample := int(s.BitDepth / 8)
	nonZero := 0
	totalSamples := len(allOutput) / bytesPerSample
	for i := 0; i+bytesPerSample <= len(allOutput); i += bytesPerSample {
		v := math.Float32frombits(binary.LittleEndian.Uint32(allOutput[i : i+bytesPerSample]))
		if v != 0 {
			nonZero++
		}
	}

	if nonZero == 0 {
		t.Fatal("output is all zeros — processing did not produce audio")
	}
	t.Logf("output: %d/%d samples non-zero (%.1f%%)", nonZero, totalSamples, 100*float64(nonZero)/float64(totalSamples))

	// Verify output values are within a sane range (no NaN, no huge values)
	for i := 0; i+bytesPerSample <= len(allOutput); i += bytesPerSample {
		v := math.Float32frombits(binary.LittleEndian.Uint32(allOutput[i : i+bytesPerSample]))
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			t.Fatalf("output contains NaN or Inf at byte offset %d", i)
		}
		if math.Abs(float64(v)) > 10.0 {
			t.Fatalf("output sample %.6f at byte offset %d exceeds sane range", v, i)
		}
	}
}

func TestShiftPitchUp(t *testing.T) {
	s := newTestShifter(12) // +12 semitones = one octave up
	defer s.Forward.Destroy()
	defer s.Inverse.Destroy()

	samplesPerFrame := testFFTFrameSize

	phase := 0.0
	var allOutput []byte
	for i := 0; i < 10; i++ {
		input, p := generateSineFrame(440, samplesPerFrame, testSampleRate, phase)
		phase = p
		output := make([]byte, len(input))
		s.processAudio(output, input)
		allOutput = append(allOutput, output...)
	}

	if len(allOutput) == 0 {
		t.Fatal("expected output data, got none")
	}

	// Basic sanity: output should exist and contain valid floats
	bytesPerSample := int(s.BitDepth / 8)
	for i := 0; i+bytesPerSample <= len(allOutput); i += bytesPerSample {
		v := math.Float32frombits(binary.LittleEndian.Uint32(allOutput[i : i+bytesPerSample]))
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			t.Fatalf("output contains NaN or Inf at byte offset %d", i)
		}
	}
	t.Logf("pitch-up produced %d bytes of output", len(allOutput))
}

// generateSineSweep generates a 2-second stereo sine sweep from startFreq to endFreq
// in float32 PCM format, split into frames of bytesPerFrame bytes each.
func generateSineSweep(startFreq, endFreq, sampleRate float64, durationSec float64, channels int) []byte {
	totalSamples := int(sampleRate * durationSec)
	bytesPerSample := 4 // float32
	buf := make([]byte, totalSamples*channels*bytesPerSample)
	phase := 0.0
	for i := 0; i < totalSamples; i++ {
		t := float64(i) / float64(totalSamples)
		freq := startFreq * math.Pow(endFreq/startFreq, t) // exponential sweep
		sample := float32(0.8 * math.Sin(phase))
		phase += 2 * math.Pi * freq / sampleRate
		bits := math.Float32bits(sample)
		offset := i * channels * bytesPerSample
		for ch := 0; ch < channels; ch++ {
			binary.LittleEndian.PutUint32(buf[offset+ch*bytesPerSample:], bits)
		}
	}
	return buf
}

// readSamplesF32 decodes interleaved float32 PCM bytes into per-channel sample slices.
func readSamplesF32(data []byte, channels int) [][]float32 {
	bytesPerSample := 4
	totalSamples := len(data) / (bytesPerSample * channels)
	out := make([][]float32, channels)
	for ch := range out {
		out[ch] = make([]float32, totalSamples)
	}
	for i := 0; i < totalSamples; i++ {
		for ch := 0; ch < channels; ch++ {
			off := (i*channels + ch) * bytesPerSample
			out[ch][i] = math.Float32frombits(binary.LittleEndian.Uint32(data[off : off+bytesPerSample]))
		}
	}
	return out
}

func TestSineSweepGlitchDetection(t *testing.T) {
	for _, semitones := range []int{0, 3, 7, 12, -12} {
		t.Run(fmt.Sprintf("shift_%+d", semitones), func(t *testing.T) {
			s := newTestShifter(semitones)
			defer s.Forward.Destroy()
			defer s.Inverse.Destroy()

			// Generate 2-second sine sweep 100 Hz → 8000 Hz
			sweep := generateSineSweep(100, 8000, testSampleRate, 2.0, testChannels)

			// Process in fftFrameSize-sized chunks
			chunkSize := testFFTFrameSize * testChannels * (testBitDepth / 8)
			output := make([]byte, len(sweep))
			for off := 0; off+chunkSize <= len(sweep); off += chunkSize {
				s.processAudio(output[off:off+chunkSize], sweep[off:off+chunkSize])
			}

			channels := readSamplesF32(output, testChannels)

			for ch := 0; ch < testChannels; ch++ {
				samples := channels[ch]
				if len(samples) < 2 {
					t.Fatalf("channel %d: too few output samples (%d)", ch, len(samples))
				}

				// Skip the initial latency region (first fftFrameSize samples may be zero/ramping)
				skip := testFFTFrameSize
				if skip >= len(samples) {
					skip = 0
				}

				// 1. Check for NaN / Inf
				for i, v := range samples {
					if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
						t.Fatalf("ch%d sample %d: NaN or Inf", ch, i)
					}
				}

				// 2. Detect discontinuities: large sample-to-sample jumps
				// With a smoothly changing signal, adjacent samples shouldn't jump
				// by more than a threshold. Higher pitch ratios produce steeper
				// waveforms, so scale the threshold accordingly.
				maxJump := float64(0)
				jumpCount := 0
				ratio := math.Exp2(math.Abs(float64(semitones)) / 12.0)
				threshold := 0.5 * ratio // scale for faster oscillation at higher pitch
				for i := skip + 1; i < len(samples); i++ {
					jump := math.Abs(float64(samples[i] - samples[i-1]))
					if jump > maxJump {
						maxJump = jump
					}
					if jump > threshold {
						jumpCount++
					}
				}

				analyzedSamples := len(samples) - skip - 1
				jumpRate := float64(jumpCount) / float64(analyzedSamples)

				t.Logf("ch%d: %d samples, maxJump=%.4f, jumps>%.1f: %d (%.2f%%)",
					ch, len(samples), maxJump, threshold, jumpCount, jumpRate*100)

				// More than 1% of samples with large jumps indicates glitching
				if jumpRate > 0.01 {
					t.Errorf("ch%d: excessive discontinuities: %d/%d (%.2f%%) samples exceed threshold %.1f",
						ch, jumpCount, analyzedSamples, jumpRate*100, threshold)
				}

				// 3. Check output isn't silent (after warmup)
				rms := float64(0)
				for i := skip; i < len(samples); i++ {
					rms += float64(samples[i]) * float64(samples[i])
				}
				rms = math.Sqrt(rms / float64(len(samples)-skip))
				t.Logf("ch%d: RMS=%.6f", ch, rms)

				if rms < 0.001 {
					t.Errorf("ch%d: output appears silent (RMS=%.6f)", ch, rms)
				}

				// 4. Check amplitude is reasonable (not blown up)
				maxAmp := float64(0)
				for i := skip; i < len(samples); i++ {
					a := math.Abs(float64(samples[i]))
					if a > maxAmp {
						maxAmp = a
					}
				}
				t.Logf("ch%d: peak=%.4f", ch, maxAmp)
				if maxAmp > 5.0 {
					t.Errorf("ch%d: output amplitude too high (peak=%.4f)", ch, maxAmp)
				}
			}
		})
	}
}

// BenchmarkShift measures throughput and latency of the processAudio loop.
func BenchmarkShift(b *testing.B) {
	for _, frameSize := range []int{256, 512, 1024} {
		for _, oversampling := range []int{4, 16, 32} {
			name := fmt.Sprintf("fft%d_os%d", frameSize, oversampling)
			b.Run(name, func(b *testing.B) {
				initShift(0)
				s := newShifter(frameSize, oversampling, testSampleRate, testBitDepth, testChannels, 2, frameSize/4, false, algos.Default())
				defer s.Forward.Destroy()
				defer s.Inverse.Destroy()

				samplesPerFrame := frameSize
				bytesPerFrame := samplesPerFrame * testChannels * (testBitDepth / 8)
				frame, _ := generateSineFrame(440, samplesPerFrame, testSampleRate, 0)
				outBuf := make([]byte, bytesPerFrame)

				// Warm up the pipeline
				for i := 0; i < 3; i++ {
					s.processAudio(outBuf, frame)
				}

				b.ResetTimer()
				b.SetBytes(int64(bytesPerFrame))

				totalSamples := 0
				for i := 0; i < b.N; i++ {
					s.processAudio(outBuf, frame)
					totalSamples += samplesPerFrame
				}

				b.StopTimer()
				elapsed := b.Elapsed()
				samplesPerSec := float64(totalSamples) / elapsed.Seconds()
				latencyPerFrame := elapsed / time.Duration(b.N)
				b.ReportMetric(samplesPerSec, "samples/s")
				b.ReportMetric(float64(latencyPerFrame.Microseconds()), "µs/frame")
			})
		}
	}
}
