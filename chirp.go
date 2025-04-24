package chirp

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
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

	// Debug enables logging for audio debugging
	Debug = false

	// DefaultEchoTimeout is the time within which echoed characters are ignored
	DefaultEchoTimeout = 1 * time.Millisecond
)

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
	minSoundGap     = 25 * time.Millisecond // Minimum time between sounds

	// Global input buffer for echo tracking
	inputTracker = &inputBuffer{
		timeout: DefaultEchoTimeout,
	}
)

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

// ChirpType represents different types of chirps
type ChirpType int

const (
	// InputChirp is played when user types
	InputChirp ChirpType = iota
	// OutputChirp is played when terminal outputs
	OutputChirp
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

// DefaultOptions returns the default chirp options
func DefaultOptions() Options {
	return Options{
		Frequency: 1000,
		Duration:  50 * time.Millisecond,
		Volume:    1.0,
	}
}

// GetChirpOptions returns pleasing options for different chirp types
func GetChirpOptions(chirpType ChirpType) Options {
	switch chirpType {
	case InputChirp:
		return Options{
			Frequency: NoteD5,                // Perfect fifth above output for local feedback
			Duration:  25 * time.Millisecond, // Short enough to avoid masking subsequent sounds
			Volume:    0.35,
		}
	case OutputChirp:
		return Options{
			Frequency: NoteG4,                // Lower note for output creates grounding effect
			Duration:  35 * time.Millisecond, // Slightly longer for better distinction
			Volume:    0.25,
		}
	default:
		return DefaultOptions()
	}
}

// Initialize sets up the audio context. It should be called once at startup.
func Initialize() error {
	_, err := initOto()
	return err
}

// initOto initializes the oto context singleton using oto/v3.
func initOto() (*oto.Context, error) {
	once.Do(func() {
		op := &oto.NewContextOptions{}
		op.SampleRate = SampleRate
		op.ChannelCount = ChannelCount
		op.Format = oto.FormatSignedInt16LE
		// Set buffer size to 10ms for balance between latency and stability
		op.BufferSize = 10 * time.Millisecond

		var readyChan chan struct{}
		otoCtx, readyChan, ctxErr = oto.NewContext(op)
		if ctxErr == nil {
			<-readyChan
		}
	})
	return otoCtx, ctxErr
}

// GenerateChirp creates a sine wave tone with an envelope based on the provided options.
func GenerateChirp(opts Options) io.Reader {
	if opts.Volume <= 0 {
		return bytes.NewReader([]byte{})
	}

	durationSeconds := opts.Duration.Seconds()
	numSamples := int(float64(SampleRate) * durationSeconds)
	data := make([]int16, numSamples*ChannelCount)
	buf := bufferPool.Get().(*bytes.Buffer)
	buf.Reset()

	// Fixed envelope parameters in seconds
	attack := 0.03  // 30ms attack
	decay := 0.05   // 50ms decay
	release := 0.1  // 100ms release
	sustain := 0.85 // 85% of peak amplitude

	// Pre-calculate common values
	sampleRateFloat := float64(SampleRate)
	baseFreq := 2 * math.Pi * opts.Frequency
	// Slight frequency modulation parameters
	modFreq := 2 * math.Pi * 8         // 8 Hz modulation
	modDepth := 0.001                  // Very subtle depth
	volumeScale := opts.Volume * 28000 // Slightly reduced for gentler sound

	for i := 0; i < numSamples; i++ {
		t := float64(i) / sampleRateFloat
		progress := t / durationSeconds

		// Calculate frequency modulation
		freqMod := 1.0 + modDepth*math.Sin(modFreq*t)
		instantFreq := baseFreq * freqMod

		envelope := calculateEnvelope(progress, attack, decay, release, sustain)
		value := math.Sin(instantFreq*t) * envelope

		// Scale to 16-bit signed range (-32768 to 32767)
		amplitude := int16(value * volumeScale)

		// Create stereo spread by slightly adjusting volumes
		// This makes the sound feel more spacious and natural
		leftVol := 0.95  // Left channel slightly quieter
		rightVol := 1.05 // Right channel slightly louder
		data[i*ChannelCount] = int16(float64(amplitude) * leftVol)
		data[i*ChannelCount+1] = int16(float64(amplitude) * rightVol)
	}

	err := binary.Write(buf, binary.LittleEndian, data)
	if err != nil {
		log.Printf("Error writing chirp data: %v", err)
		return &bytes.Buffer{}
	}
	return buf
}

func calculateEnvelope(progress, attack, decay, release, sustain float64) float64 {
	if progress >= 1.0 {
		return 0.0
	}

	// Attack phase
	if progress < attack {
		return progress / attack
	}

	// Decay phase
	if progress < attack+decay {
		decayProgress := (progress - attack) / decay
		return 1.0 - (1.0-sustain)*decayProgress
	}

	// Release phase - only enter in the final portion of the sound
	if progress > 1.0-release {
		releaseProgress := (progress - (1.0 - release)) / release
		return sustain * (1.0 - releaseProgress)
	}

	// Sustain phase - maintain the sustain level for the majority of the sound
	return sustain
}

// PlaySound plays the given audio data using the oto context.
func PlaySound(data io.Reader) error {
	if ctxErr != nil {
		return ctxErr
	}

	// Read all data into a buffer
	audioData, err := io.ReadAll(data)
	if err != nil {
		return err
	}
	if len(audioData) == 0 {
		return nil
	}

	ctx, err := initOto()
	if err != nil {
		return err
	}

	// Create a new player with the audio data
	player := ctx.NewPlayer(bytes.NewReader(audioData))
	defer player.Close()

	// Play the sound
	player.Play()

	// Wait for playback to complete
	for player.IsPlaying() {
		time.Sleep(time.Millisecond)
	}

	if buf, ok := data.(*bytes.Buffer); ok {
		bufferPool.Put(buf)
	}
	return player.Err()
}

// PlayChirp generates and plays a chirp with the given options.
func PlayChirp(opts Options) error {
	// Skip if we're still playing or in debounce period
	if IsSoundPlaying() {
		return nil
	}

	markSoundStart()

	// Check cache first
	if cached := getCachedChirp(opts); cached != nil {
		return PlaySound(cached)
	}

	// Generate new chirp
	chirpData, err := io.ReadAll(GenerateChirp(opts))
	if err != nil {
		return err
	}

	// Cache for future use
	cacheChirp(opts, chirpData)

	return PlaySound(bytes.NewReader(chirpData))
}

// getCacheKey generates a unique key for chirp options
func getCacheKey(opts Options) string {
	return fmt.Sprintf("%.0f-%.0f-%.2f", opts.Frequency, opts.Duration.Seconds()*1000, opts.Volume)
}

// getCachedChirp retrieves a cached chirp if available
func getCachedChirp(opts Options) io.Reader {
	key := getCacheKey(opts)
	if cached, ok := chirpCache.Load(key); ok {
		reader := cached.(*bytes.Reader)
		reader.Seek(0, io.SeekStart) // Reset to start
		return reader
	}
	return nil
}

// cacheChirp stores a chirp in the cache
func cacheChirp(opts Options, data []byte) {
	key := getCacheKey(opts)
	chirpCache.Store(key, bytes.NewReader(data))
}

func debugf(format string, args ...interface{}) {
	if Debug {
		log.Printf(format, args...)
	}
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
		debugf("Tracking input char: %c", c)
	}
}

// IsRecentInput checks if a character was recently input
func IsRecentInput(c byte) bool {
	inputTracker.mu.Lock()
	defer inputTracker.mu.Unlock()

	now := time.Now()
	// Clean up old entries while checking
	var validChars []inputChar
	isRecent := false

	for _, ic := range inputTracker.chars {
		age := now.Sub(ic.timestamp)
		if age < inputTracker.timeout {
			validChars = append(validChars, ic)
			if ic.char == c {
				isRecent = true
				if Debug {
					debugf("Found recent input match for char: %c (age: %v)", c, age)
				}
			}
		}
	}

	inputTracker.chars = validChars
	return isRecent
}
