/****************************************************************************
*
* COPYRIGHT 2023 Mike Hughes <mike <AT> mikehughes <DOT> info
*
****************************************************************************/

package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/data/binding"
	"fyne.io/fyne/v2/widget"
)

func gui(s *shifter) fyne.Window {
	shiftApp := app.New()
	w := shiftApp.NewWindow("Pitcher")
	w.Resize(fyne.NewSize(800, 200))

	pitch := binding.BindFloat(&s.pitchShift)
	pitch.Set(0.0)
	pitchSlider := widget.NewSliderWithData(-12.0, 12.0, pitch)
	pitchSlider.Step = 0.01
	pitchText := binding.FloatToStringWithFormat(pitch, "Pitch = %0.2f")

	vol := binding.BindFloat(&s.volume)
	vol.Set(0.5)
	volSlider := widget.NewSliderWithData(0.0, 1.0, vol)
	volSlider.Step = 0.01
	volText := binding.FloatToStringWithFormat(vol, "Volume = %0.1f")

	w.SetContent(container.NewVBox(
		widget.NewLabelWithData(pitchText),
		pitchSlider,
		widget.NewLabelWithData(volText),
		volSlider,
	))

	return w
}
