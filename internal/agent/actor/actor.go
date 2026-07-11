package actor

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/alex/codegateway/internal/provider"
	"github.com/google/uuid"
)

// ActorStatus represents the status of an actor
type ActorStatus string

const (
	ActorStatusPending  ActorStatus = "pending"
	ActorStatusRunning  ActorStatus = "running"
	ActorStatusDone     ActorStatus = "done"
	ActorStatusFailed   ActorStatus = "failed"
	ActorStatusCanceled ActorStatus = "canceled"
)

// ActorType represents the type of actor
type ActorType string

const (
	ActorTypeExplore ActorType = "explore"
	ActorTypeGeneral ActorType = "general"
	ActorTypeCustom  ActorType = "custom"
)

// Actor represents a sub-agent
type Actor struct {
	ID         string      `json:"id"`
	Type       ActorType   `json:"type"`
	Status     ActorStatus `json:"status"`
	Prompt     string      `json:"prompt"`
	Result     string      `json:"result"`
	Error      string      `json:"error,omitempty"`
	CreatedAt  time.Time   `json:"created_at"`
	FinishedAt *time.Time  `json:"finished_at,omitempty"`
}

// SpawnOptions represents options for spawning an actor
type SpawnOptions struct {
	Type    ActorType `json:"type"`
	Prompt  string    `json:"prompt"`
	Timeout time.Duration `json:"timeout"`
}

// ActorRegistry manages actors
type ActorRegistry struct {
	actors   map[string]*Actor
	provider provider.Provider
	mu       sync.RWMutex
}

// NewActorRegistry creates a new actor registry
func NewActorRegistry(provider provider.Provider) *ActorRegistry {
	return &ActorRegistry{
		actors:   make(map[string]*Actor),
		provider: provider,
	}
}

// Spawn spawns a new actor
func (r *ActorRegistry) Spawn(opts SpawnOptions) (*Actor, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	actor := &Actor{
		ID:        uuid.New().String(),
		Type:      opts.Type,
		Status:    ActorStatusPending,
		Prompt:    opts.Prompt,
		CreatedAt: time.Now(),
	}

	r.actors[actor.ID] = actor

	// Start actor in background
	go r.runActor(actor, opts.Timeout)

	return actor, nil
}

// runActor runs an actor
func (r *ActorRegistry) runActor(actor *Actor, timeout time.Duration) {
	// Update status
	r.mu.Lock()
	actor.Status = ActorStatusRunning
	r.mu.Unlock()

	// Create context with timeout
	ctx := context.Background()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	// Build system prompt based on actor type
	systemPrompt := r.buildSystemPrompt(actor.Type)

	// Call LLM
	messages := []provider.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: actor.Prompt},
	}

	resp, err := r.provider.ChatCompletion(ctx, &provider.ChatCompletionRequest{
		Model:    "gpt-4o",
		Messages: messages,
	})

	// Update actor
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	actor.FinishedAt = &now

	if err != nil {
		actor.Status = ActorStatusFailed
		actor.Error = err.Error()
	} else {
		actor.Status = ActorStatusDone
		if len(resp.Choices) > 0 {
			actor.Result = resp.Choices[0].Message.Content
		}
	}
}

// buildSystemPrompt builds the system prompt for an actor type
func (r *ActorRegistry) buildSystemPrompt(actorType ActorType) string {
	switch actorType {
	case ActorTypeExplore:
		return `You are an explore agent specialized in searching and analyzing codebases.
Your task is to explore the codebase and find relevant information.
Be thorough but concise in your findings.`
	case ActorTypeGeneral:
		return `You are a general-purpose agent.
Complete the given task to the best of your ability.
Be thorough and provide detailed results.`
	default:
		return `You are a helpful assistant.
Complete the given task to the best of your ability.`
	}
}

// Get returns an actor by ID
func (r *ActorRegistry) Get(id string) (*Actor, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	actor, ok := r.actors[id]
	if !ok {
		return nil, fmt.Errorf("actor not found: %s", id)
	}

	return actor, nil
}

// List returns all actors
func (r *ActorRegistry) List(status ActorStatus) []*Actor {
	r.mu.RLock()
	defer r.mu.RUnlock()

	actors := make([]*Actor, 0)
	for _, actor := range r.actors {
		if status == "" || actor.Status == status {
			actors = append(actors, actor)
		}
	}

	return actors
}

// Wait waits for an actor to complete
func (r *ActorRegistry) Wait(id string, timeout time.Duration) (*Actor, error) {
	deadline := time.Now().Add(timeout)

	for {
		actor, err := r.Get(id)
		if err != nil {
			return nil, err
		}

		if actor.Status == ActorStatusDone || actor.Status == ActorStatusFailed || actor.Status == ActorStatusCanceled {
			return actor, nil
		}

		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timeout waiting for actor: %s", id)
		}

		time.Sleep(time.Millisecond * 100)
	}
}

// Cancel cancels an actor
func (r *ActorRegistry) Cancel(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	actor, ok := r.actors[id]
	if !ok {
		return fmt.Errorf("actor not found: %s", id)
	}

	if actor.Status == ActorStatusRunning || actor.Status == ActorStatusPending {
		actor.Status = ActorStatusCanceled
		now := time.Now()
		actor.FinishedAt = &now
	}

	return nil
}

// Delete deletes an actor
func (r *ActorRegistry) Delete(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	actor, ok := r.actors[id]
	if !ok {
		return fmt.Errorf("actor not found: %s", id)
	}

	if actor.Status == ActorStatusRunning {
		return fmt.Errorf("cannot delete running actor: %s", id)
	}

	delete(r.actors, id)
	return nil
}

// GetStats returns actor statistics
func (r *ActorRegistry) GetStats() map[ActorStatus]int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	stats := make(map[ActorStatus]int)
	for _, actor := range r.actors {
		stats[actor.Status]++
	}

	return stats
}
