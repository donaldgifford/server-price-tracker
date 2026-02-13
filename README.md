# server price tracker

An API-first Go service that monitors eBay listings for server hardware,
extracts structured attributes via LLM (Ollama, Anthropic Claude, or compatible
backends), scores listings against historical baselines, and alerts on deals via
Discord webhooks. The CLI acts as a client to the API, and the API design
supports future integrations (Discord bot, web UI, Grafana dashboards).

