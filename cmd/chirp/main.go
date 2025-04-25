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

// GraphemeSoundConfig allows per-grapheme chirp settings
// Key is the grapheme (string), value is SoundConfig
// Example: "⏎" (Enter), "⎋" (Esc), "←" (Left Arrow)
type GraphemeSoundConfig map[string]SoundConfig

// Config holds the application configuration
type Config struct {
	Shell         string      `toml:"shell"`
	InputSound    SoundConfig `toml:"input_sound"`
	OutputSound   SoundConfig `toml:"output_sound"`
	EchoTimeoutMs int64       `toml:"echo_timeout_ms"`  // Echo timeout in milliseconds
	MinSoundGapMs int64       `toml:"min_sound_gap_ms"` // Min gap between sounds in ms
	Debug         bool        `toml:"debug"`

	// New field for per-grapheme sound overrides
	InputSoundOverrides GraphemeSoundConfig `toml:"input_sound_overrides"`
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

		// Initialize the new field
		InputSoundOverrides: make(GraphemeSoundConfig),
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

	// Normalize keys in InputSoundOverrides (optional, for robustness)
	if cfg.InputSoundOverrides == nil {
		cfg.InputSoundOverrides = make(GraphemeSoundConfig)
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

// Map control characters and escape sequences to symbolic names
var controlKeyMap = map[byte]string{
	0x1b: "esc",       // ESC
	0x7f: "backspace", // DEL/Backspace
	0x09: "tab",       // TAB
	0x0d: "enter",     // CR (Enter)
	0x0a: "enter",     // LF (Enter, for some terminals)
	0x03: "ctrl_c",    // Ctrl+C (SIGINT)
	0x04: "ctrl_d",    // Ctrl+D (EOF)
	0x0c: "ctrl_l",    // Ctrl+L (Clear screen)
}

// Map escape sequences for arrow keys and others to symbolic names
var escapeSeqMap = map[string]string{
	"[A":  "up",
	"[B":  "down",
	"[C":  "right",
	"[D":  "left",
	"[3~": "delete", // Delete key
}

// getChirpOptionsForInput returns chirp options for a given input byte slice
func getChirpOptionsForInput(cfg Config, buf []byte, i *int) chirp.Options {
	// Check for ESC sequence (arrow keys, delete key, etc.)
	if buf[*i] == 0x1b && *i+2 < len(buf) {
		// Handle longer sequences like Delete key ([3~)
		if *i+3 < len(buf) && buf[*i+1] == '[' && buf[*i+2] == '3' && buf[*i+3] == '~' {
			*i += 3 // Skip the escape sequence
			if override, ok := cfg.InputSoundOverrides["delete"]; ok {
				return chirp.Options{
					Frequency: override.Frequency,
					Duration:  time.Duration(override.DurationMs) * time.Millisecond,
					Volume:    override.Volume,
				}
			}
		}
		// Handle standard arrow keys
		if buf[*i+1] == '[' {
			// Build the sequence directly into the map lookup for efficiency
			if name, ok := escapeSeqMap[string([]byte{buf[*i+1], buf[*i+2]})]; ok {
				*i += 2 // Skip the escape sequence
				if override, ok := cfg.InputSoundOverrides[name]; ok {
					return chirp.Options{
						Frequency: override.Frequency,
						Duration:  time.Duration(override.DurationMs) * time.Millisecond,
						Volume:    override.Volume,
					}
				}
			}
		}
	}
	// Check for control key
	if name, ok := controlKeyMap[buf[*i]]; ok {
		if override, ok := cfg.InputSoundOverrides[name]; ok {
			return chirp.Options{
				Frequency: override.Frequency,
				Duration:  time.Duration(override.DurationMs) * time.Millisecond,
				Volume:    override.Volume,
			}
		}
	}
	// Fallback to default input sound
	return chirp.Options{
		Frequency: cfg.InputSound.Frequency,
		Duration:  time.Duration(cfg.InputSound.DurationMs) * time.Millisecond,
		Volume:    cfg.InputSound.Volume,
	}
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
					if isPrintableOrControl(buf[i]) {
						chirp.TrackInput(buf[i])
						opts := getChirpOptionsForInput(cfg, buf, &i)
						go chirp.PlayChirp(opts)
					}
				}
				// Play chirp only once per input batch
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
