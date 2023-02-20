/****************************************************************************
*
* COPYRIGHT 2023 Mike Hughes <mike <AT> mikehughes <DOT> info
*
****************************************************************************/

package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"

	"net/http"
	_ "net/http/pprof"

	"github.com/gen2brain/malgo"
)

var shift *int

func main() {

	guiOn := flag.Bool("gui", false, "Display GUI")
	shift = flag.Int("shift", 0, "Semitones to pitch-shift. Must be between -12 and +12")
	frameSize := flag.Int("framesize", 2048, "FFT framesize. Must be a power of 2")
	overSampling := flag.Int("oversampling", 32, "Pith shift oversampling. Must be a power of 2")
	sampleRate := flag.Int("samplerate", 44100, "Audio Sample Rate")
	periods := flag.Int("periods", 3, "Sampling periods. A period is ~ Audio Sample Rate / 100")
	flag.Parse()

	// Flag sanity checks
	if *shift < -12 || *shift > 12 {
		log.Fatal("\"shift\" flag must be between -12 and 12 inclusive")
	}

	// pprof server
	go func() {
		log.Println(http.ListenAndServe("localhost:9999", nil))
	}()

	// Setup audio stuff
	//ctx, err := malgo.InitContext([]malgo.Backend{malgo.BackendDsound}, malgo.ContextConfig{}, func(message string) {
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

	//sampleRate := 44100.0 // TODO(mike): This is a horrible hack!
	//periods := 4
	channels := 2
	format := malgo.FormatS16
	bitDepth := uint16(malgo.SampleSizeInBytes(format) * 8)
	deviceConfig := malgo.DefaultDeviceConfig(malgo.Duplex)
	deviceConfig.PerformanceProfile = malgo.LowLatency
	deviceConfig.Capture.Format = format
	deviceConfig.Capture.Channels = uint32(channels)
	deviceConfig.Playback.Format = format
	deviceConfig.Playback.Channels = uint32(channels)
	deviceConfig.SampleRate = uint32(*sampleRate)

	deviceConfig.Periods = uint32(*periods)

	// Added because it seems like the common practice. Doesn't seem to make any difference on any platform.
	deviceConfig.Alsa.NoMMap = 1
	deviceConfig.Wasapi.NoAutoConvertSRC = 1

	// Seems like the sweetspot for quality, but could be due to bugs elsewhere
	s := newShifter(*frameSize, *overSampling, float64(*sampleRate), bitDepth, channels)

	// Init GUI
	if *guiOn {
		window = gui(s)
	}

	//start pitch shifter in goroutine
	go s.shift()

	// Pitch shift callback
	deviceCallbacks := malgo.DeviceCallbacks{
		Data: s.process,
	}

	// Init audio
	device, err := malgo.InitDevice(ctx.Context, deviceConfig, deviceCallbacks)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	defer device.Uninit()

	// Start audio processing in goroutine
	go func() {
		err := device.Start()
		if err != nil {
			os.Exit(1)
		}
	}()

	// Start GUI
	switch *guiOn {
	case true:
		window.ShowAndRun()
		s.quit <- true
	default:
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt)
		fmt.Println("Press Ctrl-C / Cmd-. to exit")
		<-c
		s.quit <- true
		fmt.Println("Exiting...")
		os.Exit(0)
	}
}
