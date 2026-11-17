import { useEffect, useRef, useState, type SubmitEvent } from "react";
import Hls from "hls.js";
import {
  ArrowRight,
  Check,
  ChevronDown,
  CircleHelp,
  Clapperboard,
  Film,
  Info as InfoIcon,
  LoaderCircle,
  LockKeyhole,
  LogOut,
  Maximize,
  Pause,
  Play,
  Radio,
  Users,
  Volume2,
  VolumeX,
  X,
} from "lucide-react";
import { Button } from "./components/ui/button";
import { Input } from "./components/ui/input";
import { request, time } from "./lib";
import { Timeline } from "./timeline";
import { TranscodePanel } from "./transcode";
import type { Info, Initial, Room, Span } from "./types";

// MediaDetails displays probe results without presenting unknown metadata as measured values.
function MediaDetails({ title, info }: { title: string; info: Info }) {
  return (
    <div className="media-details">
      <h3>{title}</h3>
      <div className="metadata-grid">
        <span>Container</span>
        <strong>{info.format.format_name || "—"}</strong>
        <span>Size</span>
        <strong>
          {Number(info.format.size) > 0
            ? `${(Number(info.format.size) / 1048576).toFixed(1)} MB`
            : "Unknown"}
        </strong>
        <span>Bitrate</span>
        <strong>
          {Number(info.format.bit_rate) > 0
            ? `${(Number(info.format.bit_rate) / 1e6).toFixed(2)} Mbps`
            : "Unknown"}
        </strong>
      </div>
      {info.streams.map((stream) => (
        <div className="stream-info" key={stream.index}>
          <div>
            <span className="track-type">{stream.codec_type}</span>
            <strong>
              #{stream.index} · {stream.codec_name}
            </strong>
            <span className="dim">{stream.tags?.language || ""}</span>
          </div>
          <p>
            {[
              stream.tags?.title,
              stream.profile,
              stream.width ? `${stream.width} × ${stream.height}` : "",
              stream.pix_fmt,
              stream.avg_frame_rate && stream.avg_frame_rate !== "0/0"
                ? `${stream.avg_frame_rate} fps`
                : "",
              stream.channels
                ? `${stream.channels} channels · ${stream.sample_rate} Hz`
                : "",
              stream.color_transfer,
              stream.color_primaries,
            ]
              .filter(Boolean)
              .join(" · ") || "No additional metadata"}
          </p>
        </div>
      ))}
    </div>
  );
}

// App owns the single shared room while keeping volume and local media buffering per device.
export default function App() {
  const [info, setInfo] = useState<Initial | null>(null);
  const [room, setRoom] = useState<Room | null>(null);
  const [login, setLogin] = useState(false);
  const [password, setPassword] = useState("");
  const [name, setName] = useState(
    () => localStorage.getItem("watchd-name") || "",
  );
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [playerError, setPlayerError] = useState("");
  const [connected, setConnected] = useState(false);
  const [joined, setJoined] = useState(false);
  const [viewerID, setViewerID] = useState("");
  const [buffered, setBuffered] = useState<Span[]>([]);
  const [needsGesture, setNeedsGesture] = useState(false);
  const [muted, setMuted] = useState(false);
  const [volume, setVolume] = useState(1);
  const [position, setPosition] = useState(0);
  const [panel, setPanel] = useState<"media" | "transcode">("media");
  const video = useRef<HTMLVideoElement>(null);
  const screen = useRef<HTMLDivElement>(null);
  const socket = useRef<WebSocket | null>(null);
  const roomRef = useRef<Room | null>(null);
  const joinedRef = useRef(false);
  const gestureRef = useRef(false);
  const errorRef = useRef("");
  const suppressUntil = useRef(0);
  const clockOffset = useRef(0);
  const lastEpoch = useRef(0);

  // load discovers the room or shows the password form without reloading the page.
  async function load() {
    try {
      const response = await fetch("/room/info");
      if (response.status === 403) {
        setLogin(true);

        return;
      }

      if (!response.ok) {
        throw new Error("Could not load the room.");
      }

      const data = (await response.json()) as Initial;
      setInfo(data);
      setRoom(data.room);
      roomRef.current = data.room;
      clockOffset.current = data.room.serverTime - Date.now();
      setLogin(false);
    } catch (failure) {
      setError((failure as Error).message);
    }
  }

  useEffect(() => {
    void load();
  }, []);

  // signIn exchanges the shared password for a cookie usable by native media requests.
  async function signIn(event: SubmitEvent<HTMLFormElement>) {
    event.preventDefault();
    setBusy(true);
    setError("");

    try {
      await request("/api/login", "POST", { password });
      setPassword("");
      await load();
    } catch (failure) {
      setError((failure as Error).message);
    } finally {
      setBusy(false);
    }
  }

  // send issues explicit user commands; remote player updates never call it implicitly.
  function send(message: Record<string, unknown>) {
    if (socket.current?.readyState === WebSocket.OPEN) {
      socket.current.send(JSON.stringify(message));
    }
  }

  // seek updates the local slider immediately while the server orders the shared command.
  function seek(target: number) {
    setPosition(target);
    if (roomRef.current) {
      roomRef.current = {
        ...roomRef.current,
        position: target,
        playing: false,
      };
    }

    lastEpoch.current = 0;
    send({ type: "seek", position: target });
  }

  useEffect(() => {
    if (!info) {
      return;
    }

    let stopped = false;
    let retry: ReturnType<typeof setTimeout>;
    // connect restores the room subscription after a temporary network interruption.
    function connect() {
      const ws = new WebSocket(
        `${location.protocol === "https:" ? "wss:" : "ws:"}//${location.host}/room/ws?name=${encodeURIComponent(localStorage.getItem("watchd-name") || "Guest")}`,
      );
      socket.current = ws;
      ws.onopen = () => {
        setConnected(true);
        send({ type: "ping", clientTime: Date.now() });
      };
      ws.onmessage = (event) => {
        const message = JSON.parse(event.data);
        if (message.type === "welcome") {
          setViewerID(message.id);
          if (joinedRef.current) {
            send({ type: "join" });
          }
        }

        if (message.type === "state") {
          roomRef.current = message.room;
          setRoom(message.room);
        }

        if (message.type === "error") {
          setError(message.error);
        }

        if (message.type === "pong") {
          clockOffset.current =
            message.serverTime - (message.clientTime + Date.now()) / 2;
        }
      };
      ws.onclose = () => {
        setConnected(false);
        suppressUntil.current = performance.now() + 1000;
        video.current?.pause();
        if (!stopped) {
          retry = setTimeout(connect, 1500);
        }
      };
      ws.onerror = () => ws.close();
    }

    connect();
    const heartbeat = setInterval(
      () => send({ type: "ping", clientTime: Date.now() }),
      3000,
    );

    return () => {
      stopped = true;
      clearTimeout(retry);
      clearInterval(heartbeat);
      socket.current?.close();
    };
  }, [info]);

  const source = room?.source || "original";
  const generation = room?.transcode.generation || 0;
  const hasOutput = (room?.transcode.available || 0) > 0;
  useEffect(() => {
    const element = video.current;
    if (!info || !element) {
      return;
    }

    let hls: Hls | undefined;
    suppressUntil.current = performance.now() + 1000;
    setPlayerError("");
    errorRef.current = "";
    setBuffered([]);
    element.pause();
    element.removeAttribute("src");
    element.load();
    if (source === "transcoded" && !hasOutput) {
      return;
    }

    const path =
      source === "original"
        ? "/room/source"
        : `/room/transcode/${generation}/index.m3u8`;
    // fail keeps a failed original playable attempt separate from the manual transcode controls.
    function fail(message: string) {
      errorRef.current = message;
      setPlayerError(message);
    }

    if (source === "transcoded" && Hls.isSupported()) {
      hls = new Hls({
        startPosition: roomRef.current?.position || 0,
        maxBufferLength: 30,
        backBufferLength: 60,
      });
      hls.on(Hls.Events.ERROR, (_, data) => {
        if (data.fatal) {
          fail(
            `Playback failed (${data.details}). Check the selected codecs or try another transcode.`,
          );
        }
      });
      hls.loadSource(path);
      hls.attachMedia(element);
    } else {
      element.src = path;
    }
    // mediaError provides an actionable message instead of silently switching media sources.
    function mediaError() {
      if (element?.error) {
        fail(
          source === "original"
            ? "This browser could not play the source. Open Transcode to create a compatible version."
            : "The transcoded output could not be played. Check its codecs and the transcode log.",
        );
      }
    }

    element.addEventListener("error", mediaError);

    return () => {
      element.removeEventListener("error", mediaError);
      hls?.destroy();
      suppressUntil.current = performance.now() + 1000;
      element.pause();
      element.removeAttribute("src");
      element.load();
    };
  }, [info, source, generation, source === "transcoded" && hasOutput]);

  useEffect(() => {
    if (!info) {
      return;
    }

    const timer = setInterval(() => {
      const element = video.current;
      const state = roomRef.current;
      if (!element || !state) {
        return;
      }

      const spans: Span[] = [];
      for (let index = 0; index < element.buffered.length; index++) {
        spans.push([
          element.buffered.start(index),
          element.buffered.end(index),
        ]);
      }

      setBuffered(spans);
      const elapsed = state.playing
        ? Math.max(
            0,
            (Date.now() + clockOffset.current - state.serverTime) / 1000,
          )
        : 0;
      const target = Math.min(info.duration, state.position + elapsed);
      setPosition(target);
      const available =
        state.source === "original" ? info.duration : state.transcode.available;
      const epochChanged = lastEpoch.current !== state.epoch;
      const difference = target - element.currentTime;
      if (
        element.readyState > 0 &&
        target < available &&
        (epochChanged || Math.abs(difference) > 0.8)
      ) {
        suppressUntil.current = performance.now() + 1000;
        element.currentTime = target;
        lastEpoch.current = state.epoch;
      }

      if (
        state.playing &&
        Math.abs(difference) > 0.15 &&
        Math.abs(difference) < 0.8
      ) {
        element.playbackRate = difference > 0 ? 1.03 : 0.97;
      } else {
        element.playbackRate = 1;
      }
      if (
        state.playing &&
        joinedRef.current &&
        socket.current?.readyState === WebSocket.OPEN &&
        !gestureRef.current &&
        !errorRef.current &&
        target < available
      ) {
        if (element.paused) {
          suppressUntil.current = performance.now() + 1000;
          void element.play().catch((failure) => {
            if (failure.name === "NotAllowedError") {
              gestureRef.current = true;
              setNeedsGesture(true);
            }
          });
        }
      } else if (!element.paused) {
        suppressUntil.current = performance.now() + 1000;
        element.pause();
      }

      let localState = "buffering";
      if (errorRef.current) {
        localState = "error";
      } else if (gestureRef.current) {
        localState = "gesture";
      } else if (element.readyState >= 3 && !element.seeking) {
        localState = "ready";
      }
      send({
        type: "report",
        position: element.currentTime,
        buffered: spans,
        state: localState,
        epoch: state.epoch,
      });
    }, 300);

    return () => clearInterval(timer);
  }, [info]);

  // join unlocks audio during a real gesture before participating in shared playback.
  async function join() {
    localStorage.setItem("watchd-name", name || "Guest");
    joinedRef.current = true;
    setJoined(true);
    gestureRef.current = false;
    setNeedsGesture(false);
    send({ type: "join" });

    try {
      suppressUntil.current = performance.now() + 1000;
      await video.current?.play();
      suppressUntil.current = performance.now() + 1000;
      video.current?.pause();
      send({ type: "play" });
    } catch (failure) {
      if ((failure as DOMException).name === "NotAllowedError") {
        gestureRef.current = true;
        setNeedsGesture(true);
      } else if ((failure as DOMException).name !== "AbortError") {
        setPlayerError(
          "Could not start playback. Try the transcoded version or inspect the media information.",
        );
      }
    }
  }

  // toggle controls the shared playback intent, including pausing while others buffer.
  function toggle() {
    if (!joined || needsGesture) {
      void join();

      return;
    }

    if (roomRef.current?.wantPlay) {
      suppressUntil.current = performance.now() + 1000;
      video.current?.pause();
      send({ type: "pause" });
    } else {
      gestureRef.current = false;
      setNeedsGesture(false);
      suppressUntil.current = performance.now() + 1000;
      void video.current?.play().catch(() => {});
      send({ type: "play" });
    }
  }

  // fullscreen retains the custom timeline where supported and falls back to iPhone video fullscreen.
  function fullscreen() {
    const element = video.current as
      (HTMLVideoElement & { webkitEnterFullscreen?: () => void }) | null;
    if (document.fullscreenElement) {
      void document.exitFullscreen();
    } else if (screen.current?.requestFullscreen) {
      void screen.current
        .requestFullscreen()
        .catch((failure) => setError(failure.message));
    } else {
      element?.webkitEnterFullscreen?.();
    }
  }

  if (login) {
    return (
      <div className="login-page">
        <header className="brand">
          <Clapperboard size={24} />
          <span>
            watchd<span className="brand-period">.</span>
          </span>
        </header>
        <main className="login-card">
          <div className="login-icon">
            <Film size={32} strokeWidth={1.4} />
          </div>
          <span className="eyebrow">A PRIVATE SCREENING</span>
          <h1>
            Same movie.
            <br />
            Same moment.
          </h1>
          <p className="muted">A little room for watching together.</p>
          <form onSubmit={signIn}>
            <label>
              Your name
              <Input
                autoComplete="nickname"
                placeholder="How should we call you?"
                value={name}
                maxLength={64}
                onChange={(event) => {
                  setName(event.target.value);
                  localStorage.setItem("watchd-name", event.target.value);
                }}
              />
            </label>
            <label>
              Room password
              <Input
                type="password"
                autoComplete="current-password"
                required
                value={password}
                onChange={(event) => setPassword(event.target.value)}
                placeholder="Enter the shared password"
              />
            </label>
            {error && <div className="notice error">{error}</div>}
            <Button type="submit" disabled={busy} className="w-full">
              {busy ? <LoaderCircle className="spin" /> : <LockKeyhole />}Enter
              room
              <ArrowRight className="ml-auto" />
            </Button>
          </form>
          <p className="fine-print">
            <Users size={13} />
            One room. Everyone holds the remote.
          </p>
        </main>
        <footer>Bring the popcorn. We’ll keep your place.</footer>
      </div>
    );
  }

  if (!info || !room) {
    return (
      <div className="loading-page">
        <Clapperboard size={32} />
        <p>{error || "Opening your screening room…"}</p>
        {error && (
          <Button
            onClick={() => {
              setError("");
              void load();
            }}
          >
            Try again
          </Button>
        )}
      </div>
    );
  }

  const filename = info.source.format.filename.split(/[\\/]/).pop() || "Movie";
  const ahead = buffered.find(
    ([start, end]) => start <= position + 0.2 && end > position,
  );
  const localAhead = ahead ? Math.max(0, ahead[1] - position) : 0;
  const mediaEnd =
    source === "original" ? info.duration : room.transcode.available;
  const requiredBuffer = Math.min(
    2,
    Math.max(0, mediaEnd - room.position - 0.15),
  );
  const waiting = room.members.filter((member) => {
    if (!member.joined) {
      return false;
    }

    if (member.state !== "ready" || member.epoch !== room.epoch) {
      return true;
    }

    return !(member.buffered || []).some(
      ([start, end]) =>
        start <= room.position + 0.2 && end >= room.position + requiredBuffer,
    );
  });
  let statusText = "Paused for everyone";
  if (!connected) {
    statusText = "Connection lost · reconnecting";
  } else if (playerError) {
    statusText = "This media needs your attention";
  } else if (needsGesture) {
    statusText = "Tap to enable playback on this device";
  } else if (!joined) {
    statusText = "You’re observing · join when you’re ready";
  } else if (room.reason === "transcoding") {
    statusText = "Waiting for the server to transcode this position";
  } else if (room.reason === "partial_end") {
    statusText = "End of available output · transcode is not running";
  } else if (room.reason === "viewers") {
    if (waiting.some((member) => member.id === viewerID)) {
      statusText = "Buffering on this device";
    } else {
      const names =
        waiting.map((member) => member.name).join(", ") || "other viewers";
      statusText = "Waiting for " + names + " to buffer";
    }
  } else if (room.reason === "no_viewers") {
    statusText = "Waiting for someone to join playback";
  } else if (room.reason === "ended") {
    statusText = "That’s a wrap.";
  } else if (room.playing) {
    statusText = "Playing together";
  }
  const showBusy = connected && joined && room.wantPlay && !room.playing;
  const sourceVideo = info.source.streams.find(
    (stream) =>
      stream.codec_type === "video" && !stream.disposition?.attached_pic,
  );

  let transcodeLabel = room.transcode.state;
  if (["starting", "running"].includes(room.transcode.state)) {
    transcodeLabel = room.transcode.speed || "Starting";
  } else if (room.transcode.state === "idle") {
    transcodeLabel = "Idle";
  }

  return (
    <div className="app-shell">
      <header className="app-header">
        <a href="/" className="brand">
          <Clapperboard size={23} />
          <span>
            watchd<span className="brand-period">.</span>
          </span>
        </a>
        <div className="header-right">
          <span className="room-badge">
            <span className={`status-dot ${connected ? "" : "offline"}`} />
            {connected ? "Room connected" : "Reconnecting"}
          </span>
          <span className="member-count">
            <Users size={16} />
            {room.members.filter((member) => member.connected).length}
          </span>
        </div>
      </header>
      <main>
        <div className="movie-heading">
          <div>
            <span className="eyebrow">NOW SHOWING</span>
            <h1 title={filename}>{filename}</h1>
            <p className="movie-meta">
              {time(info.duration)}
              <span>·</span>
              {sourceVideo
                ? `${sourceVideo.width} × ${sourceVideo.height}`
                : "Video"}
              <span>·</span>
              {sourceVideo?.codec_name.toUpperCase()}
            </p>
          </div>
          <div
            className="source-picker"
            role="group"
            aria-label="Shared media source"
          >
            <button
              className={source === "original" ? "selected" : ""}
              disabled={!connected}
              onClick={() => send({ type: "source", source: "original" })}
            >
              Source
            </button>
            <button
              className={source === "transcoded" ? "selected" : ""}
              disabled={!connected || !generation}
              onClick={() => send({ type: "source", source: "transcoded" })}
            >
              Transcoded{generation > 0 && <span className="tiny-dot" />}
            </button>
          </div>
        </div>
        {error && (
          <div className="notice error" role="alert">
            {error}
            <button aria-label="Dismiss error" onClick={() => setError("")}>
              <X size={16} />
            </button>
          </div>
        )}
        <div className="watch-layout">
          <div className="main-column">
            <section
              className="player-card"
              ref={screen}
              onKeyDown={(event) => {
                if (
                  event.target instanceof HTMLInputElement ||
                  event.target instanceof HTMLSelectElement ||
                  event.target instanceof HTMLButtonElement
                ) {
                  return;
                }

                if (event.code === "Space") {
                  event.preventDefault();
                  toggle();
                }
              }}
              tabIndex={0}
              aria-label="Movie player"
            >
              <div className="screen">
                <video
                  ref={video}
                  playsInline
                  preload="auto"
                  onSeeking={() => {
                    if (
                      joinedRef.current &&
                      performance.now() > suppressUntil.current &&
                      video.current
                    ) {
                      send({
                        type: "seek",
                        position: video.current.currentTime,
                      });
                    }
                  }}
                  onPause={() => {
                    if (
                      joinedRef.current &&
                      roomRef.current?.playing &&
                      performance.now() > suppressUntil.current
                    ) {
                      send({ type: "pause" });
                    }
                  }}
                  onPlay={() => {
                    if (
                      joinedRef.current &&
                      !roomRef.current?.wantPlay &&
                      performance.now() > suppressUntil.current
                    ) {
                      send({ type: "play" });
                    }
                  }}
                  onVolumeChange={() => {
                    if (video.current) {
                      setMuted(video.current.muted);
                      setVolume(video.current.volume);
                    }
                  }}
                >
                  {source === "transcoded" &&
                    room.transcode.options.subtitleMode === "webvtt" &&
                    hasOutput && (
                      <track
                        key={generation}
                        kind="subtitles"
                        label="Room subtitles"
                        srcLang="en"
                        src={`/room/transcode/${generation}/subtitles.vtt`}
                        default
                      />
                    )}
                </video>
                {(!joined ||
                  playerError ||
                  (source === "transcoded" && !hasOutput)) && (
                  <div className="screen-overlay">
                    <div className="screen-symbol">
                      <Film size={34} strokeWidth={1.2} />
                    </div>
                    {playerError ? (
                      <>
                        <h2>Let’s try another format.</h2>
                        <p>{playerError}</p>
                        <Button
                          variant="outline"
                          onClick={() => setPanel("transcode")}
                        >
                          Open transcode
                          <ArrowRight />
                        </Button>
                      </>
                    ) : source === "transcoded" && !hasOutput ? (
                      <>
                        <LoaderCircle className="spin" />
                        <h2>Preparing the first frames.</h2>
                        <p>
                          {room.transcode.state === "failed"
                            ? "The transcode failed. Check its log below."
                            : "Playback becomes available when the first complete segment is ready."}
                        </p>
                      </>
                    ) : (
                      <>
                        <h2>Ready when you are.</h2>
                        <p>Join the room’s shared timeline.</p>
                        <Button
                          onClick={() => void join()}
                          disabled={!connected}
                        >
                          <Play />
                          Join & play
                        </Button>
                      </>
                    )}
                  </div>
                )}
                <span className="screen-label">
                  <Radio size={12} />
                  {source === "original" ? "SOURCE MEDIA" : "TRANSCODED MEDIA"}
                </span>
              </div>
              <div className="player-controls">
                <div className="playback-row">
                  <div className="playback-main">
                    <Button
                      aria-label={
                        room.wantPlay ? "Pause everyone" : "Play together"
                      }
                      size="icon"
                      onClick={toggle}
                      disabled={!connected}
                    >
                      {room.wantPlay && joined ? (
                        <Pause fill="currentColor" />
                      ) : (
                        <Play fill="currentColor" />
                      )}
                    </Button>
                    <div>
                      <strong>{statusText}</strong>
                      <span>
                        {room.changedBy
                          ? `Last control by ${room.changedBy}`
                          : "Playback controls are shared with everyone."}
                      </span>
                    </div>
                    {showBusy && (
                      <LoaderCircle size={16} className="spin shrink-0" />
                    )}
                  </div>
                  <div className="volume-controls">
                    <Button
                      variant="ghost"
                      size="icon"
                      aria-label={muted ? "Unmute" : "Mute"}
                      onClick={() => {
                        if (video.current) {
                          video.current.muted = !video.current.muted;
                        }
                      }}
                    >
                      {muted ? <VolumeX /> : <Volume2 />}
                    </Button>
                    <input
                      type="range"
                      aria-label="Volume"
                      min={0}
                      max={1}
                      step={0.05}
                      value={volume}
                      onChange={(event) => {
                        if (video.current) {
                          video.current.volume = Number(event.target.value);
                        }
                      }}
                    />
                    <Button
                      variant="ghost"
                      size="icon"
                      aria-label="Fullscreen"
                      onClick={fullscreen}
                    >
                      <Maximize />
                    </Button>
                  </div>
                </div>
                {needsGesture && (
                  <Button variant="outline" onClick={() => void join()}>
                    Enable playback
                  </Button>
                )}
                <Timeline
                  duration={info.duration}
                  position={position}
                  buffered={buffered}
                  available={
                    source === "transcoded" ? room.transcode.available : null
                  }
                  onSeek={seek}
                  disabled={!connected}
                />
              </div>
            </section>
            <div className="buffer-stats">
              <div>
                <span className="stat-label">YOUR BUFFER</span>
                <strong>
                  {time(localAhead)}
                  <small>ahead</small>
                </strong>
              </div>
              <div>
                <span className="stat-label">SERVER OUTPUT</span>
                <strong>
                  {generation ? time(room.transcode.available) : "—"}
                  <small>{generation ? "ready" : "not started"}</small>
                </strong>
              </div>
              <div>
                <span className="stat-label">TRANSCODE</span>
                <strong>
                  {transcodeLabel}
                  <small>
                    {generation
                      ? `${Math.round((room.transcode.available / info.duration) * 100)}% ready`
                      : "manual only"}
                  </small>
                </strong>
              </div>
            </div>
            <div
              className="detail-tabs"
              role="tablist"
              aria-label="Movie details"
            >
              <button
                role="tab"
                aria-selected={panel === "media"}
                onClick={() => setPanel("media")}
              >
                <InfoIcon size={16} />
                Media information
              </button>
              <button
                role="tab"
                aria-selected={panel === "transcode"}
                onClick={() => setPanel("transcode")}
              >
                <Clapperboard size={16} />
                Transcode
              </button>
            </div>
            {panel === "transcode" ? (
              <TranscodePanel
                info={info}
                status={room.transcode}
                onError={setError}
              />
            ) : (
              <section className="panel">
                <div className="section-heading">
                  <div>
                    <span className="eyebrow">UNDER THE HOOD</span>
                    <h2>The file, in detail.</h2>
                  </div>
                  <InfoIcon size={20} className="dim" />
                </div>
                <MediaDetails title="Original media" info={info.source} />
                {room.transcode.output ? (
                  <>
                    <p className="fine-print">
                      Output metadata is measured from the first complete
                      segment; its size is not the full movie size.
                    </p>
                    <MediaDetails
                      title="Transcoded output"
                      info={room.transcode.output}
                    />
                  </>
                ) : (
                  <p className="fine-print">
                    Transcoded media information will appear after the first
                    segment is generated.
                  </p>
                )}
                <details className="buffer-details">
                  <summary>
                    Buffer ranges
                    <ChevronDown size={15} />
                  </summary>
                  <p>
                    This device:{" "}
                    {buffered.length
                      ? buffered
                          .map(([start, end]) => `${time(start)}–${time(end)}`)
                          .join(", ")
                      : "No buffered media"}
                  </p>
                  <p>
                    Server:{" "}
                    {generation
                      ? `00:00–${time(room.transcode.available)}`
                      : "No transcode started"}
                  </p>
                </details>
              </section>
            )}
          </div>
          <aside className="sidebar">
            <section className="panel audience-panel">
              <div className="audience-title">
                <h2>In the room</h2>
                <span>
                  {room.members.filter((member) => member.connected).length}
                </span>
              </div>
              <p className="fine-print">A shared screen, wherever you are.</p>
              <div className="member-list">
                {room.members.map((member, index) => {
                  const range = (member.buffered || []).find(
                    ([start, end]) =>
                      start <= room.position + 0.2 && end >= room.position,
                  );
                  const seconds = range
                    ? Math.max(0, range[1] - room.position)
                    : 0;

                  let memberStatus =
                    time(seconds) +
                    " buffered · " +
                    Math.abs(member.position - room.position).toFixed(1) +
                    "s offset";
                  if (!member.connected) {
                    memberStatus = "Disconnected · waiting briefly";
                  } else if (!member.joined) {
                    memberStatus = "Observing";
                  } else if (member.state === "gesture") {
                    memberStatus = "Needs a tap to play";
                  } else if (member.state === "error") {
                    memberStatus = "Playback error";
                  }

                  return (
                    <div className="member" key={member.id}>
                      <div className={`avatar avatar-${index % 3}`}>
                        {member.name.slice(0, 1).toUpperCase()}
                      </div>
                      <div className="member-info">
                        <strong>
                          {member.name}
                          {member.id === viewerID && <span>you</span>}
                        </strong>
                        <code>{member.address}</code>
                        <p>{memberStatus}</p>
                      </div>
                      {member.joined && member.state === "ready" && (
                        <Check size={14} className="ready-check" />
                      )}
                    </div>
                  );
                })}
              </div>
              {joined && (
                <Button
                  variant="ghost"
                  className="leave-button"
                  onClick={() => {
                    joinedRef.current = false;
                    setJoined(false);
                    send({ type: "leave" });
                    suppressUntil.current = performance.now() + 1000;
                    video.current?.pause();
                  }}
                >
                  <LogOut />
                  Leave playback
                </Button>
              )}
            </section>
            <section className="room-note">
              <CircleHelp size={18} />
              <div>
                <h3>Everyone holds the remote.</h3>
                <p>
                  Play, pause, seek, or switch media for the whole room. Volume
                  stays yours.
                </p>
                <p>
                  When someone buffers, the room waits. Viewers who disconnect
                  are released after a short grace period.
                </p>
              </div>
            </section>
          </aside>
        </div>
      </main>
      <footer className="app-footer">
        <span>One file. One room. A moment together.</span>
        <span>watchd</span>
      </footer>
    </div>
  );
}
