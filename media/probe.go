package media

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"time"
)

type Stream struct {
	Index          int               `json:"index"`
	CodecType      string            `json:"codec_type"`
	CodecName      string            `json:"codec_name"`
	Profile        string            `json:"profile"`
	Width          int               `json:"width"`
	Height         int               `json:"height"`
	PixFmt         string            `json:"pix_fmt"`
	FrameRate      string            `json:"avg_frame_rate"`
	SampleRate     string            `json:"sample_rate"`
	Channels       int               `json:"channels"`
	ChannelLayout  string            `json:"channel_layout"`
	ColorTransfer  string            `json:"color_transfer"`
	ColorPrimaries string            `json:"color_primaries"`
	BitRate        string            `json:"bit_rate"`
	Duration       string            `json:"duration"`
	Tags           map[string]string `json:"tags"`
	Disposition    map[string]int    `json:"disposition"`
}

type Info struct {
	Streams []Stream `json:"streams"`
	Format  struct {
		Filename   string `json:"filename"`
		FormatName string `json:"format_name"`
		Duration   string `json:"duration"`
		Size       string `json:"size"`
		BitRate    string `json:"bit_rate"`
	} `json:"format"`
}

// Probe reads media metadata without decoding the full movie.
func Probe(ctx context.Context, path string) (*Info, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-show_streams", "-show_format", "-of", "json", path)

	data, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("probe media: %w", err)
	}

	info := &Info{}
	if err := json.Unmarshal(data, info); err != nil {
		return nil, fmt.Errorf("decode media metadata: %w", err)
	}

	return info, nil
}

// Duration returns the movie duration in seconds, falling back to stream metadata.
func (i *Info) Duration() float64 {
	duration, _ := strconv.ParseFloat(i.Format.Duration, 64)
	for _, stream := range i.Streams {
		d, _ := strconv.ParseFloat(stream.Duration, 64)
		if d > duration {
			duration = d
		}
	}

	return duration
}
