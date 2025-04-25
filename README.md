# chirp

Provides auditory feedback (chirps) for keyboard input and terminal output, enhancing the interactive terminal experience. It wraps your existing shell, playing distinct sounds for typing and for program output.

## Features

*   **Auditory Feedback:** Get immediate sound confirmation for keypresses and terminal responses.
*   **Distinct Sounds:** Different chirp sounds for user input vs. terminal output.
*   **Echo Detection:** Attempts to suppress chirps for characters immediately echoed back by the shell (e.g., during typing).
*   **Shell Agnostic:** Wraps your preferred shell (configurable via `SHELL` environment variable).
*   **Low Latency:** Uses the `oto/v3` library for audio playback, aiming for minimal delay.

## How it Works

`chirp` starts your specified shell within a pseudo-terminal (pty). It intercepts your keyboard input, plays an "input" chirp, and forwards the input to the shell. It also reads the output from the shell's pty, plays an "output" chirp (unless it detects the output is likely an echo of recent input or a large screen update), and prints the output to your actual terminal.

## Installation

Ensure you have Go installed (version 1.18 or later recommended).

1.  Clone the repository (if you haven't already):
    ```bash
    # Example: git clone <repository-url>
    # cd chirp
    ```
2.  Build the executable:
    ```bash
    go build -o chirp ./cmd/chirp
    ```
    This will create a `chirp` executable in the current directory. You can move this to a location in your `$PATH` (e.g., `~/.local/bin`, `/usr/local/bin`) for easier access.

## Running

Simply execute the compiled `chirp` binary:

```bash
./chirp
```

This will start `chirp`, which in turn will launch your default shell (or the shell specified by the `SHELL` environment variable). All interaction within this `chirp` session will have auditory feedback. Type `exit` or `Ctrl+D` in the wrapped shell to quit `chirp`.

## Configuration

Chirp can be configured using TOML configuration files. The configuration is loaded from the following locations in order of increasing priority:

1. `/usr/local/etc/chirp.toml` (system-wide)
2. `~/.config/chirp/chirp.toml` (user-specific)
3. `./chirp.toml` (local directory)

Settings from later files override those from earlier ones. Here's a sample configuration file with all available options:

```toml
# Shell to use (defaults to $SHELL environment variable)
shell = ""  # Empty means use $SHELL or fallback to /bin/sh

# Input sound configuration (when you type)
[input_sound]
frequency = 587.33  # Note D5
duration_ms = 25    # Duration in milliseconds
volume = 0.35      # Volume level (0.0 to 1.0)

# Output sound configuration (terminal output)
[output_sound]
frequency = 392.00  # Note G4
duration_ms = 35    # Duration in milliseconds
volume = 0.25      # Volume level (0.0 to 1.0)

# Per-key (symbolic name) input chirp overrides
[input_sound_overrides]
enter = { frequency = 800, duration_ms = 60, volume = 0.5 }   # Enter
esc = { frequency = 300, duration_ms = 40, volume = 0.4 }     # Esc
left = { frequency = 500, duration_ms = 30, volume = 0.3 }    # Left arrow
right = { frequency = 600, duration_ms = 30, volume = 0.3 }   # Right arrow
up = { frequency = 700, duration_ms = 30, volume = 0.3 }      # Up arrow
down = { frequency = 400, duration_ms = 30, volume = 0.3 }    # Down arrow

# Advanced settings
echo_timeout_ms = 1    # Time to wait before considering output not an echo
min_sound_gap_ms = 25  # Minimum time between sounds
debug = false          # Enable debug logging
```

### Configuration Options

- `shell`: The shell to use. If empty, uses `$SHELL` environment variable or falls back to `/bin/sh`.
- `input_sound`: Settings for sounds played when you type:
  - `frequency`: Tone frequency in Hz
  - `duration_ms`: Sound duration in milliseconds
  - `volume`: Volume level (0.0 to 1.0)
- `output_sound`: Settings for sounds played when the terminal outputs text:
  - `frequency`: Tone frequency in Hz
  - `duration_ms`: Sound duration in milliseconds
  - `volume`: Volume level (0.0 to 1.0)
- `input_sound_overrides`: Map of symbolic key names to custom sound settings for input. Example keys: `enter`, `esc`, `left`, `right`, `up`, `down`, `tab`, `backspace`. Each value is a table with `frequency`, `duration_ms`, and `volume`.
- `echo_timeout_ms`: How long to wait (in milliseconds) before considering output as not being an echo of input
- `min_sound_gap_ms`: Minimum time (in milliseconds) between consecutive sounds
- `debug`: Enable debug logging

#### Notes on Per-Key Chirp Overrides

- For control keys, use symbolic names (e.g., `enter`, `esc`, `left`, `right`, `up`, `down`, `tab`, `backspace`).
- For printable Unicode characters, use the character itself as the key.
- If a key is not listed in `input_sound_overrides`, the default `[input_sound]` settings are used.
- This system is extensible: in the future, you can add word/phrase-based chirps (e.g., for "error", "success", etc.).

## Dependencies

*   Go (for building)
*   [github.com/creack/pty](https://github.com/creack/pty) (for pseudo-terminal handling)
*   [github.com/ebitengine/oto/v3](https://github.com/ebitengine/oto) (for audio playback)
*   [golang.org/x/term](https://pkg.go.dev/golang.org/x/term) (for raw terminal mode)
