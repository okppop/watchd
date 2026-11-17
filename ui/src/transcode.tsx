import { useState } from "react";
import { ArrowRight, LoaderCircle, Square, WandSparkles } from "lucide-react";
import { Button } from "./components/ui/button";
import { request, time } from "./lib";
import type { Initial, Options, Stream, Transcode } from "./types";

// trackLabel identifies tracks without hiding their actual source stream index.
function trackLabel(stream: Stream) {
  const language = stream.tags?.language || "Undetermined";
  const title = stream.tags?.title || stream.codec_name;
  const channels = stream.channels ? ` · ${stream.channels}ch` : "";
  const defaultLabel = stream.disposition?.default ? " · Default" : "";

  return `#${stream.index} · ${language} · ${title}${channels}${defaultLabel}`;
}

// TranscodePanel exposes explicit, shared FFmpeg settings; opening it never starts a job.
export function TranscodePanel({
  info,
  status,
  onError,
}: {
  info: Initial;
  status: Transcode;
  onError: (message: string) => void;
}) {
  const videos = info.source.streams.filter(
    (stream) =>
      stream.codec_type === "video" && !stream.disposition?.attached_pic,
  );
  const audios = info.source.streams.filter(
    (stream) => stream.codec_type === "audio",
  );
  const subtitles = info.source.streams.filter(
    (stream) => stream.codec_type === "subtitle",
  );
  const [options, setOptions] = useState<Options>(() =>
    status.generation
      ? status.options
      : {
          videoTrack:
            (videos.find((track) => track.disposition?.default) || videos[0])
              ?.index ?? -1,
          videoCodec: "h264",
          encoder: info.capabilities.encoders.includes("libx264")
            ? "libx264"
            : info.capabilities.encoders[0] || "",
          audioTrack:
            (audios.find((track) => track.disposition?.default) || audios[0])
              ?.index ?? -1,
          audioCodec: audios.length ? "aac" : "off",
          subtitleTrack: subtitles[0]?.index ?? -1,
          subtitleMode: "off",
        },
  );
  const [busy, setBusy] = useState(false);
  const running = ["starting", "running"].includes(status.state);
  const subtitle = subtitles.find(
    (track) => track.index === options.subtitleTrack,
  );
  const bitmap = ["hdmv_pgs_subtitle", "dvd_subtitle", "dvb_subtitle"].includes(
    subtitle?.codec_name || "",
  );
  let issue = "";
  if (options.subtitleMode === "burn" && options.videoCodec === "copy") {
    issue = "Burning subtitles requires H.264 encoding.";
  } else if (options.subtitleMode === "webvtt" && bitmap) {
    issue = "Image subtitles must be burned into the video.";
  } else if (
    options.subtitleMode === "burn" &&
    !bitmap &&
    !info.capabilities.textBurn
  ) {
    issue = "This FFmpeg build has no libass subtitle renderer.";
  }

  // update keeps related settings in one shared form without implicit conversion decisions.
  function update(key: keyof Options, value: string | number) {
    setOptions((current) => ({ ...current, [key]: value }));
  }

  // start replaces an existing output only after the viewer confirms the destructive reset.
  async function start() {
    if (
      status.generation &&
      !window.confirm(
        "Replace the current transcode? Existing output will be deleted. Viewers using it will be paused.",
      )
    ) {
      return;
    }

    setBusy(true);

    try {
      await request("/room/transcode", "POST", options);
    } catch (error) {
      onError((error as Error).message);
    } finally {
      setBusy(false);
    }
  }

  // cancel stops generation while preserving completed, playable segments.
  async function cancel() {
    setBusy(true);

    try {
      await request("/room/transcode", "DELETE");
    } catch (error) {
      onError((error as Error).message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="panel transcode-panel">
      <div className="section-heading">
        <div>
          <span className="eyebrow">MANUAL TRANSCODE</span>
          <h2>Make it playable.</h2>
        </div>
        <WandSparkles size={20} className="dim" />
      </div>
      <p className="muted text-sm">
        One shared output, generated from beginning to end. Playback can start
        before the job finishes.
      </p>
      <div className="form-grid">
        <label>
          Video track
          <select
            value={options.videoTrack}
            onChange={(event) =>
              update("videoTrack", Number(event.target.value))
            }
          >
            {videos.map((track) => (
              <option key={track.index} value={track.index}>
                {trackLabel(track)}
              </option>
            ))}
          </select>
        </label>
        <label>
          Video output
          <select
            value={options.videoCodec}
            onChange={(event) => update("videoCodec", event.target.value)}
          >
            <option value="h264">H.264 · 8-bit</option>
            <option value="copy">Copy source codec</option>
          </select>
        </label>
        <label>
          Audio track
          <select
            value={options.audioTrack}
            disabled={options.audioCodec === "off"}
            onChange={(event) =>
              update("audioTrack", Number(event.target.value))
            }
          >
            {!audios.length && <option value={-1}>No audio tracks</option>}
            {audios.map((track) => (
              <option key={track.index} value={track.index}>
                {trackLabel(track)}
              </option>
            ))}
          </select>
        </label>
        <label>
          Audio output
          <select
            value={options.audioCodec}
            onChange={(event) => update("audioCodec", event.target.value)}
          >
            <option value="aac" disabled={!audios.length}>
              AAC · stereo
            </option>
            <option value="copy" disabled={!audios.length}>
              Copy source codec
            </option>
            <option value="off">No audio</option>
          </select>
        </label>
        <label>
          Subtitle track
          <select
            value={options.subtitleTrack}
            disabled={options.subtitleMode === "off"}
            onChange={(event) =>
              update("subtitleTrack", Number(event.target.value))
            }
          >
            {!subtitles.length && (
              <option value={-1}>No subtitle tracks</option>
            )}
            {subtitles.map((track) => (
              <option key={track.index} value={track.index}>
                {trackLabel(track)}
              </option>
            ))}
          </select>
        </label>
        <label>
          Subtitle output
          <select
            value={options.subtitleMode}
            onChange={(event) => update("subtitleMode", event.target.value)}
          >
            <option value="off">Off</option>
            <option value="webvtt" disabled={!subtitles.length}>
              WebVTT · text only
            </option>
            <option value="burn" disabled={!subtitles.length}>
              Burn into video
            </option>
          </select>
        </label>
        <label className="wide">
          Video encoder
          <select
            value={options.encoder}
            disabled={options.videoCodec === "copy"}
            onChange={(event) => update("encoder", event.target.value)}
          >
            {info.capabilities.encoders.map((encoder) => (
              <option key={encoder} value={encoder}>
                {encoder === "libx264"
                  ? "Software · x264"
                  : encoder === "h264_videotoolbox"
                    ? "Apple VideoToolbox"
                    : "Intel / VA-API · /dev/dri/renderD128"}
              </option>
            ))}
          </select>
        </label>
      </div>
      <p className="fine-print">
        Hardware encoders require a working device and driver. Copying a codec
        does not guarantee browser support. WebVTT does not preserve ASS
        styling.
      </p>
      {issue && <div className="notice warning">{issue}</div>}
      <div className="transcode-actions">
        <Button onClick={start} disabled={busy || !!issue || !videos.length}>
          {busy ? <LoaderCircle className="spin" /> : <ArrowRight />}
          {status.generation ? "Replace transcode" : "Start transcoding"}
        </Button>
        {running && (
          <Button variant="outline" onClick={cancel} disabled={busy}>
            <Square />
            Stop
          </Button>
        )}
      </div>
      {status.generation > 0 && (
        <div className="job-status">
          <div>
            <span className={`status-dot ${running ? "busy" : ""}`} />
            <strong>
              {status.state === "cancelled"
                ? "Stopped · partial output kept"
                : status.state}
            </strong>
            <span className="mono dim">{status.speed || "—"}</span>
          </div>
          <progress
            aria-label="Transcode progress"
            value={status.available}
            max={info.duration}
          />
          <p className="fine-print">
            Playable through {time(status.available)} / {time(info.duration)} ·{" "}
            {Math.round(status.bytes / 1048576)} MB
          </p>
          {status.error && <pre className="error-log">{status.error}</pre>}
        </div>
      )}
    </section>
  );
}
