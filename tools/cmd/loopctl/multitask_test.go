package main

import (
	"strings"
	"testing"
	"time"
)

// 한 세션이 여러 작업을 다루면 첫 반복 이후 모든 반복에 session.multitask 승인이 있어야 한다.
func TestMultitaskSessionNeedsApproval(t *testing.T) {
	root := sandbox(t)
	mk := func(id, task, started, approvals string) {
		write(t, root, "loop/iterations/"+id+".json", `{"id":"`+id+`","task":"`+task+`","session":{"kind":"worker","id":"s1"},"startedAt":"`+started+`","endedAt":"`+started+`","base":{"branch":"b","commit":"c"},"head":{"branch":"b","commit":"c"},"state":"no-op","hypothesis":"h","approvals":[`+approvals+`]}`)
	}
	approval := `{"action":"session.multitask","by":"person","at":"2026-10-02T09:00:00Z"}`
	mk("20261002-090000-T-001", "T-001", "2026-10-02T09:00:00Z", "")
	mk("20261002-100000-T-002", "T-002", "2026-10-02T10:00:00Z", "")
	at := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)

	errs, _ := checkRules(loadOK(t, root), at)
	if joined := strings.Join(errs, "\n"); !strings.Contains(joined, "record session.multitask approval on 20261002-100000-T-002") {
		t.Fatalf("unapproved second task not reported:\n%s", joined)
	}

	// 두 번째 반복에 승인이 있으면 통과(첫 반복은 승인 전이라도 된다).
	mk("20261002-100000-T-002", "T-002", "2026-10-02T10:00:00Z", approval)
	errs, _ = checkRules(loadOK(t, root), at)
	for _, e := range errs {
		if strings.Contains(e, "R-one-task-session") {
			t.Fatalf("approved multitask session still rejected: %s", e)
		}
	}

	// 승인 이름은 policy.humanGates에 선언된 것이어야 의미가 있다.
	if !hasHumanGate(loadOK(t, root).policy, multitaskGate) {
		t.Fatal("policy.humanGates must declare session.multitask")
	}
	if hasHumanGate(loadOK(t, root).policy, "session.typo") {
		t.Fatal("undeclared gate accepted")
	}
}
