package cursor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/util"
)

const (
	cursorLegacyUsageURL        = "https://cursor.com/api/usage"
	cursorStripeURL             = "https://cursor.com/api/auth/stripe"
	cursorCurrentPeriodUsageURL = "https://api2.cursor.sh/aiserver.v1.DashboardService/GetCurrentPeriodUsage"
)

type LegacyUsageResponse struct {
	GPT4         *LegacyModelUsage `json:"gpt-4,omitempty"`
	GPT35        *LegacyModelUsage `json:"gpt-3.5-turbo,omitempty"`
	StartOfMonth string            `json:"startOfMonth,omitempty"`
}

type LegacyModelUsage struct {
	NumRequests      float64  `json:"numRequests,omitempty"`
	NumRequestsTotal float64  `json:"numRequestsTotal,omitempty"`
	NumTokens        float64  `json:"numTokens,omitempty"`
	MaxRequestUsage  *float64 `json:"maxRequestUsage,omitempty"`
	MaxTokenUsage    *float64 `json:"maxTokenUsage,omitempty"`
}

type CurrentPeriodUsageResponse struct {
	BillingCycleStart string    `json:"billingCycleStart,omitempty"`
	BillingCycleEnd   string    `json:"billingCycleEnd,omitempty"`
	PlanUsage         PlanUsage `json:"planUsage"`
	DisplayMessage    string    `json:"displayMessage,omitempty"`
}

type PlanUsage struct {
	Limit            *float64 `json:"limit,omitempty"`
	Remaining        *float64 `json:"remaining,omitempty"`
	TotalPercentUsed *float64 `json:"totalPercentUsed,omitempty"`
	AutoPercentUsed  *float64 `json:"autoPercentUsed,omitempty"`
	APIPercentUsed   *float64 `json:"apiPercentUsed,omitempty"`
}

type StripeStatusResponse struct {
	MembershipType           string   `json:"membershipType,omitempty"`
	IndividualMembershipType string   `json:"individualMembershipType,omitempty"`
	SubscriptionStatus       string   `json:"subscriptionStatus,omitempty"`
	IsTeamMember             bool     `json:"isTeamMember,omitempty"`
	IsYearlyPlan             bool     `json:"isYearlyPlan,omitempty"`
	CustomerBalance          *float64 `json:"customerBalance,omitempty"`
	PendingCancellationDate  *string  `json:"pendingCancellationDate,omitempty"`
	LastPaymentFailed        bool     `json:"lastPaymentFailed,omitempty"`
}

type UsageSnapshot struct {
	FetchedAt             int64                       `json:"fetched_at"`
	BillingModel          string                      `json:"billing_model"`
	PlanLabel             string                      `json:"plan_label"`
	SubscriptionStatus    string                      `json:"subscription_status,omitempty"`
	CurrentUsage          float64                     `json:"current_usage"`
	UsageLimit            float64                     `json:"usage_limit"`
	Remaining             float64                     `json:"remaining"`
	PercentUsed           float64                     `json:"percent_used"`
	NextReset             float64                     `json:"next_reset,omitempty"`
	LegacyUsage           *LegacyUsageSummary         `json:"legacy_usage,omitempty"`
	CreditUsage           *CreditUsageSummary         `json:"credit_usage,omitempty"`
	RawLegacyUsage        *LegacyUsageResponse        `json:"legacy_raw,omitempty"`
	RawCurrentPeriodUsage *CurrentPeriodUsageResponse `json:"current_period_raw,omitempty"`
	RawStripeStatus       *StripeStatusResponse       `json:"stripe_raw,omitempty"`
	Warnings              []string                    `json:"warnings,omitempty"`
	Partial               map[string]string           `json:"partial,omitempty"`
}

type LegacyUsageSummary struct {
	Used        float64 `json:"used"`
	Max         float64 `json:"max"`
	PercentUsed float64 `json:"percent_used"`
	CycleStart  string  `json:"cycle_start,omitempty"`
	CycleEnd    string  `json:"cycle_end,omitempty"`
}

type CreditUsageSummary struct {
	UsedCents       float64  `json:"used_cents"`
	LimitCents      float64  `json:"limit_cents"`
	RemainingCents  float64  `json:"remaining_cents"`
	PercentUsed     float64  `json:"percent_used"`
	AutoPercentUsed *float64 `json:"auto_percent_used,omitempty"`
	APIPercentUsed  *float64 `json:"api_percent_used,omitempty"`
	CycleStart      string   `json:"cycle_start,omitempty"`
	CycleEnd        string   `json:"cycle_end,omitempty"`
}

type UsageChecker struct {
	httpClient *http.Client
}

func NewUsageChecker(cfg *config.Config) *UsageChecker {
	client := &http.Client{Timeout: 15 * time.Second}
	if cfg != nil {
		client = util.SetProxy(&cfg.SDKConfig, client)
	}
	return &UsageChecker{httpClient: client}
}

func NewUsageCheckerWithClient(client *http.Client) *UsageChecker {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &UsageChecker{httpClient: client}
}

func (c *UsageChecker) CheckUsage(ctx context.Context, userID, accessToken string) (*UsageSnapshot, error) {
	userID = NormalizeUserID(userID)
	if userID == "" {
		return nil, fmt.Errorf("cursor: user id is required for usage check")
	}
	if strings.TrimSpace(accessToken) == "" {
		return nil, fmt.Errorf("cursor: access token is required for usage check")
	}

	legacy, legacyErr := c.fetchLegacyUsage(ctx, userID, accessToken)
	current, currentErr := c.fetchCurrentPeriodUsage(ctx, accessToken)
	stripe, stripeErr := c.fetchStripeStatus(ctx, userID, accessToken)

	if legacyErr != nil && currentErr != nil {
		return nil, fmt.Errorf("cursor: usage checks failed: legacy=%v current_period=%v", legacyErr, currentErr)
	}

	snapshot := summarizeUsageSnapshot(legacy, current, stripe)
	snapshot.Partial = map[string]string{
		"legacy": "ok",
		"usage":  "ok",
		"stripe": "ok",
	}
	if legacyErr != nil {
		snapshot.Partial["legacy"] = "failed"
		snapshot.Warnings = append(snapshot.Warnings, "legacy_failed")
	}
	if currentErr != nil {
		snapshot.Partial["usage"] = "failed"
		snapshot.Warnings = append(snapshot.Warnings, "usage_failed")
	}
	if stripeErr != nil {
		snapshot.Partial["stripe"] = "failed"
		snapshot.Warnings = append(snapshot.Warnings, "stripe_failed")
	}
	return snapshot, nil
}

func NormalizeUserID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.Contains(value, "|") {
		for _, part := range strings.Split(value, "|") {
			if strings.HasPrefix(part, "user_") {
				return part
			}
		}
	}
	if strings.HasPrefix(value, "user_") {
		return value
	}
	return ""
}

func (c *UsageChecker) fetchLegacyUsage(ctx context.Context, userID, accessToken string) (*LegacyUsageResponse, error) {
	url := cursorLegacyUsageURL + "?user=" + userID
	var out LegacyUsageResponse
	if err := c.getJSON(ctx, url, cursorSessionHeaders(userID, accessToken), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *UsageChecker) fetchStripeStatus(ctx context.Context, userID, accessToken string) (*StripeStatusResponse, error) {
	var out StripeStatusResponse
	if err := c.getJSON(ctx, cursorStripeURL, cursorSessionHeaders(userID, accessToken), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *UsageChecker) fetchCurrentPeriodUsage(ctx context.Context, accessToken string) (*CurrentPeriodUsageResponse, error) {
	var out CurrentPeriodUsageResponse
	headers := map[string]string{
		"Authorization":            "Bearer " + accessToken,
		"Connect-Protocol-Version": "1",
		"Content-Type":             "application/json",
	}
	if err := c.postJSON(ctx, cursorCurrentPeriodUsageURL, []byte("{}"), headers, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func cursorSessionHeaders(userID, accessToken string) map[string]string {
	return map[string]string{
		"Cookie": "WorkosCursorSessionToken=" + userID + "%3A%3A" + accessToken,
	}
}

func (c *UsageChecker) getJSON(ctx context.Context, url string, headers map[string]string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	return c.doJSON(req, headers, out)
}

func (c *UsageChecker) postJSON(ctx context.Context, url string, body []byte, headers map[string]string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	return c.doJSON(req, headers, out)
}

func (c *UsageChecker) doJSON(req *http.Request, headers map[string]string, out any) error {
	if c == nil || c.httpClient == nil {
		return fmt.Errorf("cursor: usage checker has no http client")
	}
	req.Header.Set("User-Agent", "CLIProxyAPIPlus/cursor-usage")
	req.Header.Set("Accept", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("unauthorized (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}
	return nil
}

func summarizeUsageSnapshot(legacy *LegacyUsageResponse, current *CurrentPeriodUsageResponse, stripe *StripeStatusResponse) *UsageSnapshot {
	s := &UsageSnapshot{
		FetchedAt:             time.Now().UnixMilli(),
		BillingModel:          "unknown",
		PlanLabel:             planLabel(stripe),
		SubscriptionStatus:    subscriptionStatus(stripe),
		RawLegacyUsage:        legacy,
		RawCurrentPeriodUsage: current,
		RawStripeStatus:       stripe,
	}
	if current != nil {
		limit := valueOrZero(current.PlanUsage.Limit)
		remaining := valueOrZero(current.PlanUsage.Remaining)
		used := maxFloat(limit-remaining, 0)
		s.BillingModel = "usd_credit"
		s.CurrentUsage = used
		s.UsageLimit = limit
		s.Remaining = remaining
		s.PercentUsed = percent(used, limit)
		start := millisToRFC3339(current.BillingCycleStart)
		end := millisToRFC3339(current.BillingCycleEnd)
		if endMs := parseMillis(current.BillingCycleEnd); endMs > 0 {
			s.NextReset = float64(endMs)
		}
		s.CreditUsage = &CreditUsageSummary{UsedCents: used, LimitCents: limit, RemainingCents: remaining, PercentUsed: s.PercentUsed, AutoPercentUsed: current.PlanUsage.AutoPercentUsed, APIPercentUsed: current.PlanUsage.APIPercentUsed, CycleStart: start, CycleEnd: end}
		if legacy != nil && legacy.GPT4 != nil && legacy.GPT4.MaxRequestUsage != nil && *legacy.GPT4.MaxRequestUsage > 0 {
			legacyUsed := legacy.GPT4.NumRequests
			legacyLimit := *legacy.GPT4.MaxRequestUsage
			legacyStart, legacyEnd := legacyCycle(legacy.StartOfMonth)
			s.LegacyUsage = &LegacyUsageSummary{Used: legacyUsed, Max: legacyLimit, PercentUsed: percent(legacyUsed, legacyLimit), CycleStart: legacyStart.Format(time.RFC3339), CycleEnd: legacyEnd.Format(time.RFC3339)}
		}
		return s
	}
	if legacy != nil && legacy.GPT4 != nil && legacy.GPT4.MaxRequestUsage != nil && *legacy.GPT4.MaxRequestUsage > 0 {
		used := legacy.GPT4.NumRequests
		limit := *legacy.GPT4.MaxRequestUsage
		s.BillingModel = "request_count"
		s.CurrentUsage = used
		s.UsageLimit = limit
		s.Remaining = maxFloat(limit-used, 0)
		s.PercentUsed = percent(used, limit)
		start, end := legacyCycle(legacy.StartOfMonth)
		s.NextReset = float64(end.UnixMilli())
		s.LegacyUsage = &LegacyUsageSummary{Used: used, Max: limit, PercentUsed: s.PercentUsed, CycleStart: start.Format(time.RFC3339), CycleEnd: end.Format(time.RFC3339)}
	}
	return s
}

func planLabel(stripe *StripeStatusResponse) string {
	if stripe == nil {
		return "Cursor"
	}
	if stripe.IsTeamMember {
		return "Team"
	}
	m := strings.ToLower(strings.TrimSpace(stripe.IndividualMembershipType))
	if m == "" {
		m = strings.ToLower(strings.TrimSpace(stripe.MembershipType))
	}
	switch m {
	case "ultra":
		return "Ultra"
	case "pro_plus", "pro+":
		return "Pro+"
	case "pro":
		return "Pro"
	case "free", "":
		return "Free"
	default:
		return "Cursor"
	}
}

func subscriptionStatus(stripe *StripeStatusResponse) string {
	if stripe == nil {
		return "unknown"
	}
	return strings.TrimSpace(stripe.SubscriptionStatus)
}

func legacyCycle(startRaw string) (time.Time, time.Time) {
	start, err := time.Parse(time.RFC3339, startRaw)
	if err != nil {
		start = time.Now()
	}
	end := time.Date(start.UTC().Year(), start.UTC().Month()+1, start.UTC().Day(), start.UTC().Hour(), start.UTC().Minute(), start.UTC().Second(), 0, time.UTC)
	return start, end
}

func valueOrZero(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
}

func percent(used, limit float64) float64 {
	if limit <= 0 {
		return 0
	}
	p := used / limit * 100
	if p < 0 {
		return 0
	}
	return p
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func millisToRFC3339(raw string) string {
	ms := parseMillis(raw)
	if ms <= 0 {
		return ""
	}
	return time.UnixMilli(ms).Format(time.RFC3339)
}

func parseMillis(raw string) int64 {
	var n int64
	if _, err := fmt.Sscanf(strings.TrimSpace(raw), "%d", &n); err != nil {
		return 0
	}
	return n
}
