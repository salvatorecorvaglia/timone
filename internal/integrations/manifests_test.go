package integrations

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestHelmManifestProjectionProtectsPayloadsAndCustomKindCollisions(t *testing.T) {
	manifest := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: app
  annotations:
    private: HIDDEN-ANNOTATION
spec:
  replicas: 3
  template:
    spec:
      containers:
      - name: app
        image: example.invalid/app:v2
        command: [HIDDEN-COMMAND]
        args: [HIDDEN-ARGUMENT]
        env:
        - name: API_KEY
          value: HIDDEN-ENV
        livenessProbe:
          httpGet:
            httpHeaders:
            - name: Authorization
              value: HIDDEN-HEADER
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: settings
data:
  private: HIDDEN-CONFIG
binaryData:
  private: HIDDEN-BINARY
---
apiVersion: v1
kind: Secret
metadata:
  name: credentials
stringData:
  password: HIDDEN-SECRET
---
apiVersion: private.example/v1
kind: Deployment
metadata:
  name: custom
spec:
  arbitrary: HIDDEN-CUSTOM
`
	out, err := helmConfiguration(context.Background(), manifest)
	if err != nil || len(out) != 4 || !out[0].Supported || out[3].Supported {
		t.Fatal(out, err)
	}
	b, _ := json.Marshal(out)
	if strings.Contains(string(b), "HIDDEN-") {
		t.Fatal("payload leaked", string(b))
	}
	if !strings.Contains(string(b), "example.invalid/app:v2") || !strings.Contains(string(b), `"replicas":3`) {
		t.Fatal("safe fields missing", string(b))
	}
	if _, ok := out[3].Object["spec"]; ok {
		t.Fatal("custom Kind received built-in spec access")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := helmConfiguration(ctx, manifest); err == nil {
		t.Fatal("cancellation ignored")
	}
}
