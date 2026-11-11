package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/charmbracelet/x/ansi"
	"golang.org/x/term"
)

type runProgress struct {
	mu                          sync.Mutex
	out                         io.Writer
	terminal                    bool
	fd                          int
	bar                         progress.Model
	total, done                 int
	studentsTotal, studentsDone int
	activity                    string
	started                     time.Time
	stop                        chan struct{}
	finished                    chan struct{}
	once                        sync.Once
}

func newRunProgress(out *os.File, total, studentsTotal int) *runProgress {
	p := &runProgress{out: out, fd: int(out.Fd()), terminal: term.IsTerminal(int(out.Fd())) && os.Getenv("TERM") != "dumb", total: total, studentsTotal: studentsTotal, started: time.Now(), stop: make(chan struct{}), finished: make(chan struct{}), bar: progress.New(progress.WithWidth(14), progress.WithoutPercentage(), progress.WithSolidFill("#6d9bce"))}
	if p.terminal {
		go func() {
			defer close(p.finished)
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					p.mu.Lock()
					p.render()
					p.mu.Unlock()
				case <-p.stop:
					return
				}
			}
		}()
	} else {
		close(p.finished)
	}
	return p
}

func safeConsoleText(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
}

func (p *runProgress) start(activity string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.activity = safeConsoleText(activity)
	p.render()
}

func (p *runProgress) advance(n int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.done += n
	if p.done > p.total {
		p.done = p.total
	}
	if p.terminal {
		p.render()
	}
}

func (p *runProgress) studentFinished(name string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.studentsDone < p.studentsTotal {
		p.studentsDone++
	}
	p.activity = "Finished " + safeConsoleText(name)
	p.render()
}

func (p *runProgress) render() {
	if !p.terminal {
		fmt.Fprintf(p.out, "[%d/%d] Students %d/%d (%d pending) · %s\n", p.done, p.total, p.studentsDone, p.studentsTotal, p.studentsTotal-p.studentsDone, p.activity)
		return
	}
	fraction := 1.0
	if p.total > 0 {
		fraction = float64(p.done) / float64(p.total)
	}
	line := fmt.Sprintf("%s %d/%d  Students %d/%d (%d pending)  %s  %s", p.bar.ViewAs(fraction), p.done, p.total, p.studentsDone, p.studentsTotal, p.studentsTotal-p.studentsDone, time.Since(p.started).Round(time.Second), p.activity)
	width, _, err := term.GetSize(p.fd)
	if err == nil && width > 1 {
		line = ansi.Truncate(line, width-1, "…")
	}
	fmt.Fprintf(p.out, "\r\x1b[2K%s", line)
}

func (p *runProgress) message(format string, args ...any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.terminal {
		fmt.Fprint(p.out, "\r\x1b[2K")
	}
	fmt.Fprintf(p.out, format, args...)
	if p.terminal {
		p.render()
	}
}

func (p *runProgress) close() {
	p.once.Do(func() {
		close(p.stop)
		<-p.finished
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.terminal {
			fmt.Fprint(p.out, "\r\x1b[2K")
		}
	})
}

func printConsoleSummary(allReports []studentReport, summary summaryReport) {
	header := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("75")).Padding(0, 1)
	cell := lipgloss.NewStyle().Padding(0, 1)
	style := func(row, _ int) lipgloss.Style {
		if row == table.HeaderRow {
			return header
		}
		return cell
	}
	students := table.New().Headers("Student", "Total", "Passed", "Failed", "Compile", "Runtime", "Diff", "Missing").StyleFunc(style)
	for _, r := range allReports {
		students.Row(safeConsoleText(r.StudentName), fmt.Sprint(r.Total), fmt.Sprint(r.Passed), fmt.Sprint(r.Failed), fmt.Sprint(r.CompileFails), fmt.Sprint(r.RuntimeFails), fmt.Sprint(r.DiffFails), fmt.Sprint(r.MissingFails))
	}
	exercises := table.New().Headers("Exercise", "Students", "All pass", "Partial", "All fail", "Compile fail", "Not submitted").StyleFunc(style)
	for _, row := range summary.Rows {
		exercises.Row(fmt.Sprint(row.Exercise), fmt.Sprint(row.Total), fmt.Sprint(row.AllPass), fmt.Sprint(row.Partial), fmt.Sprint(row.AllFail), fmt.Sprint(row.CompileFail), fmt.Sprint(row.NotSubmitted))
	}
	if width, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && width > 0 {
		students.Width(width)
		exercises.Width(width)
	}
	fmt.Println("\nPer student results")
	fmt.Println(students.String())
	for _, r := range allReports {
		fmt.Printf("%s report: %s\n", safeConsoleText(r.StudentName), safeConsoleText(filepath.Join(reportRootFolder, sanitizeName(r.StudentName), "report.html")))
	}
	fmt.Println("\nGlobal summary by exercise")
	fmt.Println(exercises.String())
	fmt.Printf("Summary report: %s\n", filepath.Join(reportRootFolder, summarySubfolder, "index.html"))
}
