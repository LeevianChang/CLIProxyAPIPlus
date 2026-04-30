package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/auth/kiro"
)

// InputToken represents the token format from your data
type InputToken struct {
	AccessToken  string `json:"accessToken"`
	ExpiresAt    string `json:"expiresAt"`
	RefreshToken string `json:"refreshToken"`
	Provider     string `json:"provider"`
	ProfileArn   string `json:"profileArn"`
}

func main() {
	inputFile := flag.String("input", "", "Input JSON file containing token array")
	outputDir := flag.String("output", "auths", "Output directory for token files")
	flag.Parse()

	if *inputFile == "" {
		fmt.Println("Usage: kiro-import -input <file.json> [-output <dir>]")
		fmt.Println("\nExample:")
		fmt.Println("  kiro-import -input tokens.json -output auths")
		os.Exit(1)
	}

	// Read input file
	data, err := os.ReadFile(*inputFile)
	if err != nil {
		fmt.Printf("Error reading input file: %v\n", err)
		os.Exit(1)
	}

	// Parse input tokens
	var inputTokens []InputToken
	if err := json.Unmarshal(data, &inputTokens); err != nil {
		fmt.Printf("Error parsing input JSON: %v\n", err)
		os.Exit(1)
	}

	if len(inputTokens) == 0 {
		fmt.Println("No tokens found in input file")
		os.Exit(1)
	}

	// Create output directory
	if err := os.MkdirAll(*outputDir, 0700); err != nil {
		fmt.Printf("Error creating output directory: %v\n", err)
		os.Exit(1)
	}

	// Convert and save each token
	for i, input := range inputTokens {
		// Extract email from access token JWT
		email := kiro.ExtractEmailFromJWT(input.AccessToken)

		// Determine auth method based on provider
		authMethod := "social"
		if strings.EqualFold(input.Provider, "AWS") {
			authMethod = "builder-id"
		}

		// Create KiroTokenStorage
		storage := &kiro.KiroTokenStorage{
			Type:         "kiro",
			AccessToken:  input.AccessToken,
			RefreshToken: input.RefreshToken,
			ProfileArn:   input.ProfileArn,
			ExpiresAt:    input.ExpiresAt,
			AuthMethod:   authMethod,
			Provider:     input.Provider,
			Email:        email,
		}

		// Generate filename
		var filename string
		if email != "" {
			sanitized := kiro.SanitizeEmailForFilename(email)
			filename = fmt.Sprintf("kiro-%s-%s.json", authMethod, sanitized)
		} else {
			filename = fmt.Sprintf("kiro-%s-%d.json", authMethod, i+1)
		}

		outputPath := filepath.Join(*outputDir, filename)

		// Save token file
		if err := storage.SaveTokenToFile(outputPath); err != nil {
			fmt.Printf("Error saving token %d: %v\n", i+1, err)
			continue
		}

		fmt.Printf("✓ Saved token %d to %s\n", i+1, outputPath)
		if email != "" {
			fmt.Printf("  Email: %s\n", email)
		}
		fmt.Printf("  Provider: %s\n", input.Provider)
		fmt.Printf("  Profile ARN: %s\n", input.ProfileArn)
		fmt.Println()
	}

	fmt.Printf("Successfully imported %d tokens to %s\n", len(inputTokens), *outputDir)
}
