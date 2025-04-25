package config

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
	"github.com/rs/zerolog"

	"github.com/hiway/chirp/pkg/sample"
)

// Queue defines the configuration for a sound queue.
type Queue struct {
	Name       string               `toml:"-"`      // Name is derived from map key
	Match      []string             `toml:"match"`  // Patterns to match
	SampleName string               `toml:"sample"` // Name of the sample to play
	MaxLength  int                  `toml:"max_length"`
	Sample     *sample.SampleConfig `toml:"-"` // Linked after config load
}

// Validate checks if the queue configuration is valid.
func (q *Queue) Validate() error {
	if len(q.Match) == 0 {
		return fmt.Errorf("match patterns cannot be empty")
	}
	if q.SampleName == "" {
		return fmt.Errorf("sample name cannot be empty")
	}
	if q.MaxLength < 0 {
		return fmt.Errorf("max_length cannot be negative")
	}
	if q.MaxLength == 0 {
		q.MaxLength = 1 // Default to 1 if not specified
	}
	return nil
}

// MatchesInput checks if a byte matches any input pattern.
func (q *Queue) MatchesInput(b byte) bool {
	// TODO: Implement more sophisticated pattern matching
	s := string(b)
	for _, pattern := range q.Match {
		if pattern == s {
			return true
		}
	}
	return false
}

// MatchesOutput checks if a byte matches any output pattern.
func (q *Queue) MatchesOutput(b byte) bool {
	// For now, using the same logic as input matching
	return q.MatchesInput(b)
}

// Config holds the complete chirp configuration.
type Config struct {
	Samples map[string]*sample.SampleConfig `toml:"samples"`
	Queues  map[string]*Queue               `toml:"queues"`
}

// LoadConfig reads and validates configuration from a TOML file.
func LoadConfig(path string, log zerolog.Logger) (*Config, error) {
	log.Debug().Str("path", path).Msg("Loading configuration file")

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if _, err := toml.Decode(string(data), &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse TOML: %w", err)
	}

	// Set names from map keys and validate
	for name, s := range cfg.Samples {
		s.Name = name
		if err := s.Validate(); err != nil {
			return nil, fmt.Errorf("invalid sample '%s': %w", name, err)
		}
		log.Debug().Str("sample", name).Msg("Validated sample")
	}

	for name, queue := range cfg.Queues {
		queue.Name = name
		if err := queue.Validate(); err != nil {
			return nil, fmt.Errorf("invalid queue '%s': %w", name, err)
		}

		// Link sample
		s, ok := cfg.Samples[queue.SampleName]
		if !ok {
			return nil, fmt.Errorf("queue '%s' references unknown sample '%s'", name, queue.SampleName)
		}
		queue.Sample = s

		log.Debug().
			Str("queue", name).
			Str("sample", queue.SampleName).
			Msg("Validated and linked queue")
	}

	log.Debug().Msg("Configuration loaded and validated successfully")
	return &cfg, nil
}
