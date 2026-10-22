{{- define "timone.fullname" -}}
{{- printf "%s-timone" .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- define "timone.labels" -}}
app.kubernetes.io/name: timone
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}
{{- define "timone.customDefinitions" -}}
{{- $names := list -}}
{{- range .Values.customResources -}}
{{- $names = append $names (printf "%s.%s" .resource .group) -}}
{{- end -}}
{{- join "," $names -}}
{{- end -}}

{{/* Keep Service/Deployment names stable; RBAC identities must distinguish installations. */}}
{{- define "timone.rbacName" -}}
{{- if .Values.rbac.legacyNames -}}
{{- include "timone.fullname" . -}}
{{- else -}}
{{- $identity := printf "%s-%s" .Release.Namespace .Release.Name -}}
{{- printf "%s-%s-timone" ($identity | trunc 24 | trimSuffix "-") ($identity | sha256sum | trunc 8) -}}
{{- end -}}
{{- end -}}
