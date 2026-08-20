/****************************************************************************
*
* COPYRIGHT 2026 Mike Hughes <mike <AT> mikehughes <DOT> info
*
****************************************************************************/

package main

import (
	"fmt"
	"io"
	"log"
	"log/slog"
	"strconv"

	_ "github.com/gogpu/gg/gpu"
	"github.com/gogpu/gogpu"
	"github.com/gogpu/ui/app"
	"github.com/gogpu/ui/core/dropdown"
	"github.com/gogpu/ui/core/slider"
	"github.com/gogpu/ui/desktop"
	"github.com/gogpu/ui/geometry"
	"github.com/gogpu/ui/primitives"
	"github.com/gogpu/ui/state"
	"github.com/gogpu/ui/theme/material3"
	"github.com/gogpu/ui/widget"
	"github.com/intermernet/gominiaudio"
	"github.com/intermernet/pitcher/algos"
)

// fixedFontCanvas wraps a widget.Canvas and replaces every DrawText font size
// with a fixed value. Used to override the hard-coded 16 px font size in
// material3.DropdownPainter.
type fixedFontCanvas struct {
	widget.Canvas
	fontSize float32
}

func (c fixedFontCanvas) DrawText(text string, bounds geometry.Rect, _ float32, color widget.Color, bold bool, align widget.TextAlign) {
	c.Canvas.DrawText(text, bounds, c.fontSize, color, bold, align)
}

// smallDropdownPainter delegates to material3.DropdownPainter but paints all
// text at a smaller fixed size so it fits inside compact dropdowns.
type smallDropdownPainter struct {
	inner    material3.DropdownPainter
	fontSize float32
}

func (p smallDropdownPainter) PaintTrigger(canvas widget.Canvas, state *dropdown.TriggerPaintState) {
	p.inner.PaintTrigger(fixedFontCanvas{canvas, p.fontSize}, state)
}

func (p smallDropdownPainter) PaintMenu(canvas widget.Canvas, state *dropdown.MenuPaintState) {
	p.inner.PaintMenu(fixedFontCanvas{canvas, p.fontSize}, state)
}

func gui(s *shifter, inputs, outputs []gominiaudio.DeviceInfo, initialInputIdx, initialOutputIdx int, restartAudio func(*gominiaudio.DeviceID, *gominiaudio.DeviceID)) func() error {
	m3 := material3.New(widget.Hex(0x6750A4))
	cs := m3.Colors

	gogpuApp := gogpu.NewApp(gogpu.DefaultConfig().
		WithTitle("Pitcher").
		WithSize(800, 520).
		WithResizable(true))

	// Static device info label
	excl := "No"
	if s.exclusive {
		excl = "Yes"
	}
	infoText := fmt.Sprintf(
		"Channels: %d  |  Sample Rate: %d Hz  |  Periods: %d  |  Buffer: %d frames  |  Exclusive: %s",
		s.Channels, int(s.SampleRate), s.periods, s.bufferSize, excl)

	// Latency � updated whenever frame size or oversampling changes
	latency := state.NewSignal(fmt.Sprintf("Latency: %.1f ms", float64(s.Latency)/s.SampleRate*1000.0))
	updateLatency := func() {
		latency.Set(fmt.Sprintf("Latency: %.1f ms", float64(s.Latency)/s.SampleRate*1000.0))
	}

	// Track current DSP settings so each selector can pass the other's value
	// when calling ReinitContext.
	currentFrameSize := s.FFTFrameSize
	currentOversampling := s.Oversampling

	dropPainter := smallDropdownPainter{inner: material3.DropdownPainter{Theme: m3}, fontSize: 12}

	// Frame size selector
	frameSizeOptions := []string{"256", "512", "1024", "2048", "4096"}
	frameSizeIdx := 0
	for i, v := range frameSizeOptions {
		if v == strconv.Itoa(s.FFTFrameSize) {
			frameSizeIdx = i
			break
		}
	}
	frameSizeDrop := dropdown.New(
		dropdown.Items(frameSizeOptions...),
		dropdown.Selected(frameSizeIdx),
		dropdown.OnChange(func(_ int, v string) {
			fs, _ := strconv.Atoi(v)
			s.ReinitContext(fs, currentOversampling)
			currentFrameSize = fs
			updateLatency()
		}),
		dropdown.PainterOpt(dropPainter),
	)

	// Oversampling selector
	oversamplingOptions := []string{"1", "2", "4", "8"}
	oversamplingIdx := 0
	for i, v := range oversamplingOptions {
		if v == strconv.Itoa(s.Oversampling) {
			oversamplingIdx = i
			break
		}
	}
	oversamplingDrop := dropdown.New(
		dropdown.Items(oversamplingOptions...),
		dropdown.Selected(oversamplingIdx),
		dropdown.OnChange(func(_ int, v string) {
			ov, _ := strconv.Atoi(v)
			s.ReinitContext(currentFrameSize, ov)
			currentOversampling = ov
			updateLatency()
		}),
		dropdown.PainterOpt(dropPainter),
	)

	dspRow := primitives.HBox(
		primitives.Text("Frame Size:").FontSize(13),
		frameSizeDrop,
		primitives.Text("Oversampling:").FontSize(13),
		oversamplingDrop,
		primitives.TextFn(func() string { return latency.Get() }).FontSize(13),
	).Gap(8)

	// Pitch slider � two-way bound signal drives both the slider and the label
	pitchSig := state.NewSignal(float32(*shift))
	state.SubscribeForever(pitchSig, func(v float32) { s.PitchShift = float64(v) })
	pitchSlider := slider.New(
		slider.Min(-12),
		slider.Max(12),
		slider.Step(0.01),
		slider.ValueSignal(pitchSig),
		slider.PainterOpt(material3.SliderPainter{Theme: m3}),
	)

	// Volume slider
	volSig := state.NewSignal(float32(1.0))
	state.SubscribeForever(volSig, func(v float32) { s.Volume = float64(v) })
	volSlider := slider.New(
		slider.Min(0),
		slider.Max(1),
		slider.Step(0.01),
		slider.ValueSignal(volSig),
		slider.PainterOpt(material3.SliderPainter{Theme: m3}),
	)

	// Algorithm selector
	algoNames := algos.FullNames()
	algoIdx := 0
	for i, n := range algoNames {
		if n == s.AlgoName {
			algoIdx = i
			break
		}
	}
	algoLabelSig := state.NewSignal("Algorithm: " + s.AlgoName)
	algoDrop := dropdown.New(
		dropdown.Items(algoNames...),
		dropdown.Selected(algoIdx),
		dropdown.OnChange(func(_ int, name string) {
			for _, a := range algos.Algorithms {
				if a.FullName == name {
					s.SetAlgorithm(a)
					algoLabelSig.Set("Algorithm: " + s.AlgoName)
					break
				}
			}
		}),
		dropdown.PainterOpt(dropPainter),
	)

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

	currentCaptureID := inputs[initialInputIdx].ID
	currentPlaybackID := outputs[initialOutputIdx].ID

	inputDrop := dropdown.New(
		dropdown.Items(deviceOptionNames(inputs)...),
		dropdown.Selected(initialInputIdx),
		dropdown.OnChange(func(idx int, _ string) {
			if idx < 0 || idx >= len(inputs) {
				return
			}
			currentCaptureID = inputs[idx].ID
			pid := currentPlaybackID
			restartAudio(&currentCaptureID, &pid)
		}),
		dropdown.PainterOpt(dropPainter),
	)

	outputDrop := dropdown.New(
		dropdown.Items(deviceOptionNames(outputs)...),
		dropdown.Selected(initialOutputIdx),
		dropdown.OnChange(func(idx int, _ string) {
			if idx < 0 || idx >= len(outputs) {
				return
			}
			currentPlaybackID = outputs[idx].ID
			cid := currentCaptureID
			restartAudio(&cid, &currentPlaybackID)
		}),
		dropdown.PainterOpt(dropPainter),
	)

	// labelW is wide enough for "Output:" at 13 px so both labels align.
	const labelW float32 = 60

	inputRow := primitives.HBox(
		primitives.Box(primitives.Text("Input:").FontSize(13)).Width(labelW),
		primitives.Expanded(inputDrop),
	).Gap(8)

	outputRow := primitives.HBox(
		primitives.Box(primitives.Text("Output:").FontSize(13)).Width(labelW),
		primitives.Expanded(outputDrop),
	).Gap(8)

	deviceRow := primitives.VBox(inputRow, outputRow).Gap(4)

	// Root layout
	root := primitives.VBox(
		primitives.Text(infoText).FontSize(12),
		deviceRow,
		dspRow,
		primitives.TextFn(func() string { return algoLabelSig.Get() }).FontSize(13),
		primitives.HBox(primitives.Expanded(algoDrop)),
		primitives.TextFn(func() string {
			return fmt.Sprintf("Pitch = %.2f", pitchSig.Get())
		}).FontSize(13),
		pitchSlider,
		primitives.TextFn(func() string {
			return fmt.Sprintf("Volume = %.2f", volSig.Get())
		}).FontSize(13),
		volSlider,
	).
		Gap(8).
		Padding(16).
		Background(cs.Background)

	uiApp := app.New(
		app.WithWindowProvider(gogpuApp),
		app.WithPlatformProvider(gogpuApp),
		app.WithEventSource(gogpuApp.EventSource()),
		app.WithTheme(m3.AsTheme()),
	)
	uiApp.SetRoot(root)

	return func() error {
		// Silence the standard log package (used by desktop.go error handlers)
		// and slog.Default() so no gogpu diagnostics appear on the console.
		log.SetOutput(io.Discard)
		slog.SetDefault(slog.New(slog.DiscardHandler))
		return desktop.Run(gogpuApp, uiApp)
	}
}
