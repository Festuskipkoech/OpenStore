package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
)

func main() {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		fmt.Fprintf(os.Stderr, "error generating key: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("ops_live_%s\n", hex.EncodeToString(b))
}