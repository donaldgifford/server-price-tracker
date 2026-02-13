# Post-MVP Plan

Improvements and features to add after the MVP is running end-to-end. These are
tracked here for future work and are not blocking the initial deployment.

---

## Observability: External Service Metrics

Add Prometheus metrics for external service connectivity so we can monitor
health via Prometheus scrapes and eventually alert from Grafana.

### Prometheus gauges for service status

- [ ] `spt_external_service_up{service="ebay"}` — 1 if last eBay API call
      succeeded, 0 if failed
- [ ] `spt_external_service_up{service="ollama"}` — 1 if last LLM call
      succeeded, 0 if failed
- [ ] `spt_external_service_up{service="discord"}` — 1 if last Discord webhook
      succeeded, 0 if failed
- [ ] `spt_external_service_up{service="postgres"}` — 1 if last DB ping
      succeeded, 0 if failed

### Prometheus counters for request outcomes

- [ ] `spt_ebay_requests_total{status="success|error"}` — eBay API call counts
- [ ] `spt_ebay_request_duration_seconds` — histogram of eBay API latency
- [ ] `spt_llm_requests_total{backend="ollama|anthropic|openai_compat",status="success|error"}`
      — LLM extraction call counts
- [ ] `spt_llm_request_duration_seconds{backend="..."}` — histogram of LLM
      latency
- [ ] `spt_discord_requests_total{status="success|error"}` — Discord webhook
      call counts
- [ ] `spt_discord_request_duration_seconds` — histogram of Discord latency

### Implementation approach

- [ ] Add metrics instrumentation to `internal/ebay/browse.go` (Search method)
- [ ] Add metrics instrumentation to `pkg/extract/` (backend Generate methods)
- [ ] Add metrics instrumentation to `internal/notify/discord.go` (SendAlert)
- [ ] Add a periodic health check goroutine that pings each service and updates
      the `_up` gauges (or update them lazily on each real request)
- [ ] Register all new metrics in `internal/metrics/metrics.go`

### Grafana / Loki (future)

- [ ] Deploy Grafana Alloy or Loki to pull structured logs from the service
- [ ] Create Grafana dashboards for:
  - External service status (up/down over time)
  - eBay API success rate and latency
  - LLM extraction success rate and latency
  - Ingestion pipeline throughput (listings/min)
  - Alert volume
- [ ] Set up Grafana alerting rules for:
  - External service down for > 5 minutes
  - eBay API error rate > 10%
  - LLM extraction error rate > 20%
  - Ingestion pipeline stalled (no new listings in > 2x ingestion interval)

---

## Additional Future Work

- [ ] **Retry logic** — Add configurable retry with backoff for eBay API,
      Ollama, and Discord webhook calls
- [ ] **Circuit breaker** — Wrap external service calls with a circuit breaker
      to avoid hammering failing services
- [ ] **Viper config** — Replace `os.ExpandEnv` + YAML with Viper for
      environment variable binding, config file watching, and CLI flag overrides
- [ ] **Web UI** — Simple dashboard showing watches, recent listings, scores,
      and alert history
- [ ] **Discord bot** — Interactive Discord bot for managing watches and
      querying listings
- [ ] **Multi-marketplace** — Support eBay UK, DE, AU marketplaces
- [ ] **Sold item tracking** — Poll completed/sold items to improve baseline
      accuracy
- [ ] **Model upgrades** — Evaluate larger LLM models for better extraction
      accuracy on cluster (mistral:7b, llama3.1:8b, etc.)
