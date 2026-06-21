# Security Policy

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| main    | :white_check_mark: |
| < 1.0   | :x:                |

## Reporting a Vulnerability

**Do NOT open a public issue for security vulnerabilities.**

We take security seriously. If you discover a vulnerability, please report it responsibly:

### Preferred: GitHub Security Advisory

Use the **"Report a vulnerability"** button under the **[Security](https://github.com/Vatthu/vikram/security/advisories/new)** tab of this repository. This creates a private advisory that only maintainers can see.

### Alternative: Direct Contact

Email: [amit.vikramaditya@icloud.com](mailto:amit.vikramaditya@icloud.com)

Please include:
- Description of the vulnerability
- Steps to reproduce
- Impact assessment
- Suggested fix (if any)

## Response Timeline

| Action | Target |
|--------|--------|
| Acknowledge receipt | 48 hours |
| Initial assessment | 5 business days |
| Fix released | 30 days (critical: 7 days) |

## Security Measures

Vikram employs multiple layers of security:

- **Local-first**: Your API keys and data never leave your machine
- **Command sandboxing**: Shell commands go through an allowlist-based security middleware
- **Path traversal protection**: Workspace restriction prevents directory escapes
- **Hardware permissions**: Explicit opt-in required for camera, microphone, SMS, location, etc.
- **Deny-by-default**: Dangerous patterns (rm -rf /, sudo, fork bombs) are blocked
- **Secret scanning**: GitHub secret scanning and push protection enabled
- **Dependency auditing**: Dependabot monitors all dependencies (Go, Python, npm, GitHub Actions)
- **CodeQL analysis**: Automated code scanning on every push and pull request

## Scope

The following are in scope for security reports:

- Command injection / sandbox escapes
- Path traversal bypasses
- API key leakage
- Privilege escalation
- Authentication bypasses
- Cross-channel message injection

## Out of Scope

- Denial of service via resource consumption (self-hosted tool)
- Issues in third-party LLM providers
- Social engineering attacks
