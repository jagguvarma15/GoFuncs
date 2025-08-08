package main

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// HelloRequest represents the input for the Hello function
type HelloRequest struct {
	Name     string `json:"name"`
	Language string `json:"language,omitempty"`
	Formal   bool   `json:"formal,omitempty"`
}

// HelloResponse represents the output from the Hello function
type HelloResponse struct {
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
	Language  string    `json:"language"`
}

// Hello is a simple greeting function that demonstrates basic GoFuncs usage
func Hello(ctx context.Context, req HelloRequest) (HelloResponse, error) {
	// Validate input
	if strings.TrimSpace(req.Name) == "" {
		return HelloResponse{}, fmt.Errorf("name cannot be empty")
	}

	// Set default language
	if req.Language == "" {
		req.Language = "en"
	}

	// Generate greeting based on language and formality
	greeting := generateGreeting(req.Name, req.Language, req.Formal)

	return HelloResponse{
		Message:   greeting,
		Timestamp: time.Now(),
		Language:  req.Language,
	}, nil
}

// generateGreeting creates a greeting message based on parameters
func generateGreeting(name, language string, formal bool) string {
	switch language {
	case "es", "spanish":
		if formal {
			return fmt.Sprintf("Buenos días, señor/señora %s", name)
		}
		return fmt.Sprintf("¡Hola, %s!", name)
	case "fr", "french":
		if formal {
			return fmt.Sprintf("Bonjour, monsieur/madame %s", name)
		}
		return fmt.Sprintf("Salut, %s!", name)
	case "de", "german":
		if formal {
			return fmt.Sprintf("Guten Tag, Herr/Frau %s", name)
		}
		return fmt.Sprintf("Hallo, %s!", name)
	case "ja", "japanese":
		if formal {
			return fmt.Sprintf("%sさん、こんにちは", name)
		}
		return fmt.Sprintf("%sちゃん、こんにちは", name)
	default: // English
		if formal {
			return fmt.Sprintf("Good day, Mr./Ms. %s", name)
		}
		return fmt.Sprintf("Hello, %s!", name)
	}
}
