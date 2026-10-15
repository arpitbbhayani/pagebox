package ui

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"
)

const (
	Accent = "208"
	Ok     = "71"
	Err    = "167"
	Warn   = "179"
	Info   = "110"
	Dim    = "245"
	Faint  = "240"
)

var (
	forcedColor *bool
	quiet       bool
	assumeYes   bool
	stdinReader = bufio.NewReader(os.Stdin)
	stdinMu     sync.Mutex
)

func SetColor(enabled *bool) {
	forcedColor = enabled
}

func SetQuiet(enabled bool) {
	quiet = enabled
}

func SetYes(enabled bool) {
	assumeYes = enabled
}

func isTTY(w io.Writer) bool {
	if f, ok := w.(*os.File); ok {
		return term.IsTerminal(int(f.Fd()))
	}
	return false
}

func IsTTY(w io.Writer) bool {
	return isTTY(w)
}

func ColorEnabled(w io.Writer) bool {
	if forcedColor != nil {
		return *forcedColor
	}
	if v := os.Getenv("NO_COLOR"); v != "" {
		return false
	}
	if v := os.Getenv("FORCE_COLOR"); v != "" {
		return v != "0" && strings.ToLower(v) != "false"
	}
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	return isTTY(w)
}

func Paint(text, code string, bold bool, w io.Writer) string {
	if text == "" || !ColorEnabled(w) {
		return text
	}
	prefix := ""
	if bold {
		prefix = "\033[1m"
	}
	return fmt.Sprintf("%s\033[38;5;%sm%s\033[0m", prefix, code, text)
}

func dim(t string, w io.Writer) string    { return Paint(t, Dim, false, w) }
func faint(t string, w io.Writer) string  { return Paint(t, Faint, false, w) }
func accent(t string, w io.Writer) string { return Paint(t, Accent, false, w) }
func strong(t string, w io.Writer) string {
	if ColorEnabled(w) {
		return fmt.Sprintf("\033[1m%s\033[0m", t)
	}
	return t
}

func PrintOk(msg, detail string, w io.Writer) {
	if w == nil {
		w = os.Stdout
	}
	if !quiet {
		status("ok", msg, detail, 0, w)
	}
}

func PrintInfo(msg, detail string, w io.Writer) {
	if w == nil {
		w = os.Stdout
	}
	if !quiet {
		status("info", msg, detail, 0, w)
	}
}

func PrintErr(msg, detail string, w io.Writer) {
	if w == nil {
		w = os.Stderr
	}
	status("err", msg, detail, 0, w)
}

func PrintWarn(msg, detail string, w io.Writer) {
	if w == nil {
		w = os.Stderr
	}
	status("warn", msg, detail, 0, w)
}

func PrintStep(msg, detail string, w io.Writer) {
	if w == nil {
		w = os.Stdout
	}
	if !quiet {
		status("step", msg, detail, 0, w)
	}
}


type glyphDef struct {
	mark     string
	fallback string
	code     string
}

var glyphs = map[string]glyphDef{
	"ok":   {"✓", "[OK]", Ok},
	"err":  {"✗", "[FAIL]", Err},
	"warn": {"!", "[WARN]", Warn},
	"info": {"·", "[INFO]", Info},
	"step": {"›", ">", Accent},
}

func getGlyph(role string, w io.Writer) string {
	g := glyphs[role]
	mark := g.fallback
	if isTTY(w) {
		mark = g.mark
	}
	return Paint(mark, g.code, false, w)
}

func status(role, message, detail string, width int, w io.Writer) {
	msg := message
	if width > 0 {
		msg = fmt.Sprintf("%-*s", width, message)
	}
	line := fmt.Sprintf("%s %s", getGlyph(role, w), msg)
	if detail != "" {
		line += fmt.Sprintf("  %s", dim(detail, w))
	}
	fmt.Fprintln(w, line)
}

func Heading(text string, count *int, w io.Writer) {
	if w == nil {
		w = os.Stdout
	}
	if quiet {
		return
	}
	line := strong(text, w)
	if count != nil {
		line += fmt.Sprintf(" %s", dim(fmt.Sprintf("(%d)", *count), w))
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, line)
}

func Rule(w io.Writer) {
	if w == nil {
		w = os.Stdout
	}
	if quiet || !isTTY(w) {
		return
	}
	// Default to 80 for width if terminal size can't be fetched
	width := 80
	if f, ok := w.(*os.File); ok {
		if w, _, err := term.GetSize(int(f.Fd())); err == nil && w < 80 {
			width = w
		}
	}
	fmt.Fprintln(w, faint(strings.Repeat("─", width), w))
}

func KV(label, value string, width int, w io.Writer) {
	if w == nil {
		w = os.Stdout
	}
	if quiet {
		return
	}
	text := fmt.Sprintf("%s:", label)
	if width > 0 {
		text = fmt.Sprintf("%-*s:", width, label)
	}
	fmt.Fprintf(w, "  %s %s\n", dim(text, w), value)
}

func Bullet(text string, w io.Writer) {
	if w == nil {
		w = os.Stdout
	}
	if quiet {
		return
	}
	fmt.Fprintf(w, "  %s %s\n", faint("·", w), text)
}

func Hint(text string, w io.Writer) {
	if w == nil {
		w = os.Stdout
	}
	if quiet {
		return
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, dim(text, w))
}

func Command(text string, w io.Writer) {
	if w == nil {
		w = os.Stdout
	}
	if quiet {
		return
	}
	fmt.Fprintf(w, "  %s\n", accent(text, w))
}

// --- listings ----------------------------------------------------------------

func Widths(rows [][]interface{}) []int {
	if len(rows) == 0 {
		return []int{}
	}
	widths := make([]int, len(rows[0]))
	for _, row := range rows {
		for i, cell := range row {
			length := len(fmt.Sprintf("%v", cell))
			if length > widths[i] {
				widths[i] = length
			}
		}
	}
	return widths
}

type ColorCol struct {
	Index int
	Fn    func(string) string
}

func Row(cells []interface{}, widths []int, color *ColorCol, dims []int, w io.Writer) string {
	if w == nil {
		w = os.Stdout
	}
	out := make([]string, 0, len(cells))
	last := len(cells) - 1
	
	dimMap := make(map[int]bool)
	for _, d := range dims {
		dimMap[d] = true
	}

	for i, cell := range cells {
		text := fmt.Sprintf("%v", cell)
		if i != last && i < len(widths) {
			text = fmt.Sprintf("%-*s", widths[i], text)
		}
		
		if color != nil && i == color.Index {
			code := color.Fn(fmt.Sprintf("%v", cell))
			if code != "" {
				text = Paint(text, code, false, w)
			}
		} else if dimMap[i] {
			text = dim(text, w)
		}
		out = append(out, text)
	}
	return "  " + strings.Join(out, "  ")
}

func Table(rows [][]interface{}, color *ColorCol, dims []int, w io.Writer) {
	if w == nil {
		w = os.Stdout
	}
	if quiet {
		return
	}
	widths := Widths(rows)
	for _, cells := range rows {
		fmt.Fprintln(w, Row(cells, widths, color, dims, w))
	}
}

// Prompt asks a question.
func Prompt(text, defaultVal string) (string, error) {
	if assumeYes {
		return defaultVal, nil
	}
	suffix := ""
	if defaultVal != "" {
		suffix = fmt.Sprintf(" %s", dim(fmt.Sprintf("[%s]", defaultVal), os.Stdout))
	}
	fmt.Fprintf(os.Stdout, "%s %s%s: ", getGlyph("step", os.Stdout), text, suffix)
	
	stdinMu.Lock()
	answer, err := stdinReader.ReadString('\n')
	stdinMu.Unlock()
	if err != nil {
		fmt.Fprintln(os.Stderr)
		if defaultVal != "" {
			return defaultVal, nil
		}
		return "", fmt.Errorf("EOF")
	}
	
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return defaultVal, nil
	}
	return answer, nil
}

// Confirm asks a yes/no question.
func Confirm(question string, defaultVal bool) (bool, error) {
	if assumeYes {
		return true, nil // --yes always means true for confirmations
	}
	
	marks := "[y/N]"
	if defaultVal {
		marks = "[Y/n]"
	}
	
	for {
		fmt.Fprintf(os.Stdout, "%s %s %s ", getGlyph("step", os.Stdout), question, dim(marks, os.Stdout))
		stdinMu.Lock()
		answer, err := stdinReader.ReadString('\n')
		stdinMu.Unlock()
		if err != nil {
			fmt.Fprintln(os.Stderr)
			return false, fmt.Errorf("EOF")
		}
		
		answer = strings.ToLower(strings.TrimSpace(answer))
		if answer == "" {
			return defaultVal, nil
		}
		if answer == "y" || answer == "yes" {
			return true, nil
		}
		if answer == "n" || answer == "no" {
			return false, nil
		}
		PrintWarn("answer y or n", "", os.Stderr)
	}
}

type Spinner struct {
	message string
	mu      sync.Mutex
	stop    chan struct{}
	wg      sync.WaitGroup
	start   time.Time
}

func NewSpinner(message string) *Spinner {
	s := &Spinner{
		message: message,
		stop:    make(chan struct{}),
		start:   time.Now(),
	}
	if !isTTY(os.Stderr) {
		if !quiet {
			fmt.Fprintf(os.Stderr, "%s...\n", message)
		}
		return s
	}
	if quiet {
		return s
	}
	
	s.wg.Add(1)
	go s.run()
	return s
}

func (s *Spinner) run() {
	defer s.wg.Done()
	frames := []rune{'⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠏'}
	ticker := time.NewTicker(80 * time.Millisecond)
	defer ticker.Stop()
	
	i := 0
	for {
		select {
		case <-s.stop:
			s.erase()
			return
		case <-ticker.C:
			s.mu.Lock()
			msg := s.message
			s.mu.Unlock()
			
			elapsed := time.Since(s.start)
			timer := ""
			if elapsed >= time.Second {
				timer = fmt.Sprintf(" (%ds)", int(elapsed.Seconds()))
			}
			
			frame := frames[i%len(frames)]
			i++
			
			line := fmt.Sprintf("%s %s%s", accent(string(frame), os.Stderr), msg, dim(timer, os.Stderr))
			
			width := 80
			if w, _, err := term.GetSize(int(os.Stderr.Fd())); err == nil {
				width = w
			}
			
			// simple truncate
			fmt.Fprintf(os.Stderr, "\r%-*s", width-1, "\r"+line)
		}
	}
}

func (s *Spinner) erase() {
	width := 80
	if w, _, err := term.GetSize(int(os.Stderr.Fd())); err == nil {
		width = w
	}
	fmt.Fprintf(os.Stderr, "\r%s\r", strings.Repeat(" ", width-1))
}

func (s *Spinner) Update(message string) {
	s.mu.Lock()
	s.message = message
	s.mu.Unlock()
}

func (s *Spinner) Stop(finalOk bool) {
	if !isTTY(os.Stderr) || quiet {
		return
	}
	close(s.stop)
	s.wg.Wait()
}
