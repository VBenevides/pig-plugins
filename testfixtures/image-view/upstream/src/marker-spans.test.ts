import { describe, expect, it } from "vitest";
import {
  imageMarkerSpans,
  markerSpanAt,
  segmentAtomicImageMarkers,
} from "./marker-spans";

describe("image marker spans", () => {
  it("finds complete markers and retains UTF-16 positions", () => {
    expect(imageMarkerSpans("😀 [Image #12] x [Image #0]")).toEqual([
      { start: 3, end: 14, text: "[Image #12]" },
      { start: 17, end: 27, text: "[Image #0]" },
    ]);
    expect(
      imageMarkerSpans("[Image #] [Image #-1] [Image #x] [Image #2"),
    ).toEqual([]);
  });

  it.each(["left", "backspace"] as const)(
    "uses the trailing boundary for %s",
    (action) => {
      const text = "a[Image #1]b";
      expect(markerSpanAt(text, 1, action)).toBeUndefined();
      expect(markerSpanAt(text, 2, action)?.text).toBe("[Image #1]");
      expect(markerSpanAt(text, 11, action)?.text).toBe("[Image #1]");
      expect(markerSpanAt(text, 12, action)).toBeUndefined();
    },
  );

  it.each(["right", "delete"] as const)(
    "uses the leading boundary for %s",
    (action) => {
      const text = "a[Image #1]b";
      expect(markerSpanAt(text, 0, action)).toBeUndefined();
      expect(markerSpanAt(text, 1, action)?.text).toBe("[Image #1]");
      expect(markerSpanAt(text, 10, action)?.text).toBe("[Image #1]");
      expect(markerSpanAt(text, 11, action)).toBeUndefined();
    },
  );

  it("segments adjacent markers atomically and preserves Unicode graphemes", () => {
    const text = "e\u0301[Image #1][Image #22]😀";
    const segments = segmentAtomicImageMarkers(text);
    expect(segments.map(({ segment }) => segment)).toEqual([
      "e\u0301",
      "[Image #1]",
      "[Image #22]",
      "😀",
    ]);
    expect(segments.map(({ index }) => index)).toEqual([0, 2, 12, 23]);
    expect(segments.every(({ input }) => input === text)).toBe(true);
    expect(segmentAtomicImageMarkers("")).toEqual([]);
    expect(
      segmentAtomicImageMarkers("e\u0301").map(({ segment }) => segment),
    ).toEqual(["e\u0301"]);
  });
});
