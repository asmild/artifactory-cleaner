package cleaner

import (
	"context"
	"testing"

	"github.com/asmild/artifactory-cleaner/internal/artifactory"
)

func TestExecute_DeletesOnlyDeleteAction(t *testing.T) {
	deleter := &mockDeleter{}
	plan := CleanupPlan{
		Repository: "docker-local",
		DryRun:     false,
		artClient:  deleter,
		GroupedDecisionMap: map[string][]CleanupDecision{
			"img": {
				withAction(decision("img", "keep", now), RECENT_VERSION),
				withAction(decision("img", "remove", old), DELETE),
				withAction(decision("img", "wl", old), WHITELISTED),
			},
		},
	}

	if err := plan.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}

	if len(deleter.deleted) != 1 || deleter.deleted[0] != "img/remove" {
		t.Errorf("deleted = %v, want [img/remove]", deleter.deleted)
	}
}

func TestExecute_DryRunSkipsDeletion(t *testing.T) {
	deleter := &mockDeleter{}
	plan := CleanupPlan{
		Repository: "docker-local",
		DryRun:     true,
		artClient:  deleter,
		GroupedDecisionMap: map[string][]CleanupDecision{
			"img": {withAction(decision("img", "remove", old), DELETE)},
		},
	}

	if err := plan.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}

	if len(deleter.deleted) != 0 {
		t.Errorf("dry-run: expected no deletions, got %v", deleter.deleted)
	}
}

func TestExecute_PropagatesDeleteError(t *testing.T) {
	deleter := &mockDeleter{deleteErr: &testError{"delete failed"}}
	plan := CleanupPlan{
		Repository: "docker-local",
		DryRun:     false,
		artClient:  deleter,
		GroupedDecisionMap: map[string][]CleanupDecision{
			"img": {withAction(decision("img", "bad", old), DELETE)},
		},
	}

	if err := plan.Execute(context.Background()); err == nil {
		t.Error("expected error, got nil")
	}
}

func TestExecute_EmptyPlanIsNoop(t *testing.T) {
	deleter := &mockDeleter{}
	plan := CleanupPlan{
		Repository:         "docker-local",
		artClient:          deleter,
		GroupedDecisionMap: map[string][]CleanupDecision{},
	}

	if err := plan.Execute(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(deleter.deleted) != 0 {
		t.Errorf("expected no deletions, got %v", deleter.deleted)
	}
}

// ── compile-time interface checks ─────────────────────────────────────────────

var _ Deleter = (*artifactory.Client)(nil)

// ── helpers ───────────────────────────────────────────────────────────────────

type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }