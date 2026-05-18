const defaultVisibleStreamingTextCap = 4000;

export const capSideQuestionVisibleStreamingText = (
	visibleText: string,
	cap = defaultVisibleStreamingTextCap,
): string => {
	if (cap <= 0) {
		return "";
	}
	const text = visibleText.trim();
	if (text.length <= cap) {
		return text;
	}
	return text.slice(text.length - cap);
};
