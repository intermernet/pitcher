/****************************************************************************
*
* COPYRIGHT 2026 Mike Hughes <mike <AT> mikehughes <DOT> info
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

	"github.com/intermernet/gominiaudio"
	"github.com/intermernet/pitcher/algos"
)

var shift *int

func main() {

	guiOn := flag.Bool("gui", false, "Display GUI")
	shift = flag.Int("shift", 0, "Semitones to pitch-shift. Must be between -12 and +12")
	algoFlag := flag.String("algo", algos.Default().ShortName, "Pitch-shifting algorithm. Options: "+algos.NamesString())
	frameSize := flag.Int("framesize", 0, "FFT framesize. Must be a power of 2 (0 = use algorithm default)")
	overSampling := flag.Int("oversampling", 0, "Pitch shift oversampling. Must be a power of 2 (0 = use algorithm default)")
	sampleRate := flag.Int("samplerate", 48000, "Audio Sample Rate")
	periods := flag.Int("periods", 2, "Audio buffer periods (2 = double-buffered)")
	bufferSize := flag.Int("buffersize", 256, "Audio period size in frames (lower = less latency, may cause glitches)")
	exclusive := flag.Bool("exclusive", false, "Use WASAPI exclusive mode (locks audio device, lower latency)")
	list := flag.Bool("list", false, "List available input/output audio devices and exit")
	inputDevice := flag.Int("input", -1, "Input (capture) device number from --list (default: system default)")
	outputDevice := flag.Int("output", -1, "Output (playback) device number from --list (default: system default)")
	flag.Parse()

	// Resolve algorithm
	algo, ok := algos.Find(*algoFlag)
	if !ok {
		log.Fatalf("unknown algorithm %q — valid options: %v", *algoFlag, algos.Names())
	}

	// Apply algorithm defaults when flags were not explicitly set
	if *frameSize == 0 {
		*frameSize = algo.Defaults.FrameSize
	}
	if *overSampling == 0 {
		*overSampling = algo.Defaults.Oversampling
	}

	// Setup audio context first — needed for device enumeration and --list.
	ctx, err := gominiaudio.InitContext(nil, nil)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	defer func() {
		_ = ctx.Uninit()
	}()

	captureDevices, err := ctx.Devices(gominiaudio.DeviceTypeCapture)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	playbackDevices, err := ctx.Devices(gominiaudio.DeviceTypePlayback)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	if *list {
		printDeviceList(captureDevices, playbackDevices)
		os.Exit(0)
	}

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
	if *bufferSize < 0 {
		log.Fatal("\"buffersize\" must be non-negative")
	}

	channels := 2
	format := gominiaudio.FormatF32
	bitDepth := uint16(format.SizeInBytes() * 8)
	deviceConfig := gominiaudio.DeviceConfigInit(gominiaudio.DeviceTypeDuplex)
	deviceConfig.PerformanceProfile = gominiaudio.PerformanceProfileLowLatency
	deviceConfig.Capture.Format = format
	deviceConfig.Capture.Channels = uint32(channels)
	deviceConfig.Playback.Format = format
	deviceConfig.Playback.Channels = uint32(channels)
	deviceConfig.SampleRate = uint32(*sampleRate)

	if *exclusive {
		deviceConfig.Capture.ShareMode = gominiaudio.ShareModeExclusive
		deviceConfig.Playback.ShareMode = gominiaudio.ShareModeExclusive
	}

	if *bufferSize > 0 {
		deviceConfig.PeriodSizeInFrames = uint32(*bufferSize)
	}
	deviceConfig.Periods = uint32(*periods)

	// Allow variable-sized callbacks — avoids miniaudio's internal ring buffer
	// that adds an extra period of latency.
	deviceConfig.NoFixedSizedCallback = true

	// Platform-specific tuning
	deviceConfig.NoClip = true
	deviceConfig.NoPreSilencedOutputBuffer = false
	deviceConfig.WASAPI.NoAutoConvertSRC = true
	deviceConfig.WASAPI.NoAutoStreamRouting = false
	deviceConfig.WASAPI.NoDefaultQualitySRC = true
	deviceConfig.WASAPI.NoHardwareOffloading = false

	// Set device IDs from --input / --output (nil = system default).
	var captureDeviceID, playbackDeviceID *gominiaudio.DeviceID
	if *inputDevice >= 0 {
		if *inputDevice >= len(captureDevices) {
			log.Fatalf("input device %d not found (use --list to see available devices)", *inputDevice)
		}
		id := captureDevices[*inputDevice].ID
		captureDeviceID = &id
	}
	if *outputDevice >= 0 {
		if *outputDevice >= len(playbackDevices) {
			log.Fatalf("output device %d not found (use --list to see available devices)", *outputDevice)
		}
		id := playbackDevices[*outputDevice].ID
		playbackDeviceID = &id
	}
	deviceConfig.Capture.DeviceID = captureDeviceID
	deviceConfig.Playback.DeviceID = playbackDeviceID

	// Initial device indices for GUI dropdowns.
	initialInputIdx := findDefaultDeviceIdx(captureDevices)
	if *inputDevice >= 0 {
		initialInputIdx = *inputDevice
	}
	initialOutputIdx := findDefaultDeviceIdx(playbackDevices)
	if *outputDevice >= 0 {
		initialOutputIdx = *outputDevice
	}

	s := newShifter(*frameSize, *overSampling, float64(*sampleRate), bitDepth, channels, *periods, *bufferSize, *exclusive, algo)

	defer s.Destroy()

	// Pitch shift callback
	deviceCallbacks := gominiaudio.DeviceCallbacks{
		Data: func(_ *gominiaudio.Device, output, input []byte, frames uint32) {
			s.process(output, input, frames)
		},
	}

	// Init audio
	var device *gominiaudio.Device
	device, err = gominiaudio.InitDevice(ctx, deviceConfig, deviceCallbacks)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	defer func() { device.Uninit() }()

	// Start audio processing
	go func() {
		if startErr := device.Start(); startErr != nil {
			os.Exit(1)
		}
	}()

	// restartAudio stops the current device and opens a new one with the
	// provided capture/playback device IDs (nil = system default).
	restartAudio := func(captureID, playbackID *gominiaudio.DeviceID) {
		device.Uninit()
		if captureID != nil {
			c := *captureID
			deviceConfig.Capture.DeviceID = &c
		} else {
			deviceConfig.Capture.DeviceID = nil
		}
		if playbackID != nil {
			p := *playbackID
			deviceConfig.Playback.DeviceID = &p
		} else {
			deviceConfig.Playback.DeviceID = nil
		}
		d, initErr := gominiaudio.InitDevice(ctx, deviceConfig, deviceCallbacks)
		if initErr != nil {
			fmt.Println("device switch failed:", initErr)
			return
		}
		device = d
		if startErr := d.Start(); startErr != nil {
			fmt.Println("device start failed:", startErr)
		}
	}

	// Init GUI
	if *guiOn {
		window = gui(s, captureDevices, playbackDevices, initialInputIdx, initialOutputIdx, restartAudio)
	}

	// Start GUI or wait for interrupt
	switch *guiOn {
	case true:
		window.ShowAndRun()
	default:
		exclStr := "No"
		if *exclusive {
			exclStr = "Yes"
		}
		latencyMs := float64(s.Latency) / s.SampleRate * 1000.0
		fmt.Printf("\nPitcher — running parameters:\n")
		fmt.Printf("  Algorithm:    %s (%s)\n", algo.FullName, algo.ShortName)
		fmt.Printf("  Shift:        %+d semitones\n", *shift)
		fmt.Printf("  Frame size:   %d\n", *frameSize)
		fmt.Printf("  Oversampling: %d\n", *overSampling)
		fmt.Printf("  Sample rate:  %d Hz\n", *sampleRate)
		fmt.Printf("  Periods:      %d\n", *periods)
		fmt.Printf("  Buffer size:  %d frames\n", *bufferSize)
		fmt.Printf("  Exclusive:    %s\n", exclStr)
		fmt.Printf("  Latency:      %.1f ms\n\n", latencyMs)
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt)
		fmt.Println("Press Ctrl-C / Cmd-. to exit")
		<-c
		fmt.Println("Exiting...")
		os.Exit(0)
	}
}

func printDeviceList(inputs, outputs []gominiaudio.DeviceInfo) {
	fmt.Println("Input devices (use with --input N):")
	for i, d := range inputs {
		def := ""
		if d.IsDefault {
			def = " [default]"
		}
		fmt.Printf("  %d: %s%s\n", i, d.Name, def)
	}
	fmt.Println("\nOutput devices (use with --output N):")
	for i, d := range outputs {
		def := ""
		if d.IsDefault {
			def = " [default]"
		}
		fmt.Printf("  %d: %s%s\n", i, d.Name, def)
	}
}

func findDefaultDeviceIdx(devices []gominiaudio.DeviceInfo) int {
	for i, d := range devices {
		if d.IsDefault {
			return i
		}
	}
	return 0
}
