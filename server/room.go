package server

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/okppop/watchd/media"
)

var errViewerGone = errors.New("viewer is no longer connected")

type TimeRange [2]float64

type Member struct {
	ID        string      `json:"id"`
	Name      string      `json:"name"`
	Address   string      `json:"address"`
	Joined    bool        `json:"joined"`
	Connected bool        `json:"connected"`
	Position  float64     `json:"position"`
	Buffered  []TimeRange `json:"buffered"`
	State     string      `json:"state"`
	Epoch     int         `json:"epoch"`
	LastSeen  time.Time   `json:"-"`
}

type RoomState struct {
	Source     string       `json:"source"`
	Position   float64      `json:"position"`
	Playing    bool         `json:"playing"`
	WantPlay   bool         `json:"wantPlay"`
	Reason     string       `json:"reason"`
	Epoch      int          `json:"epoch"`
	Revision   int          `json:"revision"`
	ChangedBy  string       `json:"changedBy"`
	Members    []Member     `json:"members"`
	ServerTime int64        `json:"serverTime"`
	Transcode  media.Status `json:"transcode"`
}

type Message struct {
	Type       string      `json:"type"`
	Name       string      `json:"name"`
	Position   float64     `json:"position"`
	Source     string      `json:"source"`
	Buffered   []TimeRange `json:"buffered"`
	State      string      `json:"state"`
	Epoch      int         `json:"epoch"`
	ClientTime float64     `json:"clientTime"`
}

type Room struct {
	mu       sync.Mutex
	state    RoomState
	members  map[string]*Member
	duration float64
	updated  time.Time
}

// NewRoom creates the single shared movie timeline.
func NewRoom(duration float64) *Room {
	return &Room{
		state: RoomState{
			Source: "original",
			Reason: "paused",
			Epoch:  1,
		},
		members:  map[string]*Member{},
		duration: duration,
		updated:  time.Now(),
	}
}

// advance moves the authoritative timeline only while the room is actually playing.
func (r *Room) advance(now time.Time) {
	if r.state.Playing {
		r.state.Position = min(r.duration, r.state.Position+now.Sub(r.updated).Seconds())
	}

	r.updated = now
}

// Connect adds an observer; joining playback is an explicit browser gesture.
func (r *Room) Connect(id, name, address string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if name == "" {
		name = "Guest"
	}

	r.members[id] = &Member{
		ID:        id,
		Name:      name,
		Address:   address,
		Connected: true,
		State:     "observer",
		LastSeen:  time.Now(),
	}
}

// Disconnect allows a short grace period before a missing viewer stops blocking playback.
func (r *Room) Disconnect(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if member := r.members[id]; member != nil {
		r.advance(time.Now())
		if member.Joined {
			r.state.Playing = false
		}

		member.Connected = false
		member.State = "disconnected"
		member.LastSeen = time.Now()
	}
}

// Command applies explicit controls separately from passive player status reports.
func (r *Room) Command(id string, message Message, transcode media.Status) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	member := r.members[id]
	if member == nil {
		return errViewerGone
	}

	now := time.Now()
	r.advance(now)
	member.LastSeen = now
	if math.IsNaN(message.Position) || math.IsInf(message.Position, 0) {
		return fmt.Errorf("invalid movie position")
	}

	switch message.Type {
	case "report":
		if message.Epoch != r.state.Epoch {
			return nil
		}
		if len(message.Buffered) > 128 {
			return fmt.Errorf("too many buffered ranges")
		}
		for _, span := range message.Buffered {
			if span[0] < 0 || span[1] < span[0] || span[1] > r.duration+10 {
				return fmt.Errorf("invalid buffered range")
			}
		}
		member.Position = message.Position
		member.Buffered = message.Buffered
		member.State = message.State
		member.Epoch = message.Epoch
		return nil
	case "join":
		member.Joined = true
	case "leave":
		member.Joined = false
		member.State = "observer"
	case "play":
		if r.state.Position >= r.duration-0.08 {
			r.state.Position = 0
			r.state.Epoch++
		}
		r.state.WantPlay = true
	case "pause":
		r.state.WantPlay = false
		r.state.Playing = false
	case "seek":
		r.state.Position = max(0, min(message.Position, r.duration))
		r.state.Epoch++
		r.state.Playing = false
	case "source":
		if message.Source != "original" && message.Source != "transcoded" {
			return fmt.Errorf("unknown media source")
		}
		if message.Source == "transcoded" && transcode.Generation == 0 {
			return fmt.Errorf("start a transcode first")
		}
		r.state.Source = message.Source
		r.state.Epoch++
		r.state.Playing = false
	default:
		return fmt.Errorf("unknown room command")
	}

	r.state.Revision++
	r.state.ChangedBy = member.Name
	return nil
}

// ResetTranscode pauses viewers before replacing files used by the current output.
func (r *Room) ResetTranscode() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.advance(time.Now())
	if r.state.Source == "transcoded" {
		r.state.WantPlay = false
		r.state.Playing = false
		r.state.Epoch++
		r.state.Revision++
	}
}

// Snapshot coordinates buffering without overriding a user's explicit pause.
func (r *Room) Snapshot(transcode media.Status) RoomState {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	r.advance(now)
	for id, member := range r.members {
		if now.Sub(member.LastSeen) > 10*time.Second {
			delete(r.members, id)
		}
	}

	limit := r.duration

	finished := r.state.Source == "original" || transcode.State == "complete"
	if r.state.Source == "transcoded" {
		limit = transcode.Available
		finished = transcode.State == "complete" || transcode.State == "cancelled" || transcode.State == "failed"
	}

	r.state.Reason = "paused"
	if r.state.Source == "transcoded" && limit <= r.state.Position+0.15 {
		r.state.Reason = "transcoding"
		if finished {
			r.state.Reason = "partial_end"
		}
	}

	if r.state.Position >= r.duration-0.08 {
		r.state.Position = r.duration
		r.state.WantPlay = false
		r.state.Reason = "ended"
	}

	if r.state.WantPlay {
		r.state.Reason = "ready"
		if r.state.Source == "transcoded" && limit <= r.state.Position+0.15 {
			r.state.Reason = "transcoding"
			if finished {
				r.state.Reason = "partial_end"
				if transcode.State == "complete" {
					r.state.Reason = "ended"
				}

				r.state.WantPlay = false
			}
		}

		count := 0
		// Native mobile players may preload only a few seconds while paused.
		threshold := 2.0
		if r.state.Playing {
			threshold = 0.75
		}

		needed := min(threshold, max(0, limit-r.state.Position-0.15))
		if r.state.Reason == "ready" && !finished && limit-r.state.Position < threshold {
			r.state.Reason = "transcoding"
		}

		for _, member := range r.members {
			if !member.Joined {
				continue
			}

			count++

			ahead := 0.0
			for _, span := range member.Buffered {
				if span[0] <= r.state.Position+0.2 && span[1] >= r.state.Position {
					ahead = span[1] - r.state.Position
					break
				}
			}

			ready := member.Connected && member.Epoch == r.state.Epoch && member.State == "ready" && ahead >= needed
			if !ready && r.state.Reason == "ready" {
				r.state.Reason = "viewers"
			}
		}

		if count == 0 {
			r.state.Reason = "no_viewers"
		}
	}

	r.state.Playing = r.state.WantPlay && r.state.Reason == "ready"

	state := r.state
	state.ServerTime = now.UnixMilli()
	state.Transcode = transcode
	state.Members = make([]Member, 0, len(r.members))
	for _, member := range r.members {
		state.Members = append(state.Members, *member)
	}

	sort.Slice(state.Members, func(i, j int) bool {
		return state.Members[i].ID < state.Members[j].ID
	})
	return state
}
