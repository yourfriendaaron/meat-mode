package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/yourfriendaaron/meat-mode/internal/auth"
)

// runHashPassword prints the Argon2id hash of a password for
// MEAT_MODE_PASSWORD_HASH. On a terminal it asks twice without echo;
// otherwise it reads one line from stdin.
func runHashPassword(args []string) error {
	if len(args) > 0 {
		return errors.New("hash-password takes no arguments; it reads the password from the terminal or stdin")
	}
	pw, err := readPassword(os.Stdin, os.Stderr)
	if err != nil {
		return err
	}
	if pw == "" {
		return errors.New("the password is empty")
	}
	h, err := auth.HashPassword(pw)
	if err != nil {
		return err
	}
	fmt.Println(h)
	return nil
}

func readPassword(in *os.File, prompt io.Writer) (string, error) {
	fd := int(in.Fd())
	if !term.IsTerminal(fd) {
		line, err := bufio.NewReader(in).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", err
		}
		return strings.TrimRight(line, "\r\n"), nil
	}
	fmt.Fprint(prompt, "Password: ")
	a, err := term.ReadPassword(fd)
	fmt.Fprintln(prompt)
	if err != nil {
		return "", err
	}
	fmt.Fprint(prompt, "Again: ")
	b, err := term.ReadPassword(fd)
	fmt.Fprintln(prompt)
	if err != nil {
		return "", err
	}
	if string(a) != string(b) {
		return "", errors.New("the passwords do not match")
	}
	return string(a), nil
}
