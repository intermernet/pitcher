/****************************************************************************
*
* COPYRIGHT 2026 Mike Hughes <mike <AT> mikehughes <DOT> info
*
****************************************************************************/

package main

import (
	"sync"

	"github.com/intermernet/pitcher/algos"
)

// shifter wraps the DSP Context with audio device configuration.
type shifter struct {
	mu sync.RWMutex
	*algos.Context
	currentAlgo algos.Algorithm
	periods     int
	bufferSize  int
	exclusive   bool
}

func newShifter(fftFrameSize, oversampling int, sampleRate float64, bitDepth uint16, channels, periods, bufferSize int, exclusive bool, algo algos.Algorithm) *shifter {
	return &shifter{
		Context:     algos.NewContext(float64(*shift), fftFrameSize, oversampling, sampleRate, bitDepth, channels, algo),
		currentAlgo: algo,
		periods:     periods,
		bufferSize:  bufferSize,
		exclusive:   exclusive,
	}
}

// SetAlgorithm shadows the embedded method to keep currentAlgo in sync.
// The write lock is held for the duration so that the audio callback cannot
// observe a mismatched AlgoProcess/AlgoState pair mid-swap.
func (s *shifter) SetAlgorithm(a algos.Algorithm) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.currentAlgo = a
	s.Context.SetAlgorithm(a)
}

// ReinitContext destroys the current DSP context and builds a fresh one with
// the given frame size and oversampling factor. Ongoing audio callbacks are
// blocked for the duration of the swap via the write lock.
func (s *shifter) ReinitContext(fftFrameSize, oversampling int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pitchShift := s.PitchShift
	volume := s.Volume
	s.Forward.Destroy()
	s.Inverse.Destroy()
	s.Context = algos.NewContext(pitchShift, fftFrameSize, oversampling, s.SampleRate, s.BitDepth, int(s.Channels), s.currentAlgo)
	s.Volume = volume
}

// Destroy releases FFTW resources held by the current DSP context.
func (s *shifter) Destroy() {
	s.Forward.Destroy()
	s.Inverse.Destroy()
}

// process is the malgo audio callback. It delegates to the active algorithm.
func (s *shifter) process(pOutputSample, pInputSamples []byte, framecount uint32) {
	s.mu.RLock()
	s.AlgoProcess(s.Context, pOutputSample, pInputSamples)
	s.mu.RUnlock()
}

// processAudio is the testable entry point for the active algorithm.
func (s *shifter) processAudio(output, input []byte) {
	s.AlgoProcess(s.Context, output, input)
}
