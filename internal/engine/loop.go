package engine

import (
	"context"
	"fmt"
	"log"

	"github.com/joanneffffff/go-tiny-claw/internal/provider"
	"github.com/joanneffffff/go-tiny-claw/internal/schema"
	"github.com/joanneffffff/go-tiny-claw/internal/tools"
)

// AgentEngine is the core engine that orchestrates the agent loop
type AgentEngine struct {
	provider  provider.LLMProvider
	registry tools.Registry
	messages []schema.Message
}

// NewAgentEngine creates a new agent engine
func NewAgentEngine(p provider.LLMProvider, r tools.Registry) *AgentEngine {
	return &AgentEngine{
		provider:  p,
		registry: r,
		messages: []schema.Message{},
	}
}

// Run executes the main loop with the given user input
func (e *AgentEngine) Run(userInput string) error {
	// Add user message
	e.messages = append(e.messages, schema.Message{
		Role:    schema.RoleUser,
		Content: userInput,
	})

	log.Printf("📝 User input: %s", userInput)

	// Get available tools
	availableTools := e.registry.GetAvailableTools()

	// Send to LLM
	response, err := e.provider.Generate(context.Background(), e.messages, availableTools)
	if err != nil {
		return fmt.Errorf("LLM request failed: %w", err)
	}

	log.Printf("🤖 LLM response: %s", response.Content)

	// Add assistant response to history
	e.messages = append(e.messages, *response)

	return nil
}

// GetMessages returns the conversation history
func (e *AgentEngine) GetMessages() []schema.Message {
	return e.messages
}
