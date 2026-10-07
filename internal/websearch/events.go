package websearch

import (
	"errors"
	"fmt"
	"strings"
)

type stream struct {
	Result
	text strings.Builder
}

func (r *stream) appendText(text string) { r.text.WriteString(text); r.Text = r.text.String() }
func (r *stream) call(call Detail) {
	id := str(call, "id")
	if id != "" {
		for _, existing := range r.Calls {
			if str(existing, "id") == id {
				for k, v := range call {
					existing[k] = v
				}
				return
			}
		}
	}
	r.Calls = append(r.Calls, call)
}
func (r *stream) openAICall(item map[string]any) {
	if str(item, "type") != "web_search_call" {
		return
	}
	action := obj(item["action"])
	call := Detail{"provider": r.Kind, "raw": item}
	for _, k := range []string{"id", "status"} {
		if item[k] != nil {
			call[k] = item[k]
		}
	}
	if action["type"] != nil {
		call["actionType"] = action["type"]
	}
	var queries []string
	for _, q := range arr(action["queries"]) {
		if s, ok := q.(string); ok {
			queries = append(queries, s)
			unique(&r.Queries, s)
		}
	}
	if q := str(action, "query"); len(queries) == 0 && q != "" {
		queries = append(queries, q)
		unique(&r.Queries, q)
	}
	if len(queries) > 0 {
		call["queries"] = queries
	}
	var urls []string
	for _, v := range arr(action["sources"]) {
		s := obj(v)
		raw := str(s, "url")
		if raw == "" {
			continue
		}
		urls = append(urls, raw)
		addDetail(&r.Results, Detail{"title": first(str(s, "title"), str(s, "display_name"), str(s, "name"), title(raw)), "url": raw, "source": r.Kind + ".web_search_call.action.sources", "type": first(str(s, "type"), "url"), "raw": s})
	}
	if raw := str(action, "url"); raw != "" {
		urls = append(urls, raw)
		addDetail(&r.Results, Detail{"title": title(raw), "url": raw, "source": r.Kind + ".web_search_call.action." + str(action, "type"), "type": str(action, "type"), "raw": action})
	}
	if len(urls) > 0 {
		call["urls"] = urls
	}
	r.call(call)
}
func (r *stream) annotation(a map[string]any) {
	if str(a, "type") != "url_citation" {
		return
	}
	nested := obj(a["url_citation"])
	if nested == nil {
		nested = obj(a["urlCitation"])
	}
	raw := first(str(a, "url"), str(nested, "url"))
	if raw == "" {
		return
	}
	d := Detail{"title": first(str(a, "title"), str(nested, "title"), title(raw)), "url": raw, "source": r.Kind + ".url_citation", "type": "citation", "raw": a}
	for _, m := range []map[string]any{a, nested} {
		for _, key := range []string{"end_index", "endIndex"} {
			if m[key] != nil {
				d["endIndex"] = m[key]
				break
			}
		}
		if d["endIndex"] != nil {
			break
		}
	}
	addDetail(&r.Citations, d)
	addDetail(&r.Results, d)
}
func (r *stream) consume(event map[string]any) (bool, error) {
	if event["error"] != nil || str(event, "type") == "error" || str(event, "type") == "response.failed" {
		message := first(str(event, "message"), str(obj(event["error"]), "message"), str(obj(obj(event["response"])["error"]), "message"))
		if message == "" {
			message = "provider returned an error event"
		}
		return false, errors.New(message)
	}
	if str(event, "type") == "response.incomplete" {
		return false, fmt.Errorf("provider search response incomplete: %s", first(str(obj(obj(event["response"])["incomplete_details"]), "reason"), "unspecified reason"))
	}
	if kind := str(event, "type"); kind == "response.completed" || kind == "response.done" {
		if status := str(obj(event["response"]), "status"); status != "" && status != "completed" {
			return false, fmt.Errorf("provider search terminal status: %s", status)
		}
	}
	if r.Kind == "google" {
		data := event
		if obj(event["response"]) != nil {
			data = obj(event["response"])
		}
		candidates := arr(data["candidates"])
		if len(candidates) > 0 {
			if finish := str(obj(candidates[0]), "finishReason"); finish != "" && finish != "STOP" {
				return false, fmt.Errorf("Google search finish reason: %s", finish)
			}
		}
	}
	if r.Kind == "anthropic" && str(obj(event["delta"]), "stop_reason") == "max_tokens" {
		return false, errors.New("Anthropic search reached max_tokens before completion")
	}
	switch r.Kind {
	case "google":
		data := event
		if obj(event["response"]) != nil {
			data = obj(event["response"])
		}
		candidates := arr(data["candidates"])
		if len(candidates) == 0 {
			return false, nil
		}
		c := obj(candidates[0])
		for _, part := range arr(obj(c["content"])["parts"]) {
			r.appendText(str(obj(part), "text"))
		}
		if obj(c["groundingMetadata"]) != nil {
			r.Grounding = obj(c["groundingMetadata"])
		}
		if obj(c["urlContextMetadata"]) != nil {
			r.URLContext = obj(c["urlContextMetadata"])
		} else if obj(c["url_context_metadata"]) != nil {
			r.URLContext = obj(c["url_context_metadata"])
		}
		for _, q := range arr(r.Grounding["webSearchQueries"]) {
			if s, ok := q.(string); ok {
				unique(&r.Queries, s)
			}
		}
		if len(r.Queries) > 0 {
			unique(&r.Events, "google.groundingMetadata.webSearchQueries")
		}
		chunks := arr(r.Grounding["groundingChunks"])
		for i, chunk := range chunks {
			web := obj(obj(chunk)["web"])
			if web == nil {
				continue
			}
			addDetail(&r.Results, Detail{"title": first(str(web, "title"), "Unknown"), "url": str(web, "uri"), "source": "google.groundingChunks", "type": "web", "raw": map[string]any{"index": i, "web": web}})
		}
		for _, support := range arr(r.Grounding["groundingSupports"]) {
			s := obj(support)
			for _, v := range arr(s["groundingChunkIndices"]) {
				i := int(number(v))
				if i < 0 || i >= len(chunks) {
					continue
				}
				web := obj(obj(chunks[i])["web"])
				if web == nil {
					continue
				}
				addDetail(&r.Citations, Detail{"title": first(str(web, "title"), "Unknown"), "url": str(web, "uri"), "citedText": str(obj(s["segment"]), "text"), "source": "google.groundingSupports", "type": "citation", "raw": s})
			}
		}
		return str(c, "finishReason") != "", nil
	case "openai", "xai":
		kind := str(event, "type")
		switch kind {
		case "response.output_text.delta":
			r.appendText(str(event, "delta"))
		case "response.output_text.annotation.added":
			r.annotation(obj(event["annotation"]))
		case "response.output_item.added", "response.output_item.done":
			r.openAICall(obj(event["item"]))
		case "response.completed", "response.done":
			response := obj(event["response"])
			for _, v := range arr(response["output"]) {
				item := obj(v)
				r.openAICall(item)
				if str(item, "type") == "message" {
					for _, c := range arr(item["content"]) {
						content := obj(c)
						if str(content, "type") == "output_text" {
							for _, a := range arr(content["annotations"]) {
								r.annotation(obj(a))
							}
						}
					}
				}
			}
			return true, nil
		case "response.web_search_call.in_progress", "response.web_search_call.searching", "response.web_search_call.completed":
			unique(&r.Events, kind)
			r.call(Detail{"id": str(event, "item_id"), "provider": r.Kind, "status": strings.TrimPrefix(kind, "response.web_search_call."), "raw": event})
		}
		return false, nil
	case "anthropic":
		switch str(event, "type") {
		case "message_stop":
			return true, nil
		case "content_block_start":
			block := obj(event["content_block"])
			switch str(block, "type") {
			case "text":
				r.appendText(str(block, "text"))
			case "server_tool_use":
				if str(block, "name") == "web_search" {
					unique(&r.Events, "anthropic.content_block_start.server_tool_use.web_search")
					call := Detail{"id": str(block, "id"), "provider": "anthropic", "status": "in_progress", "actionType": "web_search", "raw": block}
					if q := str(obj(block["input"]), "query"); q != "" {
						call["queries"] = []string{q}
						unique(&r.Queries, q)
					}
					r.call(call)
				}
			case "web_search_tool_result":
				unique(&r.Events, "anthropic.content_block_start.web_search_tool_result")
				r.call(Detail{"id": str(block, "tool_use_id"), "provider": "anthropic", "status": "completed", "actionType": "web_search", "raw": block})
				if content := obj(block["content"]); str(content, "type") == "web_search_tool_result_error" {
					addDetail(&r.Results, Detail{"status": str(content, "error_code"), "source": "anthropic.web_search_tool_result_error", "type": "web_search_tool_result_error", "raw": block})
				}
				for _, v := range arr(block["content"]) {
					s := obj(v)
					raw := str(s, "url")
					if raw == "" {
						continue
					}
					name := first(str(s, "title"), title(raw))
					addDetail(&r.Citations, Detail{"title": name, "url": raw, "source": "anthropic.citation", "type": "citation", "raw": s})
					addDetail(&r.Results, Detail{"title": name, "url": raw, "pageAge": s["page_age"], "source": "anthropic.web_search_tool_result", "type": first(str(s, "type"), "web_search_result"), "raw": s})
				}
			}
		case "content_block_delta":
			delta := obj(event["delta"])
			if str(delta, "type") == "text_delta" {
				r.appendText(str(delta, "text"))
			} else if str(delta, "type") == "citations_delta" {
				c := obj(delta["citation"])
				if str(c, "type") == "web_search_result_location" && str(c, "url") != "" {
					d := Detail{"title": first(str(c, "title"), title(str(c, "url"))), "url": str(c, "url"), "citedText": str(c, "cited_text"), "source": "anthropic.citations_delta", "type": str(c, "type"), "raw": c}
					addDetail(&r.Citations, d)
					addDetail(&r.Results, d)
				}
			}
		}
		return false, nil
	}
	return false, fmt.Errorf("unsupported provider kind %s", r.Kind)
}
