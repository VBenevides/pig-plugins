// Package websearch implements provider-native grounded search transports.
package websearch

import (
	"fmt"
	"net/url"
	"path"
	"slices"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

type Source struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}
type Detail map[string]any
type Result struct {
	Text                      string
	Kind                      string
	Sources                   []Source
	Results, Citations, Calls []Detail
	Events, Queries, Warnings []string
	Grounding, URLContext     map[string]any
	Native                    bool
}

func str(m map[string]any, k string) string { s, _ := m[k].(string); return s }
func obj(value any) map[string]any          { m, _ := value.(map[string]any); return m }
func arr(value any) []any                   { a, _ := value.([]any); return a }
func number(value any) float64              { n, _ := value.(float64); return n }
func first(values ...string) string {
	for _, s := range values {
		if s != "" {
			return s
		}
	}
	return ""
}
func unique(values *[]string, value string) {
	if value != "" && !slices.Contains(*values, value) {
		*values = append(*values, value)
	}
}
func addDetail(values *[]Detail, d Detail) {
	key := func(item Detail) string {
		return str(item, "url") + "\t" + str(item, "title") + "\t" + str(item, "query") + "\t" + str(item, "citedText") + "\t" + str(item, "type")
	}
	for _, item := range *values {
		if key(item) == key(d) {
			return
		}
	}
	*values = append(*values, d)
}
func title(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	part := path.Base(strings.TrimRight(u.EscapedPath(), "/"))
	if part != "." && part != "/" && part != "" {
		return part
	}
	return first(u.Hostname(), raw)
}
func normalize(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" {
		return raw
	}
	u.Fragment = ""
	host := strings.ToLower(u.Hostname())
	if host != "youtube.com" && !strings.HasSuffix(host, ".youtube.com") && host != "youtu.be" && !strings.HasSuffix(host, ".youtu.be") {
		q := u.Query()
		for k := range q {
			if slices.Contains([]string{"ref", "referral_type", "openLinerExtension", "_clear", "lang", "api-mode"}, k) || strings.HasPrefix(strings.ToLower(k), "utm_") {
				q.Del(k)
			}
		}
		u.RawQuery = q.Encode()
	}
	return u.String()
}
func junk(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	p := strings.ToLower(u.Path)
	for _, suffix := range strings.Fields(".gz .zip .tgz .tar .woff .woff2 .ttf .otf .eot .webm .mp4 .mp3 .wav .eps .sql .csv .xls .xlsx .ppt .pptx") {
		if strings.HasSuffix(p, suffix) {
			return true
		}
	}
	return strings.HasSuffix(p, "/%")
}
func sanitized(details []Detail) []Detail {
	var out []Detail
	for _, d := range details {
		raw := str(d, "url")
		if raw != "" {
			raw = normalize(raw)
			if junk(raw) {
				continue
			}
			d["url"] = raw
		}
		addDetail(&out, d)
	}
	return out
}
func addSource(sources *[]Source, source Source) int {
	source.Title = first(source.Title, "Unknown")
	for i, s := range *sources {
		if s == source {
			return i
		}
	}
	*sources = append(*sources, source)
	return len(*sources) - 1
}

type insertion struct {
	index  int
	marker string
}

// Insertions are sorted descending, so provider offsets refer to the original answer.
func insert(text string, items []insertion) string {
	if len(items) == 0 {
		return text
	}
	slices.SortStableFunc(items, func(a, b insertion) int { return b.index - a.index })
	var result strings.Builder
	extra := 0
	for _, item := range items {
		extra += len(item.marker)
	}
	result.Grow(len(text) + extra)
	seen := map[string]bool{}
	last := 0
	for index := len(items) - 1; index >= 0; index-- {
		item := items[index]
		key := fmt.Sprint(item.index, ":", item.marker)
		if seen[key] {
			continue
		}
		seen[key] = true
		i := max(last, min(item.index, len(text)))
		result.WriteString(text[last:i])
		result.WriteString(item.marker)
		last = i
	}
	result.WriteString(text[last:])
	return strings.ToValidUTF8(result.String(), "�")
}
func citationByteIndices(text string, citations []Detail) map[int]int {
	var ends []int
	for _, c := range citations {
		if c["endIndex"] != nil {
			ends = append(ends, int(number(c["endIndex"])))
		}
	}
	slices.Sort(ends)
	ends = slices.Compact(ends)
	positions := make(map[int]int, len(ends))
	index, count := 0, 0
	for offset, r := range text {
		for index < len(ends) && count >= ends[index] {
			positions[ends[index]] = offset
			index++
		}
		if index == len(ends) {
			return positions
		}
		count += utf16.RuneLen(r)
	}
	for _, end := range ends[index:] {
		positions[end] = len(text)
	}
	return positions
}
func (r *Result) finalize() {
	if r.Text == "" {
		r.Text = "No answer available."
	}
	var sources []Source
	var inserts []insertion
	if r.Kind == "google" {
		chunks := arr(r.Grounding["groundingChunks"])
		for _, chunk := range chunks {
			web := obj(obj(chunk)["web"])
			if web != nil {
				sources = append(sources, Source{first(str(web, "title"), "Unknown"), str(web, "uri")})
			}
		}
		for _, support := range arr(r.Grounding["groundingSupports"]) {
			s := obj(support)
			segment := obj(s["segment"])
			if segment["endIndex"] == nil {
				continue
			}
			marker := ""
			for _, index := range arr(s["groundingChunkIndices"]) {
				marker += fmt.Sprintf("[%d]", int(number(index))+1)
			}
			if marker != "" {
				inserts = append(inserts, insertion{int(number(segment["endIndex"])), marker})
			}
		}
	} else {
		var positions map[int]int
		if r.Kind == "openai" {
			positions = citationByteIndices(r.Text, r.Citations)
		}
		for _, c := range r.Citations {
			raw := str(c, "url")
			if raw == "" {
				continue
			}
			n := addSource(&sources, Source{str(c, "title"), raw})
			marker := fmt.Sprintf("[%d]", n+1)
			if r.Kind == "openai" && c["endIndex"] != nil {
				inserts = append(inserts, insertion{positions[int(number(c["endIndex"]))], marker})
			} else if r.Kind == "anthropic" {
				cited := strings.TrimSpace(str(c, "citedText"))
				if cited != "" {
					if i := strings.Index(r.Text, cited); i >= 0 {
						inserts = append(inserts, insertion{i + len(cited), marker})
					}
				}
			}
		}
	}
	r.Text = insert(r.Text, inserts)
	r.Results = sanitized(r.Results)
	r.Citations = sanitized(r.Citations)
	for _, s := range sources {
		s.URL = normalize(s.URL)
		if !junk(s.URL) {
			r.Sources = append(r.Sources, s)
		}
	}
	if len(r.Sources) == 0 {
		for _, d := range append(slices.Clone(r.Citations), r.Results...) {
			raw := str(d, "url")
			if raw != "" {
				addSource(&r.Sources, Source{first(str(d, "title"), title(raw)), raw})
			}
		}
	}
	r.Native = r.Native || len(r.Events) > 0 || len(r.Calls) > 0 || len(r.Results) > 0
}
func (r Result) Format(model string, urlOnly bool) (string, map[string]any) {
	summary := r.Text
	var retrieved []string
	var failed []Detail
	metadata := r.URLContext["urlMetadata"]
	if metadata == nil {
		metadata = r.URLContext["url_metadata"]
	}
	for _, v := range arr(metadata) {
		m := obj(v)
		raw := first(str(m, "retrievedUrl"), str(m, "retrieved_url"), str(m, "url"))
		status := first(str(m, "urlRetrievalStatus"), str(m, "url_retrieval_status"))
		if status == "URL_RETRIEVAL_STATUS_SUCCESS" {
			retrieved = append(retrieved, raw)
		} else {
			failed = append(failed, Detail{"url": raw, "status": status})
		}
	}
	var sections strings.Builder
	if len(failed) > 0 {
		fmt.Fprintf(&sections, "\n\n## URL Status\n✅ Retrieved: %d\n❌ Failed: %d", len(retrieved), len(failed))
		for _, f := range failed {
			fmt.Fprintf(&sections, "\n- %s: %s", str(f, "url"), str(f, "status"))
		}
	}
	var additional []Detail
	seen := map[string]bool{}
	for _, d := range r.Results {
		raw := str(d, "url")
		if raw == "" || slices.ContainsFunc(r.Sources, func(s Source) bool { return s.URL == raw }) {
			continue
		}
		key := str(d, "title") + "\t" + raw
		if !urlOnly && seen[key] {
			continue
		}
		seen[key] = true
		additional = append(additional, d)
	}
	if urlOnly && len(retrieved)+len(failed)+len(r.Sources)+len(additional) == 0 {
		sections.WriteString("\n\n## URL Context Verification\n⚠️ No verified URL context metadata was returned by provider " + r.Kind + ". Treat the answer as ungrounded unless sources, retrieved URLs, or searchResults are present in tool details.")
	}
	if len(r.Sources) > 0 && !strings.Contains(summary, "## Sources") {
		sections.WriteString("\n\n## Sources")
		for i, s := range r.Sources {
			fmt.Fprintf(&sections, "\n%d. [%s](%s)", i+1, s.Title, s.URL)
		}
	}
	if len(additional) > 0 {
		sections.WriteString("\n\n## Additional Search Results")
		visible := additional
		if urlOnly {
			visible = visible[:min(8, len(visible))]
		}
		for i, d := range visible {
			fmt.Fprintf(&sections, "\n%d. %s - %s", i+1, first(str(d, "title"), str(d, "url")), str(d, "url"))
			if urlOnly {
				var meta []string
				for _, key := range []string{"source", "type", "status"} {
					if value := str(d, key); value != "" {
						meta = append(meta, value)
					}
				}
				if query := str(d, "query"); query != "" {
					meta = append(meta, "query="+query)
				}
				if len(meta) > 0 {
					sections.WriteString(" (" + strings.Join(meta, ", ") + ")")
				}
			}
		}
		if len(visible) < len(additional) {
			fmt.Fprintf(&sections, "\n... and %d more results in tool details.", len(additional)-len(visible))
		}
	}
	summary += sections.String()
	truncated := false
	lines := strings.Split(summary, "\n")
	if len(lines) > 2000 {
		summary = strings.Join(lines[:2000], "\n")
		truncated = true
	}
	if len(summary) > 50<<10 {
		n := 50 << 10
		for !utf8.ValidString(summary[:n]) {
			n--
		}
		summary = summary[:n]
		truncated = true
	}
	if truncated {
		summary += "\n\n[Truncated]"
	}
	count := len(r.Results)
	if count == 0 {
		count = len(r.Sources)
	}
	details := map[string]any{"sources": r.Sources, "providerKind": r.Kind, "nativeSearchUsed": r.Native, "nativeSearchEvents": r.Events, "nativeSearchCalls": r.Calls, "searchQueries": r.Queries, "searchResults": r.Results, "citations": r.Citations, "model": model, "grounded": len(r.Sources) > 0 || len(r.Results) > 0, "resultCount": count}
	if len(retrieved) > 0 || urlOnly {
		details["retrieved"] = retrieved
	}
	if len(failed) > 0 {
		details["failed"] = failed
	}
	if len(r.Warnings) > 0 {
		details["warnings"] = r.Warnings
	}
	return summary, details
}
