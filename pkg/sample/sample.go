package sample

import (
	"errors"
	"fmt"
)

// SampleConfig defines the properties of an audio sample from the config file.
// Renamed from Sample to SampleConfig to avoid confusion with the runtime Sample type.
type SampleConfig struct {
	Name      string  `toml:"-"`         // Name is derived from the map key in TOML
	Duration  int     `toml:"duration"`  // Duration in milliseconds
	Frequency int     `toml:"frequency"` // Frequency in Hz (TODO: Add support for musical notes)
	Volume    float64 `toml:"volume"`    // Volume (0.0 to 1.0)
	// TODO: Add FilePath string `toml:"file_path"` for custom WAV/OGG files
}

// Validate checks if the sample configuration is valid.
func (s *SampleConfig) Validate() error {
	if s.Duration <= 0 {
		return errors.New("sample duration must be positive")
	}
	if s.Frequency <= 0 {
		// TODO: Add validation for musical notes if implemented
		return errors.New("sample frequency must be positive")
	}
	if s.Volume < 0.0 || s.Volume > 1.0 {
		return fmt.Errorf("sample volume must be between 0.0 and 1.0, got %f", s.Volume)
	}
	return nil
}
