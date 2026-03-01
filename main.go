// Role:    Pinix CLI entrypoint (cobra root command)
// Depends: cmd, cobra
// Exports: main

package main

import (
	"fmt"
	"os"

	"github.com/epiral/pinix/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
