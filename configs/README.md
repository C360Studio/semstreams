# SemStreams Service Configurations

This directory contains configuration files for optional external services used by SemStreams.

## Directory Structure

```
configs/
├── prometheus/
│   └── prometheus.yml          # Prometheus scraping configuration
├── grafana/
│   ├── provisioning/
│   │   ├── datasources/
│   │   │   └── prometheus.yml  # Auto-configure Prometheus datasource
│   │   └── dashboards/
│   │       └── default.yml     # Dashboard provider configuration
│   └── dashboards/
│       ├── semstreams-overview.json      # Overview dashboard
│       ├── cache-performance.json        # Cache metrics
│       ├── indexmanager-metrics.json     # IndexManager metrics
│       └── graph-processor.json          # Graph processor metrics
└── README.md
```

## Usage

Start observability stack:
```bash
task services:start:observability
```

Access:
- **Prometheus**: http://localhost:9090
- **Grafana**: http://localhost:3000 (admin/admin)

See `docs/OPTIONAL_SERVICES.md` for complete documentation.
