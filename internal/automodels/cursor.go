package automodels

import (
	"context"
	"math"
	"strconv"
	"time"
)

const cursorUsageURL = "https://api2.cursor.sh/aiserver.v1.DashboardService/GetCurrentPeriodUsage"

// CursorUsage is the part of Cursor's dashboard answer that PiG shows. The
// endpoint is private and its field names come from public dashboard tools.
// Amounts are cents and times are epoch milliseconds in text.
type CursorUsage struct {
	BillingCycleEnd string           `json:"billingCycleEnd"`
	PlanUsage       *CursorPlanUsage `json:"planUsage"`
}

type CursorPlanUsage struct {
	TotalPercentUsed *float64 `json:"totalPercentUsed"`
	AutoPercentUsed  *float64 `json:"autoPercentUsed"`
	APIPercentUsed   *float64 `json:"apiPercentUsed"`
}

// FetchCursor needs the browser session token, not the API key that runs models.
func (c Client) FetchCursor(ctx context.Context, entry AuthEntry) (*CursorUsage, error) {
	var usage CursorUsage
	if err := c.fetch(ctx, entry, "Cursor", cursorUsageURL, &usage); err != nil {
		return nil, err
	}
	return &usage, nil
}

// cursorMeters lists the meters Cursor reported, in display order.
func cursorMeters(usage *CursorUsage) []quotaRow {
	if usage == nil || usage.PlanUsage == nil {
		return nil
	}
	reset, hasReset := "", false
	if ms, err := strconv.ParseFloat(usage.BillingCycleEnd, 64); err == nil && finite(ms) && ms > 0 {
		reset, hasReset = numberText(math.Floor(ms/1000)), true
	}
	var rows []quotaRow
	for _, meter := range []struct {
		label   string
		percent *float64
	}{{"Total", usage.PlanUsage.TotalPercentUsed}, {"Auto", usage.PlanUsage.AutoPercentUsed}, {"API", usage.PlanUsage.APIPercentUsed}} {
		if meter.percent == nil || !finite(*meter.percent) || *meter.percent < 0 {
			continue
		}
		rows = append(rows, quotaRow{label: meter.label, percent: roundedPercent(*meter.percent), reset: reset, hasReset: hasReset})
	}
	return rows
}

func FormatCursorUsageLines(usage *CursorUsage, now time.Time) []string {
	return renderQuotaRows(cursorMeters(usage), now.Location())
}

// CursorStatusQuota is the footer meter: the plan total.
func CursorStatusQuota(usage *CursorUsage) *StatusQuota {
	if usage == nil || usage.PlanUsage == nil || usage.PlanUsage.TotalPercentUsed == nil || !finite(*usage.PlanUsage.TotalPercentUsed) {
		return nil
	}
	return &StatusQuota{Label: "Cursor", Percent: roundedInt(*usage.PlanUsage.TotalPercentUsed)}
}

// CursorAvailable is nil when Cursor reported no usable total.
func CursorAvailable(usage *CursorUsage) *bool {
	status := CursorStatusQuota(usage)
	if status == nil {
		return nil
	}
	available := *usage.PlanUsage.TotalPercentUsed < 100
	return &available
}
