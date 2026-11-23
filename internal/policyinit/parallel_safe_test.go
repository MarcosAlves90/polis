package policyinit

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/MarcosAlves90/polis/v6/spec"
)

func TestSafeParallelInitRequiresExplicitReview(t *testing.T) {
	repo := goRepo(t)
	result, err := Init(context.Background(), Options{
		Repo: repo, Profile: ProfileGo, DryRun: true,
		ParallelSafe: []string{"lint", "dependency"},
	})
	if err != nil {
		t.Fatalf("explicit reviewed opt-in failed: %v", err)
	}
	var policy spec.Policy
	if err := json.Unmarshal(result.Policy, &policy); err != nil {
		t.Fatal(err)
	}
	for _, gate := range policy.Gates {
		want := gate.ID == "lint" || gate.ID == "dependency"
		if gate.ParallelSafe != want {
			t.Fatalf("gate %s parallel_safe=%t want %t", gate.ID, gate.ParallelSafe, want)
		}
	}

	for name, ids := range map[string][]string{
		"unknown":   {"not-a-gate"},
		"coverage":  {"coverage"},
		"disabled":  {"smoke"},
		"duplicate": {"lint", "lint"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Init(context.Background(), Options{
				Repo: repo, Profile: ProfileGo, DryRun: true, ParallelSafe: ids,
			})
			if err == nil || !strings.Contains(err.Error(), "parallel-safe") {
				t.Fatalf("expected parallel-safe rejection for %v, got %v", ids, err)
			}
		})
	}
}
