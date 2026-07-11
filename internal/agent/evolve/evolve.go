package evolve

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/alex/codegateway/internal/agent/skill"
	"github.com/alex/codegateway/internal/provider"
)

// Evolution represents the self-evolution system
type Evolution struct {
	provider    provider.Provider
	skillMgr    *skill.SkillManager
	reflectFreq time.Duration
	improveFreq time.Duration
}

// NewEvolution creates a new evolution system
func NewEvolution(provider provider.Provider, skillMgr *skill.SkillManager) *Evolution {
	return &Evolution{
		provider:    provider,
		skillMgr:    skillMgr,
		reflectFreq: time.Hour * 24,   // Reflect daily
		improveFreq: time.Hour * 168,  // Improve weekly
	}
}

// Start starts the evolution system
func (e *Evolution) Start(ctx context.Context) {
	go e.reflectLoop(ctx)
	go e.improveLoop(ctx)
	log.Println("Evolution system started")
}

// reflectLoop runs the reflection loop
func (e *Evolution) reflectLoop(ctx context.Context) {
	ticker := time.NewTicker(e.reflectFreq)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := e.Reflect(ctx); err != nil {
				log.Printf("Reflection failed: %v", err)
			}
		}
	}
}

// improveLoop runs the improvement loop
func (e *Evolution) improveLoop(ctx context.Context) {
	ticker := time.NewTicker(e.improveFreq)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := e.Improve(ctx); err != nil {
				log.Printf("Improvement failed: %v", err)
			}
		}
	}
}

// Reflect reflects on past interactions and creates insights
func (e *Evolution) Reflect(ctx context.Context) error {
	log.Println("Starting reflection...")

	// Build reflection prompt
	prompt := `Analyze recent interactions and identify:
1. Common patterns in user requests
2. Areas where the agent could improve
3. New skills that would be helpful
4. Existing skills that need refinement

Provide actionable insights for improvement.`

	// Call LLM for reflection
	messages := []provider.Message{
		{Role: "system", Content: "You are a self-improvement analyst. Analyze the agent's performance and suggest improvements."},
		{Role: "user", Content: prompt},
	}

	resp, err := e.provider.ChatCompletion(ctx, &provider.ChatCompletionRequest{
		Model:    "gpt-4o",
		Messages: messages,
	})
	if err != nil {
		return fmt.Errorf("failed to reflect: %w", err)
	}

	// Save reflection as a skill
	if len(resp.Choices) > 0 {
		reflection := resp.Choices[0].Message.Content
		e.saveReflection(reflection)
	}

	log.Println("Reflection completed")
	return nil
}

// Improve improves existing skills based on reflection
func (e *Evolution) Improve(ctx context.Context) error {
	log.Println("Starting improvement...")

	// Get all skills
	skills := e.skillMgr.List()

	for _, s := range skills {
		// Skip builtin skills
		if s.Source == "builtin" {
			continue
		}

		// Analyze skill and suggest improvements
		improvement, err := e.analyzeSkill(ctx, s)
		if err != nil {
			log.Printf("Failed to analyze skill %s: %v", s.Name, err)
			continue
		}

		if improvement != "" {
			// Apply improvement
			if err := e.applyImprovement(s, improvement); err != nil {
				log.Printf("Failed to apply improvement to %s: %v", s.Name, err)
			}
		}
	}

	log.Println("Improvement completed")
	return nil
}

// analyzeSkill analyzes a skill and suggests improvements
func (e *Evolution) analyzeSkill(ctx context.Context, s *skill.Skill) (string, error) {
	prompt := fmt.Sprintf(`Analyze this skill and suggest improvements:

Name: %s
Description: %s
Content:
%s

Suggest specific improvements to make this skill more effective.`, s.Name, s.Description, s.Content)

	messages := []provider.Message{
		{Role: "system", Content: "You are a skill improvement analyst. Suggest specific improvements for the given skill."},
		{Role: "user", Content: prompt},
	}

	resp, err := e.provider.ChatCompletion(ctx, &provider.ChatCompletionRequest{
		Model:    "gpt-4o",
		Messages: messages,
	})
	if err != nil {
		return "", err
	}

	if len(resp.Choices) > 0 {
		return resp.Choices[0].Message.Content, nil
	}

	return "", nil
}

// applyImprovement applies an improvement to a skill
func (e *Evolution) applyImprovement(s *skill.Skill, improvement string) error {
	// Update skill content
	s.Content = improvement

	// Save skill
	return e.skillMgr.Save(s)
}

// saveReflection saves a reflection
func (e *Evolution) saveReflection(reflection string) {
	// Create a skill from reflection
	s := &skill.Skill{
		Name:        fmt.Sprintf("reflection-%s", time.Now().Format("2006-01-02")),
		Description: "Self-reflection and improvement insights",
		Content:     reflection,
		Source:      "evolved",
		Triggers:    []string{"reflection", "improvement"},
	}

	if err := e.skillMgr.Save(s); err != nil {
		log.Printf("Failed to save reflection: %v", err)
	}
}

// CreateSkill creates a new skill from experience
func (e *Evolution) CreateSkill(ctx context.Context, name, description, experience string) error {
	prompt := fmt.Sprintf(`Based on this experience, create a reusable skill:

Experience:
%s

Create a skill with:
1. Clear description
2. Step-by-step procedure
3. Common pitfalls to avoid
4. Verification steps`, experience)

	messages := []provider.Message{
		{Role: "system", Content: "You are a skill creator. Create reusable skills from experiences."},
		{Role: "user", Content: prompt},
	}

	resp, err := e.provider.ChatCompletion(ctx, &provider.ChatCompletionRequest{
		Model:    "gpt-4o",
		Messages: messages,
	})
	if err != nil {
		return fmt.Errorf("failed to create skill: %w", err)
	}

	if len(resp.Choices) > 0 {
		s := &skill.Skill{
			Name:        name,
			Description: description,
			Content:     resp.Choices[0].Message.Content,
			Source:      "evolved",
			Triggers:    []string{name},
		}

		return e.skillMgr.Save(s)
	}

	return nil
}

// LearnFromFeedback learns from user feedback
func (e *Evolution) LearnFromFeedback(ctx context.Context, feedback string) error {
	prompt := fmt.Sprintf(`Analyze this feedback and extract learnings:

Feedback:
%s

Extract:
1. What went well
2. What could be improved
3. Action items for improvement`, feedback)

	messages := []provider.Message{
		{Role: "system", Content: "You are a learning analyst. Extract actionable learnings from feedback."},
		{Role: "user", Content: prompt},
	}

	resp, err := e.provider.ChatCompletion(ctx, &provider.ChatCompletionRequest{
		Model:    "gpt-4o",
		Messages: messages,
	})
	if err != nil {
		return fmt.Errorf("failed to learn from feedback: %w", err)
	}

	if len(resp.Choices) > 0 {
		e.saveReflection(resp.Choices[0].Message.Content)
	}

	return nil
}
