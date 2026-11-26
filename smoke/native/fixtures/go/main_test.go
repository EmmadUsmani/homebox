package main

import "testing"

func TestCAnswer(t *testing.T) {
	if answer := cAnswer(); answer != 42 {
		t.Fatalf("cAnswer() = %d, want 42", answer)
	}
}
