package retention

import (
	"errors"
	"testing"
	"time"
)

const testWorkspace = "2ae65e15-3e8d-4a4c-b7a7-7a7c7a54d143"

func TestPolicyBoundsAndFixedCutoff(t *testing.T) {
	now := time.Date(2026, 10, 17, 12, 13, 14, 123456000, time.UTC)
	for _, days := range []int{1, 30, 3650} {
		req, err := ForDays(testWorkspace, days, 1000, now)
		if err != nil || !req.Before.Equal(now.Add(-time.Duration(days)*24*time.Hour)) {
			t.Fatal("policy changed the cutoff")
		}
		if err = req.Validate(now.Add(time.Hour)); err != nil {
			t.Fatal("fixed reviewed cutoff expired")
		}
	}
	for _, days := range []int{-1, 0, 3651} {
		if _, err := ForDays(testWorkspace, days, 1000, now); !errors.Is(err, ErrDays) {
			t.Fatal("invalid days accepted")
		}
	}
	for _, req := range []Request{
		{WorkspaceID: "all", Before: now.Add(-30 * 24 * time.Hour), Limit: 1000},
		{WorkspaceID: "00000000-0000-0000-0000-000000000000", Before: now.Add(-30 * 24 * time.Hour), Limit: 1000},
		{WorkspaceID: testWorkspace, Before: time.Time{}, Limit: 1000},
		{WorkspaceID: testWorkspace, Before: now.Add(-time.Hour), Limit: 1000},
		{WorkspaceID: testWorkspace, Before: now.Add(time.Hour), Limit: 1000},
		{WorkspaceID: testWorkspace, Before: now.Add(-30 * 24 * time.Hour), Limit: 0},
		{WorkspaceID: testWorkspace, Before: now.Add(-30 * 24 * time.Hour), Limit: 1001},
	} {
		if err := req.Validate(now); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid retention scope accepted")
		}
	}
}
