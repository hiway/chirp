package chirp

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/rs/zerolog"

	"github.com/hiway/chirp/pkg/config"
	"github.com/hiway/chirp/pkg/player"
	"github.com/hiway/chirp/pkg/queue"
	"github.com/hiway/chirp/pkg/sample"
	"github.com/hiway/chirp/pkg/terminal"
)

// Chirp manages the terminal session with audio feedback.
type Chirp struct {
	cfg      *config.Config
	term     *terminal.Terminal
	player   player.Player
	queues   map[string]*queue.Queue
	log      zerolog.Logger
	stopOnce sync.Once
	stopChan chan struct{}
}

// DefaultConfig returns a basic configuration for testing.
func DefaultConfig() *config.Config {
	cfg := &config.Config{
		Samples: map[string]*sample.SampleConfig{
			"local": {
				Duration:  50,
				Frequency: 392, // G4
				Volume:    0.3,
			},
			"remote": {
				Duration:  50,
				Frequency: 587, // D5
				Volume:    0.3,
			},
		},
		Queues: map[string]*config.Queue{
			"local": {
				Match:      []string{"\r", "\n"},
				SampleName: "local",
				MaxLength:  1,
			},
			"remote": {
				Match:      []string{"$", "#", "%"},
				SampleName: "remote",
				MaxLength:  1,
			},
		},
	}

	// Set names and link samples
	for name, s := range cfg.Samples {
		s.Name = name
	}
	for name, q := range cfg.Queues {
		q.Name = name
		q.Sample = cfg.Samples[q.SampleName]
	}

	return cfg
}

// New creates a new Chirp instance with the given configuration.
func New(cfg *config.Config, log zerolog.Logger) (*Chirp, error) {
	log = log.With().Str("component", "chirp").Logger()

	// Create audio player
	p, err := player.NewOtoPlayer(log)
	if err != nil {
		return nil, fmt.Errorf("failed to create audio player: %w", err)
	}

	// Create queues
	queues := make(map[string]*queue.Queue)
	for name, qCfg := range cfg.Queues {
		q, err := queue.NewQueue(qCfg, p, log)
		if err != nil {
			// Clean up any queues we've already created
			for _, q := range queues {
				q.Stop()
			}
			return nil, fmt.Errorf("failed to create queue '%s': %w", name, err)
		}
		queues[name] = q
	}

	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh" // Fallback if SHELL is not set
	}

	// Create terminal
	term := terminal.NewTerminal(shell, log, os.Stdin, os.Stdout)

	c := &Chirp{
		cfg:      cfg,
		term:     term,
		player:   p,
		queues:   queues,
		log:      log,
		stopChan: make(chan struct{}),
	}

	// Set up terminal handlers
	term.HandleInput = c.handleInput
	term.HandleOutput = c.handleOutput

	return c, nil
}

// Start begins the terminal session with audio feedback.
func (c *Chirp) Start(ctx context.Context) error {
	// Start the terminal
	if err := c.term.Start(); err != nil {
		return fmt.Errorf("failed to start terminal: %w", err)
	}

	c.log.Info().Msg("Chirp started successfully")

	// Wait for context cancellation or terminal exit
	go func() {
		select {
		case <-ctx.Done():
			c.log.Info().Msg("Context canceled, stopping chirp")
			c.Stop()
		case <-c.stopChan:
			return
		}
	}()

	// Wait for terminal to exit
	if err := c.term.Wait(); err != nil {
		return fmt.Errorf("terminal exited with error: %w", err)
	}

	return nil
}

// Stop gracefully shuts down the terminal session and audio.
func (c *Chirp) Stop() {
	c.stopOnce.Do(func() {
		c.log.Debug().Msg("Stopping chirp")
		close(c.stopChan)

		// Stop all queues
		for name, q := range c.queues {
			c.log.Debug().Str("queue", name).Msg("Stopping queue")
			q.Stop()
		}

		// Stop terminal
		c.term.Stop()

		// Close audio player
		if err := c.player.Close(); err != nil {
			c.log.Error().Err(err).Msg("Error closing audio player")
		}

		c.log.Info().Msg("Chirp stopped")
	})
}

// handleInput processes terminal input and triggers sounds.
func (c *Chirp) handleInput(data []byte) error {
	for _, b := range data {
		for name, q := range c.queues {
			if q.Config.MatchesInput(b) {
				c.log.Trace().
					Str("queue", name).
					Str("char", string(b)).
					Msg("Input matched queue pattern")
				q.Add(string(b))
			}
		}
	}
	return nil
}

// handleOutput processes terminal output and triggers sounds.
func (c *Chirp) handleOutput(data []byte) error {
	for _, b := range data {
		for name, q := range c.queues {
			if q.Config.MatchesOutput(b) {
				c.log.Trace().
					Str("queue", name).
					Str("char", string(b)).
					Msg("Output matched queue pattern")
				q.Add(string(b))
			}
		}
	}
	return nil
}
