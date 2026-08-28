{{/*
Expand the name of the chart.
*/}}
{{- define "binflow.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this
(by the DNS naming spec).
*/}}
{{- define "binflow.fullname" -}}
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
{{- define "binflow.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "binflow.labels" -}}
helm.sh/chart: {{ include "binflow.chart" . }}
{{ include "binflow.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "binflow.selectorLabels" -}}
app.kubernetes.io/name: {{ include "binflow.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the ServiceAccount to use
*/}}
{{- define "binflow.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "binflow.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Image tag to use. Defaults to Chart appVersion + distroless variant.
*/}}
{{- define "binflow.imageTag" -}}
{{- if .Values.image.tag }}
{{- .Values.image.tag }}
{{- else }}
{{- printf "%s-distroless" .Chart.AppVersion }}
{{- end }}
{{- end }}

{{/*
PVC name
*/}}
{{- define "binflow.pvcName" -}}
{{- if .Values.persistence.existingClaim }}
{{- .Values.persistence.existingClaim }}
{{- else }}
{{- include "binflow.fullname" . }}
{{- end }}
{{- end }}

{{/*
binstore validation guard (M11 T-306 / ADR-0036, T-325).

Render-time refusal for value combinations the server would reject at boot
anyway (with line-pointed errors there, but an operator should learn about
them in helm land). Invoked at the top of BOTH deployment.yaml and
configmap.yaml — Helm's template render order is not guaranteed, so the
guard must fire before any template-local `required` can mislead with a
less precise message.

Checks:
  - config.binstore.enabled + config.s3.enabled    → Q5 semantic-divergence
    (the two spellings of one storage chain must not coexist);
  - providers chain shape                          → legal chains are
    [filestore], [s3], [filestore, s3] (filestore first on the dual chain);
  - migration.mode required on the dual chain, illegal on any other.
*/}}
{{- define "binflow.binstoreValidate" -}}
{{- if .Values.config.binstore.enabled }}
{{- if .Values.config.s3.enabled }}
{{- fail "config.binstore.enabled and config.s3.enabled are mutually exclusive — binstore.yaml owns the storage chain when enabled; move the S3 parameters to config.binstore.s3 and set config.s3.enabled=false (rendering both is a Q5 semantic-divergence boot refusal)" }}
{{- end }}
{{- $declared := join "," .Values.config.binstore.providers }}
{{- if and (ne $declared "filestore") (ne $declared "s3") (ne $declared "filestore,s3") }}
{{- fail (printf "config.binstore.providers [%s] is not a legal chain — legal chains are [filestore], [s3] and [filestore, s3] (the dual migration chain, filestore first)" $declared) }}
{{- end }}
{{- $dual := eq $declared "filestore,s3" }}
{{- if and $dual (not .Values.config.binstore.migration.mode) }}
{{- fail "config.binstore.migration.mode is required on the [filestore, s3] chain (bypass | dual-write | completed)" }}
{{- end }}
{{- if and (not $dual) .Values.config.binstore.migration.mode }}
{{- fail (printf "config.binstore.migration.mode %q is only legal on the [filestore, s3] chain — this chain is [%s]" .Values.config.binstore.migration.mode $declared) }}
{{- end }}
{{- end }}
{{- end }}