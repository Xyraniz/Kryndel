package kry

import "testing"

func TestPersistentArrayAppendKeepsSnapshotsAcrossChunks(t *testing.T) {
	original := arrVal(nil)
	current := original
	for index := 0; index < persistentArrayChunkSize*3+7; index++ {
		current = persistentArrayAppend(current, intVal(int64(index)))
	}

	if got := arrayLength(original); got != 0 {
		t.Fatalf("empty source snapshot length = %d, want 0", got)
	}
	if got := arrayLength(current); got != persistentArrayChunkSize*3+7 {
		t.Fatalf("appended array length = %d, want %d", got, persistentArrayChunkSize*3+7)
	}
	for _, index := range []int{0, persistentArrayChunkSize - 1, persistentArrayChunkSize, persistentArrayChunkSize*2 - 1, persistentArrayChunkSize*3 + 6} {
		if got := arrayAt(current, index); got.Kind != VInt || got.I != int64(index) {
			t.Fatalf("array item %d = %#v, want %d", index, got, index)
		}
	}
}
