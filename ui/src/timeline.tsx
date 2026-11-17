import { useState } from "react";
import { time } from "./lib";
import type { Span } from "./types";

type Props = {
  duration: number;
  position: number;
  buffered: Span[];
  available: number | null;
  onSeek: (position: number) => void;
  disabled?: boolean;
};

// Timeline keeps generated media, local buffered ranges, and playback visually distinct.
export function Timeline({
  duration,
  position,
  buffered,
  available,
  onSeek,
  disabled,
}: Props) {
  const [preview, setPreview] = useState<number | null>(null);
  const target = preview ?? position;
  const isBuffered = buffered.some(
    ([start, end]) => start <= target && end >= target,
  );
  let state = "Not transcoded yet · waiting required";
  if (isBuffered) {
    state = "Buffered on this device";
  } else if (available === null) {
    state = "Load from source";
  } else if (target < available) {
    state = "Transcoded · needs loading";
  }

  return (
    <div className="timeline">
      <div className="timeline-label">
        <span>
          {preview === null ? "MOVIE TIMELINE" : `${time(target)} · ${state}`}
        </span>
        <span>
          {time(position)} <span className="dim">/ {time(duration)}</span>
        </span>
      </div>
      <div className="timeline-rail">
        {available !== null && (
          <div
            className="generated-range"
            style={{ width: `${Math.min(100, (available / duration) * 100)}%` }}
          />
        )}
        {buffered.map(([start, end], index) => (
          <div
            key={index}
            className="buffered-range"
            style={{
              left: `${(start / duration) * 100}%`,
              width: `${((end - start) / duration) * 100}%`,
            }}
          />
        ))}
        <div
          className="played-range"
          style={{ width: `${(position / duration) * 100}%` }}
        />
        <div
          className="playhead"
          style={{ left: `${(target / duration) * 100}%` }}
        />
        <input
          aria-label="Movie position"
          aria-valuetext={`${time(target)}. ${state}`}
          type="range"
          min={0}
          max={duration}
          step={0.1}
          value={target}
          disabled={disabled}
          onPointerDown={() => setPreview(position)}
          onChange={(event) => {
            const value = Number(event.target.value);
            setPreview(value);
          }}
          onPointerUp={(event) => {
            onSeek(Number(event.currentTarget.value));
            setPreview(null);
          }}
          onPointerCancel={() => setPreview(null)}
          onKeyUp={(event) => {
            if (
              [
                "ArrowLeft",
                "ArrowRight",
                "Home",
                "End",
                "PageUp",
                "PageDown",
              ].includes(event.key)
            ) {
              onSeek(Number(event.currentTarget.value));
              setPreview(null);
            }
          }}
          onBlur={() => setPreview(null)}
          onPointerMove={(event) => {
            if (event.buttons === 0 && event.pointerType === "mouse") {
              const bounds = event.currentTarget.getBoundingClientRect();
              setPreview(
                Math.max(
                  0,
                  Math.min(
                    duration,
                    ((event.clientX - bounds.left) / bounds.width) * duration,
                  ),
                ),
              );
            }
          }}
          onPointerLeave={(event) => {
            if (event.buttons === 0) {
              setPreview(null);
            }
          }}
        />
      </div>
      <div className="timeline-legend">
        <span>
          <i className="played-dot" />
          Played
        </span>
        <span>
          <i className="buffered-dot" />
          Buffered on your device
        </span>
        {available !== null && (
          <span>
            <i className="generated-dot" />
            Transcoded · {time(available)}
          </span>
        )}
      </div>
    </div>
  );
}
