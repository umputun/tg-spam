// Command authhash prints a bcrypt hash for SERVER_AUTH_HASH, the web UI password of the
// antispam stacks. Without it the bot generates a random password on every start and
// prints it to the log. README suggests htpasswd or mkpasswd, neither of which is
// installed on the machines this team works from; this needs only a checked-out repo.
//
// The password is read from stdin so it stays out of shell history and the process list:
//
//	printf '%s' 'the password' | go run ./deploy/wb-compat/authhash
package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

func main() {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, "can't read password from stdin:", err)
		os.Exit(1)
	}

	pass := strings.TrimRight(string(data), "\r\n")
	if pass == "" {
		fmt.Fprintln(os.Stderr, "empty password, pass it on stdin")
		os.Exit(1)
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.DefaultCost)
	if err != nil {
		fmt.Fprintln(os.Stderr, "can't hash password:", err)
		os.Exit(1)
	}
	fmt.Println(string(hash))
}
