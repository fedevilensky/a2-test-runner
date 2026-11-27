package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/a-h/templ"

	"github.com/fedevilensky/a2-test-runner/models"
	"github.com/fedevilensky/a2-test-runner/repo"
	"github.com/fedevilensky/a2-test-runner/server"
	"github.com/fedevilensky/a2-test-runner/service"
)

type application struct {
	results   *repo.SQLite
	staticDir string
	mu        sync.Mutex
}

func openApplication(root string) (*application, error) {
	if root == "" {
		return nil, errors.New("output directory is required")
	}
	if err := os.MkdirAll(filepath.Join(root, "data"), 0o755); err != nil {
		return nil, err
	}
	db, err := repo.Open(filepath.Join(root, "data", "results.sqlite"))
	if err != nil {
		return nil, err
	}
	return &application{results: db, staticDir: filepath.Join(root, "static")}, nil
}

func (a *application) execute(
	ctx context.Context, cfg service.Config, progress func(string),
) error {
	return a.executeWithResults(ctx, cfg, progress, nil)
}

func (a *application) executeWithResults(
	ctx context.Context,
	cfg service.Config,
	progress func(string),
	completed func(models.StudentReportData),
) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	// Reject static-path collisions before changing existing results.
	existing, err := a.results.List(ctx)
	if err != nil {
		return err
	}
	if cfg.Incremental {
		cfg.Cached = make(map[string]models.StudentReportData, len(existing))
		for _, report := range existing {
			cfg.Cached[report.StudentName] = report
		}
	}
	names := make(map[string]string)
	for _, old := range existing {
		names[sanitizeName(old.StudentName)] = old.StudentName
	}
	for _, student := range cfg.Students {
		name := filepath.Base(filepath.Clean(student.Path))
		key := sanitizeName(name)
		if key == "index.html" {
			return fmt.Errorf("student folder %q conflicts with the combined report", name)
		}
		if prior, ok := names[key]; ok && prior != name {
			return fmt.Errorf("student names %q and %q map to the same report folder", prior, name)
		}
		names[key] = name
	}
	return service.RunWithCompleted(ctx, cfg, progress, func(report models.StudentReportData) error {
		for gi := range report.Groups {
			for ri := range report.Groups[gi].Results {
				service.EnrichDiff(&report.Groups[gi].Results[ri])
			}
		}
		var err error
		if len(cfg.Selection.Exercises) == 0 {
			err = a.results.Replace(ctx, report)
		} else {
			err = a.results.Merge(ctx, report, cfg.Selection)
		}
		if err != nil {
			return fmt.Errorf("save %s: %w", report.StudentName, err)
		}
		if progress != nil {
			progress(fmt.Sprintf("Generating HTML reports after %s", report.StudentName))
		}
		stored, err := a.results.List(ctx)
		if err != nil {
			return err
		}
		if err := publishUpdatedReports(ctx, a.staticDir, stored, report.StudentName); err != nil {
			return fmt.Errorf("results saved, but report generation failed: %w", err)
		}
		if progress != nil {
			progress(fmt.Sprintf("%s: report available", report.StudentName))
		}
		if completed != nil {
			completed(report)
		}
		return nil
	})
}

func (a *application) report(ctx context.Context, id string) (templ.Component, error) {
	data, err := a.results.List(ctx)
	if err != nil {
		return nil, err
	}
	return reportComponent(data, id)
}

func serveApplication(root string, port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("invalid listen port %d", port)
	}
	app, err := openApplication(root)
	if err != nil {
		return err
	}
	defer func() { _ = app.results.Close() }()
	stored, err := app.results.List(context.Background())
	if err != nil {
		return err
	}
	if len(stored) > 0 {
		if err := publishReports(context.Background(), app.staticDir, stored); err != nil {
			return fmt.Errorf("regenerate static reports: %w", err)
		}
	}
	srvConfig := server.Config{
		StaticDir: app.staticDir, Run: app.execute, List: app.results.List, Report: app.report,
	}
	handler, err := server.New(srvConfig)
	if err != nil {
		return err
	}
	defer func() { _ = handler.Close() }()
	listener, err := net.Listen(
		"tcp", net.JoinHostPort("127.0.0.1", fmt.Sprint(port)),
	)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Test runner available at http://%s\n", listener.Addr())
	httpServer := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	done := make(chan error, 1)
	go func() { done <- httpServer.Serve(listener) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			return err
		}
		if err := <-done; !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}
