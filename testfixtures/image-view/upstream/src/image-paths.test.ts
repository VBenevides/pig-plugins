import { describe, expect, it } from "vitest";
import {
  extractImagePaths,
  normalizeDetectedImagePath,
} from "./image-paths.ts";

describe("image path normalization", () => {
  it("preserves escaped and quoted POSIX image paths", () => {
    expect(extractImagePaths('"/tmp/My Photo.png" ./My\\ Photo.jpg')).toEqual([
      { raw: '"/tmp/My Photo.png"', path: "/tmp/My Photo.png" },
      { raw: "./My\\ Photo.jpg", path: `${process.cwd()}/My Photo.jpg` },
    ]);
  });

  it("maps Windows drive paths to WSL without changing UNC paths", () => {
    const options = {
      platform: "linux" as const,
      env: { WSL_INTEROP: "enabled" },
    };
    expect(normalizeDetectedImagePath("C:\\Images\\Photo.png", options)).toBe(
      "/mnt/c/Images/Photo.png",
    );
    expect(
      normalizeDetectedImagePath("\\\\server\\Images\\Photo.png", options),
    ).toBe("\\\\server\\Images\\Photo.png");
  });

  it("recognizes drive and UNC paths with their original spelling", () => {
    expect(
      extractImagePaths("C:\\Images\\Photo.png \\\\server\\Images\\Photo.jpg", {
        platform: "win32",
      }).map((image) => image.raw),
    ).toEqual(["C:\\Images\\Photo.png", "\\\\server\\Images\\Photo.jpg"]);
  });
});
