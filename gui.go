/****************************************************************************
*
* COPYRIGHT 2025 Mike Hughes <mike <AT> mikehughes <DOT> info
*
****************************************************************************/

package main

import (
	"fmt"
	"log"
	"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/data/binding"
	"fyne.io/fyne/v2/widget"
	"github.com/intermernet/pitcher/algos"
)

var window fyne.Window

func gui(s *shifter) fyne.Window {
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

	// Layout
	w.SetContent(container.NewVBox(
		info,
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
