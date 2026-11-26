package main

/*
static int answer(void) { return 42; }
*/
import "C"

func cAnswer() int {
	return int(C.answer())
}
