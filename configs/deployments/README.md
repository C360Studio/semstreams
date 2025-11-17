# Deployment Configuration Examples

This directory contains example configurations for different deployment scenarios.

## Configuration Files

### JSON Configuration Files

**`minimal.json`** - Bare minimum configuration
```json
{
  "port": 8080,
  "nats": { "url": "nats://localhost:4222" }
}
```
- Use when: Testing or development with defaults
- All other settings use built-in defaults

**`development.json`** - Development environment
- Debug logging enabled
- Metrics and health checks
- Local NATS connection
- Use when: Local development and debugging

**`production-small.json`** - Small edge device (2GB RAM, 2 cores)
- Warn-level logging (saves storage)
- Resource limits: 512MB RAM, 50% CPU
- Aggressive reconnection for intermittent connectivity
- Use when: Constrained edge hardware

**`production-medium.json`** - Medium edge device (4GB RAM, 4 cores)
- Info-level logging
- Resource limits: 1GB RAM, 80% CPU
- Use when: Standard edge deployment

**`production-full.json`** - Complete configuration reference
- Documents all available configuration options
- Includes TLS, security, features, storage settings
- Use when: Understanding all available options
- Reference for customizing your own config

### Docker Compose Files

**`docker-compose.production.yml`** - Production single-node deployment
- Complete production setup with NATS + SemStreams
- Resource limits configured for edge devices
- Health checks and restart policies
- Security hardening (read-only, cap_drop, etc.)
- Internal network for NATS isolation
- Use when: Deploying to production edge device

## Usage

### Quick Start (Development)

```bash
# Copy example config
cp configs/deployments/development.json config.json

# Start with docker compose
docker compose up -d
```

### Production Deployment

```bash
# 1. Copy production config
cp configs/deployments/production-medium.json /opt/semstreams/config.json

# 2. Set secrets
echo "NATS_PASSWORD=your-secure-password" > /opt/semstreams/.env
chmod 600 /opt/semstreams/.env

# 3. Copy docker compose
cp configs/deployments/docker-compose.production.yml /opt/semstreams/docker-compose.yml

# 4. Start services
cd /opt/semstreams
docker compose up -d

# 5. Verify
curl http://localhost:8080/health
```

### Multi-Location Deployment

For multiple edge locations, each runs independently with output→input component connections:

**Location A** (sends data):
- Use `production-medium.json` as base
- Add WebSocket output component in flow configuration
- Flow outputs to `ws://location-a:9000`

**Location B** (receives data):
- Use `production-medium.json` as base
- Add WebSocket input component in flow configuration
- Flow inputs from `ws://location-a:9000`

See [PRODUCTION.md](../../docs/deployment/PRODUCTION.md#multi-location-deployment) for complete multi-location patterns.

## Environment Variable Overrides

All config files can be overridden with environment variables:

```bash
# Override port
SEMSTREAMS_PORT=9090

# Override NATS URL
SEMSTREAMS_NATS_URL=nats://remote-nats:4222

# Set secrets (always use env vars for secrets!)
SEMSTREAMS_NATS_PASSWORD=secret-from-vault
```

See [CONFIGURATION.md](../../docs/deployment/CONFIGURATION.md) for complete environment variable documentation.

## Configuration Precedence

When SemStreams starts:

1. **Code Defaults** - Built-in sensible defaults
2. **Config File** - `/etc/semstreams/config.json` (mounted in container)
3. **Environment Variables** - `SEMSTREAMS_*` overrides
4. **NATS KV Store** - Runtime updates via semstreams-ui

**Important**: Container restart resets to file + env vars (intentional for infrastructure-as-code).

## Customization

To create your own configuration:

1. Start with the appropriate base:
   - Small edge device: `production-small.json`
   - Standard deployment: `production-medium.json`
   - Need all options: `production-full.json`

2. Customize for your environment:
   - Adjust resource limits based on hardware
   - Configure log level based on storage constraints
   - Set up TLS if needed
   - Enable features as required

3. **Never put secrets in config files** - always use environment variables

4. Validate before deployment:
   ```bash
   # Check JSON syntax
   jq . < your-config.json

   # Test with Docker
   docker run --rm \
     -v $(pwd)/your-config.json:/etc/semstreams/config.json:ro \
     ghcr.io/c360/semstreams:latest validate
   ```

## See Also

- [Configuration Guide](../../docs/deployment/CONFIGURATION.md) - Complete configuration reference
- [Production Deployment](../../docs/deployment/PRODUCTION.md) - Production deployment patterns
- [Operations Runbook](../../docs/deployment/OPERATIONS.md) - Day-to-day operations
