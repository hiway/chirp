package player

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"sync"
	"time"

	"github.com/ebitengine/oto/v3"
	"github.com/rs/zerolog"

	"github.com/hiway/chirp/pkg/sample"
)

const (
	// SampleRate is the number of samples per second
	SampleRate = 48000
	// ChannelCount represents stereo audio
	ChannelCount = 2
	// BitDepthInBytes represents 16-bit audio
	BitDepthInBytes = 2
	// BufferSizeSamples represents number of samples for the audio buffer
	BufferSizeSamples = 480 // 10ms at 48kHz

	// DefaultMinSoundGap is the minimum time between playing sounds
	DefaultMinSoundGap = 25 * time.Millisecond
)

// Player is the interface for playing audio samples.
type Player interface {
	Play(sample *sample.SampleConfig) error
	Close() error
}

var (
	otoCtx *oto.Context
	once   sync.Once
	ctxErr error
)

// initOtoContext initializes the oto context singleton.
func initOtoContext() (*oto.Context, error) {
	once.Do(func() {
		op := &oto.NewContextOptions{}
		op.SampleRate = SampleRate
		op.ChannelCount = ChannelCount
		op.Format = oto.FormatSignedInt16LE
		// BufferSize is calculated by Oto based on SampleRate, ChannelCount, and Format.
		// We don't need to set op.BufferSize explicitly unless overriding.

		var readyChan chan struct{}
		otoCtx, readyChan, ctxErr = oto.NewContext(op)
		if ctxErr == nil {
			<-readyChan // Wait for the context to be ready
		}
	})
	return otoCtx, ctxErr
}

// OtoPlayer uses the ebitengine/oto/v3 library to play sounds.
type OtoPlayer struct {
	log           zerolog.Logger
	ctx           *oto.Context
	minSoundGap   time.Duration
	lastSoundTime time.Time
	mu            sync.Mutex // Protects lastSoundTime
}

// NewOtoPlayer creates a new player using the Oto library.
func NewOtoPlayer(log zerolog.Logger) (*OtoPlayer, error) {
	ctx, err := initOtoContext()
	if err != nil {
		log.Error().Err(err).Msg("Failed to initialize Oto audio context")
		return nil, fmt.Errorf("failed to initialize audio context: %w", err)
	}
	log.Debug().Msg("Oto audio context initialized successfully")

	return &OtoPlayer{
		log:         log.With().Str("player_type", "oto").Logger(),
		ctx:         ctx,
		minSoundGap: DefaultMinSoundGap,
	}, nil
}

// SetMinSoundGap sets the minimum duration between sounds.
func (p *OtoPlayer) SetMinSoundGap(gap time.Duration) {
	p.mu.Lock()
	p.minSoundGap = gap
	p.mu.Unlock()
	p.log.Debug().Dur("min_gap_ms", gap).Msg("Set minimum sound gap")
}

// isSoundPlaying checks if we're within the minimum gap between sounds.
func (p *OtoPlayer) isSoundPlaying() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return time.Since(p.lastSoundTime) < p.minSoundGap
}

// markSoundStart updates the last sound time.
func (p *OtoPlayer) markSoundStart() {
	p.mu.Lock()
	p.lastSoundTime = time.Now()
	p.mu.Unlock()
}

// Play generates and plays the audio for the given sample.
func (p *OtoPlayer) Play(sample *sample.SampleConfig) error {
	if p.isSoundPlaying() {
		p.log.Trace().Str("sample_name", sample.Name).Msg("Skipping sound due to minimum gap")
		return nil // Not an error, just respecting the gap
	}
	p.markSoundStart()

	p.log.Debug().
		Str("sample_name", sample.Name).
		Int("duration_ms", sample.Duration).
		Int("frequency_hz", sample.Frequency).
		Float64("volume", sample.Volume).
		Msg("Generating and playing sample")

	// Generate audio data
	data, err := p.generateChirp(sample)
	if err != nil {
		p.log.Error().Err(err).Str("sample_name", sample.Name).Msg("Failed to generate chirp data")
		return fmt.Errorf("failed to generate chirp for sample '%s': %w", sample.Name, err)
	}
	if data == nil {
		p.log.Debug().Str("sample_name", sample.Name).Msg("Skipping playback for zero-volume or zero-duration sample")
		return nil // Nothing to play
	}

	// Play the generated data
	if err := p.playSound(bytes.NewReader(data)); err != nil {
		p.log.Error().Err(err).Str("sample_name", sample.Name).Msg("Failed to play sound")
		return fmt.Errorf("failed to play sound for sample '%s': %w", sample.Name, err)
	}

	p.log.Trace().Str("sample_name", sample.Name).Msg("Finished playing sample")
	return nil
}

// generateChirp creates a sine wave with ADSR envelope based on SampleConfig.
func (p *OtoPlayer) generateChirp(sample *sample.SampleConfig) ([]byte, error) {
	if sample.Volume <= 0 || sample.Duration <= 0 {
		return nil, nil // Nothing to generate
	}

	duration := time.Duration(sample.Duration) * time.Millisecond
	sampleRate := float64(SampleRate) // Using the constant instead of getting from context
	numSamples := int(duration.Seconds() * sampleRate)
	data := make([]int16, numSamples*ChannelCount)

	// ADSR parameters (as fraction of total duration)
	// TODO: Make ADSR configurable per sample?
	attack := 0.1  // 10% attack
	decay := 0.2   // 20% decay
	sustain := 0.7 // 70% of peak amplitude
	release := 0.3 // 30% release

	// Pre-calculate frequency and amplitude
	omega := 2.0 * math.Pi * float64(sample.Frequency)
	amplitude := sample.Volume * 32767.0 // Scale to 16-bit range

	for i := 0; i < numSamples; i++ {
		t := float64(i) / sampleRate
		phase := omega * t

		// Calculate envelope
		progress := float64(i) / float64(numSamples)
		envelope := calculateEnvelope(progress, attack, decay, sustain, release)

		// Generate sample value
		sampleValue := amplitude * envelope * math.Sin(phase)
		value := int16(sampleValue)

		// Stereo output (write same value to both channels)
		data[i*ChannelCount] = value   // Left channel
		data[i*ChannelCount+1] = value // Right channel
	}

	// Convert to bytes
	buf := new(bytes.Buffer)
	if err := binary.Write(buf, binary.LittleEndian, data); err != nil {
		return nil, fmt.Errorf("failed to write audio data to buffer: %w", err)
	}
	return buf.Bytes(), nil
}

// calculateEnvelope applies ADSR envelope to the sound.
func calculateEnvelope(progress, attack, decay, sustain, release float64) float64 {
	if progress < attack {
		// Attack phase
		return progress / attack
	}
	if progress < attack+decay {
		// Decay phase
		decayProgress := (progress - attack) / decay
		return 1.0 - (1.0-sustain)*decayProgress
	}
	if progress > 1.0-release {
		// Release phase
		releaseProgress := (progress - (1.0 - release)) / release
		// Ensure sustain is not negative if release is long
		if sustain < 0 {
			sustain = 0
		}
		return sustain * (1.0 - releaseProgress)
	}
	// Sustain phase
	return sustain
}

// playSound plays the raw audio data from an io.Reader.
func (p *OtoPlayer) playSound(reader io.Reader) error {
	player := p.ctx.NewPlayer(reader)
	defer player.Close() // Ensure player resources are released

	player.Play()

	// Wait for playback to complete. This is blocking.
	// For concurrent playback, this needs to run in a separate goroutine.
	// However, our queue model processes sounds sequentially per queue.
	for player.IsPlaying() {
		time.Sleep(time.Millisecond) // Prevent busy-waiting
	}

	if err := player.Err(); err != nil {
		return fmt.Errorf("oto player error: %w", err)
	}
	return nil
}

// Close cleans up the OtoPlayer resources.
func (p *OtoPlayer) Close() error {
	p.log.Debug().Msg("Closing OtoPlayer")
	// The Oto context is typically global and shared, so we don't close it here.
	// If specific player resources needed cleanup, it would happen here.
	return nil
}

// --- StubPlayer (Kept for testing) ---

// StubPlayer is a simple player implementation that logs playback and simulates duration.
type StubPlayer struct {
	log zerolog.Logger
}

// NewStubPlayer creates a new StubPlayer.
func NewStubPlayer(log zerolog.Logger) *StubPlayer {
	return &StubPlayer{log: log.With().Str("player_type", "stub").Logger()}
}

// Play simulates playing a sample by logging and sleeping.
func (p *StubPlayer) Play(sample *sample.SampleConfig) error {
	p.log.Debug().
		Str("sample_name", sample.Name).
		Int("duration_ms", sample.Duration).
		Int("frequency_hz", sample.Frequency).
		Float64("volume", sample.Volume).
		Msg("Simulating playing sample")

	// Simulate playback duration
	time.Sleep(time.Duration(sample.Duration) * time.Millisecond)

	p.log.Trace().Str("sample_name", sample.Name).Msg("Finished simulating sample")
	return nil
}

// Close cleans up the StubPlayer resources.
func (p *StubPlayer) Close() error {
	p.log.Debug().Msg("Closing StubPlayer")
	return nil
}
