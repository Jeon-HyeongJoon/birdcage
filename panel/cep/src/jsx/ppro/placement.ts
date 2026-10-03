// Pure placement logic shared by the ExtendScript bundle and the panel/core tests.
// ASCII ONLY (bundled into ExtendScript, see ppro.ts).

/** A clip on a track as [startSeconds, endSeconds). */
export type Span = [number, number];

/**
 * Returns the lowest track index whose clips do not overlap [start, end), or -1.
 * Touching edges do not overlap (a clip ending exactly at start is fine).
 */
export const firstFreeTrack = (tracks: Span[][], start: number, end: number): number => {
  const eps = 0.0005; // half a millisecond: Premiere times are ticks, seconds are rounded
  for (let i = 0; i < tracks.length; i++) {
    let free = true;
    const clips = tracks[i];
    for (let j = 0; j < clips.length; j++) {
      if (clips[j][0] < end - eps && clips[j][1] > start + eps) {
        free = false;
        break;
      }
    }
    if (free) return i;
  }
  return -1;
};

/** First targeted track index, or 0 when none is targeted (or targeting is unknown). */
export const firstTargeted = (targeted: boolean[]): number => {
  for (let i = 0; i < targeted.length; i++) {
    if (targeted[i]) return i;
  }
  return 0;
};
