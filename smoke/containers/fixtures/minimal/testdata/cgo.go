package main

// #include <unistd.h>
import "C"

import (
	"fmt"
	"log"
)

func main() {
	if C.geteuid() == 0 {
		log.Fatal("running as root")
	}
	fmt.Println("cgo:42")
}
