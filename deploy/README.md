# Conductor deployment artifacts (Profile C)

Sample artifacts for the SPEC §3.2 cloud profile. These are starting points,
not production-hardened automation.

## Contents

- [`../Dockerfile`](../Dockerfile) — multi-stage build producing a minimal
  static single-binary image (build flags mirror the `Makefile` `build`
  target). Build with:

  ```sh
  docker build -t conductor:latest .
  ```

- [`helm/conductor/`](helm/conductor) — minimal Helm chart (Deployment +
  Service + ConfigMap, optional PVC). Install with:

  ```sh
  helm install conductor deploy/helm/conductor \
    --set image.repository=conductor --set image.tag=latest \
    --set envFromSecret=conductor-secrets
  ```

- [`k8s/conductor.yaml`](k8s/conductor.yaml) — equivalent plain manifest for
  operators not using Helm:

  ```sh
  kubectl apply -f deploy/k8s/conductor.yaml
  ```

## Secrets

Never inline tracker or provider API keys in the harness config or chart
values. Provide them through a Kubernetes `Secret` and reference them as `$VAR`
in `HARNESS.md`; Conductor expands them at config load (SPEC §6.2) and the
audit writer redacts their resolved values (SPEC §21.1).
