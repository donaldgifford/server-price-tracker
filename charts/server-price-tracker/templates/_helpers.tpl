{{/*
Expand the name of the chart.
*/}}
{{- define "server-price-tracker.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "server-price-tracker.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "server-price-tracker.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "server-price-tracker.labels" -}}
helm.sh/chart: {{ include "server-price-tracker.chart" . }}
{{ include "server-price-tracker.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "server-price-tracker.selectorLabels" -}}
app.kubernetes.io/name: {{ include "server-price-tracker.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "server-price-tracker.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "server-price-tracker.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
ConfigMap name
*/}}
{{- define "server-price-tracker.configMapName" -}}
{{- printf "%s-config" (include "server-price-tracker.fullname" .) }}
{{- end }}

{{/*
Secret name — use existingSecret if set, otherwise generate from fullname
*/}}
{{- define "server-price-tracker.secretName" -}}
{{- if and (not .Values.secret.create) .Values.secret.existingSecret }}
{{- .Values.secret.existingSecret }}
{{- else }}
{{- printf "%s-secrets" (include "server-price-tracker.fullname" .) }}
{{- end }}
{{- end }}

{{/*
Database host — when CNPG is enabled, use the CNPG -rw service; otherwise use config value
*/}}
{{- define "server-price-tracker.databaseHost" -}}
{{- if .Values.cnpg.enabled }}
{{- printf "${DB_HOST}" }}
{{- else }}
{{- .Values.config.database.host }}
{{- end }}
{{- end }}

{{/*
CNPG cluster name
*/}}
{{- define "server-price-tracker.cnpgClusterName" -}}
{{- printf "%s-db" (include "server-price-tracker.fullname" .) }}
{{- end }}

{{/*
CNPG auto-generated app secret name (<cluster>-app)
*/}}
{{- define "server-price-tracker.cnpgSecretName" -}}
{{- printf "%s-app" (include "server-price-tracker.cnpgClusterName" .) }}
{{- end }}

{{/*
Ollama resource name
*/}}
{{- define "server-price-tracker.ollamaName" -}}
{{- printf "%s-ollama" (include "server-price-tracker.fullname" .) }}
{{- end }}

{{/*
Ollama endpoint — when Ollama is deployed by this chart, use the in-cluster service
*/}}
{{- define "server-price-tracker.ollamaEndpoint" -}}
{{- if .Values.ollama.enabled }}
{{- printf "http://%s:11434" (include "server-price-tracker.ollamaName" .) }}
{{- else }}
{{- .Values.config.llm.ollama.endpoint }}
{{- end }}
{{- end }}
