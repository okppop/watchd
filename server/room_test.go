package server

import (
	"testing"
	"time"

	"github.com/okppop/watchd/media"
)

func TestRoomBuffering(t *testing.T) {
	for _, test := range []struct {
		name         string
		source       string
		available    float64
		state        string
		secondBuffer float64
		secondState  string
		wantPlay     bool
		reason       string
		playing      bool
	}{
		{
			name:         "both ready",
			source:       "original",
			available:    0,
			state:        "idle",
			secondBuffer: 20,
			secondState:  "ready",
			wantPlay:     true,
			reason:       "ready",
			playing:      true,
		},
		{
			name:         "slow viewer",
			source:       "original",
			available:    0,
			state:        "idle",
			secondBuffer: 1,
			secondState:  "buffering",
			wantPlay:     true,
			reason:       "viewers",
			playing:      false,
		},
		{
			name:         "gesture required",
			source:       "original",
			available:    0,
			state:        "idle",
			secondBuffer: 20,
			secondState:  "gesture",
			wantPlay:     true,
			reason:       "viewers",
			playing:      false,
		},
		{
			name:         "manual pause wins",
			source:       "original",
			available:    0,
			state:        "idle",
			secondBuffer: 20,
			secondState:  "ready",
			wantPlay:     false,
			reason:       "paused",
			playing:      false,
		},
		{
			name:         "server behind",
			source:       "transcoded",
			available:    1,
			state:        "running",
			secondBuffer: 20,
			secondState:  "ready",
			wantPlay:     true,
			reason:       "transcoding",
			playing:      false,
		},
		{
			name:         "partial end",
			source:       "transcoded",
			available:    0,
			state:        "cancelled",
			secondBuffer: 20,
			secondState:  "ready",
			wantPlay:     true,
			reason:       "partial_end",
			playing:      false,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			room := NewRoom(100)
			room.Connect("a", "Alice", "127.0.0.1:1000")
			room.Connect("b", "Bob", "127.0.0.1:2000")
			room.state.Source = test.source
			room.state.WantPlay = test.wantPlay
			for _, id := range []string{"a", "b"} {
				member := room.members[id]
				member.Joined = true
				member.State = "ready"
				member.Epoch = room.state.Epoch
				member.Buffered = []TimeRange{{0, 20}}
			}

			room.members["b"].Buffered = []TimeRange{{0, test.secondBuffer}}
			room.members["b"].State = test.secondState

			state := room.Snapshot(media.Status{
				State:     test.state,
				Available: test.available,
			})
			if state.Reason != test.reason || state.Playing != test.playing {
				t.Fatalf("got %s playing=%v", state.Reason, state.Playing)
			}
		})
	}
}

func TestRoomCommands(t *testing.T) {
	for _, test := range []struct {
		name         string
		command      Message
		wantPosition float64
		wantEpoch    int
		wantError    bool
	}{
		{
			name: "seek clamps",
			command: Message{
				Type:     "seek",
				Position: 200,
			},
			wantPosition: 100,
			wantEpoch:    2,
			wantError:    false,
		},
		{
			name: "source invalidation",
			command: Message{
				Type:   "source",
				Source: "transcoded",
			},
			wantPosition: 0,
			wantEpoch:    2,
			wantError:    false,
		},
		{
			name: "invalid source",
			command: Message{
				Type:   "source",
				Source: "remote-url",
			},
			wantPosition: 0,
			wantEpoch:    1,
			wantError:    true,
		},
		{
			name:         "pause",
			command:      Message{Type: "pause"},
			wantPosition: 0,
			wantEpoch:    1,
			wantError:    false,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			room := NewRoom(100)
			room.Connect("a", "Alice", "[::1]:1234")

			err := room.Command("a", test.command, media.Status{Generation: 1})
			if (err != nil) != test.wantError {
				t.Fatalf("unexpected error: %v", err)
			}

			if room.state.Position != test.wantPosition || room.state.Epoch != test.wantEpoch {
				t.Fatalf("unexpected state: %+v", room.state)
			}

			room.members["a"].LastSeen = time.Now().Add(-11 * time.Second)
			if len(room.Snapshot(media.Status{}).Members) != 0 {
				t.Fatal("stale viewer was not removed")
			}
		})
	}
}

func TestLastViewerDisconnectFreezesTimeline(t *testing.T) {
	for _, gap := range []time.Duration{time.Minute, time.Hour} {
		t.Run(gap.String(), func(t *testing.T) {
			room := NewRoom(7200)
			room.Connect("a", "Alice", "127.0.0.1:1000")
			room.members["a"].Joined = true
			room.state.Position = 10
			room.state.WantPlay = true
			room.state.Playing = true
			room.Disconnect("a")

			position := room.state.Position
			room.updated = time.Now().Add(-gap)

			state := room.Snapshot(media.Status{})
			if state.Position != position || state.Playing {
				t.Fatalf("timeline advanced without viewers: %+v", state)
			}
		})
	}
}
