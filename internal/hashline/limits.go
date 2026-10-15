package hashline

const (
	MaxEditOperations = 256
	MaxEditLines      = 100000
)

// Subtract from fixed budgets instead of summing untrusted lengths, so checks
// cannot overflow. Check the serialized representation before allocating it.
func validateResultSize(file ParsedFile) error {
	if len(file.Lines) > MaxEditLines {
		return editErrorf("Edit result exceeds %d lines; nothing was written.", MaxEditLines)
	}
	remaining := MaxEditBytes
	if file.BOM {
		remaining -= len(byteOrderMark)
	}
	for _, line := range file.Lines {
		for _, part := range []string{line.Text, line.EOL} {
			if len(part) > remaining {
				return editErrorf("Edit result exceeds %d bytes; nothing was written.", MaxEditBytes)
			}
			remaining -= len(part)
		}
	}
	return nil
}
