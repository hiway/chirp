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
	SampleRate = 44100
	// ChannelCount represents mono audio
	ChannelCount = 1
	// BitDepthInBytes represents 16-bit audio
	BitDepthInBytes = 2
	// BufferSize is the size of the audio buffer
	BufferSize = 1024
)

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
		Duration:  15 * time.Millisecond,
		Volume:    1.0,
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
		// Use 16-bit signed format for better audio quality
		op.Format = oto.FormatSignedInt16LE
		// Keep small buffer size (5ms) for low latency
		op.BufferSize = 5 * time.Millisecond

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

	numSamples := int(float64(SampleRate) * opts.Duration.Seconds())
	data := make([]int16, numSamples*ChannelCount)
	buf := bufferPool.Get().(*bytes.Buffer)
	buf.Reset()

	// Envelope parameters
	attack := 0.005  // 5ms
	decay := 0.01    // 10ms
	release := 0.015 // 15ms
	sustain := 0.7   // 70% of peak amplitude

	// Pre-calculate common values
	angularFreq := 2 * math.Pi * opts.Frequency
	sampleRateFloat := float64(SampleRate)
	durationSeconds := opts.Duration.Seconds()
	// Scale to 16-bit signed range (-32768 to 32767)
	volumeScale := opts.Volume * 32767 // Maximum amplitude for 16-bit audio

	for i := 0; i < numSamples; i++ {
		t := float64(i) / sampleRateFloat
		progress := t / durationSeconds

		envelope := calculateEnvelope(progress, attack, decay, release, sustain)
		value := math.Sin(angularFreq*t) * envelope

		// Scale to 16-bit signed range (-32768 to 32767)
		amplitude := int16(value * volumeScale)

		for ch := 0; ch < ChannelCount; ch++ {
			data[i*ChannelCount+ch] = amplitude
		}
	}

	err := binary.Write(buf, binary.LittleEndian, data)
	if err != nil {
		log.Printf("Error writing chirp data: %v", err)
		return &bytes.Buffer{}
	}
	return buf
}

func calculateEnvelope(progress, attack, decay, release, sustain float64) float64 {
	if progress < attack {
		return progress / attack
	} else if progress < attack+decay {
		decayProgress := (progress - attack) / decay
		return 1.0 - (1.0-sustain)*decayProgress
	} else if progress > 1.0-release {
		releaseProgress := (progress - (1.0 - release)) / release
		return sustain * (1.0 - releaseProgress)
	}
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

	// Set initial volume and play
	player.SetVolume(1.0)
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
