/****************************************************************************
*
* COPYRIGHT 2023 Mike Hughes <mike <AT> mikehughes <DOT> info
*
****************************************************************************/

package main

import (
	"fmt"
	"os"

	// "net/http"
	// _ "net/http/pprof"

	"github.com/gen2brain/malgo"
)

func main() {
	// go func() {
	// 	log.Println(http.ListenAndServe("localhost:9999", nil))
	// }()

	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, func(message string) {
		fmt.Printf("LOG: %v", message)
	})
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	defer func() {
		_ = ctx.Uninit()
		ctx.Free()
	}()

	sampleRate := 44100.0
	channels := 2
	format := malgo.FormatS16
	bitDepth := uint16(malgo.SampleSizeInBytes(format) * 8)
	deviceConfig := malgo.DefaultDeviceConfig(malgo.Duplex)
	deviceConfig.PerformanceProfile = malgo.LowLatency
	deviceConfig.Capture.Format = format
	deviceConfig.Capture.Channels = uint32(channels)
	deviceConfig.Playback.Format = format
	deviceConfig.Playback.Channels = uint32(channels)
	deviceConfig.SampleRate = uint32(sampleRate)
	deviceConfig.Alsa.NoMMap = 1
	deviceConfig.Wasapi.NoAutoConvertSRC = 1

	s := newShifter(4.0, 2048, 32, sampleRate, bitDepth, channels)

	window := gui(s)

	deviceCallbacks := malgo.DeviceCallbacks{
		Data: s.shift,
	}
	device, err := malgo.InitDevice(ctx.Context, deviceConfig, deviceCallbacks)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	defer device.Uninit()

	go func() {
		err := device.Start()
		if err != nil {
			os.Exit(1)
		}
	}()

	window.ShowAndRun()
}
