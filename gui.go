/****************************************************************************
*
* COPYRIGHT 2026 Mike Hughes <mike <AT> mikehughes <DOT> info
*
****************************************************************************/

package main

import (
	"embed"
	"fmt"
	"io/fs"
	"strconv"
	"sync"

	"github.com/intermernet/gominiaudio"
	"github.com/intermernet/pitcher/algos"
	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist
var assets embed.FS

// AppState is the serializable snapshot of GUI-relevant state.
type AppState struct {
	Shift        float64 `json:"shift"`
	Volume       float64 `json:"volume"`
	AlgoName     string  `json:"algoName"`
	FrameSize    int     `json:"frameSize"`
	Oversampling int     `json:"oversampling"`
	LatencyMs    float64 `json:"latencyMs"`
	Channels     int     `json:"channels"`
	SampleRate   int     `json:"sampleRate"`
	Periods      int     `json:"periods"`
	BufferSize   int     `json:"bufferSize"`
	Exclusive    bool    `json:"exclusive"`
	InputIdx     int     `json:"inputIdx"`
	OutputIdx    int     `json:"outputIdx"`
}

// DeviceOption is a single audio device entry for the frontend dropdowns.
type DeviceOption struct {
	Name      string `json:"name"`
	IsDefault bool   `json:"isDefault"`
}

// PitcherService exposes all GUI-controllable operations to the Wails frontend.
type PitcherService struct {
	mu               sync.Mutex
	s                *shifter
	inputs           []gominiaudio.DeviceInfo
	outputs          []gominiaudio.DeviceInfo
	restartAudio     func(*gominiaudio.DeviceID, *gominiaudio.DeviceID)
	currentInputIdx  int
	currentOutputIdx int
}

func (ps *PitcherService) latencyMs() float64 {
	return float64(ps.s.Latency) / ps.s.SampleRate * 1000.0
}

// GetState returns a snapshot of the current application state.
func (ps *PitcherService) GetState() AppState {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return AppState{
		Shift:        ps.s.PitchShift,
		Volume:       ps.s.Volume,
		AlgoName:     ps.s.AlgoName,
		FrameSize:    ps.s.FFTFrameSize,
		Oversampling: ps.s.Oversampling,
		LatencyMs:    ps.latencyMs(),
		Channels:     int(ps.s.Channels),
		SampleRate:   int(ps.s.SampleRate),
		Periods:      ps.s.periods,
		BufferSize:   ps.s.bufferSize,
		Exclusive:    ps.s.exclusive,
		InputIdx:     ps.currentInputIdx,
		OutputIdx:    ps.currentOutputIdx,
	}
}

// GetAlgoNames returns all available algorithm full names.
func (ps *PitcherService) GetAlgoNames() []string {
	return algos.FullNames()
}

// GetFrameSizes returns the available FFT frame size option strings.
func (ps *PitcherService) GetFrameSizes() []string {
	return []string{"256", "512", "1024", "2048", "4096"}
}

// GetOversamplingOptions returns the available oversampling option strings.
func (ps *PitcherService) GetOversamplingOptions() []string {
	return []string{"1", "2", "4", "8"}
}

// GetInputDevices returns the available input audio devices.
func (ps *PitcherService) GetInputDevices() []DeviceOption {
	opts := make([]DeviceOption, len(ps.inputs))
	for i, d := range ps.inputs {
		name := d.Name
		if d.IsDefault {
			name = fmt.Sprintf("%s [default]", name)
		}
		opts[i] = DeviceOption{Name: name, IsDefault: d.IsDefault}
	}
	return opts
}

// GetOutputDevices returns the available output audio devices.
func (ps *PitcherService) GetOutputDevices() []DeviceOption {
	opts := make([]DeviceOption, len(ps.outputs))
	for i, d := range ps.outputs {
		name := d.Name
		if d.IsDefault {
			name = fmt.Sprintf("%s [default]", name)
		}
		opts[i] = DeviceOption{Name: name, IsDefault: d.IsDefault}
	}
	return opts
}

// SetPitch sets the pitch-shift value in semitones (-12 to +12).
func (ps *PitcherService) SetPitch(value float64) {
	ps.s.PitchShift = value
}

// SetVolume sets the output volume (0.0 to 1.0).
func (ps *PitcherService) SetVolume(value float64) {
	ps.s.Volume = value
}

// SetAlgorithm switches to the algorithm identified by its full name.
func (ps *PitcherService) SetAlgorithm(name string) {
	for _, a := range algos.Algorithms {
		if a.FullName == name {
			ps.s.SetAlgorithm(a)
			return
		}
	}
}

// SetFrameSize changes the FFT frame size and returns the updated latency in ms.
func (ps *PitcherService) SetFrameSize(sizeStr string) float64 {
	size, err := strconv.Atoi(sizeStr)
	if err != nil || size == 0 {
		return ps.latencyMs()
	}
	ps.s.ReinitContext(size, ps.s.Oversampling)
	return ps.latencyMs()
}

// SetOversampling changes the oversampling factor and returns the updated latency in ms.
func (ps *PitcherService) SetOversampling(osStr string) float64 {
	os, err := strconv.Atoi(osStr)
	if err != nil || os == 0 {
		return ps.latencyMs()
	}
	ps.s.ReinitContext(ps.s.FFTFrameSize, os)
	return ps.latencyMs()
}

// SetInputDevice changes the audio input device by index.
func (ps *PitcherService) SetInputDevice(idx int) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if idx < 0 || idx >= len(ps.inputs) {
		return
	}
	ps.currentInputIdx = idx
	captureID := ps.inputs[idx].ID
	playbackID := ps.outputs[ps.currentOutputIdx].ID
	ps.restartAudio(&captureID, &playbackID)
}

// SetOutputDevice changes the audio output device by index.
func (ps *PitcherService) SetOutputDevice(idx int) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if idx < 0 || idx >= len(ps.outputs) {
		return
	}
	ps.currentOutputIdx = idx
	captureID := ps.inputs[ps.currentInputIdx].ID
	playbackID := ps.outputs[idx].ID
	ps.restartAudio(&captureID, &playbackID)
}

// gui constructs a Wails v3 application and returns a function that runs it.
func gui(s *shifter, inputs, outputs []gominiaudio.DeviceInfo, initialInputIdx, initialOutputIdx int, restartAudio func(*gominiaudio.DeviceID, *gominiaudio.DeviceID)) func() error {
	svc := &PitcherService{
		s:                s,
		inputs:           inputs,
		outputs:          outputs,
		restartAudio:     restartAudio,
		currentInputIdx:  initialInputIdx,
		currentOutputIdx: initialOutputIdx,
	}

	return func() error {
		subFS, err := fs.Sub(assets, "frontend/dist")
		if err != nil {
			return fmt.Errorf("embedded assets: %w", err)
		}

		app := application.New(application.Options{
			Name:        "Pitcher",
			Description: "Real-time pitch shifter",
			Services: []application.Service{
				application.NewService(svc),
			},
			Assets: application.AssetOptions{
				Handler: application.BundledAssetFileServer(subFS),
			},
			Mac: application.MacOptions{
				ApplicationShouldTerminateAfterLastWindowClosed: true,
			},
		})

		app.Window.NewWithOptions(application.WebviewWindowOptions{
			Title:            "Pitcher",
			Width:            820,
			Height:           600,
			MinWidth:         620,
			MinHeight:        440,
			BackgroundColour: application.NewRGB(18, 18, 18),
			URL:              "/",
			InitialPosition:  application.WindowCentered,
		})

		return app.Run()
	}
}
