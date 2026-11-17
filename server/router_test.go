package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/okppop/watchd/config"
	"go.uber.org/zap"
)

func TestAuthenticationAndMediaRoutes(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("FFmpeg is not installed")
	}

	path := filepath.Join(t.TempDir(), "source.mp4")

	cmd := exec.Command(
		"ffmpeg",
		"-nostdin",
		"-loglevel", "error",
		"-y",
		"-f", "lavfi",
		"-i", "color=size=160x90:rate=12",
		"-t", "2",
		"-c:v", "libx264",
		path,
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture: %v %s", err, output)
	}

	server, err := NewServer(config.Config{
		Path:     path,
		Password: "secret",
	}, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		_ = server.Close()
	})

	host := httptest.NewServer(server.Handler)
	defer host.Close()

	response, err := http.Post(host.URL+"/api/login", "application/json", bytes.NewBufferString(`{"password":"secret"}`))
	if err != nil {
		t.Fatal(err)
	}

	cookies := response.Cookies()
	response.Body.Close()
	if response.StatusCode != 204 || len(cookies) != 1 {
		t.Fatal("login did not set a session")
	}

	for _, test := range []struct {
		name, method, path, body, byteRange string
		auth                                bool
		status                              int
	}{
		{
			name:      "login rejection",
			method:    "POST",
			path:      "/api/login",
			body:      `{"password":"wrong"}`,
			byteRange: "",
			auth:      false,
			status:    403,
		},
		{
			name:      "source protected",
			method:    "GET",
			path:      "/room/source",
			body:      "",
			byteRange: "",
			auth:      false,
			status:    403,
		},
		{
			name:      "metadata protected",
			method:    "GET",
			path:      "/room/info",
			body:      "",
			byteRange: "",
			auth:      false,
			status:    403,
		},
		{
			name:      "output protected",
			method:    "GET",
			path:      "/room/transcode/1/index.m3u8",
			body:      "",
			byteRange: "",
			auth:      false,
			status:    403,
		},
		{
			name:      "source range",
			method:    "GET",
			path:      "/room/source",
			body:      "",
			byteRange: "bytes=0-99",
			auth:      true,
			status:    206,
		},
		{
			name:      "source metadata",
			method:    "GET",
			path:      "/room/info",
			body:      "",
			byteRange: "",
			auth:      true,
			status:    200,
		},
		{
			name:      "reject invalid track",
			method:    "POST",
			path:      "/room/transcode",
			body:      `{"videoTrack":999,"videoCodec":"h264","audioCodec":"off","subtitleMode":"off"}`,
			byteRange: "",
			auth:      true,
			status:    400,
		},
		{
			name:      "do not serve source symlink",
			method:    "GET",
			path:      "/room/transcode/1/source",
			body:      "",
			byteRange: "",
			auth:      true,
			status:    404,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			request, err := http.NewRequest(test.method, host.URL+test.path, bytes.NewBufferString(test.body))
			if err != nil {
				t.Fatal(err)
			}

			request.Header.Set("Content-Type", "application/json")
			if test.byteRange != "" {
				request.Header.Set("Range", test.byteRange)
			}

			if test.auth {
				request.AddCookie(cookies[0])
			}

			response, err := http.DefaultClient.Do(request)
			if err != nil {
				t.Fatal(err)
			}

			defer response.Body.Close()

			if response.StatusCode != test.status {
				t.Fatalf("got %d, want %d", response.StatusCode, test.status)
			}
		})
	}

	for _, authenticated := range []bool{false, true} {
		t.Run(map[bool]string{
			false: "websocket protected",
			true:  "websocket authenticated",
		}[authenticated], func(t *testing.T) {
			headers := http.Header{}
			if authenticated {
				headers.Set("Cookie", cookies[0].String())
			}

			connection, response, err := websocket.DefaultDialer.Dial("ws"+host.URL[4:]+"/room/ws", headers)
			if !authenticated {
				if err == nil || response.StatusCode != 403 {
					t.Fatal("unauthenticated websocket accepted")
				}

				response.Body.Close()
				return
			}

			if err != nil {
				t.Fatal(err)
			}

			defer connection.Close()

			var welcome map[string]json.RawMessage
			if err := connection.ReadJSON(&welcome); err != nil {
				t.Fatal(err)
			}

			if string(welcome["type"]) != `"welcome"` {
				t.Fatal("missing welcome message")
			}
		})
	}
}
