package skill

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Skill represents a skill
type Skill struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Content     string   `json:"content"`
	Triggers    []string `json:"triggers"`
	Source      string   `json:"source"`
	Path        string   `json:"path"`
}

// SkillManager manages skills
type SkillManager struct {
	skills   map[string]*Skill
	builtin  string
	custom   string
}

// NewSkillManager creates a new skill manager
func NewSkillManager(builtinPath, customPath string) *SkillManager {
	return &SkillManager{
		skills:  make(map[string]*Skill),
		builtin: builtinPath,
		custom:  customPath,
	}
}

// Discover discovers skills from paths
func (m *SkillManager) Discover() error {
	// Discover builtin skills
	if m.builtin != "" {
		if err := m.discoverFromPath(m.builtin, "builtin"); err != nil {
			return fmt.Errorf("failed to discover builtin skills: %w", err)
		}
	}

	// Discover custom skills
	if m.custom != "" {
		if err := m.discoverFromPath(m.custom, "custom"); err != nil {
			// Custom skills directory might not exist, that's OK
			if !os.IsNotExist(err) {
				return fmt.Errorf("failed to discover custom skills: %w", err)
			}
		}
	}

	return nil
}

// discoverFromPath discovers skills from a path
func (m *SkillManager) discoverFromPath(basePath, source string) error {
	return filepath.Walk(basePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Look for SKILL.md files
		if !info.IsDir() && info.Name() == "SKILL.md" {
			skill, err := m.loadSkill(path, source)
			if err != nil {
				return fmt.Errorf("failed to load skill %s: %w", path, err)
			}
			m.skills[skill.Name] = skill
		}

		return nil
	})
}

// loadSkill loads a skill from a file
func (m *SkillManager) loadSkill(path, source string) (*Skill, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read skill file: %w", err)
	}

	skill := &Skill{
		Content: string(content),
		Source:  source,
		Path:    path,
	}

	// Parse frontmatter
	m.parseFrontmatter(skill, string(content))

	// If no name extracted, use directory name
	if skill.Name == "" {
		dir := filepath.Dir(path)
		skill.Name = filepath.Base(dir)
	}

	return skill, nil
}

// parseFrontmatter parses YAML frontmatter from skill content
func (m *SkillManager) parseFrontmatter(skill *Skill, content string) {
	scanner := bufio.NewScanner(strings.NewReader(content))
	inFrontmatter := false
	frontmatter := ""

	for scanner.Scan() {
		line := scanner.Text()

		if line == "---" {
			if inFrontmatter {
				// End of frontmatter
				m.parseYAML(skill, frontmatter)
				return
			}
			inFrontmatter = true
			continue
		}

		if inFrontmatter {
			frontmatter += line + "\n"
		}
	}
}

// parseYAML parses simple YAML key-value pairs
func (m *SkillManager) parseYAML(skill *Skill, yaml string) {
	lines := strings.Split(yaml, "\n")
	for _, line := range lines {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		switch key {
		case "name":
			skill.Name = strings.Trim(value, "\"'")
		case "description":
			skill.Description = strings.Trim(value, "\"'")
		case "triggers":
			// Parse array
			value = strings.Trim(value, "[]")
			triggers := strings.Split(value, ",")
			for _, t := range triggers {
				skill.Triggers = append(skill.Triggers, strings.TrimSpace(strings.Trim(t, "\"'")))
			}
		}
	}
}

// Get returns a skill by name
func (m *SkillManager) Get(name string) (*Skill, error) {
	skill, ok := m.skills[name]
	if !ok {
		return nil, fmt.Errorf("skill not found: %s", name)
	}
	return skill, nil
}

// List returns all skills
func (m *SkillManager) List() []*Skill {
	skills := make([]*Skill, 0, len(m.skills))
	for _, skill := range m.skills {
		skills = append(skills, skill)
	}
	return skills
}

// FindByTrigger finds skills by trigger
func (m *SkillManager) FindByTrigger(trigger string) []*Skill {
	trigger = strings.ToLower(trigger)
	skills := make([]*Skill, 0)

	for _, skill := range m.skills {
		for _, t := range skill.Triggers {
			if strings.Contains(strings.ToLower(t), trigger) {
				skills = append(skills, skill)
				break
			}
		}
	}

	return skills
}

// Save saves a skill to disk
func (m *SkillManager) Save(skill *Skill) error {
	if skill.Path == "" {
		// Generate path
		skill.Path = filepath.Join(m.custom, skill.Name, "SKILL.md")
	}

	// Ensure directory exists
	dir := filepath.Dir(skill.Path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Build content
	content := "---\n"
	content += fmt.Sprintf("name: %s\n", skill.Name)
	content += fmt.Sprintf("description: %s\n", skill.Description)
	if len(skill.Triggers) > 0 {
		triggers := make([]string, len(skill.Triggers))
		for i, t := range skill.Triggers {
			triggers[i] = fmt.Sprintf("\"%s\"", t)
		}
		content += fmt.Sprintf("triggers: [%s]\n", strings.Join(triggers, ", "))
	}
	content += "---\n\n"
	content += skill.Content

	// Write file
	if err := os.WriteFile(skill.Path, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write skill: %w", err)
	}

	// Register skill
	m.skills[skill.Name] = skill

	return nil
}

// Delete deletes a skill
func (m *SkillManager) Delete(name string) error {
	skill, ok := m.skills[name]
	if !ok {
		return fmt.Errorf("skill not found: %s", name)
	}

	// Only allow deleting custom skills
	if skill.Source != "custom" {
		return fmt.Errorf("cannot delete builtin skill: %s", name)
	}

	// Delete file
	if err := os.Remove(skill.Path); err != nil {
		return fmt.Errorf("failed to delete skill file: %w", err)
	}

	// Remove from map
	delete(m.skills, name)

	return nil
}
