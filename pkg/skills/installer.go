package skills

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Vatthu/vikram/pkg/config"
)

type SkillInstaller struct {
	workspace     string
	client        *http.Client
	githubAPIBase string
}

type AvailableSkill struct {
	Name        string   `json:"name"`
	Repository  string   `json:"repository"`
	Description string   `json:"description"`
	Author      string   `json:"author"`
	Tags        []string `json:"tags"`
}

type BuiltinSkill struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Enabled bool   `json:"enabled"`
}

func NewSkillInstaller(workspace string) *SkillInstaller {
	return &SkillInstaller{
		workspace:     workspace,
		client:        &http.Client{Timeout: 30 * time.Second},
		githubAPIBase: "https://api.github.com",
	}
}

func (si *SkillInstaller) InstallFromGitHub(ctx context.Context, repo string) error {
	spec, err := parseGitHubRepo(repo)
	if err != nil {
		return err
	}

	skillDir := filepath.Join(si.workspace, "skills", spec.SkillName())
	if _, err := os.Stat(skillDir); err == nil {
		return fmt.Errorf("skill '%s' already exists", spec.SkillName())
	}

	archiveURL := fmt.Sprintf("%s/repos/%s/%s/tarball", strings.TrimRight(si.githubAPIBase, "/"), spec.Owner, spec.Repo)
	req, err := http.NewRequestWithContext(ctx, "GET", archiveURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := si.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch skill: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to fetch skill: HTTP %d", resp.StatusCode)
	}

	tmpDir, err := os.MkdirTemp("", "vikram-skill-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := extractTarball(resp.Body, tmpDir); err != nil {
		return fmt.Errorf("failed to extract skill archive: %w", err)
	}

	sourceDir, err := locateExtractedSkillDir(tmpDir, spec.Path)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(skillDir, 0700); err != nil {
		return fmt.Errorf("failed to create skill directory: %w", err)
	}

	if err := copySkillDir(sourceDir, skillDir); err != nil {
		return fmt.Errorf("failed to install skill contents: %w", err)
	}

	return nil
}

type gitHubRepoSpec struct {
	Owner string
	Repo  string
	Path  string
}

func (s gitHubRepoSpec) SkillName() string {
	if s.Path != "" {
		return filepath.Base(s.Path)
	}
	return s.Repo
}

func parseGitHubRepo(input string) (gitHubRepoSpec, error) {
	normalized := strings.TrimSpace(input)
	normalized = strings.TrimPrefix(normalized, "https://github.com/")
	normalized = strings.TrimPrefix(normalized, "http://github.com/")
	normalized = strings.Trim(normalized, "/")
	normalized = strings.TrimSuffix(normalized, ".git")

	parts := strings.Split(normalized, "/")
	if len(parts) < 2 {
		return gitHubRepoSpec{}, fmt.Errorf("invalid GitHub repo %q; expected owner/repo[/path]", input)
	}

	owner := strings.TrimSpace(parts[0])
	repo := strings.TrimSpace(parts[1])
	subpath := filepath.Clean(strings.Join(parts[2:], "/"))
	if subpath == "." {
		subpath = ""
	}
	if owner == "" || repo == "" {
		return gitHubRepoSpec{}, fmt.Errorf("invalid GitHub repo %q; expected owner/repo[/path]", input)
	}
	if strings.Contains(subpath, "..") {
		return gitHubRepoSpec{}, fmt.Errorf("invalid skill path %q", input)
	}

	return gitHubRepoSpec{
		Owner: owner,
		Repo:  repo,
		Path:  strings.TrimPrefix(subpath, "/"),
	}, nil
}

func extractTarball(r io.Reader, dest string) error {
	gzr, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer gzr.Close()

	destRoot := filepath.Clean(dest)
	tr := tar.NewReader(gzr)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		if header.Name == "" || filepath.IsAbs(header.Name) {
			return fmt.Errorf("invalid archive entry path: %s", header.Name)
		}
		cleanName := filepath.Clean(header.Name)
		if cleanName == "." || cleanName == ".." || strings.HasPrefix(cleanName, ".."+string(filepath.Separator)) {
			return fmt.Errorf("archive entry escapes extraction root: %s", header.Name)
		}

		cleanTarget := filepath.Join(destRoot, cleanName)
		relToRoot, err := filepath.Rel(destRoot, cleanTarget)
		if err != nil {
			return fmt.Errorf("invalid archive entry path: %s", header.Name)
		}
		if relToRoot == ".." || strings.HasPrefix(relToRoot, ".."+string(filepath.Separator)) {
			return fmt.Errorf("archive entry escapes extraction root: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(cleanTarget, 0755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(cleanTarget), 0755); err != nil {
				return err
			}
			f, err := os.OpenFile(cleanTarget, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
		}
	}
}

func locateExtractedSkillDir(extractRoot string, subpath string) (string, error) {
	entries, err := os.ReadDir(extractRoot)
	if err != nil {
		return "", fmt.Errorf("failed to read extracted archive: %w", err)
	}
	if len(entries) == 0 {
		return "", fmt.Errorf("skill archive is empty")
	}

	repoRoot := extractRoot
	if len(entries) == 1 && entries[0].IsDir() {
		repoRoot = filepath.Join(extractRoot, entries[0].Name())
	}

	sourceDir := repoRoot
	if subpath != "" {
		sourceDir = filepath.Join(repoRoot, filepath.FromSlash(subpath))
	}

	skillFile := filepath.Join(sourceDir, "SKILL.md")
	if _, err := os.Stat(skillFile); err != nil {
		return "", fmt.Errorf("skill not found at %q inside GitHub archive", subpath)
	}

	return sourceDir, nil
}

func copySkillDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		if info.IsDir() {
			return os.MkdirAll(target, 0700)
		}

		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return err
		}

		in, err := os.Open(path)
		if err != nil {
			return err
		}

		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
		if err != nil {
			if cerr := in.Close(); cerr != nil {
				return fmt.Errorf("failed to open output file: %v (additionally failed to close input file: %w)", err, cerr)
			}
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			inErr := in.Close()
			outErr := out.Close()
			if inErr != nil {
				return fmt.Errorf("copy failed: %v (additionally failed to close input file: %w)", err, inErr)
			}
			if outErr != nil {
				return fmt.Errorf("copy failed: %v (additionally failed to close output file: %w)", err, outErr)
			}
			return err
		}
		if err := in.Close(); err != nil {
			out.Close()
			return err
		}
		return out.Close()
	})
}

func (si *SkillInstaller) Uninstall(skillName string) error {
	// Validate skill name to prevent path traversal
	if strings.Contains(skillName, "..") || strings.Contains(skillName, "/") || strings.Contains(skillName, "\\") {
		return fmt.Errorf("invalid skill name: %q", skillName)
	}

	skillDir := filepath.Join(si.workspace, "skills", skillName)

	if _, err := os.Stat(skillDir); os.IsNotExist(err) {
		return fmt.Errorf("skill '%s' not found", skillName)
	}

	if err := os.RemoveAll(skillDir); err != nil {
		return fmt.Errorf("failed to remove skill: %w", err)
	}

	return nil
}

func (si *SkillInstaller) ListAvailableSkills(ctx context.Context) ([]AvailableSkill, error) {
	url := "https://raw.githubusercontent.com/amit-vikramaditya/vikram-skills/main/skills.json"

	client := si.client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch skills list: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch skills list: HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var skills []AvailableSkill
	if err := json.Unmarshal(body, &skills); err != nil {
		return nil, fmt.Errorf("failed to parse skills list: %w", err)
	}

	return skills, nil
}

func (si *SkillInstaller) ListBuiltinSkills() []BuiltinSkill {
	builtinSkillsDir := filepath.Join(si.workspace, "skills")
	if info, err := os.Stat(builtinSkillsDir); err != nil || !info.IsDir() {
		builtinSkillsDir = config.GlobalSkillsDir()
	}

	entries, err := os.ReadDir(builtinSkillsDir)
	if err != nil {
		return nil
	}

	var skills []BuiltinSkill
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillName := entry.Name()
		skillFile := filepath.Join(builtinSkillsDir, skillName, "SKILL.md")
		if _, err := os.Stat(skillFile); err != nil {
			continue
		}

		skills = append(skills, BuiltinSkill{
			Name:    skillName,
			Path:    filepath.Join(builtinSkillsDir, skillName),
			Enabled: true,
		})
	}
	return skills
}
