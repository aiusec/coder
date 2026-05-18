import { describe, expect, it } from "vitest";
import { capSideQuestionVisibleStreamingText } from "./chatSideQuestionContext";

describe("capSideQuestionVisibleStreamingText", () => {
	it("trims visible text", () => {
		expect(capSideQuestionVisibleStreamingText("  visible answer\n")).toBe(
			"visible answer",
		);
	});

	it("keeps the newest visible text when capped", () => {
		expect(capSideQuestionVisibleStreamingText("old newest", 6)).toBe("newest");
	});
});
