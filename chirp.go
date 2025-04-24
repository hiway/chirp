package chirp

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"log"
	"math"
	"sync"
	"time"

	"github.com/ebitengine/oto/v3"
)

const (
	// SampleRate is the number of samples per second
	SampleRate = 48000
	// ChannelCount represents stereo audio (better quality than mono)
	ChannelCount = 2
	// BitDepthInBytes represents 16-bit audio
	BitDepthInBytes = 2
	// BufferSize represents number of samples, smaller means lower latency
	BufferSizeSamples = 480 // 10ms at 48kHz

	// Musical note frequencies (A4 = 440Hz standard tuning)
	// Using perfect fifth (3:2 ratio) for local feedback
	NoteG4 = 392.00 // Local output note
	NoteD5 = 587.33 // Local input note (perfect fifth above G4)

	// Using major third (5:4 ratio) for network feedback
	NoteE5 = 659.25 // Network input note
	NoteC5 = 523.25 // Network output note (major third below E5)

	// DefaultEchoTimeout is the time within which echoed characters are ignored
	DefaultEchoTimeout = 1 * time.Millisecond
)

// Debug and configuration variables
var (
	Debug       = false
	echoTimeout = DefaultEchoTimeout
	minSoundGap = 25 * time.Millisecond
)

// SetDebug enables or disables debug logging
func SetDebug(enabled bool) {
	Debug = enabled
}

// SetEchoTimeout sets the duration within which echoed characters are ignored
func SetEchoTimeout(timeout time.Duration) {
	echoTimeout = timeout
	if inputTracker != nil {
		inputTracker.timeout = timeout
	}
}

// SetMinSoundGap sets the minimum duration between sounds
func SetMinSoundGap(gap time.Duration) {
	minSoundGap = gap
}

// Debugf prints debug messages when Debug is true
func Debugf(format string, args ...interface{}) {
	if Debug {
		log.Printf(format, args...)
	}
}

// inputBuffer tracks recent input characters for echo detection
type inputBuffer struct {
	mu      sync.Mutex
	chars   []inputChar
	timeout time.Duration
}

type inputChar struct {
	char      byte
	timestamp time.Time
}

var (
	otoCtx *oto.Context
	once   sync.Once
	ctxErr error

	// Buffer pool for audio data
	bufferPool = sync.Pool{
		New: func() interface{} {
			return new(bytes.Buffer)
		},
	}

	// Cache for commonly used chirp patterns
	chirpCache sync.Map // map[string]*bytes.Reader

	// Sound state management
	lastSoundTime   time.Time
	soundStateMutex sync.Mutex

	// Global input buffer for echo tracking
	inputTracker = &inputBuffer{
		timeout: echoTimeout,
	}
)

// Options contains parameters for generating a chirp sound
type Options struct {
	// Frequency in Hz
	Frequency float64
	// Duration of the chirp
	Duration time.Duration
	// Volume level (0.0 to 1.0)
	Volume float64
}

// Initialize sets up the audio context
func Initialize() error {
	_, err := initOto()
	return err
}

// initOto initializes the oto context singleton
func initOto() (*oto.Context, error) {
	once.Do(func() {
		op := &oto.NewContextOptions{}
		op.SampleRate = SampleRate
		op.ChannelCount = ChannelCount
		op.Format = oto.FormatSignedInt16LE
		op.BufferSize = BufferSizeSamples

		var readyChan chan struct{}
		otoCtx, readyChan, ctxErr = oto.NewContext(op)
		if ctxErr == nil {
			<-readyChan
		}
	})
	return otoCtx, ctxErr
}

// IsSoundPlaying checks if we're within the minimum gap between sounds
func IsSoundPlaying() bool {
	soundStateMutex.Lock()
	defer soundStateMutex.Unlock()
	return time.Since(lastSoundTime) < minSoundGap
}

// markSoundStart updates the last sound time
func markSoundStart() {
	soundStateMutex.Lock()
	lastSoundTime = time.Now()
	soundStateMutex.Unlock()
}

// PlayChirp generates and plays a chirp with the given options
func PlayChirp(opts Options) error {
	if IsSoundPlaying() {
		return nil
	}
	markSoundStart()

	// Generate chirp data
	data := generateChirp(opts)
	if data == nil {
		return fmt.Errorf("failed to generate chirp data")
	}

	return playSound(data)
}

// generateChirp creates a sine wave with ADSR envelope
func generateChirp(opts Options) []byte {
	if opts.Volume <= 0 {
		return nil
	}

	sampleRate := float64(SampleRate)
	numSamples := int(opts.Duration.Seconds() * sampleRate)
	data := make([]int16, numSamples*ChannelCount)

	// ADSR parameters (as fraction of total duration)
	attack := 0.1  // 10% attack
	decay := 0.2   // 20% decay
	sustain := 0.7 // 70% of peak amplitude
	release := 0.3 // 30% release

	// Pre-calculate frequency values
	omega := 2.0 * math.Pi * opts.Frequency
	amplitude := opts.Volume * 32767.0 // Scale to 16-bit range

	for i := 0; i < numSamples; i++ {
		t := float64(i) / sampleRate
		phase := omega * t

		// Calculate envelope
		progress := float64(i) / float64(numSamples)
		envelope := calculateEnvelope(progress, attack, decay, sustain, release)

		// Generate sample
		sample := amplitude * envelope * math.Sin(phase)
		value := int16(sample)

		// Stereo output
		data[i*2] = value   // Left channel
		data[i*2+1] = value // Right channel
	}

	// Convert to bytes
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.LittleEndian, data)
	return buf.Bytes()
}

// calculateEnvelope applies ADSR envelope to the sound
func calculateEnvelope(progress, attack, decay, sustain, release float64) float64 {
	if progress < attack {
		return progress / attack
	}
	if progress < attack+decay {
		decayProgress := (progress - attack) / decay
		return 1.0 - (1.0-sustain)*decayProgress
	}
	if progress > 1.0-release {
		releaseProgress := (progress - (1.0 - release)) / release
		return sustain * (1.0 - releaseProgress)
	}
	return sustain
}

// playSound plays the raw audio data
func playSound(data []byte) error {
	if len(data) == 0 {
		return nil
	}

	ctx, err := initOto()
	if err != nil {
		return err
	}

	player := ctx.NewPlayer(bytes.NewReader(data))
	defer player.Close()

	player.Play()

	// Wait for playback to complete
	for player.IsPlaying() {
		time.Sleep(time.Millisecond)
	}

	return player.Err()
}

// TrackInput records a character that was just input by the user
func TrackInput(c byte) {
	inputTracker.mu.Lock()
	defer inputTracker.mu.Unlock()

	now := time.Now()
	// Remove old entries
	var validChars []inputChar
	for _, ic := range inputTracker.chars {
		if now.Sub(ic.timestamp) < inputTracker.timeout {
			validChars = append(validChars, ic)
		}
	}

	// Add new character
	validChars = append(validChars, inputChar{
		char:      c,
		timestamp: now,
	})

	inputTracker.chars = validChars
	if Debug {
		Debugf("Tracking input char: %c", c)
	}
}

// IsRecentInput checks if a character was recently input
func IsRecentInput(c byte) bool {
	inputTracker.mu.Lock()
	defer inputTracker.mu.Unlock()

	now := time.Now()
	isRecent := false

	// Clean up old entries while checking
	var validChars []inputChar
	for _, ic := range inputTracker.chars {
		if now.Sub(ic.timestamp) < inputTracker.timeout {
			validChars = append(validChars, ic)
			if ic.char == c {
				isRecent = true
			}
		}
	}

	inputTracker.chars = validChars
	if isRecent && Debug {
		Debugf("Found recent input match for char: %c", c)
	}

	return isRecent
}
