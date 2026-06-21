# Releasing Vikram

This project is released through the `Create Tag and Release` GitHub Actions workflow in `.github/workflows/release.yml`.

## Preconditions

- `main` contains the release commits you want to publish.
- You have GitHub push access to `amit-vikramaditya/vikram`.
- GitHub Actions is enabled for the repository.
- Optional: Docker Hub secrets/vars are configured if you want Docker Hub publishing in addition to GHCR.

## Local Preflight

Run the local pre-release checks before publishing:

```bash
make release-check
```

This verifies:

- formatting is clean
- `go vet ./...`
- `go test ./...`
- `make build`
- `install.sh` shell syntax
- workflow and GoReleaser YAML parsing
- source installer smoke test
- end-to-end local-provider smoke test using a stub OpenAI-compatible endpoint

## Publish

1. Push `main`:

```bash
git push origin main
```

2. Open GitHub Actions and run `Create Tag and Release`.

3. Supply a semantic version tag such as:

```text
v0.1.0
```

4. Set `draft` or `prerelease` if needed.

The workflow will:

- rerun formatting, vet, test, and installer smoke checks
- create and push the tag
- build release archives with GoReleaser
- publish the GitHub release
- publish GHCR container images
- publish Docker Hub images if Docker Hub credentials are configured

## Post-Release Verification

After the GitHub release is live, verify the published installer path:

```bash
curl -fsSL https://raw.githubusercontent.com/amit-vikramaditya/vikram/main/install.sh | bash -s -- --version vX.Y.Z
vikram version
```

On Windows:

```powershell
$installer = Join-Path $env:TEMP "vikram-install.ps1"
Invoke-WebRequest "https://raw.githubusercontent.com/amit-vikramaditya/vikram/main/install.ps1" -OutFile $installer
powershell -ExecutionPolicy Bypass -File $installer -Version vX.Y.Z
vikram.exe version
```

## If Publish Fails

- If `git push` fails, fix GitHub auth on the publishing machine first.
- If the workflow fails before tagging, fix the reported gate and rerun.
- If the release fails after the tag is created, inspect the failed workflow logs before retrying.
