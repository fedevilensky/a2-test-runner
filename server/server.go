// Package server exposes the local report runner over HTTP.
package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/a-h/templ"
	"github.com/fedevilensky/a2-test-runner/models"
	"github.com/fedevilensky/a2-test-runner/service"
)

type Config struct {
	StaticDir  string
	Run        func(context.Context, service.Config, func(string)) error
	List       func(context.Context) ([]models.StudentReportData, error)
	Report     func(context.Context, string) (templ.Component, error)
	PickFolder func(context.Context, string) (string, error)
}
type Server struct {
	cfg       Config
	mux       *http.ServeMux
	mu        sync.Mutex
	status    status
	uploadDir string
	ctx       context.Context
	cancel    context.CancelFunc
	jobs      sync.WaitGroup
	closed    bool
}
type status struct {
	Running   bool               `json:"running"`
	Messages  []string           `json:"messages"`
	Error     string             `json:"error,omitempty"`
	Progress  models.RunProgress `json:"progress"`
	StartedAt time.Time          `json:"started_at,omitempty"`
}

func New(cfg Config) (*Server, error) {
	dir, err := os.MkdirTemp("", "a2-uploads-*")
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{cfg: cfg, mux: http.NewServeMux(), uploadDir: dir, ctx: ctx, cancel: cancel}
	s.mux.HandleFunc("GET /{$}", s.index)
	s.mux.HandleFunc("POST /api/pick-folder", s.pickFolder)
	s.mux.HandleFunc("GET /api/discover", s.discover)
	s.mux.HandleFunc("GET /api/exercises", s.exercises)
	s.mux.HandleFunc("GET /api/results", s.results)
	s.mux.HandleFunc("GET /api/summary", s.summary)
	s.mux.HandleFunc("GET /api/static-reports", s.staticReports)
	s.mux.HandleFunc("GET /api/status", s.getStatus)
	s.mux.HandleFunc("POST /api/upload", s.upload)
	s.mux.HandleFunc("POST /api/run", s.run)
	s.mux.HandleFunc("GET /reports", s.report)
	s.mux.HandleFunc("GET /reports/{student}", s.report)
	s.mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.Dir(cfg.StaticDir))))
	s.mux.HandleFunc("GET /app.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		_, _ = io.WriteString(w, appJS)
	})
	return s, nil
}

func (s *Server) Close() error {
	s.mu.Lock()
	s.closed = true
	s.cancel()
	s.mu.Unlock()
	s.jobs.Wait()
	return os.RemoveAll(s.uploadDir)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Prevent DNS rebinding and cross-site requests to this local execution service.
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	ip := net.ParseIP(host)
	if !strings.EqualFold(host, "localhost") && (ip == nil || !ip.IsLoopback()) {
		http.Error(w, "local host required", http.StatusForbidden)
		return
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || !strings.EqualFold(u.Host, r.Host) || (u.Scheme != "http" && u.Scheme != "https") {
			http.Error(w, "cross-origin request denied", http.StatusForbidden)
			return
		}
	}
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		http.Error(w, "cross-origin request denied", http.StatusForbidden)
		return
	}
	s.mux.ServeHTTP(w, r)
}

func jsonReply(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		return
	}
}

func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	if err := page().Render(r.Context(), w); err != nil {
		return
	}
}

func (s *Server) pickFolder(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&request); err != nil {
		http.Error(w, "invalid folder picker request", http.StatusBadRequest)
		return
	}
	initial := ""
	if request.Path != "" {
		if path, err := filepath.Abs(request.Path); err == nil {
			if info, err := os.Stat(path); err == nil && info.IsDir() {
				initial = path
			}
		}
	}
	picker := s.cfg.PickFolder
	if picker == nil {
		picker = pickNativeFolder
	}
	path, err := picker(r.Context(), initial)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if path == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() || !filepath.IsAbs(path) {
		http.Error(w, "the selected folder is not available", http.StatusBadRequest)
		return
	}
	jsonReply(w, map[string]string{"path": path})
}

func pickNativeFolder(ctx context.Context, initial string) (string, error) {
	args := []string{"--file-selection", "--directory", "--title=Select a folder"}
	if initial != "" {
		args = append(args, "--filename="+strings.TrimRight(initial, string(filepath.Separator))+string(filepath.Separator))
	}
	command := exec.CommandContext(ctx, "zenity", args...)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 && stderr.Len() == 0 {
			return "", nil
		}
		return "", fmt.Errorf("could not open the folder picker (install zenity and run in a desktop session): %w %s", err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimRight(string(output), "\r\n"), nil
}

func (s *Server) discover(w http.ResponseWriter, r *http.Request) {
	base := r.URL.Query().Get("base")
	pattern := r.URL.Query().Get("pattern")
	if base == "" {
		http.Error(w, "base path required", 400)
		return
	}
	if pattern == "" {
		pattern = "*"
	}
	paths, err := service.DiscoverStudents(base, pattern)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if paths == nil {
		paths = []string{}
	}
	jsonReply(w, paths)
}

func (s *Server) exercises(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if path == "" {
		http.Error(w, "tests folder required", http.StatusBadRequest)
		return
	}
	exercises, err := service.DiscoverTestSuite(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	jsonReply(w, exercises)
}

func (s *Server) results(w http.ResponseWriter, r *http.Request) {
	items, err := s.cfg.List(r.Context())
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	jsonReply(w, items)
}

func (s *Server) summary(w http.ResponseWriter, r *http.Request) {
	items, err := s.cfg.List(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	students, summary := service.BuildReportViews(items)
	jsonReply(w, map[string]any{"students": students, "summary": summary})
}

func (s *Server) staticReports(w http.ResponseWriter, r *http.Request) {
	entries, err := os.ReadDir(s.cfg.StaticDir)
	if errors.Is(err, os.ErrNotExist) {
		jsonReply(w, []string{})
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var reports []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, err := os.Stat(filepath.Join(s.cfg.StaticDir, entry.Name(), "report.html"))
		if err == nil && info.Mode().IsRegular() {
			reports = append(reports, entry.Name())
		}
	}
	if reports == nil {
		reports = []string{}
	}
	jsonReply(w, reports)
}

func (s *Server) report(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Report == nil {
		http.NotFound(w, r)
		return
	}
	comp, err := s.cfg.Report(r.Context(), r.PathValue("student"))
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if err = comp.Render(r.Context(), w); err != nil {
		return
	}
}

func (s *Server) getStatus(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	jsonReply(w, s.status)
}

func (s *Server) run(w http.ResponseWriter, r *http.Request) {
	var cfg service.Config
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&cfg); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if cfg.TestsFolder == "" || len(cfg.Students) == 0 {
		http.Error(w, "tests folder and at least one student required", 400)
		return
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		http.Error(w, "server is closing", http.StatusServiceUnavailable)
		return
	}
	if s.status.Running {
		s.mu.Unlock()
		http.Error(w, "a run is already active", http.StatusConflict)
		return
	}
	s.status = status{Running: true, Messages: []string{"Starting run…"}, StartedAt: time.Now()}
	s.jobs.Add(1)
	s.mu.Unlock()
	go func() {
		defer s.jobs.Done()
		cfg.OnProgress = func(progress models.RunProgress) {
			s.mu.Lock()
			defer s.mu.Unlock()
			s.status.Progress = progress
		}
		err := s.cfg.Run(s.ctx, cfg, func(message string) {
			s.mu.Lock()
			defer s.mu.Unlock()
			s.status.Messages = append(s.status.Messages, message)
		})
		s.mu.Lock()
		defer s.mu.Unlock()
		s.status.Running = false
		if err != nil {
			s.status.Error = err.Error()
		} else {
			s.status.Messages = append(s.status.Messages, "Run complete.")
		}
	}()
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) upload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 512<<20)
	if err := r.ParseMultipartForm(16 << 20); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	defer func() { _ = r.MultipartForm.RemoveAll() }()
	files := r.MultipartForm.File["files"]
	paths := r.MultipartForm.Value["paths"]
	if len(files) == 0 || len(files) != len(paths) {
		http.Error(w, "files and relative paths required", 400)
		return
	}
	dir, err := os.MkdirTemp(s.uploadDir, "folder-*")
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.RemoveAll(dir)
		}
	}()
	roots := map[string]bool{}
	for i, h := range files {
		p := strings.ReplaceAll(paths[i], "\\", "/")
		clean := filepath.Clean(filepath.FromSlash(p))
		if p == "" || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			http.Error(w, "invalid upload path", 400)
			return
		}
		parts := strings.Split(clean, string(filepath.Separator))
		if len(parts) < 2 {
			http.Error(w, "select a folder, not individual files", 400)
			return
		}
		roots[parts[0]] = true
		dest := filepath.Join(dir, clean)
		if err = os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		src, e := h.Open()
		if e != nil {
			http.Error(w, e.Error(), 400)
			return
		}
		out, e := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if e != nil {
			_ = src.Close()
			http.Error(w, e.Error(), 400)
			return
		}
		_, e = io.Copy(out, src)
		closeErr := errors.Join(src.Close(), out.Close())
		if e = errors.Join(e, closeErr); e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
	}
	names := make([]string, 0, len(roots))
	for root := range roots {
		names = append(names, filepath.Join(dir, root))
	}
	sort.Strings(names)
	ok = true
	jsonReply(w, names)
}
