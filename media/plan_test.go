package media

import (
	"strings"
	"testing"
)

func TestArguments(t *testing.T) {
	info := &Info{
		Streams: []Stream{
			{
				Index:     0,
				CodecType: "video",
				CodecName: "hevc",
			},
			{
				Index:     1,
				CodecType: "audio",
				CodecName: "dts",
			},
			{
				Index:     2,
				CodecType: "subtitle",
				CodecName: "hdmv_pgs_subtitle",
			},
			{
				Index:     3,
				CodecType: "subtitle",
				CodecName: "ass",
			},
		},
	}

	capabilities := Capabilities{
		Encoders: []string{"libx264", "h264_vaapi"},
		TextBurn: true,
		ToneMap:  true,
	}

	base := Options{
		VideoTrack:    0,
		VideoCodec:    "h264",
		Encoder:       "libx264",
		AudioTrack:    1,
		AudioCodec:    "aac",
		SubtitleMode:  "off",
		SubtitleTrack: 2,
	}
	for _, test := range []struct {
		name      string
		change    func(*Options)
		wantError string
		contains  string
	}{
		{
			name:      "compatible encoding",
			change:    func(o *Options) {},
			wantError: "",
			contains:  "-c:v libx264",
		},
		{
			name: "invalid video",
			change: func(o *Options) {
				o.VideoTrack = 9
			},
			wantError: "video track",
			contains:  "",
		},
		{
			name: "invalid audio",
			change: func(o *Options) {
				o.AudioTrack = 9
			},
			wantError: "audio track",
			contains:  "",
		},
		{
			name: "no audio",
			change: func(o *Options) {
				o.AudioTrack = -1
				o.AudioCodec = "off"
			},
			wantError: "",
			contains:  "-an",
		},
		{
			name: "PGS overlay",
			change: func(o *Options) {
				o.SubtitleMode = "burn"
			},
			wantError: "",
			contains:  "[base][0:2]overlay",
		},
		{
			name: "PGS is not text",
			change: func(o *Options) {
				o.SubtitleMode = "webvtt"
			},
			wantError: "image subtitles",
			contains:  "",
		},
		{
			name: "burn cannot copy",
			change: func(o *Options) {
				o.SubtitleMode = "burn"
				o.VideoCodec = "copy"
			},
			wantError: "requires video encoding",
			contains:  "",
		},
		{
			name: "reject DTS copy",
			change: func(o *Options) {
				o.AudioCodec = "copy"
			},
			wantError: "audio copy",
			contains:  "",
		},
		{
			name: "unsupported encoder",
			change: func(o *Options) {
				o.Encoder = "shell-command"
			},
			wantError: "encoder",
			contains:  "",
		},
		{
			name: "ASS rendering",
			change: func(o *Options) {
				o.SubtitleMode = "burn"
				o.SubtitleTrack = 3
			},
			wantError: "",
			contains:  "subtitles=filename=source:si=1:fontsdir=fonts",
		},
		{
			name: "hardware PGS upload",
			change: func(o *Options) {
				o.SubtitleMode = "burn"
				o.Encoder = "h264_vaapi"
			},
			wantError: "",
			contains:  "format=nv12,hwupload[v]",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			options := base
			test.change(&options)

			args, err := Arguments(info, options, capabilities)
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) {
					t.Fatalf("got %v, want error containing %q", err, test.wantError)
				}

				return
			}

			if err != nil {
				t.Fatal(err)
			}

			if !strings.Contains(strings.Join(args, " "), test.contains) {
				t.Fatalf("missing %q in %v", test.contains, args)
			}
		})
	}
}
