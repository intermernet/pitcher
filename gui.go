/****************************************************************************
*
* COPYRIGHT 2026 Mike Hughes <mike <AT> mikehughes <DOT> info
*
****************************************************************************/

package main

import (
	"fmt"
	"log"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/data/binding"
	"fyne.io/fyne/v2/widget"
	"github.com/intermernet/gominiaudio"
	"github.com/intermernet/pitcher/algos"
)

var window fyne.Window

func gui(s *shifter, inputs, outputs []gominiaudio.DeviceInfo, initialInputIdx, initialOutputIdx int, restartAudio func(*gominiaudio.DeviceID, *gominiaudio.DeviceID)) fyne.Window {
	shiftApp := app.New()

	// Define app icon and set window title / size
	icon, err := fyne.LoadResourceFromPath("resource/audio-waveform-icon-8.jpg")
	if err != nil {
		log.Fatalln("icon file not found")
	}
	shiftApp.SetIcon(icon)
	w := shiftApp.NewWindow("Pitcher")
	w.Resize(fyne.NewSize(800, 320))

	// Static device info
	excl := "No"
	if s.exclusive {
		excl = "Yes"
	}
	info := widget.NewLabel(fmt.Sprintf(
		"Channels: %d  |  Sample Rate: %d Hz  |  Periods: %d  |  Buffer: %d frames  |  Exclusive: %s",
		s.Channels, int(s.SampleRate), s.periods, s.bufferSize, excl))
	info.Wrapping = fyne.TextWrapWord

	// Latency display — updated whenever frame size or oversampling changes
	latencyStr := binding.NewString()
	updateLatency := func() {
		latencyStr.Set(fmt.Sprintf("Latency: %.1f ms", float64(s.Latency)/s.SampleRate*1000.0))
	}
	updateLatency()
	latencyLabel := widget.NewLabelWithData(latencyStr)

	// Track current DSP settings so each selector can pass the other's value
	// when calling ReinitContext.
	currentFrameSize := s.FFTFrameSize
	currentOversampling := s.Oversampling

	// Frame size selector
	frameSizeSelect := widget.NewSelect([]string{"256", "512", "1024", "2048", "4096"}, nil)
	frameSizeSelect.SetSelected(strconv.Itoa(s.FFTFrameSize))
	frameSizeSelect.OnChanged = func(v string) {
		fs, _ := strconv.Atoi(v)
		s.ReinitContext(fs, currentOversampling)
		currentFrameSize = fs
		updateLatency()
	}

	// Oversampling selector
	oversamplingSelect := widget.NewSelect([]string{"1", "2", "4", "8"}, nil)
	oversamplingSelect.SetSelected(strconv.Itoa(s.Oversampling))
	oversamplingSelect.OnChanged = func(v string) {
		ov, _ := strconv.Atoi(v)
		s.ReinitContext(currentFrameSize, ov)
		currentOversampling = ov
		updateLatency()
	}

	dspRow := container.NewHBox(
		widget.NewLabel("Frame Size:"),
		frameSizeSelect,
		widget.NewLabel("  Oversampling:"),
		oversamplingSelect,
		widget.NewLabel("  "),
		latencyLabel,
	)

	// Pitch slider — use NewFloat+listener so the binding survives a ReinitContext
	pitch := binding.NewFloat()
	pitch.Set(float64(*shift))
	pitch.AddListener(binding.NewDataListener(func() {
		v, _ := pitch.Get()
		s.PitchShift = v
	}))
	pitchSlider := widget.NewSliderWithData(-12.0, 12.0, pitch)
	pitchSlider.Step = 0.01
	pitchText := binding.FloatToStringWithFormat(pitch, "Pitch = %0.2f")

	// Volume slider
	vol := binding.NewFloat()
	vol.Set(1.0)
	vol.AddListener(binding.NewDataListener(func() {
		v, _ := vol.Get()
		s.Volume = v
	}))
	volSlider := widget.NewSliderWithData(0.0, 1.0, vol)
	volSlider.Step = 0.01
	volText := binding.FloatToStringWithFormat(vol, "Volume = %0.1f")

	// Algorithm selector
	algoLabel := widget.NewLabel("Algorithm: " + s.AlgoName)
	algoSelect := widget.NewSelect(algos.FullNames(), func(selected string) {
		for _, a := range algos.Algorithms {
			if a.FullName == selected {
				s.SetAlgorithm(a)
				algoLabel.SetText("Algorithm: " + s.AlgoName)
				break
			}
		}
	})
	algoSelect.SetSelected(s.AlgoName)

	// Device selectors
	deviceOptionNames := func(devices []gominiaudio.DeviceInfo) []string {
		names := make([]string, len(devices))
		for i, d := range devices {
			if d.IsDefault {
				names[i] = fmt.Sprintf("%d: %s [default]", i, d.Name)
			} else {
				names[i] = fmt.Sprintf("%d: %s", i, d.Name)
			}
		}
		return names
	}
	inputNames := deviceOptionNames(inputs)
	outputNames := deviceOptionNames(outputs)

	// Track current device IDs so each dropdown can preserve the other on restart.
	currentCaptureID := inputs[initialInputIdx].ID
	currentPlaybackID := outputs[initialOutputIdx].ID

	parseDeviceIdx := func(selected string) int {
		parts := strings.SplitN(selected, ":", 2)
		idx, _ := strconv.Atoi(strings.TrimSpace(parts[0]))
		return idx
	}

	inputSelect := widget.NewSelect(inputNames, nil)
	if initialInputIdx < len(inputNames) {
		inputSelect.SetSelected(inputNames[initialInputIdx])
	}
	inputSelect.OnChanged = func(selected string) {
		idx := parseDeviceIdx(selected)
		if idx < 0 || idx >= len(inputs) {
			return
		}
		currentCaptureID = inputs[idx].ID
		pid := currentPlaybackID
		restartAudio(&currentCaptureID, &pid)
	}

	outputSelect := widget.NewSelect(outputNames, nil)
	if initialOutputIdx < len(outputNames) {
		outputSelect.SetSelected(outputNames[initialOutputIdx])
	}
	outputSelect.OnChanged = func(selected string) {
		idx := parseDeviceIdx(selected)
		if idx < 0 || idx >= len(outputs) {
			return
		}
		currentPlaybackID = outputs[idx].ID
		cid := currentCaptureID
		restartAudio(&cid, &currentPlaybackID)
	}

	deviceRow := container.NewHBox(
		widget.NewLabel("Input:"),
		inputSelect,
		widget.NewLabel("  Output:"),
		outputSelect,
	)

	// Layout
	w.SetContent(container.NewVBox(
		info,
		deviceRow,
		dspRow,
		algoLabel,
		algoSelect,
		widget.NewLabelWithData(pitchText),
		pitchSlider,
		widget.NewLabelWithData(volText),
		volSlider,
	))

	return w
}
