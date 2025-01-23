/****************************************************************************
*
* COPYRIGHT 2023 Mike Hughes <mike <AT> mikehughes <DOT> info
*
****************************************************************************/

package main

import (
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
		widget.NewLabelWithData(pitchText),
		pitchSlider,
		widget.NewLabelWithData(volText),
		volSlider,
	))

	return w
}
