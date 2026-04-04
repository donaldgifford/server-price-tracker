# server-price-tracker

![Version: 0.1.18](https://img.shields.io/badge/Version-0.1.18-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 0.7.0](https://img.shields.io/badge/AppVersion-0.7.0-informational?style=flat-square)

eBay server hardware deal tracker with LLM extraction, scoring, and Discord alerts

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| affinity | object | `{}` |  |
| autoscaling.enabled | bool | `false` |  |
| autoscaling.maxReplicas | int | `100` |  |
| autoscaling.minReplicas | int | `1` |  |
| autoscaling.targetCPUUtilizationPercentage | int | `80` |  |
| cnpg | object | `{"bootstrap":{"database":"spt","owner":"spt"},"enabled":false,"imageName":"ghcr.io/cloudnative-pg/postgresql:17.2","instances":1,"managed":{"services":{"disabledDefaultServices":["ro","r"]}},"monitoring":{"customQueriesConfigMap":[],"enablePodMonitor":false},"pooler":{"enabled":false,"instances":1,"monitoring":{"enablePodMonitor":false},"pgbouncer":{"defaultPoolSize":25,"maxClientConnections":100,"parameters":{},"poolMode":"transaction"},"tcpRoute":{"annotations":{},"enabled":false,"parentRefs":[{"group":"gateway.networking.k8s.io","kind":"Gateway","name":"internal","namespace":"gateway"}]},"type":"rw"},"postgresql":{"parameters":{"max_connections":"200","shared_buffers":"256MB","timezone":"UTC"}},"resources":{"limits":{"cpu":"1","memory":"1Gi"},"requests":{"cpu":"100m","memory":"256Mi"}},"storage":{"size":"10Gi","storageClass":""}}` | CloudNativePG PostgreSQL cluster |
| cnpg.pooler | object | `{"enabled":false,"instances":1,"monitoring":{"enablePodMonitor":false},"pgbouncer":{"defaultPoolSize":25,"maxClientConnections":100,"parameters":{},"poolMode":"transaction"},"tcpRoute":{"annotations":{},"enabled":false,"parentRefs":[{"group":"gateway.networking.k8s.io","kind":"Gateway","name":"internal","namespace":"gateway"}]},"type":"rw"}` | CNPG Pooler (PgBouncer connection pooling) |
| cnpg.pooler.pgbouncer.parameters | object | `{}` | Extra pgbouncer.ini parameters (key-value pairs) |
| cnpg.pooler.pgbouncer.poolMode | string | `"transaction"` | PgBouncer pool mode: session, transaction, or statement |
| cnpg.pooler.tcpRoute | object | `{"annotations":{},"enabled":false,"parentRefs":[{"group":"gateway.networking.k8s.io","kind":"Gateway","name":"internal","namespace":"gateway"}]}` | Gateway API TCPRoute for external database access |
| cnpg.pooler.type | string | `"rw"` | Pooler type: rw (read-write primary) or ro (read-only replicas) |
| config | object | `{"database":{"host":"${DB_HOST}","name":"${DB_NAME}","password":"${DB_PASSWORD}","pool_size":10,"port":5432,"sslmode":"require","user":"${DB_USER}"},"ebay":{"app_id":"${EBAY_APP_ID}","browse_url":"${EBAY_BROWSE_URL}","cert_id":"${EBAY_CERT_ID}","marketplace":"EBAY_US","max_calls_per_cycle":50,"rate_limit":{"burst":10,"daily_limit":5000,"per_second":5},"token_url":"${EBAY_TOKEN_URL}"},"llm":{"anthropic":{"model":""},"backend":"ollama","concurrency":4,"ollama":{"endpoint":"http://ollama.ollama.svc:11434","model":"mistral:7b-instruct-v0.3-q5_K_M"},"openai_compat":{"endpoint":"","model":""},"timeout":"30s","use_grammar":true},"logging":{"format":"json","level":"info"},"notifications":{"discord":{"enabled":true,"webhook_url":"${DISCORD_WEBHOOK_URL}"}},"schedule":{"baseline_interval":"6h","ingestion_interval":"30m","re_extraction_interval":"","stagger_offset":"30s"},"scoring":{"baseline_window_days":90,"min_baseline_samples":10,"weights":{"condition":0.15,"price":0.4,"quality":0.1,"quantity":0.1,"seller":0.2,"time":0.05}},"server":{"host":"0.0.0.0","port":8080,"read_timeout":"30s","write_timeout":"30s"}}` | Application configuration (mirrors Go Config struct). Non-secret values are rendered as literals. Secret values use ${ENV_VAR} placeholders resolved at runtime by os.ExpandEnv(). |
| fullnameOverride | string | `""` |  |
| httpRoute.annotations | object | `{}` |  |
| httpRoute.enabled | bool | `false` |  |
| httpRoute.hostnames[0] | string | `"chart-example.local"` |  |
| httpRoute.parentRefs[0].group | string | `"gateway.networking.k8s.io"` |  |
| httpRoute.parentRefs[0].kind | string | `"Gateway"` |  |
| httpRoute.parentRefs[0].name | string | `"internal"` |  |
| httpRoute.parentRefs[0].namespace | string | `"gateway"` |  |
| httpRoute.rules[0].matches[0].path.type | string | `"PathPrefix"` |  |
| httpRoute.rules[0].matches[0].path.value | string | `"/"` |  |
| image.pullPolicy | string | `"IfNotPresent"` |  |
| image.repository | string | `"ghcr.io/donaldgifford/server-price-tracker"` |  |
| image.tag | string | `""` |  |
| imagePullSecrets | list | `[]` |  |
| ingress.annotations | object | `{}` |  |
| ingress.className | string | `""` |  |
| ingress.enabled | bool | `false` |  |
| ingress.hosts[0].host | string | `"chart-example.local"` |  |
| ingress.hosts[0].paths[0].path | string | `"/"` |  |
| ingress.hosts[0].paths[0].pathType | string | `"ImplementationSpecific"` |  |
| ingress.tls | list | `[]` |  |
| livenessProbe.httpGet.path | string | `"/healthz"` |  |
| livenessProbe.httpGet.port | string | `"http"` |  |
| livenessProbe.initialDelaySeconds | int | `5` |  |
| livenessProbe.periodSeconds | int | `15` |  |
| migration | object | `{"enabled":true,"resources":{"limits":{"cpu":"200m","memory":"128Mi"},"requests":{"cpu":"50m","memory":"64Mi"}}}` | Migration init container |
| nameOverride | string | `""` |  |
| nodeSelector | object | `{}` |  |
| ollama | object | `{"enabled":false,"gpu":{"count":1,"enabled":true},"image":{"pullPolicy":"IfNotPresent","repository":"ollama/ollama","tag":"latest"},"model":"mistral:7b-instruct-v0.3-q5_K_M","nodeSelector":{"nvidia.com/gpu.present":"true"},"persistence":{"enabled":true,"size":"30Gi","storageClass":""},"resources":{"limits":{"cpu":"4","memory":"8Gi"},"requests":{"cpu":"1","memory":"4Gi"}}}` | Ollama LLM backend (StatefulSet) |
| podAnnotations | object | `{}` |  |
| podLabels | object | `{}` |  |
| podSecurityContext | object | `{}` |  |
| readinessProbe.httpGet.path | string | `"/readyz"` |  |
| readinessProbe.httpGet.port | string | `"http"` |  |
| readinessProbe.initialDelaySeconds | int | `3` |  |
| readinessProbe.periodSeconds | int | `10` |  |
| replicaCount | int | `1` |  |
| resources | object | `{}` |  |
| secret | object | `{"create":true,"existingSecret":"","values":{"ANTHROPIC_API_KEY":"","DB_HOST":"localhost","DB_NAME":"spt","DB_PASSWORD":"","DB_USER":"spt","DISCORD_WEBHOOK_URL":"","EBAY_APP_ID":"","EBAY_BROWSE_URL":"","EBAY_CERT_ID":"","EBAY_TOKEN_URL":""}}` | Secret management. Set create=true to render a Secret from values below. Set create=false and existingSecret to reference a user-managed Secret. |
| securityContext | object | `{}` |  |
| service.port | int | `8080` |  |
| service.type | string | `"ClusterIP"` |  |
| serviceAccount.annotations | object | `{}` |  |
| serviceAccount.automount | bool | `true` |  |
| serviceAccount.create | bool | `true` |  |
| serviceAccount.name | string | `""` |  |
| serviceMonitor | object | `{"enabled":false,"interval":"30s","labels":{},"path":"/metrics"}` | Prometheus ServiceMonitor |
| tests | object | `{"connection":{"enabled":true}}` | Helm test hooks |
| tolerations | list | `[]` |  |
| volumeMounts | list | `[]` |  |
| volumes | list | `[]` | Extra volumes and volumeMounts for user additions |

----------------------------------------------
Autogenerated from chart metadata using [helm-docs v1.14.2](https://github.com/norwoodj/helm-docs/releases/v1.14.2)
