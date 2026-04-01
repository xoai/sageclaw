package agent

import (
	"testing"

	"github.com/xoai/sageclaw/pkg/canonical"
)

func tc(name string) canonical.ToolCall {
	return canonical.ToolCall{ID: name, Name: name, Input: []byte("{}")}
}

func TestPartitionBatches_Empty(t *testing.T) {
	batches := PartitionBatches(nil)
	if len(batches) != 0 {
		t.Errorf("expected 0 batches, got %d", len(batches))
	}
}

func TestPartitionBatches_AllConcurrent(t *testing.T) {
	calls := []pendingToolCall{
		{call: tc("read_file"), concurrencySafe: true},
		{call: tc("web_fetch"), concurrencySafe: true},
		{call: tc("memory_search"), concurrencySafe: true},
	}
	batches := PartitionBatches(calls)
	if len(batches) != 1 {
		t.Fatalf("expected 1 batch, got %d", len(batches))
	}
	if !batches[0].Concurrent {
		t.Error("expected batch to be concurrent")
	}
	if len(batches[0].Calls) != 3 {
		t.Errorf("expected 3 calls, got %d", len(batches[0].Calls))
	}
}

func TestPartitionBatches_AllExclusive(t *testing.T) {
	calls := []pendingToolCall{
		{call: tc("write_file"), concurrencySafe: false},
		{call: tc("edit"), concurrencySafe: false},
	}
	batches := PartitionBatches(calls)
	if len(batches) != 1 {
		t.Fatalf("expected 1 batch, got %d", len(batches))
	}
	if batches[0].Concurrent {
		t.Error("expected batch to be exclusive")
	}
	if len(batches[0].Calls) != 2 {
		t.Errorf("expected 2 calls, got %d", len(batches[0].Calls))
	}
}

func TestPartitionBatches_Mixed(t *testing.T) {
	calls := []pendingToolCall{
		{call: tc("read_file"), concurrencySafe: true},
		{call: tc("web_fetch"), concurrencySafe: true},
		{call: tc("edit"), concurrencySafe: false},
		{call: tc("read_file"), concurrencySafe: true},
		{call: tc("memory_search"), concurrencySafe: true},
	}
	batches := PartitionBatches(calls)
	if len(batches) != 3 {
		t.Fatalf("expected 3 batches, got %d", len(batches))
	}

	// Batch 0: concurrent [read_file, web_fetch]
	if !batches[0].Concurrent || len(batches[0].Calls) != 2 {
		t.Errorf("batch 0: concurrent=%v, calls=%d", batches[0].Concurrent, len(batches[0].Calls))
	}
	// Batch 1: exclusive [edit]
	if batches[1].Concurrent || len(batches[1].Calls) != 1 {
		t.Errorf("batch 1: concurrent=%v, calls=%d", batches[1].Concurrent, len(batches[1].Calls))
	}
	// Batch 2: concurrent [read_file, memory_search]
	if !batches[2].Concurrent || len(batches[2].Calls) != 2 {
		t.Errorf("batch 2: concurrent=%v, calls=%d", batches[2].Concurrent, len(batches[2].Calls))
	}
}

func TestPartitionBatches_SingleTool(t *testing.T) {
	calls := []pendingToolCall{
		{call: tc("datetime"), concurrencySafe: true},
	}
	batches := PartitionBatches(calls)
	if len(batches) != 1 {
		t.Fatalf("expected 1 batch, got %d", len(batches))
	}
	if !batches[0].Concurrent {
		t.Error("expected concurrent")
	}
}

func TestPartitionBatches_Alternating(t *testing.T) {
	calls := []pendingToolCall{
		{call: tc("read_file"), concurrencySafe: true},
		{call: tc("write_file"), concurrencySafe: false},
		{call: tc("read_file"), concurrencySafe: true},
		{call: tc("write_file"), concurrencySafe: false},
	}
	batches := PartitionBatches(calls)
	if len(batches) != 4 {
		t.Fatalf("expected 4 batches, got %d", len(batches))
	}
	for i, b := range batches {
		expectedConc := (i % 2) == 0
		if b.Concurrent != expectedConc {
			t.Errorf("batch %d: concurrent=%v, want %v", i, b.Concurrent, expectedConc)
		}
		if len(b.Calls) != 1 {
			t.Errorf("batch %d: expected 1 call, got %d", i, len(b.Calls))
		}
	}
}
