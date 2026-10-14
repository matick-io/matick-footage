// Command footage runs the footage index core: the scanner, the core's own
// ingest stages and the HTTP API. `footage validate` checks sidecar files.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/matick-io/matick-footage/server/internal/api"
	"github.com/matick-io/matick-footage/server/internal/config"
	"github.com/matick-io/matick-footage/server/internal/pipeline"
	"github.com/matick-io/matick-footage/server/internal/scan"
	"github.com/matick-io/matick-footage/server/internal/sidecar"
	"github.com/matick-io/matick-footage/server/internal/storage"
	"github.com/matick-io/matick-footage/server/internal/store"
)

func main() {
	args := os.Args[1:]
	cmd := "serve"
	if len(args) > 0 {
		cmd, args = args[0], args[1:]
	}
	var err error
	switch cmd {
	case "serve":
		err = serve()
	case "validate":
		err = validate(args)
	default:
		err = fmt.Errorf("unknown command %q; want serve or validate", cmd)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "footage:", err)
		os.Exit(1)
	}
}

func serve() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	sources, err := storage.OpenAll(ctx, cfg)
	if err != nil {
		return err
	}
	defer sources.Close()

	scanner := &scan.Scanner{DB: db, Sources: sources.All, StableFor: cfg.StableFor, TranscribeModel: cfg.TranscribeModel}
	go scanner.Run(ctx, cfg.ScanInterval)
	go (&pipeline.Worker{DB: db, Sources: sources}).Run(ctx)

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           (&api.Server{DB: db, Cfg: cfg, Sources: sources}).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	for _, src := range sources.All {
		slog.Info("source", "name", src.Name, "root", src.Root(), "library", src.Library)
	}
	slog.Info("serving", "addr", cfg.Addr, "data", cfg.Data)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// validate reports every problem in the given sidecars and fails if any has one.
func validate(paths []string) error {
	if len(paths) == 0 {
		return errors.New("usage: footage validate <file.events.jsonl>...")
	}
	bad := 0
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		parsed, err := sidecar.Parse(f)
		f.Close()
		switch {
		case err != nil:
			bad++
			fmt.Printf("%s: %v\n", p, err)
		case len(parsed.Problems) > 0:
			bad++
			for _, prob := range parsed.Problems {
				fmt.Printf("%s: %s\n", p, prob)
			}
		default:
			fmt.Printf("%s: ok, %d entries\n", p, len(parsed.Entries))
		}
	}
	if bad > 0 {
		return fmt.Errorf("%d of %d files have problems", bad, len(paths))
	}
	return nil
}
