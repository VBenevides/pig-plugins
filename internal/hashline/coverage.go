package hashline

// fullyDisplayedRanges records inclusive source ranges, excluding shortened
// lines. It is display metadata, not edit authorization or retained state.
func fullyDisplayedRanges(file ParsedFile, from, to int) [][2]int {
	ranges := make([][2]int, 0)
	for line := from; line <= to; line++ {
		if utf16Length(file.Lines[line-1].Text) > MaxLineChars {
			continue
		}
		if len(ranges) > 0 && ranges[len(ranges)-1][1] == line-1 {
			ranges[len(ranges)-1][1] = line
			continue
		}
		ranges = append(ranges, [2]int{line, line})
	}
	return ranges
}
