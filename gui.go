/****************************************************************************
*
* COPYRIGHT 2025 Mike Hughes <mike <AT> mikehughes <DOT> info
*
****************************************************************************/

package main

import (
	"fmt"
	"log"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/data/binding"
	"fyne.io/fyne/v2/widget"
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
	w.Resize(fyne.NewSize(800, 200))

	// Info text
	excl := "No"
	if s.exclusive {
		excl = "Yes"
	}
	info := widget.NewLabel(fmt.Sprintf("Channels: %d, Frame size: %d, Oversampling: %d, Sample Rate: %d Hz", s.channels, s.fftFrameSize, s.oversampling, int(s.sampleRate)))
	info.Wrapping = fyne.TextWrapWord
	info2 := widget.NewLabel(fmt.Sprintf("Periods: %d, Buffer Size: %d frames, Exclusive Mode: %s", s.periods, s.bufferSize, excl))
	info2.Wrapping = fyne.TextWrapWord

	// Pitch slider
	pitch := binding.BindFloat(&s.pitchShift)
	pitch.Set(float64(*shift))
	pitchSlider := widget.NewSliderWithData(-12.0, 12.0, pitch)
	pitchSlider.Step = 0.01
	pitchText := binding.FloatToStringWithFormat(pitch, "Pitch = %0.2f")

	// Volume slider
	vol := binding.BindFloat(&s.volume)
	vol.Set(1.0)
	volSlider := widget.NewSliderWithData(0.0, 1.0, vol)
	volSlider.Step = 0.01
	volText := binding.FloatToStringWithFormat(vol, "Volume = %0.1f")

	// Layout
	w.SetContent(container.NewVBox(
		info,
		info2,
		widget.NewLabelWithData(pitchText),
		pitchSlider,
		widget.NewLabelWithData(volText),
		volSlider,
	))

	return w
}
