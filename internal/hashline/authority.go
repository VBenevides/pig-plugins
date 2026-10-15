package hashline

func requireDisplayedEdits(request Cancellation, read retainedRead, edits any) error {
	file, err := ParseFileContext(request, string(read.content))
	if err != nil {
		return err
	}
	// Validate the complete batch before inspecting its coverage; runtime validation
	// remains authoritative, independently of the tool schema.
	if _, err := ApplyEditsContext(request, file, edits); err != nil {
		return err
	}
	for index, raw := range edits.([]any) {
		if err := request.Err(); err != nil {
			return err
		}
		item, err := resolveOp(file, raw, index)
		if err != nil {
			return err
		}
		if item.first < 0 {
			continue
		} // Insertion does not destroy source text.
		seen := false
		for _, region := range read.coverage {
			if region[0] <= item.first+1 && region[1] >= item.last+1 {
				seen = true
				break
			}
		}
		if !seen {
			return editErrorf("edits[%d]: source lines %d-%d were not fully displayed. Read the complete range before replacing or deleting it; nothing was edited.", index, item.first+1, item.last+1)
		}
	}
	return nil
}
