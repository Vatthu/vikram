// Vikram — Enterprise AI Engineering Team
// License: MIT

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	vikramassets "github.com/Vatthu/vikram"
	"github.com/Vatthu/vikram/pkg/config"
	"github.com/Vatthu/vikram/pkg/skills"
)

var embeddedFiles = vikramassets.Workspace

var (
	version   = "dev"
	gitCommit string
	buildTime string
	goVersion string
)

const logo = "Vikram"

// formatVersion returns the version string with optional git commit
func formatVersion() string {
	v := version
	if gitCommit != "" {
		v += fmt.Sprintf(" (git: %s)", gitCommit)
	}
	return v
}

// formatBuildInfo returns build time and go version info
func formatBuildInfo() (build string, goVer string) {
	if buildTime != "" {
		build = buildTime
	}
	goVer = goVersion
	if goVer == "" {
		goVer = runtime.Version()
	}
	return
}

func printVersion() {
	fmt.Printf("Vikram %s\n", formatVersion())
	build, goVer := formatBuildInfo()
	if build != "" {
		fmt.Printf("  Build: %s\n", build)
	}
	if goVer != "" {
		fmt.Printf("  Go: %s\n", goVer)
	}
}

func getConfigPath() string {
	return config.ConfigPath()
}

func historyFilePath(name string) string {
	historyDir := filepath.Join(config.HomeDir(), "history")
	if err := os.MkdirAll(historyDir, 0700); err == nil {
		return filepath.Join(historyDir, name)
	}
	return filepath.Join(os.TempDir(), name)
}

func loadConfig() (*config.Config, error) {
	return config.LoadConfig(getConfigPath())
}

func main() {
	if len(os.Args) < 2 {
		printHelp()
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "onboard":
		onboardCmd()
	case "configure":
		configureCmd()
	case "doctor":
		doctorCmd()
	case "agent":
		agentCmd()
	case "client":
		clientCmd()
	case "gateway":
		gatewayCmd()
	case "status":
		statusCmd()
	case "auth":
		authCmd()
	case "telegram":
		telegramCmd()
	case "cron":
		cronCmd()
	case "skills":
		if len(os.Args) < 3 {
			skillsHelp()
			return
		}

		subcommand := os.Args[2]

		cfg, err := loadConfig()
		if err != nil {
			fmt.Printf("Error loading config: %v\n", err)
			os.Exit(1)
		}

		workspace := cfg.WorkspacePath()
		installer := skills.NewSkillInstaller(workspace)
		// Get global config directory and built-in skills directory
		globalDir := filepath.Dir(getConfigPath())
		globalSkillsDir := filepath.Join(globalDir, "skills")
		builtinSkillsDir := detectBuiltinSkillsDir(workspace)
		skillsLoader := skills.NewSkillsLoader(workspace, globalSkillsDir, builtinSkillsDir)

		switch subcommand {
		case "list":
			skillsListCmd(skillsLoader)
		case "install":
			skillsInstallCmd(installer)
		case "remove", "uninstall":
			if len(os.Args) < 4 {
				fmt.Println("Usage: vikram skills remove <skill-name>")
				return
			}
			skillsRemoveCmd(installer, os.Args[3])
		case "install-builtin":
			skillsInstallBuiltinCmd(workspace)
		case "list-builtin":
			skillsListBuiltinCmd(workspace)
		case "search":
			skillsSearchCmd(installer)
		case "show":
			if len(os.Args) < 4 {
				fmt.Println("Usage: vikram skills show <skill-name>")
				return
			}
			skillsShowCmd(skillsLoader, os.Args[3])
		default:
			fmt.Printf("Unknown skills command: %s\n", subcommand)
			skillsHelp()
		}
	case "version", "--version", "-v":
		printVersion()
	case "smoke":
		smokeCmd()
	case "help", "--help", "-h":
		printHelp()
	default:
		fmt.Printf("Unknown command: %s\n", command)
		printHelp()
		os.Exit(1)
	}
}

func printHelp() {
	fmt.Printf("Vikram v%s\n\n", version)

	fmt.Println(titleStyle.Render("  ✨ First time here?"))
	fmt.Printf("     %s\n\n", stepStyle.Render("Run:  vikram onboard   ← 2-minute setup wizard"))

	fmt.Println("Usage: vikram <command>")
	fmt.Println()
	fmt.Println("Getting started:")
	fmt.Println("  onboard     Guided setup wizard — start here if you're new")
	fmt.Println("  doctor      Check that everything is configured and working")
	fmt.Println()
	fmt.Println("Daily use:")
	fmt.Println("  agent       Chat with your AI assistant")
	fmt.Println("  client      Connect to a remote Vikram gateway")
	fmt.Println("  gateway     Start V1 gateway")
	fmt.Println("  status      Show V1 status")
	fmt.Println()
	fmt.Println("Management:")
	fmt.Println("  configure   Change settings (workspace, model, channels, tools)")
	fmt.Println("  skills      Manage skills  (install, list, remove)")
	fmt.Println("  cron        Manage scheduled tasks")
	fmt.Println("  auth        Manage authentication (login, logout, status)")
	fmt.Println("  telegram    Manage Telegram pairing and access")
	fmt.Println("  smoke       Run operational smoke checks")
	fmt.Println("  version     Show version information")
	fmt.Println()
	fmt.Printf("%s\n", stepStyle.Render("  Tip: run  vikram agent -m \"your question\"  to ask something quickly."))
}
