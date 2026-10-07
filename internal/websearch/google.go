package websearch

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

func resolveGoogle(ctx context.Context, r *Result) {
	resolved := map[string]string{}
	attempted := map[string]bool{}
	client := http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for _, items := range [][]Detail{r.Results, r.Citations} {
		for _, d := range items {
			raw := str(d, "url")
			if !strings.HasPrefix(raw, "https://vertexaisearch.cloud.google.com/grounding-api-redirect/") || attempted[raw] || len(attempted) >= 20 {
				continue
			}
			attempted[raw] = true
			child, cancel := context.WithTimeout(ctx, 5*time.Second)
			request, err := http.NewRequestWithContext(child, "HEAD", raw, nil)
			if err == nil {
				response, e := client.Do(request)
				err = e
				if response != nil {
					if location := response.Header.Get("Location"); location != "" {
						resolved[raw] = location
					}
					response.Body.Close()
				}
			}
			cancel()
			if err != nil {
				r.Warnings = append(r.Warnings, fmt.Sprintf("Google grounding redirect resolution failed for source %d; original URL retained", len(attempted)))
			}
		}
	}
	for _, items := range [][]Detail{r.Results, r.Citations} {
		for _, d := range items {
			if value := resolved[str(d, "url")]; value != "" {
				d["url"] = value
				if str(d, "title") == "Unknown" || str(d, "title") == "" {
					d["title"] = title(value)
				}
			}
		}
	}
	for _, chunk := range arr(r.Grounding["groundingChunks"]) {
		web := obj(obj(chunk)["web"])
		if value := resolved[str(web, "uri")]; value != "" {
			web["uri"] = value
		}
	}
}
