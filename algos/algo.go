/****************************************************************************
*
* COPYRIGHT 2025 Mike Hughes <mike <AT> mikehughes <DOT> info
*
****************************************************************************/

package algos

import "fmt"

// Defaults holds sane default parameters for an algorithm.
type Defaults struct {
	FrameSize    int
	Oversampling int
}

// Algorithm describes a pitch-shifting algorithm.
type Algorithm struct {
	// FullName is the human-readable name shown in the GUI drop-down.
	FullName string
	// ShortName is the short identifier used with the --algo flag.
	ShortName string
	// Defaults holds the recommended operating parameters for this algorithm.
	Defaults Defaults
	// Process is the per-algorithm audio processing function.
	Process func(ctx *Context, output, input []byte)
	// NewState optionally allocates algorithm-specific state stored in Context.AlgoState.
	// May be nil for stateless algorithms.
	NewState func(ctx *Context) interface{}
}

// Algorithms is the ordered list of available algorithms. The first is the default.
var Algorithms = []Algorithm{
	{
		FullName:  "Phase Vocoder",
		ShortName: "phasvoc",
		Defaults:  Defaults{FrameSize: 512, Oversampling: 4},
		Process:   ProcessPhaseVocoder,
	},
	{
		FullName:  "Pitch-Synchronous Overlap-Add (PSOLA)",
		ShortName: "psola",
		Defaults:  Defaults{FrameSize: 256, Oversampling: 2},
		Process:   ProcessPSOLA,
	},
	{
		FullName:  "Sines/Transients/Noise (STN)",
		ShortName: "stn",
		Defaults:  Defaults{FrameSize: 2048, Oversampling: 4},
		Process:   ProcessSTN,
		NewState:  NewSTNState,
	},
	{
		FullName:  "Low Latency STFT",
		ShortName: "llstft",
		Defaults:  Defaults{FrameSize: 512, Oversampling: 4},
		Process:   ProcessLLSTFT,
		NewState:  NewLLSTFTState,
	},
	{
		FullName:  "Waveform Similarity Overlap-Add (WSOLA)",
		ShortName: "wsola",
		Defaults:  Defaults{FrameSize: 512, Oversampling: 2},
		Process:   ProcessWSOLA,
		NewState:  NewWSOLAState,
	},
	{
		FullName:  "Based on Signalsmith Stretch",
		ShortName: "sss",
		Defaults:  Defaults{FrameSize: 2048, Oversampling: 4},
		Process:   ProcessSSS,
		NewState:  NewSSSState,
	},
}

// Find returns the Algorithm matching shortName and whether it was found.
func Find(shortName string) (Algorithm, bool) {
	for _, a := range Algorithms {
		if a.ShortName == shortName {
			return a, true
		}
	}
	return Algorithm{}, false
}

// Default returns the first (default) algorithm.
func Default() Algorithm {
	return Algorithms[0]
}

// Names returns all short names, for use in flag help text.
func Names() []string {
	names := make([]string, len(Algorithms))
	for i, a := range Algorithms {
		names[i] = a.ShortName
	}
	return names
}

// FullNames returns all full names, for use in GUI selectors.
func FullNames() []string {
	names := make([]string, len(Algorithms))
	for i, a := range Algorithms {
		names[i] = a.FullName
	}
	return names
}

// NamesString returns a formatted string of all short names for help text.
func NamesString() string {
	return fmt.Sprintf("%v", Names())
}
