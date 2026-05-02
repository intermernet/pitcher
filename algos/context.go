/****************************************************************************
*
* COPYRIGHT 2026 Mike Hughes <mike <AT> mikehughes <DOT> info
*
****************************************************************************/

package algos

import (
	"encoding/binary"
	"math"

	"github.com/runningwild/go-fftw/fftw"
)

// Context holds all shared DSP state used by pitch-shifting algorithms.
type Context struct {
	PitchShift                        float64
	FFTFrameSize                      int
	Oversampling                      int
	SampleRate                        float64
	BitDepth                          uint16
	Channels                          uint16
	Step                              int
	Latency                           int
	FreqPerBin                        float64
	Expected                          float64
	FrameIndex                        []int
	Stack, Frame                      [][]float64
	FFTWData                          *fftw.Array
	Forward, Inverse                  *fftw.Plan
	Magnitudes, Frequencies           []float64
	SynthMagnitudes, SynthFrequencies []float64
	LastPhase, SumPhase               [][]float64
	OutAcc                            [][]float64
	Window, WindowFactors             []float64
	Reals, Imags                      []float64
	F64Buf                            []float64
	Volume                            float64
	// Active algorithm
	AlgoProcess func(ctx *Context, output, input []byte)
	AlgoName    string
	AlgoState   interface{}
}

// NewContext allocates and initialises DSP processing state.
func NewContext(pitchShift float64, fftFrameSize, oversampling int, sampleRate float64, bitDepth uint16, channels int, algo Algorithm) *Context {
	c := new(Context)
	c.PitchShift = pitchShift
	c.FFTFrameSize = fftFrameSize
	c.Oversampling = oversampling
	c.SampleRate = sampleRate
	c.BitDepth = bitDepth
	c.Channels = uint16(channels)
	c.Step = fftFrameSize / oversampling
	c.Latency = fftFrameSize - c.Step
	c.FFTWData = fftw.NewArray(fftFrameSize)
	c.Forward = fftw.NewPlan(c.FFTWData, c.FFTWData, fftw.Forward, fftw.Estimate)
	c.Inverse = fftw.NewPlan(c.FFTWData, c.FFTWData, fftw.Backward, fftw.Estimate)
	c.Magnitudes = make([]float64, fftFrameSize)
	c.Frequencies = make([]float64, fftFrameSize)
	c.SynthMagnitudes = make([]float64, fftFrameSize)
	c.SynthFrequencies = make([]float64, fftFrameSize)
	c.FrameIndex = make([]int, channels)
	c.Stack = make([][]float64, channels)
	c.Frame = make([][]float64, channels)
	c.LastPhase = make([][]float64, channels)
	c.SumPhase = make([][]float64, channels)
	c.OutAcc = make([][]float64, channels)
	for ch := 0; ch < channels; ch++ {
		c.FrameIndex[ch] = c.Latency
		c.Stack[ch] = make([]float64, fftFrameSize)
		c.Frame[ch] = make([]float64, fftFrameSize)
		c.LastPhase[ch] = make([]float64, fftFrameSize/2+1)
		c.SumPhase[ch] = make([]float64, fftFrameSize/2+1)
		c.OutAcc[ch] = make([]float64, 2*fftFrameSize)
	}
	c.Volume = 1.0
	c.SetAlgorithm(algo)

	c.Expected = 2 * math.Pi * float64(c.Step) / float64(fftFrameSize)
	c.FreqPerBin = sampleRate / float64(fftFrameSize)

	c.Window = make([]float64, fftFrameSize)
	c.WindowFactors = make([]float64, fftFrameSize)
	c.Reals = make([]float64, fftFrameSize)
	c.Imags = make([]float64, fftFrameSize)
	c.F64Buf = make([]float64, max(fftFrameSize, 8192))
	t := 0.0
	for i := 0; i < fftFrameSize; i++ {
		// Hanning window
		w := -0.5*math.Cos(t) + 0.5
		c.Window[i] = w
		c.WindowFactors[i] = w * (2.0 / float64(fftFrameSize*oversampling))
		t += (math.Pi * 2.0) / float64(fftFrameSize)
	}

	return c
}

// SetAlgorithm switches the active pitch-shifting algorithm at runtime.
func (c *Context) SetAlgorithm(a Algorithm) {
	c.AlgoProcess = a.Process
	c.AlgoName = a.FullName
	if a.NewState != nil {
		c.AlgoState = a.NewState(c)
	} else {
		c.AlgoState = nil
	}
}

// bytesToFloat64 decodes a single channel from interleaved float32 PCM bytes into dst.
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

// float64ToFloat32Bytes encodes a float64 sample as a little-endian float32 into d[i:i+4].
func float64ToFloat32Bytes(d []byte, i int, f float64) {
	binary.LittleEndian.PutUint32(d[i:i+4], math.Float32bits(float32(f)))
}
