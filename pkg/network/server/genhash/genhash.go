package main

import (
	"fmt"
	"log"
	"os"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/ssh/terminal"
)

func main() {
	fmt.Println("Type password (echo is off) and return: ")
	password, err := terminal.ReadPassword(int(os.Stdin.Fd()))
	if err != nil {
		log.Fatal("Couldd not read password: %v", err)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		log.Fatal("Coild not hash input password:%v", err)
	}
	fmt.Printf("\nThe hash is: %s\n", &hash)
}
