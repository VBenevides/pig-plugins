import { describe, expect, it } from "vitest";
import {
  collectTextContent,
  extractSavedScreenshotPaths,
  hasInlineImageContent,
  inferMimeType,
  isScreenshotToolResult,
  resolveMaybeRelativePath,
} from "./path-utils.ts";

describe("screenshot paths", () => {
  it("extracts case-insensitive multiline messages and strips terminal punctuation", () => {
    expect(
      extractSavedScreenshotPaths(
        "Saved screenshot to   /tmp/My Photo.png.\nother output\nsaved screenshot to\t/tmp/next.jpg",
      ),
    ).toEqual(["/tmp/My Photo.png", "/tmp/next.jpg"]);
  });

  it("ignores empty messages and bounds whitespace-only matching", () => {
    expect(
      extractSavedScreenshotPaths("Saved screenshot to " + " ".repeat(100000)),
    ).toEqual([]);
    expect(
      extractSavedScreenshotPaths("Saved screenshot to\n/tmp/photo.png"),
    ).toEqual(["/tmp/photo.png"]);
  });
});

describe("image metadata", () => {
  it("recognizes supported extensions without accepting unrelated files", () => {
    for (const [extension, mime] of Object.entries({
      PNG: "image/png",
      jpg: "image/jpeg",
      jpeg: "image/jpeg",
      gif: "image/gif",
      webp: "image/webp",
    }))
      expect(inferMimeType(`/tmp/photo.${extension}`)).toBe(mime);
    expect(inferMimeType("/tmp/photo.txt")).toBeNull();
    expect(resolveMaybeRelativePath("photo.png", "/tmp")).toBe(
      "/tmp/photo.png",
    );
    expect(resolveMaybeRelativePath("/tmp/photo.png", "/elsewhere")).toBe(
      "/tmp/photo.png",
    );
  });

  it("recognizes screenshot tools directly and through details", () => {
    expect(isScreenshotToolResult({ toolName: "take_screenshot" })).toBe(true);
    expect(
      isScreenshotToolResult({
        toolName: "wrapper",
        details: { tool: "chrome_devtools_take_screenshot" },
      }),
    ).toBe(true);
    for (const details of [
      undefined,
      null,
      "take_screenshot",
      {},
      { tool: 3 },
    ]) {
      expect(isScreenshotToolResult({ toolName: "wrapper", details })).toBe(
        false,
      );
    }
  });

  it("collects only nonempty text and identifies inline images", () => {
    expect(
      collectTextContent([
        { type: "text", text: "first" },
        { type: "image" },
        { type: "text", text: "" },
        { type: "text", text: "last" },
      ]),
    ).toBe("first\nlast");
    expect(hasInlineImageContent([{ type: "image" }])).toBe(true);
    expect(hasInlineImageContent([{ type: "text" }])).toBe(false);
  });
});
