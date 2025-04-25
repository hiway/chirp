# Chirp: Auditory Feedback CLI Program

## Overview

Chirp is a CLI tool that provides auditory feedback in terminal sessions (local or over SSH). It intercepts input/output streams, detects configured Unicode graphemes, control characters, or words, and plays associated audio samples as feedback. Configuration is managed via TOML files, allowing users to define sound samples, queues, and matching rules.

---

## Design Notes

### 1. CLI Invocation & Shell Management
- On launch, Chirp starts a new shell (autodetected, default, or user-overridden).
- It acts as a wrapper, intercepting both input and output streams.
- Works transparently over SSH and local terminals.

### 2. Stream Interception & Matching
- Monitors both input and output streams for:
  - Unicode graphemes
  - Control characters
  - Words (as defined in config)
- Matching is configurable and can be case-sensitive or insensitive.

### 3. Queues & Audio Playback
- Each match type (grapheme, word, control char) is associated with a named queue.
- Each queue has:
  - A player instance
  - Configurable max length (default: 1)
  - Associated audio sample
- When a match is detected:
  - If the queue is not full, the item is added.
  - If full, new items are dropped until the queue is cleared.
- The player plays the sample for each item in the queue, sequentially.
- Multiple queues can play in parallel with low latency.

### 4. Audio Sample Configuration
- Samples are defined in TOML:
  - Duration (ms)
  - Frequency (Hz) or musical note (e.g., "A4")
  - Volume (0.0–1.0)
- Each queue references a sample.

### 5. Extensibility
- Support for custom sound files (WAV/OGG) in addition to synthesized tones.

---

## API Documentation

### Configuration (TOML)

```toml
# chirp.toml

[queues.alnum]
match = ["a", "b", "c", "A", "B", "C"] # Unicode graphemes or words
sample = "beep"
max_length = 1

[queues.enter]
match = ["\n", "\r"]
sample = "ding"
max_length = 1

[samples.beep]
duration = 100      # ms
frequency = 440     # Hz
volume = 0.8

[samples.ding]
duration = 200
frequency = 880
volume = 0.6
```

### CLI Usage

```
chirp [--config path/to/chirp.toml] [--shell /bin/zsh] 
```

- `--config`: Path to TOML config file.
- `--shell`: Override autodetected shell.

### Programmatic API (Go)

#### Types

```go
type Sample struct {
    Name     string
    Duration int     // ms
    Frequency int    // Hz or musical note
    Volume   float64 // 0.0–1.0
}

type Queue struct {
    Name      string
    Match     []string // Graphemes, control chars, or words
    Sample    *Sample
    MaxLength int
    Items     []string
    Player    *Player
}

type Player struct {
    // Plays samples for items in the queue
    Play(sample *Sample, count int)
}
```

#### Main Flow

```go
func main() {
    // Parse config
    // Start shell
    // Intercept streams
    // For each match, add to queue
    // For each queue, play samples as items arrive
}
```

---

## Example Flow

1. User runs `chirp`.
2. Chirp starts a shell, intercepts streams.
3. User types "a" (matched by `queues.alnum`).
4. "a" is added to the `alnum` queue.
5. Player for `alnum` plays "beep" sample.
6. If user types "a" again before playback ends and queue is full, the new "a" is dropped.