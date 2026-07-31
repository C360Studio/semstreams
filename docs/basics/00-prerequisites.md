# Prerequisites

This guide covers everything you need to set up your development environment for SemStreams.

## Required Software

| Software | Version | Purpose |
|----------|---------|---------|
| Go | 1.25+ | Build and run SemStreams |
| Docker | Latest | Run NATS, E2E tests, deployment |
| Task | Latest | Task runner for build commands |

## Quick Verification

Run this command to check all prerequisites at once:

```bash
task dev:check:prerequisites
```

Or verify each tool manually:

```bash
go version          # Should show go1.25 or higher
docker info         # Should show Docker daemon info
task --version      # Should show Task version
```

## Installation Instructions

### Go

Go 1.25 or higher is required.

**macOS (Homebrew)**:
```bash
brew install go
```

**Linux (apt)**:
```bash
# Add Go repository
sudo add-apt-repository ppa:longsleep/golang-backports
sudo apt update
sudo apt install golang-go
```

**All platforms**:
Download from [go.dev/dl](https://go.dev/dl/) and follow the installation instructions.

**Verify**:
```bash
go version
# go version go1.25.0 darwin/arm64
```

### Docker

Docker is used to run NATS for local development and for E2E tests.

**macOS**:
```bash
brew install --cask docker
# Then open Docker Desktop from Applications
```

**Linux**:
```bash
# Install Docker Engine
curl -fsSL https://get.docker.com -o get-docker.sh
sudo sh get-docker.sh

# Add your user to the docker group
sudo usermod -aG docker $USER

# Log out and back in, then verify
docker info
```

**Windows**:
Download and install [Docker Desktop for Windows](https://www.docker.com/products/docker-desktop/).

**Verify**:
```bash
docker info
# Should show Docker daemon information without errors
```

### Task

Task is a modern alternative to Make. We use it for all build and test commands.

**All platforms (recommended)**:
```bash
go install github.com/go-task/task/v3/cmd/task@latest
```

**macOS (Homebrew)**:
```bash
brew install go-task
```

**Verify**:
```bash
task --version
# Task version: v3.x.x
```

## NATS Server

NATS is the only runtime dependency. For local development, we run it in Docker.

### Start NATS for Development

The simplest way is to use the task command:

```bash
task dev:nats:start
```

This starts a NATS server with JetStream enabled at `nats://localhost:4222`.

### Manual Docker Command

If you prefer to run Docker directly:

```bash
docker run -d \
  --name semstreams-nats \
  -p 4222:4222 \
  -p 8222:8222 \
  nats:2.14-alpine -js -m 8222
```

Ports:
- **4222**: NATS client connections
- **8222**: HTTP monitoring endpoint

### Verify NATS is Running

```bash
# Check container status
docker ps --filter name=semstreams-nats

# Check NATS health endpoint
curl -s http://localhost:8222/healthz
# {"status":"ok"}
```

### Stop NATS

```bash
task dev:nats:stop
```

Or manually:
```bash
docker stop semstreams-nats
```

### Clean Up (Remove Container and Data)

```bash
task dev:nats:clean
```

## Optional: NATS CLI

The NATS CLI is useful for debugging and exploring JetStream:

```bash
# Install
go install github.com/nats-io/natscli/nats@latest

# Verify
nats --version

# List streams
nats stream ls

# Subscribe to all messages
nats sub '>'
```

## Troubleshooting

### "go: command not found"

Your Go installation isn't in PATH. Add this to your shell profile (~/.bashrc, ~/.zshrc):

```bash
export PATH=$PATH:$(go env GOPATH)/bin
```

Then reload your shell or run `source ~/.bashrc`.

### "Cannot connect to the Docker daemon"

Docker Desktop isn't running. Start it from your Applications folder (macOS) or system tray (Windows/Linux).

### "task: command not found"

The Task binary isn't in your PATH. If you installed with `go install`, ensure `$GOPATH/bin` is in your PATH:

```bash
export PATH=$PATH:$(go env GOPATH)/bin
```

### "connection refused" when connecting to NATS

NATS isn't running. Start it with:

```bash
task dev:nats:start
```

### Port 4222 already in use

Another NATS instance or application is using the port. Find and stop it:

```bash
# Find what's using port 4222
lsof -i :4222

# Or use a different port
docker run -d --name semstreams-nats -p 4223:4222 nats:2.14-alpine -js
# Then update configs/hello-world.json to use nats://localhost:4223
```

## Next Steps

Once your environment is set up:

1. [Build and run SemStreams](../../README.md#your-first-5-minutes)
2. [Understand the architecture](02-architecture.md)
3. [Learn about the Graphable interface](03-graphable-interface.md)
4. [Build your first processor](05-first-processor.md)
