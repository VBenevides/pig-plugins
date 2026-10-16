package automodels

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

const cursorUsageURL = "https://cursor.com/api/usage-summary"

// CursorUsage is account dashboard data, not inference entitlement or token usage.
// Nothing here participates in model routing or cooldown decisions.
type CursorUsage struct {
	FetchedAt time.Time
	ResetsAt  *time.Time
	Pools     []CursorPool
}

type CursorPool struct {
	Name      string
	Unit      string
	Used      *float64
	Limit     *float64
	Remaining *float64
}

// FetchCursor uses the selected native OAuth account, with no CLI or credential
// file. Data is fetched on demand; no global or cross-account cache is retained.
func (c Client) FetchCursor(ctx context.Context, entry AuthEntry) (*CursorUsage, error) {
	var payload map[string]any
	if err := c.fetch(ctx, entry, "Cursor", cursorUsageURL, &payload); err != nil {
		return nil, err
	}
	usage, err := parseCursorUsage(payload)
	if err != nil {
		return nil, err
	}
	usage.FetchedAt = time.Now()
	return usage, nil
}

func cursorUserID(token string) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || len(token) > maxReadBytes {
		return "", errors.New("Cursor usage: token subject unavailable")
	}
	data, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return "", errors.New("Cursor usage: token subject unavailable")
	}
	var claims struct {
		Sub string `json:"sub"`
	}
	if json.Unmarshal(data, &claims) != nil {
		return "", errors.New("Cursor usage: token subject unavailable")
	}
	// This unverified claim only constructs the session cookie. The remote service
	// must authenticate it; it is never used to grant access locally.
	_, id, found := strings.Cut(claims.Sub, "|")
	if !found {
		id = claims.Sub
	}
	if len(id) == 0 || len(id) > 256 {
		return "", errors.New("Cursor usage: token subject unavailable")
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return "", errors.New("Cursor usage: unsupported token subject")
		}
	}
	return id, nil
}

func cursorNumber(value any) *float64 {
	var n float64
	switch v := value.(type) {
	case float64:
		n = v
	case string:
		var err error
		n, err = strconv.ParseFloat(v, 64)
		if err != nil {
			return nil
		}
	default:
		return nil
	}
	if math.IsNaN(n) || math.IsInf(n, 0) || n < 0 {
		return nil
	}
	return &n
}

func cursorObject(value any) map[string]any { result, _ := value.(map[string]any); return result }

func cursorCentsPool(name string, bucket map[string]any) *CursorPool {
	if bucket == nil || bucket["enabled"] == false {
		return nil
	}
	used, limit, remaining := cursorNumber(bucket["used"]), cursorNumber(bucket["limit"]), cursorNumber(bucket["remaining"])
	if limit != nil && *limit == 0 {
		limit = nil
	}
	if (used == nil || *used == 0) && limit != nil && remaining != nil && *remaining <= *limit {
		v := *limit - *remaining
		used = &v
	}
	if used == nil {
		return nil
	}
	// Missing limits are unknown, not unlimited. Preserve reported remaining only
	// when consistent, or derive it from a valid reported limit.
	if limit != nil {
		if remaining == nil || *remaining > *limit || math.Abs(*used+*remaining-*limit) > 0.01 {
			v := math.Max(0, *limit-*used)
			remaining = &v
		}
	}
	scale := func(p *float64) *float64 {
		if p == nil {
			return nil
		}
		v := *p / 100
		return &v
	}
	return &CursorPool{Name: name, Unit: "USD", Used: scale(used), Limit: scale(limit), Remaining: scale(remaining)}
}

func parseCursorUsage(payload map[string]any) (*CursorUsage, error) {
	individual := cursorObject(payload["individualUsage"])
	if individual == nil {
		return nil, errors.New("Cursor usage: dashboard schema unavailable")
	}
	usage := &CursorUsage{}
	// Only explicit reset dates are shown. Do not fabricate a month from a start
	// date: calendar rollover and account billing rules need not agree.
	for _, key := range []string{"billingCycleEnd", "endOfMonth", "resetsAt", "nextReset"} {
		if date, ok := payload[key].(string); ok {
			if value, err := time.Parse(time.RFC3339, date); err == nil {
				usage.ResetsAt = &value
				break
			}
		}
		if n := cursorNumber(payload[key]); n != nil && *n > 0 && *n < 1e15 {
			v := *n
			if v < 1e12 {
				v *= 1000
			}
			value := time.UnixMilli(int64(v))
			usage.ResetsAt = &value
			break
		}
	}
	if overall := cursorCentsPool("Personal Usage", cursorObject(individual["overall"])); overall != nil {
		usage.Pools = append(usage.Pools, *overall)
	} else if plan := cursorObject(individual["plan"]); plan != nil && plan["enabled"] != false {
		auto, api := cursorNumber(plan["autoPercentUsed"]), cursorNumber(plan["apiPercentUsed"])
		if auto != nil {
			usage.Pools = append(usage.Pools, CursorPool{Name: "Cursor Models", Unit: "percent", Used: auto})
		}
		if api != nil {
			pool := CursorPool{Name: "Other Models", Unit: "percent", Used: api}
			if limit := cursorNumber(plan["limit"]); limit != nil && *limit > 0 {
				dollars := *limit / 100
				used := dollars * (*api / 100)
				remaining := math.Max(0, dollars-used)
				if !math.IsInf(used, 0) {
					pool = CursorPool{Name: "Other Models", Unit: "USD", Used: &used, Limit: &dollars, Remaining: &remaining}
				}
			}
			usage.Pools = append(usage.Pools, pool)
		}
		if auto == nil && api == nil {
			if total := cursorNumber(plan["totalPercentUsed"]); total != nil {
				usage.Pools = append(usage.Pools, CursorPool{Name: "Personal Usage", Unit: "percent", Used: total})
			} else if pool := cursorCentsPool("Personal Usage", plan); pool != nil {
				usage.Pools = append(usage.Pools, *pool)
			}
		}
	}
	if pool := cursorCentsPool("On-Demand Usage", cursorObject(individual["onDemand"])); pool != nil {
		usage.Pools = append(usage.Pools, *pool)
	}
	if len(usage.Pools) == 0 {
		return nil, errors.New("Cursor usage: no recognized account meters; quota unknown")
	}
	return usage, nil
}

func FormatCursorUsageLines(usage *CursorUsage) []string {
	if usage == nil {
		return nil
	}
	lines := []string{"  Cursor account meters (not inference eligibility):"}
	for _, pool := range usage.Pools {
		used := "unknown"
		if pool.Used != nil {
			used = fmt.Sprintf("%.2f", *pool.Used)
		}
		if pool.Unit == "percent" {
			lines = append(lines, "  "+pool.Name+": "+used+"% used")
			continue
		}
		limit, remaining := "unknown", "unknown"
		if pool.Limit != nil {
			limit = fmt.Sprintf("$%.2f", *pool.Limit)
		}
		if pool.Remaining != nil {
			remaining = fmt.Sprintf("$%.2f", *pool.Remaining)
		}
		lines = append(lines, "  "+pool.Name+": $"+used+" used / "+limit+" limit; remaining "+remaining)
	}
	if usage.ResetsAt != nil {
		lines = append(lines, "  Reset: "+usage.ResetsAt.UTC().Format(time.RFC3339))
	} else {
		lines = append(lines, "  Reset: unknown")
	}
	lines = append(lines, "  Fetched: "+usage.FetchedAt.UTC().Format(time.RFC3339)+" (no cached balance)")
	return lines
}
