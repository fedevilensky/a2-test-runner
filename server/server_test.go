package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/a-h/templ"
	"github.com/fedevilensky/a2-test-runner/models"
	"github.com/fedevilensky/a2-test-runner/service"
)

func testServer(t *testing.T) *Server {
	t.Helper()
	s, err := New(Config{StaticDir: t.TempDir(), Run: func(context.Context, service.Config, func(string)) error { return nil }, List: func(context.Context) ([]models.StudentReportData, error) { return nil, nil }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
	})
	return s
}

func TestUploadPreservesFoldersAndRejectsTraversal(t *testing.T) {
	s := testServer(t)
	upload := func(path string) *httptest.ResponseRecorder {
		t.Helper()
		var body bytes.Buffer
		m := multipart.NewWriter(&body)
		f, err := m.CreateFormFile("files", "source.cpp")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = io.WriteString(f, "content"); err != nil {
			t.Fatal(err)
		}
		if err = m.WriteField("paths", path); err != nil {
			t.Fatal(err)
		}
		if err = m.Close(); err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(http.MethodPost, "http://localhost/api/upload", &body)
		r.Header.Set("Content-Type", m.FormDataContentType())
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	w := upload("alice/exercise/source.cpp")
	if w.Code != http.StatusOK {
		t.Fatal(w.Code, w.Body.String())
	}
	var roots []string
	if err := json.Unmarshal(w.Body.Bytes(), &roots); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(roots[0], "exercise", "source.cpp"))
	if err != nil || string(data) != "content" {
		t.Fatalf("file: %s, %v", data, err)
	}
	if got := upload("../escape/file"); got.Code != http.StatusBadRequest {
		t.Fatalf("traversal: %d", got.Code)
	}
}

func TestRejectCrossOriginAndForeignHost(t *testing.T) {
	s := testServer(t)
	for _, tc := range []struct{ url, origin string }{{"http://example.com/api/status", ""}, {"http://localhost/api/status", "https://evil.example"}} {
		r := httptest.NewRequest(http.MethodGet, tc.url, nil)
		r.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != http.StatusForbidden {
			t.Fatalf("%s: %d", tc.url, w.Code)
		}
	}
}

func TestDiscoverStudents(t *testing.T) {
	s := testServer(t)
	base := t.TempDir()
	if err := os.Mkdir(filepath.Join(base, "student-one"), 0o700); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, "http://localhost/api/discover?base="+base+"&pattern=student*", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "student-one") {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestRunHTTPPreservesSelectionsAndBuildOverrides(t *testing.T) {
	received := make(chan service.Config, 1)
	finish := make(chan struct{})
	s, err := New(Config{StaticDir: t.TempDir(), Run: func(_ context.Context, cfg service.Config, progress func(string)) error {
		received <- cfg
		progress("Running case")
		<-finish
		return nil
	}, List: func(context.Context) ([]models.StudentReportData, error) { return nil, nil }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	payload := `{"Timeout":12000000000,"TestsFolder":"/tests","Students":[{"Path":"/students/alice","Build":{"Standard":"c++20","Optimization":"-O3","Extra":["-Wall"]}}],"Selection":{"Exercises":{"2":["one.in.txt"]}},"Build":{"Standard":"c++11","Optimization":"-O0"}}`
	r := httptest.NewRequest(http.MethodPost, "http://localhost/api/run", strings.NewReader(payload))
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusAccepted {
		t.Fatalf("run: %d %s", w.Code, w.Body.String())
	}
	cfg := <-received
	if cfg.Timeout != 12*time.Second || cfg.TestsFolder != "/tests" || len(cfg.Students) != 1 || cfg.Students[0].Build.Optimization != "-O3" || cfg.Students[0].Build.Standard != "c++20" || len(cfg.Selection.Exercises[2]) != 1 || cfg.Selection.Exercises[2][0] != "one.in.txt" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	r = httptest.NewRequest(http.MethodPost, "http://localhost/api/run", strings.NewReader(payload))
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("concurrent run status: %d", w.Code)
	}
	close(finish)
}

func TestCloseCancelsAndWaitsForRun(t *testing.T) {
	started, canceled, release, closed := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan error, 1)
	s, err := New(Config{StaticDir: t.TempDir(), Run: func(ctx context.Context, _ service.Config, _ func(string)) error {
		close(started)
		<-ctx.Done()
		close(canceled)
		<-release
		return ctx.Err()
	}})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "http://localhost/api/run", strings.NewReader(`{"TestsFolder":"/tests","Students":[{"Path":"/students/a"}]}`))
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusAccepted {
		t.Fatal(w.Code)
	}
	<-started
	go func() { closed <- s.Close() }()
	<-canceled
	if _, err := os.Stat(s.uploadDir); err != nil {
		t.Fatalf("uploads removed while run active: %v", err)
	}
	select {
	case err := <-closed:
		t.Fatalf("Close returned while run active: %v", err)
	default:
	}
	close(release)
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.uploadDir); !os.IsNotExist(err) {
		t.Fatalf("uploads remain after close: %v", err)
	}
}

func TestReportRouteUsesStudentName(t *testing.T) {
	var got string
	s, err := New(Config{StaticDir: t.TempDir(), Report: func(_ context.Context, id string) (templ.Component, error) { got = id; return templ.Raw("report"), nil }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	r := httptest.NewRequest(http.MethodGet, "http://localhost/reports/Alice%20Smith", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusOK || got != "Alice Smith" || w.Body.String() != "report" {
		t.Fatalf("status=%d name=%q body=%q", w.Code, got, w.Body.String())
	}
}

func TestNativeFolderSelection(t *testing.T) {
	s := testServer(t)
	selected := t.TempDir()
	var starts []string
	s.cfg.PickFolder = func(_ context.Context, initial string) (string, error) {
		starts = append(starts, initial)
		if len(starts) == 1 {
			return selected, nil
		}
		return "", nil
	}
	for _, tc := range []struct {
		path   string
		status int
	}{
		{selected, http.StatusOK},
		{filepath.Join(selected, "missing"), http.StatusNoContent},
		{"", http.StatusNoContent},
	} {
		payload, err := json.Marshal(map[string]string{"path": tc.path})
		if err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "http://localhost/api/pick-folder", bytes.NewReader(payload)))
		if w.Code != tc.status {
			t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
		}
		if tc.status == http.StatusOK {
			var result map[string]string
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result["path"] != selected {
				t.Fatalf("selected path = %q", result["path"])
			}
		}
	}
	if len(starts) != 3 || starts[0] != selected || starts[1] != "" || starts[2] != "" {
		t.Fatalf("initial paths = %q", starts)
	}
}

func TestExerciseDiscoveryReturnsInputFiles(t *testing.T) {
	s := testServer(t)
	root := t.TempDir()
	for _, directory := range []string{"exercise12", "prueba2", "notes"} {
		if err := os.Mkdir(filepath.Join(root, directory), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"large.in.txt", "large.out.txt", "small.in.txt"} {
		if err := os.WriteFile(filepath.Join(root, "exercise12", name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	request := httptest.NewRequest(http.MethodGet, "http://localhost/api/exercises?"+url.Values{"path": {root}}.Encode(), nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, request)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", w.Code, w.Body.String())
	}
	var exercises []models.AvailableExercise
	if err := json.Unmarshal(w.Body.Bytes(), &exercises); err != nil {
		t.Fatal(err)
	}
	if len(exercises) != 2 || exercises[0].Number != 2 || exercises[1].Number != 12 || exercises[1].Folder != "exercise12" || len(exercises[1].Cases) != 2 || exercises[1].Cases[0] != "large.in.txt" || exercises[1].Cases[1] != "small.in.txt" {
		t.Fatalf("unexpected exercises: %#v", exercises)
	}
}

func TestStaticReportsIncludeExistingFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "older student"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "older student", "report.html"), []byte("report"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "empty"), 0o700); err != nil {
		t.Fatal(err)
	}
	s, err := New(Config{StaticDir: root})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://localhost/api/static-reports", nil))
	var reports []string
	if err := json.Unmarshal(w.Body.Bytes(), &reports); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusOK || len(reports) != 1 || reports[0] != "older student" {
		t.Fatalf("status %d reports %#v", w.Code, reports)
	}
}

func TestSummaryUsesStoredResults(t *testing.T) {
	s, err := New(Config{StaticDir: t.TempDir(), List: func(context.Context) ([]models.StudentReportData, error) {
		return []models.StudentReportData{{StudentName: "alice", Groups: []models.ExerciseGroup{{Exercise: 1, Results: []models.TestCaseResult{{Passed: true}}}}}}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://localhost/api/summary", nil))
	var response struct {
		Students []models.StudentReportData `json:"students"`
		Summary  models.SummaryReport       `json:"summary"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusOK || len(response.Students) != 1 || response.Students[0].Passed != 1 || len(response.Summary.Rows) != 1 || response.Summary.Rows[0].AllPass != 1 {
		t.Fatalf("unexpected summary: status %d, %+v", w.Code, response)
	}
}
