package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/rs/zerolog"

	"github.com/hiway/chirp/pkg/chirp"
	"github.com/hiway/chirp/pkg/config"
)

var (
	configFile string
	debug      bool
)

func init() {
	flag.StringVar(&configFile, "config", "", "path to config file (optional)")
	flag.BoolVar(&debug, "debug", false, "enable debug logging")
	flag.Parse()
}

func main() {
	// Set up logging
	logLevel := zerolog.InfoLevel
	if debug {
		logLevel = zerolog.DebugLevel
	}
	log := zerolog.New(os.Stderr).
		Level(logLevel).
		With().
		Timestamp().
		Logger()

	// Load configuration
	var cfg *config.Config
	var err error
	if configFile != "" {
		cfg, err = config.LoadConfig(configFile, log)
		if err != nil {
			log.Error().Err(err).Str("path", configFile).Msg("Failed to load config file")
			os.Exit(1)
		}
		log.Info().Str("path", configFile).Msg("Loaded configuration file")
	} else {
		cfg = chirp.DefaultConfig()
		log.Info().Msg("Using default configuration")
	}

	// Create chirp instance
	c, err := chirp.New(cfg, log)
	if err != nil {
		log.Error().Err(err).Msg("Failed to create chirp")
		os.Exit(1)
	}

	// Set up signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigChan
		log.Info().Str("signal", sig.String()).Msg("Received signal, shutting down")
		cancel()
	}()

	// Start chirp
	if err := c.Start(ctx); err != nil {
		log.Error().Err(err).Msg("Chirp exited with error")
		os.Exit(1)
	}
}
