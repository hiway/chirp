package main

import (
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/adrg/xdg"
	"github.com/creack/pty"
	"golang.org/x/term"

	"github.com/hiway/chirp"
)

// Config holds the application configuration
type Config struct {
	Shell         string      `toml:"shell"`
	InputSound    SoundConfig `toml:"input_sound"`
	OutputSound   SoundConfig `toml:"output_sound"`
	EchoTimeoutMs int64       `toml:"echo_timeout_ms"`  // Echo timeout in milliseconds
	MinSoundGapMs int64       `toml:"min_sound_gap_ms"` // Min gap between sounds in ms
	Debug         bool        `toml:"debug"`
}

// SoundConfig defines parameters for a chirp sound
type SoundConfig struct {
	Frequency  float64 `toml:"frequency"`
	DurationMs int64   `toml:"duration_ms"` // Duration in milliseconds
	Volume     float64 `toml:"volume"`
}

// Default configuration values
func defaultConfig() Config {
	return Config{
		Shell: os.Getenv("SHELL"), // Default to SHELL env var first
		InputSound: SoundConfig{
			Frequency:  chirp.NoteD5, // 587.33
			DurationMs: 25,
			Volume:     0.35,
		},
		OutputSound: SoundConfig{
			Frequency:  chirp.NoteG4, // 392.00
			DurationMs: 35,
			Volume:     0.25,
		},
		EchoTimeoutMs: 1,  // Default 1ms echo timeout
		MinSoundGapMs: 25, // Default 25ms min sound gap
		Debug:         false,
	}
}

// loadConfig loads configuration from standard locations
func loadConfig() Config {
	cfg := defaultConfig()

	// Define config file paths in order of increasing priority
	// 1. System-wide
	// 2. User-specific (XDG)
	// 3. Local directory
	configFiles := []string{
		"/usr/local/etc/chirp.toml", // System-wide (adjust path if needed)
	}

	// User config dir (e.g., ~/.config/chirp/chirp.toml)
	userConfigPath, err := xdg.ConfigFile("chirp/chirp.toml")
	if err == nil {
		configFiles = append(configFiles, userConfigPath)
	} else {
		log.Printf("Warning: Could not determine user config directory: %v", err)
	}

	// Local config file
	configFiles = append(configFiles, "./chirp.toml")

	// Load configs, merging settings. Later files override earlier ones.
	for _, file := range configFiles {
		if _, err := os.Stat(file); err == nil {
			if _, err := toml.DecodeFile(file, &cfg); err != nil {
				log.Printf("Warning: Failed to load config file '%s': %v", file, err)
			} else {
				if cfg.Debug {
					log.Printf("Loaded config from: %s", file)
				}
			}
		} else if !os.IsNotExist(err) {
			log.Printf("Warning: Error checking config file '%s': %v", file, err)
		}
	}

	// Fallback shell if still empty after loading config files
	if cfg.Shell == "" {
		cfg.Shell = os.Getenv("SHELL")
		if cfg.Shell == "" {
			cfg.Shell = "/bin/sh"
		}
	}

	// Apply loaded settings that require initialization
	chirp.SetDebug(cfg.Debug)
	chirp.SetEchoTimeout(time.Duration(cfg.EchoTimeoutMs) * time.Millisecond)
	chirp.SetMinSoundGap(time.Duration(cfg.MinSoundGapMs) * time.Millisecond)

	if cfg.Debug {
		log.Printf("Final config: %+v", cfg)
	}

	return cfg
}

func main() {
	// Load configuration
	cfg := loadConfig()

	// Initialize audio context early
	if err := chirp.Initialize(); err != nil {
		log.Fatalf("failed to initialize audio context: %v", err)
	}

	// Use shell from config
	cmd := exec.Command(cfg.Shell)
	if cfg.Debug {
		log.Printf("Using shell: %s", cfg.Shell)
	}

	// Start the command with a pty
	p, err := pty.Start(cmd)
	if err != nil {
		log.Fatalf("failed to start pty: %v", err)
	}
	defer p.Close()

	// Handle window resizes
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGWINCH)
	go func() {
		for range ch {
			_ = pty.InheritSize(os.Stdin, p)
		}
	}()
	ch <- syscall.SIGWINCH // Initial resize

	// Switch stdin to raw mode
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		log.Fatalf("failed to set raw mode: %v", err)
	}
	defer term.Restore(int(os.Stdin.Fd()), oldState)

	// Create chirp options from loaded config
	keypressChirp := chirp.Options{
		Frequency: cfg.InputSound.Frequency,
		Duration:  time.Duration(cfg.InputSound.DurationMs) * time.Millisecond,
		Volume:    cfg.InputSound.Volume,
	}
	outputChirp := chirp.Options{
		Frequency: cfg.OutputSound.Frequency,
		Duration:  time.Duration(cfg.OutputSound.DurationMs) * time.Millisecond,
		Volume:    cfg.OutputSound.Volume,
	}

	// Read user keystrokes, play chirp and forward to pty
	go func() {
		buf := make([]byte, 32) // Increased buffer size for UTF-8 sequences
		for {
			n, err := os.Stdin.Read(buf)
			if err != nil {
				if err != io.EOF {
					log.Printf("Stdin read error: %v", err)
				}
				// Attempt to close pty gracefully on stdin EOF/error
				p.Close()
				return
			}
			if n > 0 {
				// Only track and chirp for printable characters and common control chars
				for i := 0; i < n; i++ {
					// Use the simpler isPrintableOrControl check for now
					if isPrintableOrControl(buf[i]) {
						chirp.TrackInput(buf[i])
					}
				}
				// Play chirp only once per input batch
				go chirp.PlayChirp(keypressChirp) // Run in goroutine to avoid blocking input
				if _, err := p.Write(buf[:n]); err != nil {
					log.Printf("Pty write error: %v", err)
					return // Exit goroutine on pty write error
				}
			}
		}
	}()

	// Read pty output, play chirp and print to stdout
	bufOut := make([]byte, 8192) // Increased for better burst handling
	for {
		n, err := p.Read(bufOut)
		if err != nil {
			// Check for EOF or common pty close errors
			if err == io.EOF || strings.Contains(err.Error(), "input/output error") || strings.Contains(err.Error(), "file descriptor closed") {
				chirp.Debugf("PTY read loop finished: %v", err)
			} else {
				log.Printf("Pty read error: %v", err)
			}
			break // Exit loop on any read error or EOF
		}
		if n > 0 {
			shouldChirp := true
			printableCount := 0

			// Check each character in output
			for i := 0; i < n; i++ {
				// Count printable characters (using the simpler check for now)
				if isPrintableOrControl(bufOut[i]) {
					printableCount++
					if chirp.IsRecentInput(bufOut[i]) {
						shouldChirp = false
						break // Stop checking this buffer if echo detected
					}
				}
			}

			// Skip chirp for large bursts of characters (like screen clears)
			if printableCount > 100 { // TODO: Make this configurable?
				shouldChirp = false
			}

			// Only chirp if we have printable characters and not in a sound debounce period
			if shouldChirp && printableCount > 0 && !chirp.IsSoundPlaying() {
				go chirp.PlayChirp(outputChirp) // Run in goroutine
			}

			if _, err := os.Stdout.Write(bufOut[:n]); err != nil {
				log.Printf("Stdout write error: %v", err)
				break // Exit loop on stdout write error
			}
		}
	}

	// Reset cursor position before exiting (might not always be necessary but good practice)
	os.Stdout.Write([]byte("\r"))

	// Wait for the command to exit
	if cmd.Process != nil {
		waitResult, waitErr := cmd.Process.Wait()
		if waitErr != nil {
			log.Printf("Error waiting for command exit: %v", waitErr)
		} else {
			chirp.Debugf("Shell process exited: %s", waitResult.String())
		}
	} else {
		chirp.Debugf("Shell process was nil, could not wait.")
	}
	chirp.Debugf("Chirp exiting.")
}

// isPrintableOrControl returns true for simplicity now.
// TODO: Refine this check if needed, maybe make it configurable.
func isPrintableOrControl(b byte) bool {
	return true // Allow all characters for now
}
