# zombiewatch

[![ci](https://github.com/sh-shukla/zombiewatch/actions/workflows/ci.yml/badge.svg)](https://github.com/sh-shukla/zombiewatch/actions/workflows/ci.yml)

Every cluster accumulates the undead: pods stuck in `Terminating` behind a finalizer that will never clear, nodes that died but whose objects linger, `Released` PersistentVolumes whose storage is still provisioned **and still billed**, evicted pods nobody garbage-collected. None of them page anyone — they just sit there costing money and hiding real signal.

**zombiewatch is a Kubernetes operator with one job: find the undead and tell your webhook.** It never deletes anything — it makes the invisible visible and leaves the kill decision to a human.

## The CRD

```yaml
apiVersion: zombiewatch.sh-shukla.dev/v1alpha1
kind: ZombieWatch
metadata:
  name: default
spec:
  interval: 5m
  checks:
    terminatingPods: { enabled: true, olderThan: 15m }
    deadNodes:       { enabled: true, olderThan: 10m }
    orphanedPVs:     { enabled: true, olderThan: 1h }
    failedPods:      { enabled: true, olderThan: 24h }
  notify:
    webhookURL: https://hooks.slack.com/services/XXX/YYY/ZZZ
    format: slack        # or "generic" structured JSON
    resendAfter: 6h      # re-ping while a zombie stays undead
```

```
$ kubectl get zw
NAME      ZOMBIES   LASTSCAN   NOTIFYERROR
default   3         12s
```

Slack gets:

> 🧟 **zombiewatch** [default]: 3 zombie(s) found
> • `terminating-pods` Pod prod/worker-x2j — stuck Terminating for 42m (finalizers: example.com/lock)
> • `dead-nodes` Node node-7 — NotReady for 3h — workloads have moved on, the node object lingers
> • `orphaned-pvs` PersistentVolume pvc-9f2… — Released — claim deleted, storage still provisioned (reclaimPolicy: Retain)

## What counts as a zombie

| Check | Detects | Why it matters |
|---|---|---|
| `terminatingPods` | `deletionTimestamp` older than threshold | a finalizer is wedged; deploys and namespace deletions hang behind it |
| `deadNodes` | `Ready != True` longer than threshold | dead capacity skews scheduling and autoscaler math |
| `orphanedPVs` | `Released` PVs, plus `Available` ones unclaimed past threshold | storage billed with no consumer — the silent line item |
| `failedPods` | `Failed`/`Evicted` pods older than threshold | noise that buries real failures in every `kubectl get pods` |

## Install

```bash
kubectl apply -f config/crd/ -f config/deploy/
kubectl apply -f config/samples/zombiewatch.yaml   # edit the webhook URL first
```

Or run it against your current kubeconfig without deploying anything:

```bash
go run . --metrics-bind-address=0 --health-probe-bind-address=0
```

## Design

- **Detect, don't delete.** Deleting someone's stuck pod is an incident waiting to happen; a webhook ping is not. (A guarded, opt-in reap mode is on the roadmap.)
- **Notifications dedupe.** A zombie is reported once, then again only every `resendAfter` — your channel gets signal, not a siren.
- **Failures stay loud.** A failed scan or webhook delivery lands in `.status.lastNotifyError`, the CR's events, and the exit of the loop — never swallowed.
- **Honest approximations.** Kubernetes records no phase-transition time for PVs, so `Released` volumes are reported immediately and `Available` ones by age. The README says so because the code can't know better.
- Unit-tested against fake clients — scan logic, webhook formats, and the resend window are all proven in CI without a cluster.

## License

[MIT](LICENSE)
