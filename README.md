# chirp

Provides auditory feedback (chirps) for keyboard input and terminal output, enhancing the interactive terminal experience. It wraps your existing shell, playing distinct sounds for typing and for program output.

## Features

* **Auditory Feedback:** Get immediate sound confirmation for keypresses and terminal output
* **Pattern Matching:** Configure different sounds for different input/output patterns
* **Queue-based Design:** Smart sound queuing to prevent audio overload
* **Shell Agnostic:** Works with any shell (configurable via `SHELL` environment variable)
* **Low Latency:** Uses the `oto/v3` library for efficient audio playback
* **Highly Configurable:** TOML-based configuration for samples and pattern matching

## Installation

Ensure you have Go installed (version 1.20 or later recommended).

```bash
go install github.com/hiway/chirp/cmd/chirp@latest
```

Or build from source:

```bash
git clone https://github.com/hiway/chirp
cd chirp
go build -o chirp ./cmd/chirp
```

## Usage

Simply run `chirp` to start a new shell session with audio feedback:

```bash
chirp
```

Use the `-config` flag to specify a custom configuration file:

```bash
chirp -config /path/to/chirp.toml
```

Enable debug logging with:

```bash
chirp -debug
```

## Configuration

Chirp uses TOML for configuration. Here's a sample configuration file:

```toml
# Define your sound samples
[samples]
  [samples.local]
    frequency = 392  # G4 note
    duration = 50    # milliseconds
    volume = 0.3     # 0.0 to 1.0

  [samples.remote]
    frequency = 587  # D5 note
    duration = 50
    volume = 0.3

# Define pattern matching queues
[queues]
  [queues.local]
    match = ["\r", "\n"]  # Match Enter key
    sample = "local"      # Use local sample
    max_length = 1        # Queue size

  [queues.remote]
    match = ["$", "#", "%"]  # Match shell prompts
    sample = "remote"
    max_length = 1
```

### Configuration Options

#### Samples

Each sample in the `[samples]` section defines a sound:
- `frequency`: Tone frequency in Hz
- `duration`: Sound duration in milliseconds
- `volume`: Volume level from 0.0 to 1.0

#### Queues

Each queue in the `[queues]` section defines pattern matching:
- `match`: List of strings to match in input/output
- `sample`: Name of the sample to play when matched
- `max_length`: Maximum queue size (prevents sound spam)

## Package Structure

Chirp is organized into several packages:

- `pkg/chirp`: Core package providing the main API
- `pkg/config`: Configuration loading and validation
- `pkg/player`: Audio playback using oto
- `pkg/queue`: Pattern matching and sound queuing
- `pkg/sample`: Sample configuration
- `pkg/terminal`: PTY and shell management
