package main

import "github.com/colbytimm/alchemist/cmd"

func main() {
	rootCmd := cmd.Root()
	if err := rootCmd.Execute(); err != nil {
		panic(err)
	}
}
