package chirp

import (
	"bytes"
	"encoding/binary"
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
		op.Format = oto.FormatSignedInt16LE

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
	numSamples := int(float64(SampleRate) * opts.Duration.Seconds())
	data := make([]int16, numSamples*ChannelCount)
	buf := &bytes.Buffer{}

	// Envelope parameters
	attack := 0.005  // 5ms
	decay := 0.01    // 10ms
	release := 0.015 // 15ms
	sustain := 0.7   // 70% of peak amplitude

	angularFreq := 2 * math.Pi * opts.Frequency

	for i := 0; i < numSamples; i++ {
		t := float64(i) / float64(SampleRate)
		progress := t / opts.Duration.Seconds()

		envelope := calculateEnvelope(progress, attack, decay, release, sustain)
		value := math.Sin(angularFreq*t) * envelope * opts.Volume

		// Scale to 16-bit range with headroom
		amplitude := int16(value * 26000)

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
	ctx, err := initOto()
	if err != nil {
		return err
	}

	player := ctx.NewPlayer(data)
	defer player.Close()
	player.Play()

	for player.IsPlaying() {
		time.Sleep(time.Millisecond)
	}
	return player.Err()
}

// PlayChirp generates and plays a chirp with the given options.
func PlayChirp(opts Options) error {
	return PlaySound(GenerateChirp(opts))
}
