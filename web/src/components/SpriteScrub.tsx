import { useState, type CSSProperties } from "react";
import type { Sprite } from "../api";

// Candidate for ui-assets: a hover-scrub thumbnail drawn from sprite sheets.

/** CSS for the sprite tile nearest time t. */
export function spriteStyle(sprite: Sprite, t: number): CSSProperties {
  const perSheet = sprite.cols * sprite.rows;
  const last = Math.max(0, sprite.count - 1);
  const i = Math.min(Math.max(0, Math.round(t / sprite.interval)), last);
  const sheet = Math.floor(i / perSheet) + 1;
  const j = i % perSheet;
  const col = j % sprite.cols;
  const row = Math.floor(j / sprite.cols);
  const pct = (n: number, of: number) => (of > 1 ? (n / (of - 1)) * 100 : 0);
  return {
    backgroundImage: `url(${sprite.url.replace("{sheet}", String(sheet))})`,
    backgroundSize: `${sprite.cols * 100}% ${sprite.rows * 100}%`,
    backgroundPosition: `${pct(col, sprite.cols)}% ${pct(row, sprite.rows)}%`,
  };
}

interface SpriteScrubProps {
  sprite: Sprite | null;
  /** The range a horizontal hover sweeps over. */
  tIn: number;
  tOut: number;
  /** What it shows at rest. */
  poster?: number;
  className?: string;
}

/** A 16:9 thumbnail that scrubs through [tIn, tOut] as the pointer moves across it. */
export function SpriteScrub({ sprite, tIn, tOut, poster, className = "" }: SpriteScrubProps) {
  const [hover, setHover] = useState<number | null>(null);
  const t = hover ?? poster ?? tIn;
  return (
    <div
      className={`relative aspect-video w-full overflow-hidden bg-black ${className}`}
      onPointerMove={(e) => {
        const box = e.currentTarget.getBoundingClientRect();
        const f = Math.min(Math.max((e.clientX - box.left) / box.width, 0), 1);
        setHover(tIn + f * (tOut - tIn));
      }}
      onPointerLeave={() => setHover(null)}
    >
      {sprite ? <div className="absolute inset-0" style={spriteStyle(sprite, t)} /> : null}
      {hover !== null && tOut > tIn ? (
        <div className="absolute inset-x-0 bottom-0 h-0.5 bg-white/20">
          <div className="h-full bg-white/80" style={{ width: `${((hover - tIn) / (tOut - tIn)) * 100}%` }} />
        </div>
      ) : null}
    </div>
  );
}
