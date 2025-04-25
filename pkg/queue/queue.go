package queue

import (
	"fmt"
	"sync"

	"github.com/rs/zerolog"

	"github.com/hiway/chirp/pkg/config"
	"github.com/hiway/chirp/pkg/player"
)

// Queue manages pattern matching and sound triggering for a set of patterns.
type Queue struct {
	Config   *config.Queue
	player   player.Player
	log      zerolog.Logger
	itemChan chan string
	stopOnce sync.Once
	stopChan chan struct{}
}

// NewQueue creates a new queue with the given configuration.
func NewQueue(cfg *config.Queue, player player.Player, log zerolog.Logger) (*Queue, error) {
	if cfg.Sample == nil {
		return nil, fmt.Errorf("queue '%s' has nil sample configuration", cfg.Name)
	}

	q := &Queue{
		Config:   cfg,
		player:   player,
		log:      log.With().Str("queue", cfg.Name).Logger(),
		itemChan: make(chan string, cfg.MaxLength),
		stopChan: make(chan struct{}),
	}

	// Start the sound playing goroutine
	go q.run()

	return q, nil
}

// Add attempts to queue a matched item for playback.
func (q *Queue) Add(item string) {
	select {
	case q.itemChan <- item:
		q.log.Trace().Str("item", item).Msg("Item added to queue")
	default:
		q.log.Debug().Str("item", item).Msg("Queue full, dropping item")
	}
}

// Stop signals the queue to stop processing items.
func (q *Queue) Stop() {
	q.stopOnce.Do(func() {
		q.log.Debug().Msg("Stopping queue")
		close(q.stopChan)
	})
}

// run processes queued items and triggers sounds.
func (q *Queue) run() {
	q.log.Debug().Msg("Queue processor started")
	defer q.log.Debug().Msg("Queue processor stopped")

	for {
		select {
		case <-q.stopChan:
			return
		case item := <-q.itemChan:
			q.log.Trace().
				Str("item", item).
				Int("frequency", q.Config.Sample.Frequency).
				Int("duration", q.Config.Sample.Duration).
				Float64("volume", q.Config.Sample.Volume).
				Msg("Playing sound for queued item")

			if err := q.player.Play(q.Config.Sample); err != nil {
				q.log.Error().Err(err).Str("item", item).Msg("Failed to play sound")
			}
		}
	}
}
