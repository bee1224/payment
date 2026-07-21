package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	delivery "payment-service/internal/delivery/http"
)

func main() {
	fmt.Fprint(os.Stderr, "Password: ")
	password, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && len(password) == 0 {
		fmt.Fprintln(os.Stderr, "read password:", err)
		os.Exit(1)
	}
	hash, err := delivery.NewAdminPasswordHash(strings.TrimRight(password, "\r\n"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "hash password:", err)
		os.Exit(1)
	}
	fmt.Println(hash)
}
