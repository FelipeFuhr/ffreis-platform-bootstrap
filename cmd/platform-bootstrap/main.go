package main

import (
	"os"

	"github.com/ffreis/platform-bootstrap/cmd"
)

func main() {
	os.Exit(cmd.Execute())
}
