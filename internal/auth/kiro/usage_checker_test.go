package kiro

import "testing"

func TestSummarizeUsageAggregatesBreakdownsAndFreeTrial(t *testing.T) {
	usage := &UsageQuotaResponse{
		NextDateReset: 123456,
		UsageBreakdownList: []UsageBreakdownExtended{
			{
				ResourceType:              "CREDIT",
				CurrentUsageWithPrecision: 10.5,
				UsageLimitWithPrecision:   50,
				FreeTrialInfo: &FreeTrialInfoExtended{
					CurrentUsageWithPrecision: 20.25,
					UsageLimitWithPrecision:   500,
				},
			},
			{
				ResourceType:              "CREDIT",
				CurrentUsageWithPrecision: 1.25,
				UsageLimitWithPrecision:   25,
			},
		},
	}

	currentUsage, usageLimit, nextReset := SummarizeUsage(usage)
	if currentUsage != 32 {
		t.Fatalf("currentUsage = %v, want 32", currentUsage)
	}
	if usageLimit != 575 {
		t.Fatalf("usageLimit = %v, want 575", usageLimit)
	}
	if nextReset != 123456 {
		t.Fatalf("nextReset = %v, want 123456", nextReset)
	}
}

func TestSummarizeUsageNil(t *testing.T) {
	currentUsage, usageLimit, nextReset := SummarizeUsage(nil)
	if currentUsage != 0 || usageLimit != 0 || nextReset != 0 {
		t.Fatalf("SummarizeUsage(nil) = (%v, %v, %v), want zeros", currentUsage, usageLimit, nextReset)
	}
}
