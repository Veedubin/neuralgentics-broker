package main

import (
	"fmt"
	"log"

	"neuralgentics-broker/src/neuralgentics/broker"
	"neuralgentics-broker/src/neuralgentics/broker/access"
	"neuralgentics-broker/src/neuralgentics/broker/types"
)

func main() {
	b := broker.NewBrokerWithWorkspace(".")

	// Register example test servers with descriptions and capabilities.
	testServers := []types.ServerConfig{
		{
			Name:         "filesystem",
			Command:      "npx",
			Args:         []string{"-y", "@modelcontextprotocol/server-filesystem", "/tmp"},
			Type:         "stdio",
			Env:          map[string]string{"NODE_PATH": "/usr/local/lib/node_modules"},
			Description:  "File system operations for reading and writing files",
			Capabilities: []string{"file", "io"},
		},
		{
			Name:         "memory",
			Command:      "npx",
			Args:         []string{"-y", "@modelcontextprotocol/server-memory"},
			Type:         "stdio",
			Description:  "Persistent knowledge graph and memory storage",
			Capabilities: []string{"memory", "search"},
		},
		{
			Name:         "playwright",
			Command:      "npx",
			Args:         []string{"-y", "@modelcontextprotocol/server-playwright"},
			Type:         "stdio",
			Description:  "Browser automation and web scraping",
			Capabilities: []string{"browser", "scraping"},
		},
	}

	for _, cfg := range testServers {
		if err := b.RegisterServer(cfg); err != nil {
			log.Fatalf("Failed to register server %q: %v", cfg.Name, err)
		}
		fmt.Printf("Registered server: %s\n", cfg.Name)
	}

	// Populate tools for demonstration (normally acquired via Initialize + ListTools).
	demoTools := []types.ToolSummary{
		{Server: "filesystem", Name: "read_file", Description: "Read a file from disk"},
		{Server: "filesystem", Name: "write_file", Description: "Write content to a file on disk"},
		{Server: "memory", Name: "create_entity", Description: "Create a knowledge graph entity"},
		{Server: "memory", Name: "search_memories", Description: "Search stored memories by query"},
		{Server: "playwright", Name: "browser_click", Description: "Click a browser element"},
		{Server: "playwright", Name: "browser_navigate", Description: "Navigate to a URL"},
	}

	for i := range testServers {
		name := testServers[i].Name
		var serverTools []types.ToolSummary
		for _, t := range demoTools {
			if t.Server == name {
				serverTools = append(serverTools, t)
			}
		}
		b.SetTools(name, serverTools)
	}

	// List all registered servers.
	fmt.Println("\nRegistered servers:")
	for _, status := range b.ListServers() {
		fmt.Printf("  - %s (running: %v, tools: %d)\n", status.Name, status.Running, len(status.Tools))
	}

	// === Catalog Demo ===

	fmt.Println("\n--- Catalog: BuildServerCatalog(\"orchestrator\") ---")

	orchCatalog := b.BuildServerCatalog("orchestrator")
	fmt.Printf("Orchestrator catalog (%d servers, %d total tools):\n",
		len(orchCatalog.Servers), orchCatalog.TotalTools)
	for _, s := range orchCatalog.Servers {
		fmt.Printf("  - %s: %s [%v] (%d tools, %s)\n",
			s.Name, s.Description, s.Capabilities, s.ToolsCount, s.Status)
	}

	fmt.Println("\n--- Catalog: BuildServerCatalog(\"coder\") ---")

	coderCatalog := b.BuildServerCatalog("coder")
	fmt.Printf("Coder catalog (%d servers):\n", len(coderCatalog.Servers))
	for _, s := range coderCatalog.Servers {
		fmt.Printf("  - %s: %s\n", s.Name, s.Description)
	}

	fmt.Println("\n--- Catalog: ExpandServer(\"filesystem\") ---")

	fsTools, err := b.ExpandServer("filesystem")
	if err != nil {
		fmt.Printf("  Error: %v\n", err)
	} else {
		fmt.Printf("Filesystem tools (%d):\n", len(fsTools.Tools))
		for _, t := range fsTools.Tools {
			fmt.Printf("  - %s: %s\n", t.Name, t.Description)
		}
	}

	// === Access Control Demo ===

	fmt.Println("\n--- Access Control ---")

	ac := b.AccessControl()
	fmt.Printf("Can coder access filesystem? %v\n", ac.CanAccess("coder", "filesystem"))
	fmt.Printf("Can coder access playwright? %v\n", ac.CanAccess("coder", "playwright"))
	fmt.Printf("Can orchestrator access playwright? %v\n", ac.CanAccess("orchestrator", "playwright"))

	unauthErr := access.ErrUnauthorized{
		Role:             "coder",
		Server:           "playwright",
		Reason:           "role coder cannot access server playwright",
		AvailableServers: ac.GetAccessibleServers("coder"),
	}
	fmt.Printf("ErrUnauthorized: %v\n", unauthErr)

	// === InjectPrompt Demo ===

	fmt.Println("\n--- InjectPrompt(\"orchestrator\") ---")

	prompt, err := b.InjectPrompt("orchestrator")
	if err != nil {
		fmt.Printf("  Error: %v\n", err)
	} else {
		fmt.Println(prompt)
	}

	// === Intent Matching Demo (preserved) ===

	fmt.Println("--- Intent Matching Demo ---")
	intents := []string{
		"read a file",
		"search my memories",
		"write data to disk",
		"create a new entity",
	}

	fmt.Println("\nIntent matching (role=orchestrator, all servers):")
	for _, intentStr := range intents {
		match, err := b.MatchIntent("orchestrator", intentStr)
		if err != nil {
			fmt.Printf("  %q → error: %v\n", intentStr, err)
			continue
		}
		fmt.Printf("  %q → %s/%s (score: %.2f)\n",
			intentStr, match.Tool.Server, match.Tool.Name, match.Score)
	}

	fmt.Println("\nIntent matching (role=coder, filtered servers):")
	for _, intentStr := range intents {
		match, err := b.MatchIntent("coder", intentStr)
		if err != nil {
			fmt.Printf("  %q → error: %v\n", intentStr, err)
			continue
		}
		fmt.Printf("  %q → %s/%s (score: %.2f)\n",
			intentStr, match.Tool.Server, match.Tool.Name, match.Score)
	}

	fmt.Println("\nBroker demonstration complete.")
}
