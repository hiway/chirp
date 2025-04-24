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
		buf := make([]byte, 1)
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
				go chirp.PlayChirp(keypressChirp)
				if _, err := p.Write(buf[:n]); err != nil {
					log.Printf("Pty write error: %v", err)
					return
				}
			}
		}
	}()

	// Read pty output, play chirp and print to stdout
	bufOut := make([]byte, 1024)
	for {
		n, err := p.Read(bufOut)
		if err != nil {
			if err != io.EOF && !strings.Contains(err.Error(), "input/output error") {
				log.Printf("Pty read error: %v", err)
			}
			break
		}
		if n > 0 {
			go chirp.PlayChirp(outputChirp)
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
