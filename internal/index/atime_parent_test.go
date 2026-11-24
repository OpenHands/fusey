package index

import "testing"

// A read touches atime. That must not mark the index structurally dirty,
// because IsDirty is what triggers a full snapshot rewrite on every persist
// tick — a pure-read workload would otherwise rewrite the whole index every
// FUSEY_PERSIST_INTERVAL.
func TestAtimeDoesNotDirtyIndex(t *testing.T) {
	idx := New(4096)
	idx.MarkClean()

	idx.TouchAtime(RootIno, 12345)

	if idx.IsDirty() {
		t.Error("IsDirty() must be false after an atime-only change")
	}
	if !idx.IsAtimeDirty() {
		t.Error("IsAtimeDirty() must be true after TouchAtime")
	}

	idx.MarkClean()
	if idx.IsAtimeDirty() {
		t.Error("MarkClean must clear atimeDirty")
	}
}

// Structural mutations must still set the dirty flag.
func TestStructuralChangeStillDirty(t *testing.T) {
	idx := New(4096)
	idx.MarkClean()

	if _, err := idx.MakeFile(RootIno, "f", 0o644, 0, 0, 0, now()); err != nil {
		t.Fatal(err)
	}
	if !idx.IsDirty() {
		t.Error("IsDirty() must be true after MakeFile")
	}
}

// The parent map must stay correct across every structural mutation that can
// change a child's parent, and must survive a snapshot round-trip.
func TestParentMapConsistency(t *testing.T) {
	idx := New(4096)
	n := now()

	d, err := idx.MakeDir(RootIno, "d", 0o755, 0, 0, n)
	if err != nil {
		t.Fatal(err)
	}
	f, err := idx.MakeFile(d, "f", 0o644, 0, 0, 0, n)
	if err != nil {
		t.Fatal(err)
	}
	if got := idx.GetParentIno(f); got != d {
		t.Errorf("GetParentIno(f) = %d, want %d", got, d)
	}

	// Renaming the directory itself keeps its parent at root.
	if err := idx.Rename(RootIno, "d", RootIno, "d2", n); err != nil {
		t.Fatal(err)
	}
	if got := idx.GetParentIno(d); got != RootIno {
		t.Errorf("after rename GetParentIno(d) = %d, want %d", got, RootIno)
	}
	if got := idx.GetParentIno(f); got != d {
		t.Errorf("child parent must not change when its parent is renamed, got %d want %d", got, d)
	}

	// Moving a file to a different parent must update its parent pointer.
	other, err := idx.MakeDir(RootIno, "other", 0o755, 0, 0, n)
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.Rename(d, "f", other, "f", n); err != nil {
		t.Fatal(err)
	}
	if got := idx.GetParentIno(f); got != other {
		t.Errorf("after move GetParentIno(f) = %d, want %d", got, other)
	}

	// Round-trip through the snapshot format rebuilds the map.
	snap := idx.Snapshot()
	idx2 := FromSnapshot(snap, 4096)
	if got := idx2.GetParentIno(f); got != other {
		t.Errorf("after round-trip GetParentIno(f) = %d, want %d", got, other)
	}
	if got := idx2.GetParentIno(d); got != RootIno {
		t.Errorf("after round-trip GetParentIno(d) = %d, want %d", got, RootIno)
	}

	// Removing an entry must drop the child's parent mapping.
	if err := idx.RemoveDirEntry(other, "f", n); err != nil {
		t.Fatal(err)
	}
	if _, ok := idx.Lookup(other, "f"); ok {
		t.Error("entry should be gone after RemoveDirEntry")
	}
}
