package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"time"

	"github.com/Vatthu/vikram/pkg/config"
	"github.com/Vatthu/vikram/pkg/skills"
)
func skillsHelp() {
	fmt.Println("\nSkills commands:")
	fmt.Println("  list                    List installed skills")
	fmt.Println("  install <repo>          Install skill from GitHub")
	fmt.Println("  install-builtin          Install all builtin skills to workspace")
	fmt.Println("  list-builtin             List available builtin skills")
	fmt.Println("  remove <name>           Remove installed skill")
	fmt.Println("  search                  Search available skills")
	fmt.Println("  show <name>             Show skill details")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  vikram skills list")
	fmt.Println("  vikram skills install amit-vikramaditya/vikram-skills/weather")
	fmt.Println("  vikram skills install-builtin")
	fmt.Println("  vikram skills list-builtin")
	fmt.Println("  vikram skills remove weather")
}

func skillsListCmd(loader *skills.SkillsLoader) {
	allSkills := loader.ListSkills()

	if len(allSkills) == 0 {
		fmt.Println("No skills installed.")
		return
	}

	fmt.Println("\nInstalled Skills:")
	fmt.Println("------------------")
	for _, skill := range allSkills {
		fmt.Printf("  ✓ %s (%s)\n", skill.Name, skill.Source)
		if skill.Description != "" {
			fmt.Printf("    %s\n", skill.Description)
		}
	}
}

func skillsInstallCmd(installer *skills.SkillInstaller) {
	if len(os.Args) < 4 {
		fmt.Println("Usage: vikram skills install <github-repo>")
		fmt.Println("Example: vikram skills install amit-vikramaditya/vikram-skills/weather")
		return
	}

	repo := os.Args[3]
	fmt.Printf("Installing skill from %s...\n", repo)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := installer.InstallFromGitHub(ctx, repo); err != nil {
		fmt.Printf("✗ Failed to install skill: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Skill '%s' installed successfully!\n", filepath.Base(repo))
}

func skillsRemoveCmd(installer *skills.SkillInstaller, skillName string) {
	fmt.Printf("Removing skill '%s'...\n", skillName)

	if err := installer.Uninstall(skillName); err != nil {
		fmt.Printf("✗ Failed to remove skill: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Skill '%s' removed successfully!\n", skillName)
}

func detectBuiltinSkillsDir(workspace string) string {
	candidates := []string{
		filepath.Join(workspace, "skills"),
		config.GlobalSkillsDir(),
		filepath.Join(".", "workspace", "skills"),
		filepath.Join(".", "cmd", "vikram", "workspace", "skills"),
		filepath.Join(".", "skills"),
	}

	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		clean := filepath.Clean(candidate)
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}

		if info, err := os.Stat(clean); err == nil && info.IsDir() {
			return clean
		}
	}

	return ""
}

func readSkillDescription(skillFile string) string {
	data, err := os.ReadFile(skillFile)
	if err != nil {
		return "No description"
	}

	lines := strings.Split(string(data), "\n")
	if len(lines) < 3 || strings.TrimSpace(lines[0]) != "---" {
		return "No description"
	}

	for i := 1; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "---" {
			break
		}
		if strings.HasPrefix(line, "description:") {
			desc := strings.TrimSpace(strings.TrimPrefix(line, "description:"))
			desc = strings.Trim(desc, "\"'")
			if desc != "" {
				return desc
			}
		}
	}

	return "No description"
}

func skillsInstallBuiltinCmd(workspace string) {
	builtinSkillsDir := detectBuiltinSkillsDir(workspace)
	if builtinSkillsDir == "" {
		fmt.Println("✗ No builtin skills directory found.")
		fmt.Println("  Run `vikram onboard` first, or execute from the source repository.")
		return
	}

	workspaceSkillsDir := filepath.Join(workspace, "skills")
	builtinAbs, _ := filepath.Abs(builtinSkillsDir)
	workspaceAbs, _ := filepath.Abs(workspaceSkillsDir)
	if builtinAbs == workspaceAbs {
		fmt.Println("✓ Builtin skills are already present in your workspace.")
		return
	}

	fmt.Printf("Copying builtin skills from %s to workspace...\n", builtinSkillsDir)

	entries, err := os.ReadDir(builtinSkillsDir)
	if err != nil {
		fmt.Printf("✗ Failed to read builtin skills directory: %v\n", err)
		return
	}

	copied := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		skillName := entry.Name()
		builtinPath := filepath.Join(builtinSkillsDir, skillName)
		skillFile := filepath.Join(builtinPath, "SKILL.md")
		if _, err := os.Stat(skillFile); err != nil {
			continue
		}

		workspacePath := filepath.Join(workspaceSkillsDir, skillName)
		if err := os.MkdirAll(workspacePath, 0755); err != nil {
			fmt.Printf("✗ Failed to create directory for %s: %v\n", skillName, err)
			continue
		}
		if err := copyDirectory(builtinPath, workspacePath); err != nil {
			fmt.Printf("✗ Failed to copy %s: %v\n", skillName, err)
			continue
		}
		copied++
	}

	fmt.Printf("\n✓ Installed %d builtin skill(s)\n", copied)
}

func skillsListBuiltinCmd(workspace string) {
	builtinSkillsDir := detectBuiltinSkillsDir(workspace)
	if builtinSkillsDir == "" {
		fmt.Println("No builtin skills directory found.")
		return
	}

	fmt.Println("\nAvailable Builtin Skills:")
	fmt.Println("-----------------------")

	entries, err := os.ReadDir(builtinSkillsDir)
	if err != nil {
		fmt.Printf("Error reading builtin skills: %v\n", err)
		return
	}

	if len(entries) == 0 {
		fmt.Println("No builtin skills available.")
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			skillName := entry.Name()
			skillFile := filepath.Join(builtinSkillsDir, skillName, "SKILL.md")

			description := readSkillDescription(skillFile)
			status := "✓"
			fmt.Printf("  %s  %s\n", status, entry.Name())
			if description != "" {
				fmt.Printf("     %s\n", description)
			}
		}
	}
}

func skillsSearchCmd(installer *skills.SkillInstaller) {
	fmt.Println("Searching for available skills...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	availableSkills, err := installer.ListAvailableSkills(ctx)
	if err != nil {
		fmt.Printf("✗ Failed to fetch skills list: %v\n", err)
		return
	}

	if len(availableSkills) == 0 {
		fmt.Println("No skills available.")
		return
	}

	fmt.Printf("\nAvailable Skills (%d):\n", len(availableSkills))
	fmt.Println("--------------------")
	for _, skill := range availableSkills {
		fmt.Printf("  📦 %s\n", skill.Name)
		fmt.Printf("     %s\n", skill.Description)
		fmt.Printf("     Repo: %s\n", skill.Repository)
		if skill.Author != "" {
			fmt.Printf("     Author: %s\n", skill.Author)
		}
		if len(skill.Tags) > 0 {
			fmt.Printf("     Tags: %v\n", skill.Tags)
		}
		fmt.Println()
	}
}

func skillsShowCmd(loader *skills.SkillsLoader, skillName string) {
	content, ok := loader.LoadSkill(skillName)
	if !ok {
		fmt.Printf("✗ Skill '%s' not found\n", skillName)
		return
	}

	fmt.Printf("\n📦 Skill: %s\n", skillName)
	fmt.Println("----------------------")
	fmt.Println(content)
}

