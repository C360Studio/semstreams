# HTTP POST Output

Sends NATS messages to HTTP/HTTPS endpoints via POST requests with automatic retry and exponential backoff.

## Purpose

The HTTP POST output component bridges stream processing pipelines to external HTTP services, enabling webhook
integration, API calls, and third-party service communication. It consumes messages from NATS subjects or JetStream
and delivers them as HTTP POST requests with configurable retry logic, custom headers, and TLS support including
mutual TLS and ACME certificate management.

## Configuration

```yaml
name: webhook-sender
type: output.httppost
config:
  ports:
    inputs:
      - name: webhook_input
        type: nats                    # or "jetstream"
        subject: events.webhook       # NATS subject pattern
        required: true
  url: https://api.example.com/events # Target HTTP endpoint (required)
  timeout: 30                          # Request timeout in seconds (default: 30)
  retry_count: 3                       # Number of retry attempts (default: 3, max: 10)
  content_type: application/json       # Content-Type header (default: application/json)
  headers:
    Authorization: Bearer ${API_TOKEN} # Custom headers (env var expansion supported)
    X-Source: semstreams
```

### Configuration Fields

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `url` | string | Yes | - | HTTP/HTTPS endpoint URL |
| `timeout` | int | No | 30 | Request timeout in seconds (0-300) |
| `retry_count` | int | No | 3 | Number of retry attempts (0-10) |
| `content_type` | string | No | application/json | Content-Type header value |
| `headers` | map[string]string | No | {} | Custom HTTP headers |

## Input/Output Ports

### Input Ports

The component accepts NATS or JetStream input ports configured in the `ports.inputs` section:

**NATS Input:**

```yaml
ports:
  inputs:
    - name: events
      type: nats
      subject: system.events.>
      required: true
```

**JetStream Input:**

```yaml
ports:
  inputs:
    - name: durable_events
      type: jetstream
      subject: orders.created
      stream_name: ORDERS          # Optional, derived from subject if omitted
      required: true
```

For JetStream inputs, the component automatically creates a durable consumer with explicit acknowledgment and a
maximum of 5 delivery attempts.

### Output Ports

The HTTP POST output has no NATS output ports. Messages are delivered exclusively to the configured HTTP endpoint.

## Retry and Error Handling

### Retry Logic

Failed HTTP requests are automatically retried with exponential backoff:

```
Attempt 1: Immediate
Attempt 2: After 100ms  (1*1*100)
Attempt 3: After 400ms  (2*2*100)
Attempt 4: After 900ms  (3*3*100)
```

Retry count configured via `retry_count` parameter (default: 3, maximum: 10).

### Retryable Conditions

The following errors trigger automatic retry:

- Network errors (connection refused, timeout, DNS failures)
- 5xx server errors (500, 502, 503, 504)
- 429 Too Many Requests

### Non-Retryable Conditions

The following errors fail immediately without retry:

- 4xx client errors (400, 401, 403, 404, except 429)
- Invalid URL or configuration errors
- Request body marshaling errors
- Context cancellation

### Status Code Handling

| Code Range | Behavior | Description |
|------------|----------|-------------|
| 2xx | Success | Request accepted, no retry |
| 3xx | Redirect | Automatically followed (up to 10 redirects) |
| 4xx | Error | Client error, not retried (except 429) |
| 5xx | Retry | Server error, retried with backoff |

## Authentication Options

### Bearer Token Authentication

```yaml
headers:
  Authorization: Bearer ${API_TOKEN}  # Read from environment variable
```

### Basic Authentication

```yaml
headers:
  Authorization: Basic dXNlcjpwYXNzd29yZA==  # Base64 encoded user:password
```

### Custom API Key

```yaml
headers:
  X-API-Key: ${API_KEY}
  X-Custom-Auth: ${AUTH_SECRET}
```

### Mutual TLS (mTLS)

Platform-level TLS configuration enables mutual TLS authentication:

```yaml
security:
  tls:
    client:
      ca_files:
        - /etc/certs/ca.pem
      mtls:
        enabled: true
        cert_file: /etc/certs/client.pem
        key_file: /etc/certs/client-key.pem
```

The HTTP client automatically uses the configured TLS settings.

### ACME Certificate Management

Automatic certificate provisioning via Let's Encrypt or custom ACME CA:

```yaml
security:
  tls:
    client:
      mode: acme
      acme:
        enabled: true
        directory_url: https://acme-v02.api.letsencrypt.org/directory
        email: admin@example.com
        domains:
          - client.example.com
        challenge_type: http-01
        storage_path: /var/lib/acme/certs
```

Certificates are automatically renewed before expiration.

## Example Use Cases

### Webhook Integration

Send stream events to webhook.site for debugging:

```yaml
name: debug-webhook
type: output.httppost
config:
  ports:
    inputs:
      - name: debug_events
        type: nats
        subject: debug.>
        required: true
  url: https://webhook.site/unique-id-here
  timeout: 5
  retry_count: 1
```

### Third-Party API Integration

Post enriched events to external analytics platform:

```yaml
name: analytics-export
type: output.httppost
config:
  ports:
    inputs:
      - name: analytics_events
        type: jetstream
        subject: analytics.pageview
        stream_name: ANALYTICS
        required: true
  url: https://api.analytics.com/v1/events
  timeout: 10
  retry_count: 5
  headers:
    Authorization: Bearer ${ANALYTICS_API_KEY}
    X-Source: semstreams
```

### Metrics Ingestion

Send metrics to observability platform with high retry count for data reliability:

```yaml
name: metrics-ingest
type: output.httppost
config:
  ports:
    inputs:
      - name: metrics
        type: nats
        subject: metrics.>
        required: true
  url: https://metrics.platform.io/ingest
  timeout: 15
  retry_count: 10
  content_type: application/json
  headers:
    X-Tenant-ID: ${TENANT_ID}
    Authorization: Bearer ${METRICS_TOKEN}
```

### Multi-Endpoint Fanout

Use multiple httppost components to send the same data to different endpoints:

```yaml
# Component 1: Primary endpoint
name: primary-webhook
type: output.httppost
config:
  ports:
    inputs:
      - name: orders
        type: jetstream
        subject: orders.>
        stream_name: ORDERS
        required: true
  url: https://primary.api.com/orders
  retry_count: 5

# Component 2: Backup endpoint
name: backup-webhook
type: output.httppost
config:
  ports:
    inputs:
      - name: orders_backup
        type: jetstream
        subject: orders.>
        stream_name: ORDERS
        required: true
  url: https://backup.api.com/orders
  retry_count: 3
```

## Performance Characteristics

- **Throughput**: Network-dependent, typically 100-1000 requests/second
- **Memory**: O(concurrent requests), minimal overhead per message
- **Latency**: Network RTT + server processing time + retry delays
- **Connections**: HTTP keep-alive enabled, connections reused across requests
- **Concurrency**: Thread-safe, handles multiple concurrent messages

## Observability

### Health Status

```go
health := component.Health()
// Healthy: true if component is running
// ErrorCount: Total failed requests after all retries
// Uptime: Time since Start()
```

### Data Flow Metrics

```go
metrics := component.DataFlow()
// ErrorRate: Percentage of failed requests (errors / total)
// LastActivity: Timestamp of last message processed
```

## Security Considerations

- **HTTPS Required**: Always use HTTPS for sensitive data (authentication tokens, PII)
- **Environment Variables**: Store API keys and secrets in environment variables, never hardcode
- **Certificate Validation**: SSL certificate validation enabled by default (no InsecureSkipVerify)
- **Timeouts**: Always configure timeouts to prevent hanging requests and resource exhaustion
- **Header Sanitization**: Sensitive headers logged at debug level only

## Limitations

Current version does not support:

- Request batching (one HTTP request per message)
- Circuit breaker pattern
- Rate limiting
- Request signing (HMAC, AWS Signature v4)
- HTTP methods other than POST (add support via config if needed)
- Response body processing (responses are read and discarded)

## Thread Safety

The component is fully thread-safe:

- HTTP client can be used from multiple goroutines
- Start/Stop can be called from any goroutine
- Metrics updates use atomic operations
- Proper mutex protection for shared state

## Error Reporting

All errors use the `pkg/errs` package for consistent classification:

- **Invalid**: Configuration errors (bad URL, invalid timeout)
- **Transient**: Retryable errors (network failures, 5xx responses)
- **Fatal**: Unrecoverable errors (missing dependencies)

Errors are logged with structured context including URL, status code, and retry attempt number.

## Testing

Run unit tests:

```bash
go test ./output/httppost -v
```

Run integration tests (requires Docker):

```bash
go test ./output/httppost -tags=integration -v
```

## Related Components

- **output/file**: Write messages to disk
- **output/websocket**: Stream messages to WebSocket clients
- **gateway/http**: HTTP server for inbound requests
- **processor/jsonmap**: Transform messages before HTTP POST
