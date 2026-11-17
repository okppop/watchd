# watchd

One video file, one shared room. Go + React/shadcn, with manual FFmpeg transcoding.

## Build and run

Requires Go 1.26.5+, Node.js 22.12+ (or a newer supported Node version), npm, `ffmpeg`, and `ffprobe` on `PATH`. Runtime requires only the built binary and FFmpeg/ffprobe. Linux and macOS are supported.

```sh
make build
./bin/watchd --path /movies/movie.mkv --listen '[::]:8080' --password 123456
```

Existing flags are `--path`, `--listen`, `--password`, and `--log`. An empty password allows entry without signing in. There is no `--file` flag and no automatic or startup transcoding.

The React application is embedded at build time. After changing the UI, rebuild it and the Go binary. For frontend development, run the Go server on port 8080 and `npm --prefix ui run dev`; Vite proxies the API, media and WebSocket connection.

## Watching together

Sign in and click **Join & play** to participate. Everyone can play, pause, seek, change the shared media source, and configure transcoding. Volume is local. Observers do not block playback. The room waits for participating viewers to buffer; disconnected viewers are removed after a ten-second grace period.

The timeline uses the full original movie duration and shows separate layers for played time, the current device's buffered ranges, and completed transcode segments. Only complete, readable segments count as transcoded. Dragging beyond generated output waits for sequential transcoding to reach that point.

The online list reports the socket peer address, including its port. With a reverse proxy this is the proxy's address; forwarded IP headers are not trusted implicitly.

## Manual transcoding

The source is served unchanged with HTTP Range support. If the browser cannot play its container, codecs, or embedded tracks, open **Transcode**. There is no automatic fallback.

- Select a video track and H.264 encoding or stream copy (H.264/HEVC).
- Select an audio track and AAC stereo, stream copy (AAC/MP3/AC-3/E-AC-3), or no audio.
- Disable subtitles, extract text subtitles as WebVTT, or burn subtitles into the video.
- PGS/VobSub subtitles require burning and therefore video encoding. ASS/SSA styling requires an FFmpeg build with libass; WebVTT conversion does not preserve complex styling.
- Attached TTF/OTF fonts are extracted for text subtitle burning.
- H.264 conversion of HDR sources requires FFmpeg's `zscale` and `tonemap` filters. The form/API reports missing support instead of silently producing incorrect SDR colors.

The first version exposes H.264 and stream copy, not an arbitrary FFmpeg command editor. Stream copy is not a promise of browser codec compatibility. Copy mode preserves source keyframe spacing, so initial segment generation can take longer.

Software x264 is the default encoder when available. Apple VideoToolbox and VA-API are offered when compiled into FFmpeg. Their presence in the menu does not prove the local hardware/driver is usable; a failed job reports FFmpeg's diagnostic output. The VA-API option uses `/dev/dri/renderD128`; Haswell systems normally require the i965 driver. HEVC Main 10 decoding remains on the CPU on an i3-4170.

Transcoding starts only on request, from the beginning, and continues to the end even when playback is paused. All viewers share one output. Starting a job does not switch media automatically. Once segments are available, select **Transcoded** to watch while generation continues.

Replacing a job deletes its previous output after confirmation, pauses viewers using it, and keeps the current movie position. Stopping a job retains complete segments as partial output. A failed job also preserves completed segments and its diagnostic log.

## Temporary files

For `/movies/movie.mkv`, output lives in `/movies/movie.mkv.transcode/`. The source directory must be writable. A source-file advisory lock prevents two watchd instances from sharing the same movie/output directory.

Normal shutdown (including SIGINT/SIGTERM) stops FFmpeg and removes the output directory. A recognized directory left by a crash is cleaned at the next startup. Unrecognized directories and symlinks are rejected rather than deleted. The original video is never modified. A forced kill or power loss cannot perform immediate cleanup.

There is one output version and no persistent transcode library. Ensure there is enough disk space for the generated output; write failures are surfaced in the transcode panel.

## Verification

```sh
make test
```

Tests are table-driven and include real FFmpeg generation, argument validation, ownership/cleanup, authentication/Range requests, and room buffering under the race detector. FFmpeg integration tests skip if FFmpeg is missing; their sample encoding requires libx264.

Browser checks use an existing local Chrome installation and the temporary 30-second sample below. Stop any other watchd instance on the same file first.

```sh
mkdir -p /tmp/watchd-fixtures
ffmpeg -y -f lavfi -i 'testsrc2=size=640x360:rate=24' \
  -f lavfi -i 'sine=frequency=440:sample_rate=48000' -t 30 \
  -c:v libx264 -preset ultrafast -c:a aac -movflags +faststart \
  /tmp/watchd-fixtures/source.mp4
./bin/watchd --path /tmp/watchd-fixtures/source.mp4 --listen 127.0.0.1:8080 --password testpass
# In another terminal:
cd ui && npx playwright test
```

The browser tests cover desktop/mobile-width layouts, login, shared pause/seek, and HLS playback with two viewers. Phone-width Chrome testing is not a substitute for real iPhone/Android or Firefox verification.
