// Command genkey prints a new Ed25519 signing key seed, for SHORTLY_JWT_PRIVATE_KEY.
//
//	go run ./cmd/genkey >> .env
package main

import (
	"fmt"

	"github.com/JP0722/shortly/internal/auth"
)

func main() {
	seed, err := auth.GenerateKeySeed()
	if err != nil {
		panic(err)
	}
	fmt.Printf("SHORTLY_JWT_PRIVATE_KEY=%s\n", seed)
}
