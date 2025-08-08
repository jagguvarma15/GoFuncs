package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/jagguvarma15/GoFuncs/internal/cli"
)

const (
	version = "v0.1.0"
	usage   = `GoFuncs CLI - Distributed Function Execution Platform

Usage:
  gofuncs <command> [options]

Commands:
  deploy      Deploy a function to the cluster
  call        Execute a function
  list        List all deployed functions
  remove      Remove a function from the cluster
  status      Show cluster status
  nodes       List executor nodes
  version     Show version information

Options:
  -coordinator string    Coordinator address (default "localhost:8080")
  -verbose              Enable verbose output
  -help                 Show help information

Examples:
  # Deploy a function
  gofuncs deploy --function=Hello --file=hello.go

  # Execute a function
  gofuncs call Hello '{"name": "World"}'

  # List all functions
  gofuncs list

  # Show cluster status
  gofuncs status

For more information about a specific command:
  gofuncs <command> --help
`
)

func main() {
	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(1)
	}

	command := os.Args[1]

	// Global flags
	var (
		coordinator = flag.String("coordinator", "localhost:8080", "Coordinator address")
		verbose     = flag.Bool("verbose", false, "Enable verbose output")
		help        = flag.Bool("help", false, "Show help information")
	)

	// Parse global flags from remaining args
	flag.CommandLine.Parse(os.Args[2:])

	if *help {
		fmt.Print(usage)
		os.Exit(0)
	}

	// Create CLI client
	client := cli.NewClient(cli.Config{
		CoordinatorAddress: *coordinator,
		Verbose:            *verbose,
	})

	// Execute command
	var err error
	switch command {
	case "deploy":
		err = handleDeployCommand(client, flag.Args())
	case "call", "execute":
		err = handleCallCommand(client, flag.Args())
	case "list", "ls":
		err = handleListCommand(client, flag.Args())
	case "remove", "rm", "delete":
		err = handleRemoveCommand(client, flag.Args())
	case "status":
		err = handleStatusCommand(client, flag.Args())
	case "nodes":
		err = handleNodesCommand(client, flag.Args())
	case "version", "--version", "-v":
		err = handleVersionCommand(client, flag.Args())
	case "help", "--help", "-h":
		fmt.Print(usage)
		os.Exit(0)
	default:
		fmt.Printf("Unknown command: %s\n\n", command)
		fmt.Print(usage)
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func handleDeployCommand(client *cli.Client, args []string) error {
	deployCmd := flag.NewFlagSet("deploy", flag.ExitOnError)
	functionName := deployCmd.String("function", "", "Function name (required)")
	functionFile := deployCmd.String("file", "", "Function source file (required)")
	version := deployCmd.String("version", "v1.0.0", "Function version")

	deployCmd.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: gofuncs deploy --function=NAME --file=FILE [options]\n\n")
		fmt.Fprintf(os.Stderr, "Deploy a Go function to the cluster.\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		deployCmd.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExample:\n")
		fmt.Fprintf(os.Stderr, "  gofuncs deploy --function=Hello --file=hello.go --version=v1.0.0\n")
	}

	if err := deployCmd.Parse(args); err != nil {
		return err
	}

	if *functionName == "" || *functionFile == "" {
		deployCmd.Usage()
		return fmt.Errorf("function name and file are required")
	}

	return client.DeployFunction(*functionName, *functionFile, *version)
}

func handleCallCommand(client *cli.Client, args []string) error {
	if len(args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: gofuncs call <function-name> <input-json>\n\n")
		fmt.Fprintf(os.Stderr, "Execute a function with the given input.\n\n")
		fmt.Fprintf(os.Stderr, "Examples:\n")
		fmt.Fprintf(os.Stderr, "  gofuncs call Hello '{\"name\": \"World\"}'\n")
		fmt.Fprintf(os.Stderr, "  gofuncs call ProcessData '{\"data\": [1,2,3]}'\n")
		return fmt.Errorf("function name and input are required")
	}

	functionName := args[0]
	inputJSON := args[1]

	// Validate JSON input
	var inputData interface{}
	if err := json.Unmarshal([]byte(inputJSON), &inputData); err != nil {
		return fmt.Errorf("invalid JSON input: %w", err)
	}

	return client.CallFunction(functionName, inputJSON)
}

func handleListCommand(client *cli.Client, args []string) error {
	listCmd := flag.NewFlagSet("list", flag.ExitOnError)
	format := listCmd.String("format", "table", "Output format: table, json")

	listCmd.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: gofuncs list [options]\n\n")
		fmt.Fprintf(os.Stderr, "List all deployed functions.\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		listCmd.PrintDefaults()
	}

	if err := listCmd.Parse(args); err != nil {
		return err
	}

	return client.ListFunctions(*format)
}

func handleRemoveCommand(client *cli.Client, args []string) error {
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "Usage: gofuncs remove <function-name>\n\n")
		fmt.Fprintf(os.Stderr, "Remove a function from the cluster.\n\n")
		fmt.Fprintf(os.Stderr, "Example:\n")
		fmt.Fprintf(os.Stderr, "  gofuncs remove Hello\n")
		return fmt.Errorf("function name is required")
	}

	functionName := args[0]
	return client.RemoveFunction(functionName)
}

func handleStatusCommand(client *cli.Client, args []string) error {
	statusCmd := flag.NewFlagSet("status", flag.ExitOnError)
	format := statusCmd.String("format", "table", "Output format: table, json")

	statusCmd.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: gofuncs status [options]\n\n")
		fmt.Fprintf(os.Stderr, "Show cluster status and health information.\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		statusCmd.PrintDefaults()
	}

	if err := statusCmd.Parse(args); err != nil {
		return err
	}

	return client.ShowStatus(*format)
}

func handleNodesCommand(client *cli.Client, args []string) error {
	nodesCmd := flag.NewFlagSet("nodes", flag.ExitOnError)
	format := nodesCmd.String("format", "table", "Output format: table, json")

	nodesCmd.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: gofuncs nodes [options]\n\n")
		fmt.Fprintf(os.Stderr, "List all executor nodes in the cluster.\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		nodesCmd.PrintDefaults()
	}

	if err := nodesCmd.Parse(args); err != nil {
		return err
	}

	return client.ListNodes(*format)
}

func handleVersionCommand(client *cli.Client, args []string) error {
	fmt.Printf("GoFuncs CLI %s\n", version)
	fmt.Println("Distributed Function Execution Platform")
	fmt.Println("https://github.com/jagguvarma15/GoFuncs")
	return nil
}
