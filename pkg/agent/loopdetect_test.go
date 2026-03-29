package agent

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestLoopDetect_IdenticalWarn(t *testing.T) {
	s := NewToolLoopState()
	args := json.RawMessage(`{"path": "/foo.txt"}`)
	result := "file contents here"

	for i := 0; i < 3; i++ {
		s.Record("read_file", args, result, false)
	}

	verdict, reason := s.Check("read_file", args, result)
	if verdict != LoopWarn {
		t.Errorf("expected LoopWarn after 3 identical calls, got %d: %s", verdict, reason)
	}
}

func TestLoopDetect_IdenticalKill(t *testing.T) {
	s := NewToolLoopState()
	args := json.RawMessage(`{"path": "/foo.txt"}`)
	result := "file contents here"

	for i := 0; i < 5; i++ {
		s.Record("read_file", args, result, false)
	}

	verdict, reason := s.Check("read_file", args, result)
	if verdict != LoopKill {
		t.Errorf("expected LoopKill after 5 identical calls, got %d: %s", verdict, reason)
	}
}

func TestLoopDetect_SameResultWarn(t *testing.T) {
	s := NewToolLoopState()
	result := "no results found"

	for i := 0; i < 4; i++ {
		args := json.RawMessage(fmt.Sprintf(`{"query": "search_%d"}`, i))
		s.Record("web_search", args, result, false)
	}

	verdict, reason := s.Check("web_search", json.RawMessage(`{"query": "search_new"}`), result)
	if verdict != LoopWarn {
		t.Errorf("expected LoopWarn for same-result loop, got %d: %s", verdict, reason)
	}
}

func TestLoopDetect_SameResultKill(t *testing.T) {
	s := NewToolLoopState()
	result := "no results found"

	for i := 0; i < 6; i++ {
		args := json.RawMessage(fmt.Sprintf(`{"query": "search_%d"}`, i))
		s.Record("web_search", args, result, false)
	}

	verdict, reason := s.Check("web_search", json.RawMessage(`{"query": "search_final"}`), result)
	if verdict != LoopKill {
		t.Errorf("expected LoopKill for same-result loop, got %d: %s", verdict, reason)
	}
}

func TestLoopDetect_ReadOnlyStuckWarn(t *testing.T) {
	s := NewToolLoopState()

	// Low uniqueness: re-reading same 2-3 files.
	for i := 0; i < 8; i++ {
		args := json.RawMessage(fmt.Sprintf(`{"path": "/file%d.txt"}`, i%2))
		s.Record("read_file", args, fmt.Sprintf("content%d", i%2), false)
	}

	verdict, _ := s.Check("read_file", json.RawMessage(`{"path": "/file0.txt"}`), "content0")
	if verdict != LoopWarn {
		t.Errorf("expected LoopWarn for stuck read-only streak at 8, got %d", verdict)
	}
}

func TestLoopDetect_ReadOnlyStuckKill(t *testing.T) {
	s := NewToolLoopState()

	for i := 0; i < 12; i++ {
		args := json.RawMessage(fmt.Sprintf(`{"path": "/file%d.txt"}`, i%2))
		s.Record("read_file", args, fmt.Sprintf("content%d", i%2), false)
	}

	verdict, _ := s.Check("read_file", json.RawMessage(`{"path": "/file0.txt"}`), "content0")
	if verdict != LoopKill {
		t.Errorf("expected LoopKill for stuck read-only streak at 12, got %d", verdict)
	}
}

func TestLoopDetect_ReadOnlyExplorationLenient(t *testing.T) {
	s := NewToolLoopState()

	// High uniqueness: reading many different files.
	for i := 0; i < 10; i++ {
		args := json.RawMessage(fmt.Sprintf(`{"path": "/file%d.txt"}`, i))
		s.Record("read_file", args, fmt.Sprintf("unique_content_%d", i), false)
	}

	verdict, _ := s.Check("read_file", json.RawMessage(`{"path": "/file10.txt"}`), "unique_content_10")
	if verdict != LoopOK {
		t.Errorf("expected LoopOK for exploration at 10 calls, got %d", verdict)
	}
}

func TestLoopDetect_MutatingResets(t *testing.T) {
	s := NewToolLoopState()

	// Build up a read-only streak.
	for i := 0; i < 7; i++ {
		args := json.RawMessage(fmt.Sprintf(`{"path": "/file%d.txt"}`, i%2))
		s.Record("read_file", args, "content", false)
	}

	// Mutating call should reset streak.
	s.Record("write_file", json.RawMessage(`{"path": "/out.txt"}`), "ok", true)

	if s.streakLen != 0 {
		t.Errorf("expected streak reset after mutating call, got %d", s.streakLen)
	}

	// After mutation, a NEW unique read should be OK (strategy 3 is reset).
	// Note: strategy 1 still sees old entries in the recent buffer — that's
	// correct, it prevents the same identical pattern from recurring.
	verdict, _ := s.Check("read_file", json.RawMessage(`{"path": "/new_file.txt"}`), "new_content")
	if verdict != LoopOK {
		t.Errorf("expected LoopOK for new read after mutating reset, got %d", verdict)
	}
}

func TestLoopDetect_DifferentToolsDontConflict(t *testing.T) {
	s := NewToolLoopState()
	result := "same content"

	// 3 calls to read_file + 3 calls to web_search with same result.
	// Should NOT trigger same-result (different tools).
	for i := 0; i < 3; i++ {
		s.Record("read_file", json.RawMessage(fmt.Sprintf(`{"path": "/%d"}`, i)), result, false)
		s.Record("web_search", json.RawMessage(fmt.Sprintf(`{"q": "%d"}`, i)), result, false)
	}

	verdict, _ := s.Check("read_file", json.RawMessage(`{"path": "/new"}`), result)
	if verdict == LoopKill {
		t.Error("different tools should not trigger kill")
	}
}

func TestIsMutating(t *testing.T) {
	if !IsMutating("write_file") {
		t.Error("write_file should be mutating")
	}
	if !IsMutating("exec") {
		t.Error("exec should be mutating")
	}
	if IsMutating("read_file") {
		t.Error("read_file should not be mutating")
	}
	if IsMutating("web_search") {
		t.Error("web_search should not be mutating")
	}
}

func TestStableHash_KeyOrder(t *testing.T) {
	a := stableHash(json.RawMessage(`{"b": 2, "a": 1}`))
	b := stableHash(json.RawMessage(`{"a": 1, "b": 2}`))
	if a != b {
		t.Error("stableHash should produce same result regardless of key order")
	}
}
