package provider

import "github.com/joanneffffff/go-tiny-claw/internal/schema"

// LLMProvider defines the interface for LLM providers
type LLMProvider interface {
	// SendMessage sends a message to the LLM and returns the response
	SendMessage(messages []schema.Message) (*schema.Message, error)

	// GetName returns the provider name
	GetName() string
}
