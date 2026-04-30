package skills

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/v1claw/levik/pkg/config"
)

func TestParseGitHubRepo(t *testing.T) {
	spec, err := parseGitHubRepo("amit-vikramaditya/levik-skills/weather")
	if err != nil {
		t.Fatalf("parseGitHubRepo returned error: %v", err)
	}
	if spec.Owner != "amit-vikramaditya" || spec.Repo != "levik-skills" || spec.Path != "weather" {
		t.Fatalf("unexpected spec: %+v", spec)
	}

	urlSpec, err := parseGitHubRepo("https://github.com/amit-vikramaditya/levik-skills/weather")
	if err != nil {
		t.Fatalf("parseGitHubRepo returned error for URL: %v", err)
	}
	if urlSpec != spec {
		t.Fatalf("URL parse mismatch: got %+v want %+v", urlSpec, spec)
	}
}

func TestSkillInstaller_InstallFromGitHub_CopiesWholeSkillDirectory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/test-owner/test-repo/tarball" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/gzip")
		if _, err := w.Write(makeSkillTarball(t)); err != nil {
			t.Fatalf("write response: %v", err)
		}
	}))
	defer server.Close()

	workspace := t.TempDir()
	installer := NewSkillInstaller(workspace)
	installer.client = server.Client()
	installer.githubAPIBase = server.URL

	if err := installer.InstallFromGitHub(context.Background(), "test-owner/test-repo/weather"); err != nil {
		t.Fatalf("InstallFromGitHub returned error: %v", err)
	}

	skillRoot := filepath.Join(workspace, "skills", "weather")
	for _, rel := range []string{"SKILL.md", "scripts/helper.sh", "references/guide.md"} {
		path := filepath.Join(skillRoot, rel)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected installed file %s: %v", rel, err)
		}
	}
}

func TestSkillInstaller_ListBuiltinSkills_FallsBackToGlobalSkills(t *testing.T) {
	workspace := t.TempDir()
	home := t.TempDir()
	t.Setenv(config.HomeEnvVar, home)

	globalSkillDir := filepath.Join(home, "skills", "weather")
	requireNoError(t, os.MkdirAll(globalSkillDir, 0755))
	requireNoError(t, os.WriteFile(filepath.Join(globalSkillDir, "SKILL.md"), []byte("# Weather\n"), 0644))

	installer := NewSkillInstaller(workspace)
	skills := installer.ListBuiltinSkills()

	if len(skills) != 1 {
		t.Fatalf("expected 1 builtin skill, got %d", len(skills))
	}
	if skills[0].Name != "weather" {
		t.Fatalf("expected skill name weather, got %s", skills[0].Name)
	}
}

func requireNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func makeSkillTarball(t *testing.T) []byte {
	t.Helper()

	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)

	files := map[string]string{
		"test-owner-test-repo-abcdef/weather/SKILL.md":             "---\nname: weather\ndescription: Weather skill\n---\n",
		"test-owner-test-repo-abcdef/weather/scripts/helper.sh":    "#!/bin/sh\necho helper\n",
		"test-owner-test-repo-abcdef/weather/references/guide.md":  "# Guide\n",
		"test-owner-test-repo-abcdef/README.md":                    "ignore me\n",
		"test-owner-test-repo-abcdef/weather/nested/notes/info.md": "nested\n",
	}

	for name, content := range files {
		hdr := &tar.Header{
			Name: name,
			Mode: 0600,
			Size: int64(len(content)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("write header: %v", err)
		}
		if _, err := io.WriteString(tw, content); err != nil {
			t.Fatalf("write content: %v", err)
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("close tar writer: %v", err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}
	return buf.Bytes()
}
