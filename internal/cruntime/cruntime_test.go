package cruntime

import (
	"strings"
	"testing"
)

func TestSourceAssemblesRuntimePartsOnceInDependencyOrder(t *testing.T) {
	// Keep this list explicit so a fragment reorder or an unreviewed omission is
	// caught even if cRuntimeParts and Source are changed together.
	wantParts := []string{
		cRuntimePrelude,
		cRuntimeStrings,
		cRuntimeFloat,
		cRuntimeDisplay,
		cRuntimeEquality,
		cRuntimeArithmetic,
		cRuntimeOutput,
		cRuntimeBuiltins,
		cRuntimeIndex,
		cRuntimeFileBase,
		cRuntimeSHA256,
		cRuntimeJSON,
		cRuntimeFileSystem,
		cRuntimeProcess,
		cRuntimeConcurrency,
		cRuntimeDispatch,
		cRuntimeCollections,
		cRuntimeTCP,
		cRuntimeHTTP,
		cRuntimeSQLite,
		cRuntimeCrypto,
	}
	if len(cRuntimeParts) != len(wantParts) {
		t.Fatalf("runtime has %d assembled parts, want %d", len(cRuntimeParts), len(wantParts))
	}
	for i, want := range wantParts {
		if cRuntimeParts[i] != want {
			t.Errorf("runtime part %d is out of dependency order or missing", i)
		}
	}

	source := Source()
	for i, part := range wantParts {
		if part == "" {
			t.Errorf("runtime part %d is empty", i)
			continue
		}
		if got := strings.Count(source, part); got != 1 {
			t.Errorf("runtime part %d appears %d times in Source(), want exactly once", i, got)
		}
	}
	if want := strings.Join(wantParts, ""); source != want {
		t.Fatal("Source() does not equal the runtime fragments concatenated in dependency order")
	}
}

func TestSourceContainsCoreRuntimeAndHostIntegrationMarkers(t *testing.T) {
	source := Source()
	markers := []string{
		"static void k_fmt_float(",      // float formatting
		"static void k_disp(",           // display
		"static int k_equal(",           // equality
		"static long long k_add_i(",     // checked arithmetic
		"static KValue k_map_remove(",   // collections
		"static KValue k_fs_read_text(", // filesystem host integration
		"static KValue k_process_run(",  // process host integration
		"static KValue k_tcp_connect(",  // TCP host integration
		"static KValue k_http_request(", // HTTP host integration
		"static KValue k_sqlite_open(",  // SQLite host integration
	}
	for _, marker := range markers {
		if !strings.Contains(source, marker) {
			t.Errorf("assembled C runtime is missing marker %q", marker)
		}
	}
}
