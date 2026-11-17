package media

import (
	"fmt"
	"strconv"
	"strings"
)

type Options struct {
	VideoTrack    int    `json:"videoTrack"`
	VideoCodec    string `json:"videoCodec"`
	Encoder       string `json:"encoder"`
	AudioTrack    int    `json:"audioTrack"`
	AudioCodec    string `json:"audioCodec"`
	SubtitleTrack int    `json:"subtitleTrack"`
	SubtitleMode  string `json:"subtitleMode"`
}

type Capabilities struct {
	Encoders []string `json:"encoders"`
	TextBurn bool     `json:"textBurn"`
	ToneMap  bool     `json:"toneMap"`
}

// Arguments validates stream selections and builds one sequential HLS output.
func Arguments(info *Info, options Options, capabilities Capabilities) ([]string, error) {
	var video, audio, subtitle *Stream
	for index := range info.Streams {
		stream := &info.Streams[index]
		if stream.Index == options.VideoTrack && stream.CodecType == "video" && stream.Disposition["attached_pic"] != 1 {
			video = stream
		}

		if stream.Index == options.AudioTrack && stream.CodecType == "audio" {
			audio = stream
		}

		if stream.Index == options.SubtitleTrack && stream.CodecType == "subtitle" {
			subtitle = stream
		}
	}

	if video == nil {
		return nil, fmt.Errorf("select a valid video track")
	}

	if options.AudioCodec != "off" && audio == nil {
		return nil, fmt.Errorf("select a valid audio track")
	}

	if options.SubtitleMode != "off" && subtitle == nil {
		return nil, fmt.Errorf("select a valid subtitle track")
	}

	if options.SubtitleMode != "off" && options.SubtitleMode != "webvtt" && options.SubtitleMode != "burn" {
		return nil, fmt.Errorf("unsupported subtitle mode")
	}

	bitmap := subtitle != nil &&
		(subtitle.CodecName == "hdmv_pgs_subtitle" ||
			subtitle.CodecName == "dvd_subtitle" ||
			subtitle.CodecName == "dvb_subtitle")
	if options.SubtitleMode == "webvtt" {
		if bitmap {
			return nil, fmt.Errorf("image subtitles must be burned into the video")
		}

		switch subtitle.CodecName {
		case "subrip", "webvtt", "ass", "ssa", "mov_text", "text":
		default:
			return nil, fmt.Errorf("this subtitle format cannot be converted to WebVTT")
		}
	}

	if options.VideoCodec != "h264" && options.VideoCodec != "copy" {
		return nil, fmt.Errorf("unsupported video codec")
	}

	if options.VideoCodec == "copy" {
		if options.SubtitleMode == "burn" {
			return nil, fmt.Errorf("burning subtitles requires video encoding")
		}

		if video.CodecName != "h264" && video.CodecName != "hevc" {
			return nil, fmt.Errorf("video copy supports H.264 and HEVC in this HLS output")
		}
	}

	if options.SubtitleMode == "burn" && !bitmap && !capabilities.TextBurn {
		return nil, fmt.Errorf("text subtitle burning requires FFmpeg built with libass")
	}

	args := []string{"-map", "0:" + strconv.Itoa(video.Index)}
	if options.VideoCodec == "copy" {
		args = append(args, "-c:v", "copy")
	} else {
		found := false
		for _, encoder := range capabilities.Encoders {
			if encoder == options.Encoder {
				found = true
			}
		}

		if !found {
			return nil, fmt.Errorf("selected encoder is not available in this FFmpeg build")
		}

		filters := ""
		if video.ColorTransfer == "smpte2084" || video.ColorTransfer == "arib-std-b67" {
			if !capabilities.ToneMap {
				return nil, fmt.Errorf("HDR to H.264 SDR requires FFmpeg with zscale and tonemap filters")
			}

			filters = "zscale=t=linear:npl=100,format=gbrpf32le,zscale=p=bt709,tonemap=hable,zscale=t=bt709:m=bt709:r=tv,"
		}

		filters += "scale=trunc(iw/2)*2:trunc(ih/2)*2,format=yuv420p"
		if options.SubtitleMode == "burn" {
			if bitmap {
				args = []string{
					"-filter_complex", fmt.Sprintf("[0:%d]%s[base];[base][0:%d]overlay=eof_action=pass:repeatlast=0[v]", video.Index, filters, subtitle.Index),
					"-map", "[v]",
				}
			} else {
				// The input is a fixed local symlink, avoiding filter-language path escaping.
				subtitleIndex := 0
				for _, stream := range info.Streams {
					if stream.CodecType == "subtitle" && stream.Index < subtitle.Index {
						subtitleIndex++
					}
				}

				filters += fmt.Sprintf(",subtitles=filename=source:si=%d:fontsdir=fonts", subtitleIndex)
				args = append(args, "-vf", filters)
			}
		} else {
			args = append(args, "-vf", filters)
		}

		if options.Encoder == "h264_vaapi" {
			// Upload filtered software frames; Haswell cannot decode HEVC Main 10 in hardware.
			for index := range args {
				if args[index] == "-vf" {
					args[index+1] += ",format=nv12,hwupload"
				}

				if args[index] == "-filter_complex" {
					args[index+1] = strings.Replace(args[index+1], "[v]", ",format=nv12,hwupload[v]", 1)
				}
			}

			args = append([]string{"-vaapi_device", "/dev/dri/renderD128"}, args...)
		}

		args = append(args, "-c:v", options.Encoder, "-force_key_frames", "expr:gte(t,n_forced*2)")
		switch options.Encoder {
		case "libx264":
			args = append(args, "-preset", "veryfast", "-crf", "23", "-pix_fmt", "yuv420p")
		case "h264_vaapi":
			args = append(args, "-qp", "23")
		case "h264_videotoolbox":
			args = append(args, "-b:v", "5M", "-pix_fmt", "yuv420p")
		}
	}

	switch options.AudioCodec {
	case "off":
		args = append(args, "-an")
	case "aac":
		args = append(
			args,
			"-map", "0:"+strconv.Itoa(audio.Index),
			"-c:a", "aac",
			"-b:a", "192k",
			"-ac", "2",
			"-ar", "48000",
		)
	case "copy":
		if audio.CodecName != "aac" && audio.CodecName != "mp3" && audio.CodecName != "ac3" && audio.CodecName != "eac3" {
			return nil, fmt.Errorf("audio copy supports AAC, MP3, AC-3 and E-AC-3 in this HLS output")
		}
		args = append(args, "-map", "0:"+strconv.Itoa(audio.Index), "-c:a", "copy")
	default:
		return nil, fmt.Errorf("unsupported audio codec")
	}

	args = append(
		args,
		"-sn",
		"-map_metadata", "-1",
		"-max_muxing_queue_size", "4096",
		"-f", "hls",
		"-hls_time", "2",
		"-hls_list_size", "0",
		"-hls_playlist_type", "event",
		"-hls_flags", "temp_file",
		"-hls_segment_filename", "segment-%06d.ts",
		"index.m3u8",
	)
	return args, nil
}
