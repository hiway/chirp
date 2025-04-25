package terminal

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/creack/pty"
	"github.com/rs/zerolog"
	"golang.org/x/term"
)

// Terminal manages the pseudo-terminal (PTY) for the wrapped shell.
type Terminal struct {
	log       zerolog.Logger
	shellPath string
	ptyFile   *os.File
	cmd       *exec.Cmd
	stopOnce  sync.Once
	stopChan  chan struct{}
	stdin     io.Reader
	stdout    io.Writer

	// Callbacks for processing data
	HandleInput  func(data []byte) error
	HandleOutput func(data []byte) error
}

// NewTerminal creates a new Terminal instance.
func NewTerminal(shellPath string, log zerolog.Logger, stdin io.Reader, stdout io.Writer) *Terminal {
	return &Terminal{
		log:       log.With().Str("component", "terminal").Logger(),
		shellPath: shellPath,
		stopChan:  make(chan struct{}),
		stdin:     stdin,
		stdout:    stdout,
	}
}

// Start launches the shell in a PTY and begins I/O handling.
func (t *Terminal) Start() error {
	t.log.Debug().Str("shell", t.shellPath).Msg("Starting terminal")

	// Create the command
	t.cmd = exec.Command(t.shellPath)

	// Start the command with a PTY
	var err error
	t.ptyFile, err = pty.Start(t.cmd)
	if err != nil {
		t.log.Error().Err(err).Msg("Failed to start PTY")
		return fmt.Errorf("failed to start pty: %w", err)
	}
	t.log.Debug().Msg("PTY started successfully")

	// Handle window resizes
	go t.handleResizes()

	// Set stdin to raw mode
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		// Attempt cleanup before returning error
		t.ptyFile.Close()
		t.log.Error().Err(err).Msg("Failed to set raw mode on stdin")
		return fmt.Errorf("failed to set raw mode: %w", err)
	}
	// Ensure state is restored on exit
	go func() {
		<-t.stopChan // Wait for stop signal
		term.Restore(int(os.Stdin.Fd()), oldState)
		t.log.Debug().Msg("Restored terminal state")
	}()

	// Start I/O copying goroutines
	go t.copyInput()
	go t.copyOutput()

	t.log.Info().Msg("Terminal session started")
	return nil
}

// Stop signals the terminal and associated goroutines to shut down.
func (t *Terminal) Stop() {
	t.stopOnce.Do(func() {
		t.log.Debug().Msg("Stopping terminal")
		close(t.stopChan) // Signal goroutines to stop
		if t.ptyFile != nil {
			t.ptyFile.Close() // Close the PTY file descriptor
		}
		t.log.Info().Msg("Terminal session stopped")
	})
}

// Wait waits for the underlying shell command to exit.
func (t *Terminal) Wait() error {
	if t.cmd == nil || t.cmd.Process == nil {
		return fmt.Errorf("command not started or already finished")
	}
	waitResult, err := t.cmd.Process.Wait()
	if err != nil {
		t.log.Error().Err(err).Msg("Error waiting for shell command exit")
		return fmt.Errorf("error waiting for command: %w", err)
	}
	t.log.Debug().Str("status", waitResult.String()).Msg("Shell process exited")
	// Ensure Stop is called if Wait finishes first (e.g., shell exits normally)
	t.Stop()
	return nil
}

// handleResizes listens for SIGWINCH and updates the PTY size.
func (t *Terminal) handleResizes() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGWINCH)
	defer signal.Stop(ch)
	defer close(ch)

	// Initial resize
	if err := pty.InheritSize(os.Stdin, t.ptyFile); err != nil {
		t.log.Warn().Err(err).Msg("Failed initial PTY resize")
	}

	for {
		select {
		case <-ch:
			if err := pty.InheritSize(os.Stdin, t.ptyFile); err != nil {
				t.log.Warn().Err(err).Msg("Failed to resize PTY")
			}
		case <-t.stopChan:
			t.log.Debug().Msg("Resize handler stopping")
			return
		}
	}
}

// copyInput reads from stdin, calls HandleInput, and writes to the PTY.
func (t *Terminal) copyInput() {
	buf := make([]byte, 1024) // Buffer for reading stdin
	for {
		select {
		case <-t.stopChan:
			t.log.Debug().Msg("Input copier stopping")
			return
		default:
			n, err := t.stdin.Read(buf)
			if err != nil {
				if err != io.EOF && !strings.Contains(err.Error(), "file descriptor closed") {
					t.log.Error().Err(err).Msg("Stdin read error")
				}
				t.Stop() // Trigger shutdown on stdin error/EOF
				return
			}
			if n > 0 {
				data := buf[:n]
				// Call the input handler (for chirping)
				if t.HandleInput != nil {
					if err := t.HandleInput(data); err != nil {
						t.log.Error().Err(err).Msg("Input handler failed")
					}
				}
				// Write to PTY
				if _, writeErr := t.ptyFile.Write(data); writeErr != nil {
					t.log.Error().Err(writeErr).Msg("PTY write error")
					t.Stop() // Trigger shutdown on PTY write error
					return
				}
			}
		}
	}
}

// copyOutput reads from the PTY, calls HandleOutput, and writes to stdout.
func (t *Terminal) copyOutput() {
	buf := make([]byte, 8192) // Buffer for reading PTY output
	for {
		select {
		case <-t.stopChan:
			t.log.Debug().Msg("Output copier stopping")
			return
		default:
			n, err := t.ptyFile.Read(buf)
			if err != nil {
				// Check for EOF or common PTY close errors
				if err == io.EOF || strings.Contains(err.Error(), "input/output error") || strings.Contains(err.Error(), "file descriptor closed") {
					t.log.Debug().Err(err).Msg("PTY read loop finished normally")
				} else {
					t.log.Error().Err(err).Msg("PTY read error")
				}
				t.Stop() // Trigger shutdown on PTY read error/EOF
				return
			}
			if n > 0 {
				data := buf[:n]
				// Call the output handler (for chirping)
				if t.HandleOutput != nil {
					if err := t.HandleOutput(data); err != nil {
						t.log.Error().Err(err).Msg("Output handler failed")
					}
				}
				// Write to stdout
				if _, writeErr := t.stdout.Write(data); writeErr != nil {
					t.log.Error().Err(writeErr).Msg("Stdout write error")
					t.Stop() // Trigger shutdown on stdout write error
					return
				}
			}
		}
	}
}
