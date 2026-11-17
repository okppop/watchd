package media

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestManagerLifecycle(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("FFmpeg is not installed")
	}

	for _, scenario := range []string{"complete", "cancel", "unowned-directory", "stale-owned-directory"} {
		t.Run(scenario, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "movie ' sample.mp4")

			cmd := exec.Command(
				"ffmpeg",
				"-nostdin",
				"-hide_banner",
				"-loglevel", "error",
				"-y",
				"-f", "lavfi",
				"-i", "testsrc2=size=160x90:rate=12",
				"-t", "8",
				"-c:v", "libx264",
				"-preset", "ultrafast",
				path,
			)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("fixture: %v: %s", err, output)
			}

			if strings.Contains(scenario, "directory") {
				if err := os.Mkdir(path+".transcode", 0700); err != nil {
					t.Fatal(err)
				}

				if scenario == "stale-owned-directory" {
					if err := os.WriteFile(filepath.Join(path+".transcode", ".watchd"), []byte("watchd-v1\n"+path), 0600); err != nil {
						t.Fatal(err)
					}
				} else {
					if err := os.WriteFile(filepath.Join(path+".transcode", "keep.txt"), []byte("keep me"), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}

			manager, err := NewManager(path)
			if scenario == "unowned-directory" {
				if err == nil {
					manager.Close()
					t.Fatal("unowned directory was accepted")
				}

				if _, err := os.Stat(filepath.Join(path+".transcode", "keep.txt")); err != nil {
					t.Fatal("unrelated file was removed")
				}

				return
			}

			if err != nil {
				t.Fatal(err)
			}

			t.Cleanup(func() {
				_ = manager.Close()
			})
			if other, err := NewManager(path); err == nil {
				other.Close()
				t.Fatal("duplicate manager acquired the same source")
			}

			options := Options{
				VideoTrack:   0,
				VideoCodec:   "h264",
				Encoder:      "libx264",
				AudioCodec:   "off",
				SubtitleMode: "off",
			}
			if err := manager.Start(options); err != nil {
				t.Fatal(err)
			}

			if scenario == "cancel" {
				manager.Cancel()
			}

			deadline := time.Now().Add(20 * time.Second)

			var status Status
			for time.Now().Before(deadline) {
				status = manager.Snapshot()
				if status.State == "complete" || status.State == "failed" || status.State == "cancelled" {
					break
				}

				time.Sleep(20 * time.Millisecond)
			}

			if scenario == "cancel" {
				if status.State != "cancelled" {
					t.Fatalf("got %s", status.State)
				}
			} else {
				if status.State != "complete" || status.Available < 7.5 || status.Output == nil {
					t.Fatalf("incomplete transcode: %+v", status)
				}

				if _, err := Probe(t.Context(), filepath.Join(manager.Dir, "index.m3u8")); err != nil {
					t.Fatal(err)
				}
			}

			if err := manager.Close(); err != nil {
				t.Fatal(err)
			}

			if _, err := os.Stat(path + ".transcode"); !os.IsNotExist(err) {
				t.Fatalf("output was not cleaned up: %v", err)
			}

			if _, err := os.Stat(path); err != nil {
				t.Fatal("source was removed")
			}
		})
	}
}
