package main

import (
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

func main() {
	tokens := []string{
		"",
	}

	fmt.Println("Hashed tokens for ansible vault:")
	fmt.Println()

	for i, token := range tokens {
		hashed, err := bcrypt.GenerateFromPassword([]byte(token), bcrypt.DefaultCost)
		if err != nil {
			fmt.Printf("Error hashing token %d: %v\n", i+1, err)
			continue
		}
		fmt.Printf("Token %d: %s\n", i+1, string(hashed))
	}
}
