package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	if os.Geteuid() == 0 {
		log.Fatal("running as root")
	}

	client := &http.Client{Timeout: 15 * time.Second}
	response, err := client.Get("https://example.com/")
	if err != nil {
		log.Fatal(err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		log.Fatalf("unexpected HTTPS status: %s", response.Status)
	}
	fmt.Println("https:200")
}
