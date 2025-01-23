/****************************************************************************
*
* COPYRIGHT 2025 Mike Hughes <mike <AT> mikehughes <DOT> info
*
****************************************************************************/

package main

import (
	"flag"
	"fmt"
	"log"
	"math"
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
	if *frameSize == 0 || math.Ceil(math.Log2(float64(*frameSize))) != math.Floor(math.Log2(float64(*frameSize))) {
		log.Fatal("\"framesize\" must be a power of 2")
	}
	if *overSampling == 0 || math.Ceil(math.Log2(float64(*overSampling))) != math.Floor(math.Log2(float64(*overSampling))) {
		log.Fatal("\"oversampling\" must be a power of 2")
	}
	if *sampleRate <= 0 {
		log.Fatal("\"samplerate\" must be a positive integer")
	}
	if *periods <= 0 {
		log.Fatal("\"periods\" must be a positive integer")
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

	channels := 2
	format := malgo.FormatF32
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
	deviceConfig.NoClip = 1
	deviceConfig.NoPreSilencedOutputBuffer = 0
	deviceConfig.Wasapi.NoAutoConvertSRC = 1
	deviceConfig.Wasapi.NoAutoStreamRouting = 0
	deviceConfig.Wasapi.NoDefaultQualitySRC = 0
	deviceConfig.Wasapi.NoHardwareOffloading = 0

	s := newShifter(*frameSize, *overSampling, float64(*sampleRate), bitDepth, channels)

	defer s.forward.Destroy()
	defer s.inverse.Destroy()

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
