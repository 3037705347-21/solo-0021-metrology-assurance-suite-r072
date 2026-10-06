package domain

import (
	"testing"
	"time"
)

func TestPurgeableBeforeOnlyReleasesSealedCases(t *testing.T) {
	cutoff := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	old := cutoff.Add(-24 * time.Hour)
	future := cutoff.Add(24 * time.Hour)

	sealedAt := func(at time.Time) VerificationCase {
		return VerificationCase{
			ID:        "case-sealed",
			DeviceID:  "device-1",
			Steward:   "Lin",
			State:     CaseSealed,
			Version:   3,
			OpenedAt:  old,
			UpdatedAt: at,
			SealedAt:  &at,
		}
	}

	unsealed := func(state CaseState) VerificationCase {
		return VerificationCase{
			ID:        "case-open",
			DeviceID:  "device-1",
			Steward:   "Lin",
			State:     state,
			Version:   2,
			OpenedAt:  old,
			UpdatedAt: old,
		}
	}

	tests := []struct {
		name string
		item VerificationCase
		want bool
	}{
		{"sealed well before cutoff", sealedAt(old), true},
		{"sealed exactly at cutoff is retained", sealedAt(cutoff), false},
		{"sealed after cutoff is retained", sealedAt(future), false},
		{"awaiting case is retained", unsealed(CaseAwaitingMeasurement), false},
		{"review-ready case is retained", unsealed(CaseReviewReady), false},
		{"recheck-required case is retained", unsealed(CaseRecheckRequired), false},
		{"sealed state without seal timestamp is retained", func() VerificationCase {
			item := unsealed(CaseSealed)
			return item
		}(), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.item.PurgeableBefore(cutoff); got != tt.want {
				t.Fatalf("PurgeableBefore() = %v, want %v", got, tt.want)
			}
		})
	}
}
