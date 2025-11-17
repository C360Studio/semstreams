package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	// Define flags
	configPath := flag.String("config", "graphql-config.json", "Path to config file")
	outputDir := flag.String("output", "generated", "Output directory for generated code")
	schemaPath := flag.String("schema", "", "Path to GraphQL schema (overrides config)")

	// Parse flags
	flag.Parse()

	// Run generation
	if err := runGenerate(*configPath, *outputDir, *schemaPath); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Code generation complete!")
}
