package main

import "testing"

func TestPathContains(t *testing.T) {
	if !pathContains(`C:\\Windows;C:\\Kryndel\\bin`, `c:\\kryndel\\bin`) {
		t.Fatal("expected PATH entry to be found case-insensitively")
	}
	if pathContains(`C:\\Windows;C:\\Kryndel\\bin-old`, `C:\\Kryndel\\bin`) {
		t.Fatal("must not match a PATH prefix")
	}
	if pathContains(`C:\\Windows`, `C:\\Kryndel\\bin`) {
		t.Fatal("unexpected PATH entry")
	}
}
