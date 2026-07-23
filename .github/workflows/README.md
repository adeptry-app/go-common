# GitHub Actions CI/CD

## Workflows

### CI Pipeline (`ci.yml`)

Comprehensive continuous integration pipeline that runs on:

- Pull requests to `main` or `develop`
- Pushes to `main` or `develop`
- Manual workflow dispatch

**Jobs:**

1. **Lint** - Code quality checks with golangci-lint
2. **Test** - Unit tests with race detection and coverage reporting
3. **Vulnerability Scan** - Dependency security scanning with govulncheck
4. **Build Verification** - Verify all packages compile successfully
5. **Security Analysis** - Static security analysis with gosec
6. **Code Quality** - Format checks, go vet, and ineffassign detection

**Security Features:**

- Results uploaded to GitHub Security tab (SARIF format)
- Fails on CRITICAL/HIGH vulnerabilities
- Codecov integration for coverage tracking

## Status Badges

Add these to your README.md:

```markdown
![CI](https://github.com/adeptry-app/go-common/workflows/CI/badge.svg)
[![Go Report Card](https://goreportcard.com/badge/github.com/adeptry-app/go-common)](https://goreportcard.com/report/github.com/adeptry-app/go-common)
[![codecov](https://codecov.io/gh/adeptry-app/go-common/branch/main/graph/badge.svg)](https://codecov.io/gh/adeptry-app/go-common)
```

## Local Testing

Using Task:

```bash
task format            # Format code
task test              # Run tests
task test:coverage     # Run tests with coverage report
task lint              # Run golangci-lint
task security:vuln     # Check for vulnerabilities
task ci:all            # Run all CI checks
task dev:install-tools # Install dev tools (golangci-lint, govulncheck, etc.)
```

## Configuration Files

- `.golangci.yml` - golangci-lint configuration (if present)
