package media

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Status struct {
	Generation int     `json:"generation"`
	State      string  `json:"state"`
	Options    Options `json:"options"`
	Processed  float64 `json:"processed"`
	Available  float64 `json:"available"`
	Speed      string  `json:"speed"`
	Error      string  `json:"error"`
	Output     *Info   `json:"output,omitempty"`
	Bytes      int64   `json:"bytes"`
}

type Manager struct {
	Path         string
	Dir          string
	Source       *Info
	Capabilities Capabilities
	op           sync.Mutex
	mu           sync.Mutex
	status       Status
	cancel       context.CancelFunc
	done         chan struct{}
	file         *os.File
	closed       bool
}

// NewManager probes one source and reserves its adjacent, owned transcode directory.
func NewManager(path string) (*Manager, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}

	info, err := Probe(context.Background(), path)
	if err != nil {
		return nil, err
	}

	if info.Duration() <= 0 {
		return nil, fmt.Errorf("source must have a known, positive duration")
	}

	hasVideo := false
	for _, stream := range info.Streams {
		if stream.CodecType == "video" && stream.Disposition["attached_pic"] != 1 {
			hasVideo = true
		}
	}

	if !hasVideo {
		return nil, fmt.Errorf("source has no playable video track")
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, fmt.Errorf("source is already in use by another watchd instance: %w", err)
	}

	manager := &Manager{
		Path:   path,
		Dir:    path + ".transcode",
		Source: info,
		file:   file,
		status: Status{State: "idle"},
	}

	ok := false
	defer func() {
		if !ok {
			file.Close()
		}
	}()

	for _, kind := range []string{"encoders", "filters"} {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)

		data, err := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-"+kind).Output()
		cancel()
		if err != nil {
			return nil, fmt.Errorf("inspect FFmpeg %s: %w", kind, err)
		}

		names := map[string]bool{}
		for _, line := range strings.Split(string(data), "\n") {
			fields := strings.Fields(line)
			if len(fields) > 1 {
				names[fields[1]] = true
			}
		}

		if kind == "encoders" {
			for _, name := range []string{"libx264", "h264_videotoolbox", "h264_vaapi"} {
				if names[name] {
					manager.Capabilities.Encoders = append(manager.Capabilities.Encoders, name)
				}
			}
		} else {
			manager.Capabilities.TextBurn = names["subtitles"]
			manager.Capabilities.ToneMap = names["zscale"] && names["tonemap"]
		}
	}

	if stat, err := os.Lstat(manager.Dir); err == nil {
		if !stat.IsDir() || stat.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("transcode path exists and is not an owned directory")
		}

		marker, err := os.ReadFile(filepath.Join(manager.Dir, ".watchd"))
		if err != nil || string(marker) != "watchd-v1\n"+path {
			return nil, fmt.Errorf("refusing to remove unrecognized directory %s", manager.Dir)
		}

		if err := os.RemoveAll(manager.Dir); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	if err := os.Mkdir(manager.Dir, 0700); err != nil {
		return nil, err
	}

	if err := os.WriteFile(filepath.Join(manager.Dir, ".watchd"), []byte("watchd-v1\n"+path), 0600); err != nil {
		return nil, err
	}

	ok = true
	return manager, nil
}

// Start replaces the single transcode output after validating all user selections.
func (m *Manager) Start(options Options) error {
	args, err := Arguments(m.Source, options, m.Capabilities)
	if err != nil {
		return err
	}

	m.op.Lock()
	defer m.op.Unlock()

	if m.closed {
		return fmt.Errorf("media manager is closed")
	}

	m.stop()

	entries, err := os.ReadDir(m.Dir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.Name() != ".watchd" {
			if err := os.RemoveAll(filepath.Join(m.Dir, entry.Name())); err != nil {
				return err
			}
		}
	}

	if err := os.Symlink(m.Path, filepath.Join(m.Dir, "source")); err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.done = make(chan struct{})
	m.mu.Lock()
	m.status = Status{
		Generation: m.status.Generation + 1,
		State:      "starting",
		Options:    options,
	}
	m.mu.Unlock()
	go m.run(ctx, args, options, m.done)
	return nil
}

// stop waits for FFmpeg to finish before any output files can be replaced.
func (m *Manager) stop() {
	if m.cancel != nil {
		m.cancel()
		<-m.done
		m.cancel = nil
	}
}

// Cancel preserves completed segments and stops further media generation.
func (m *Manager) Cancel() {
	m.op.Lock()
	defer m.op.Unlock()

	m.stop()
}

// Close stops child processes, removes owned output, and releases the source lock.
func (m *Manager) Close() error {
	m.op.Lock()
	defer m.op.Unlock()

	if m.closed {
		return nil
	}

	m.closed = true
	m.stop()

	err := os.RemoveAll(m.Dir)
	return errors.Join(err, m.file.Close())
}

// Snapshot reports only complete, accessible segments as available movie time.
func (m *Manager) Snapshot() Status {
	m.mu.Lock()

	status := m.status
	m.mu.Unlock()

	data, err := os.ReadFile(filepath.Join(m.Dir, "index.m3u8"))
	if err != nil {
		return status
	}

	var duration float64
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "#EXTINF:") {
			duration, _ = strconv.ParseFloat(strings.TrimSuffix(strings.TrimPrefix(line, "#EXTINF:"), ","), 64)
		} else if strings.HasPrefix(line, "segment-") && filepath.Base(line) == line {
			stat, err := os.Stat(filepath.Join(m.Dir, line))
			if err != nil {
				break
			}

			status.Available += duration
			status.Bytes += stat.Size()
			duration = 0
		}
	}

	status.Available = min(status.Available, m.Source.Duration())
	return status
}

// run extracts optional text subtitles and executes one sequential FFmpeg job.
func (m *Manager) run(ctx context.Context, args []string, options Options, done chan struct{}) {
	defer close(done)

	var failure error

	var output tailOutput
	defer func() {
		// Finalize a partial playlist so cancelled or failed output remains playable.
		path := filepath.Join(m.Dir, "index.m3u8")
		if data, err := os.ReadFile(path); err == nil && !strings.Contains(string(data), "#EXT-X-ENDLIST") {
			data = append(data, []byte("\n#EXT-X-ENDLIST\n")...)
			if err := os.WriteFile(path+".tmp", data, 0600); err == nil {
				_ = os.Rename(path+".tmp", path)
			}
		}

		m.mu.Lock()
		defer m.mu.Unlock()

		switch {
		case ctx.Err() != nil:
			m.status.State = "cancelled"
		case failure != nil:
			m.status.State = "failed"
			m.status.Error = failure.Error() + "\n" + string(output.data)
		default:
			m.status.State = "complete"
		}
	}()

	if options.SubtitleMode == "webvtt" {
		cmd := exec.CommandContext(
			ctx,
			"ffmpeg",
			"-nostdin",
			"-hide_banner",
			"-loglevel", "error",
			"-y",
			"-i", "source",
			"-map", "0:"+strconv.Itoa(options.SubtitleTrack),
			"-c:s", "webvtt",
			"-f", "webvtt",
			"subtitles.tmp",
		)
		cmd.Dir = m.Dir
		cmd.Stderr = &output
		if failure = cmd.Run(); failure != nil {
			return
		}

		if failure = os.Rename(filepath.Join(m.Dir, "subtitles.tmp"), filepath.Join(m.Dir, "subtitles.vtt")); failure != nil {
			return
		}
	}

	if options.SubtitleMode == "burn" && m.Capabilities.TextBurn {
		if failure = os.MkdirAll(filepath.Join(m.Dir, "fonts"), 0700); failure != nil {
			return
		}

		for _, stream := range m.Source.Streams {
			if stream.CodecType != "attachment" {
				continue
			}

			name := strings.ToLower(stream.Tags["filename"])
			if !strings.HasSuffix(name, ".ttf") && !strings.HasSuffix(name, ".otf") {
				continue
			}

			font := fmt.Sprintf("fonts/%d%s", stream.Index, filepath.Ext(name))

			cmd := exec.CommandContext(
				ctx,
				"ffmpeg",
				"-nostdin",
				"-loglevel", "error",
				"-y",
				"-dump_attachment:"+strconv.Itoa(stream.Index),
				font,
				"-i", "source",
				"-t", "0",
				"-f", "null",
				"-",
			)
			cmd.Dir = m.Dir
			cmd.Stderr = &output
			if failure = cmd.Run(); failure != nil {
				return
			}
		}
	}

	command := append([]string{
		"-nostdin",
		"-hide_banner",
		"-loglevel", "warning",
		"-y",
		"-progress", "pipe:1",
		"-stats_period", "0.5",
		"-i", "source",
	}, args...)

	cmd := exec.CommandContext(ctx, "ffmpeg", command...)
	cmd.Dir = m.Dir
	cmd.Stderr = &output
	cmd.Cancel = func() error {
		return cmd.Process.Signal(os.Interrupt)
	}
	cmd.WaitDelay = 3 * time.Second

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		failure = err
		return
	}

	if failure = cmd.Start(); failure != nil {
		return
	}

	m.mu.Lock()
	m.status.State = "running"
	m.mu.Unlock()

	scanner := bufio.NewScanner(stdout)

	probed := false
	for scanner.Scan() {
		key, value, _ := strings.Cut(scanner.Text(), "=")
		m.mu.Lock()
		switch key {
		case "out_time_us":
			microseconds, _ := strconv.ParseFloat(value, 64)
			m.status.Processed = max(0, min(microseconds/1e6, m.Source.Duration()))
		case "speed":
			m.status.Speed = strings.TrimSpace(value)
		}

		m.mu.Unlock()
		if key == "progress" && !probed {
			segment := filepath.Join(m.Dir, "segment-000000.ts")
			if _, err := os.Stat(segment); err == nil {
				info, err := Probe(ctx, segment)
				if err == nil {
					m.mu.Lock()
					m.status.Output = info
					m.mu.Unlock()
					probed = true
				}
			}
		}
	}

	failure = errors.Join(cmd.Wait(), scanner.Err())
}

type tailOutput struct{ data []byte }

// Write retains a bounded diagnostic tail without allowing FFmpeg logs to fill memory.
func (w *tailOutput) Write(data []byte) (int, error) {
	w.data = append(w.data, data...)
	if len(w.data) > 16384 {
		w.data = w.data[len(w.data)-16384:]
	}

	return len(data), nil
}
