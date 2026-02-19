package cmd

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// confirm prompts the user for y/n confirmation
func confirm(input io.Reader, prompt string) bool {
	reader := bufio.NewReader(input)
	fmt.Printf("%s [y/N]: ", prompt)
	response, err := reader.ReadString('\n')
	if err != nil {
		return false
	}
	response = strings.ToLower(strings.TrimSpace(response))
	return response == "y" || response == "yes"
}
