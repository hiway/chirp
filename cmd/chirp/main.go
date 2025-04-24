package main

import (
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	"github.com/creack/pty"
	"golang.org/x/term"

	"github.com/hiway/chirp"
)

func main() {
	// Initialize audio context early
	if err := chirp.Initialize(); err != nil {
		log.Fatalf("failed to initialize audio context: %v", err)
	}

	// Determine shell
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	cmd := exec.Command(shell)

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

	// Options for different chirp sounds
	keypressChirp := chirp.GetChirpOptions(chirp.InputChirp)
	outputChirp := chirp.GetChirpOptions(chirp.OutputChirp)

	// Read user keystrokes, play chirp and forward to pty
	go func() {
		buf := make([]byte, 32) // Increased buffer size for UTF-8 sequences
		for {
			n, err := os.Stdin.Read(buf)
			if err != nil {
				if err != io.EOF {
					log.Printf("Stdin read error: %v", err)
				}
				p.Close()
				return
			}
			if n > 0 {
				// Only track and chirp for printable characters and common control chars
				for i := 0; i < n; i++ {
					if isPrintableOrControl(buf[i]) {
						chirp.TrackInput(buf[i])
					}
				}
				// Play chirp only once per input batch
				chirp.PlayChirp(keypressChirp)
				if _, err := p.Write(buf[:n]); err != nil {
					log.Printf("Pty write error: %v", err)
					return
				}
			}
		}
	}()

	// Read pty output, play chirp and print to stdout
	bufOut := make([]byte, 8192) // Increased for better burst handling
	for {
		n, err := p.Read(bufOut)
		if err != nil {
			if err != io.EOF && !strings.Contains(err.Error(), "input/output error") {
				log.Printf("Pty read error: %v", err)
			}
			break
		}
		if n > 0 {
			shouldChirp := true
			printableCount := 0

			// Check each character in output
			for i := 0; i < n; i++ {
				// Count printable characters
				if isPrintableOrControl(bufOut[i]) {
					printableCount++
					if chirp.IsRecentInput(bufOut[i]) {
						shouldChirp = false
						break
					}
				}
			}

			// Skip chirp for large bursts of characters (like screen clears)
			if printableCount > 100 {
				shouldChirp = false
			}

			// Only chirp if we have printable characters and not in a sound debounce period
			if shouldChirp && printableCount > 0 && !chirp.IsSoundPlaying() {
				go chirp.PlayChirp(outputChirp)
			}

			if _, err := os.Stdout.Write(bufOut[:n]); err != nil {
				log.Printf("Stdout write error: %v", err)
				break
			}
		}
	}

	// Reset cursor position before exiting
	os.Stdout.Write([]byte("\r"))

	// Wait for the command to exit
	if cmd.Process != nil {
		_, err = cmd.Process.Wait()
		if err != nil {
			log.Printf("Error waiting for command exit: %v", err)
		}
	}
}

// isPrintableOrControl returns true for printable ASCII characters and common control characters
func isPrintableOrControl(b byte) bool {
	// Common control characters (newline, carriage return, tab, backspace)
	// if b == '\n' || b == '\r' || b == '\t' || b == '\b' {
	// 	return true
	// }
	// Printable ASCII range
	// return b >= 32 && b <= 126
	return true // Allow all characters
}
