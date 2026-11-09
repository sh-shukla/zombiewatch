// Package scan finds zombie resources: objects the cluster is still paying
// for that will never come back to life on their own. Read-only by design.
package scan

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Config holds resolved thresholds. A zero duration disables that check.
type Config struct {
	Namespaces           []string
	TerminatingPodsAfter time.Duration
	DeadNodesAfter       time.Duration
	OrphanedPVsMinAge    time.Duration
	FailedPodsAfter      time.Duration
}

// Zombie is one undead resource and why it counts as one.
type Zombie struct {
	Check  string    `json:"check"`
	Kind   string    `json:"kind"`
	Object string    `json:"object"`
	Since  time.Time `json:"since"`
	Detail string    `json:"detail"`
}

// Key identifies a zombie across scans, for notification dedup.
func (z Zombie) Key() string { return z.Check + "/" + z.Kind + "/" + z.Object }

// Run executes every enabled check. Errors are returned, never swallowed:
// a scan that half-ran must not look like a clean cluster.
func Run(ctx context.Context, c client.Client, cfg Config, now time.Time) ([]Zombie, error) {
	var zombies []Zombie

	if cfg.TerminatingPodsAfter > 0 || cfg.FailedPodsAfter > 0 {
		pods, err := listPods(ctx, c, cfg.Namespaces)
		if err != nil {
			return nil, fmt.Errorf("listing pods: %w", err)
		}
		zombies = append(zombies, podZombies(pods, cfg, now)...)
	}

	if cfg.DeadNodesAfter > 0 {
		var nodes corev1.NodeList
		if err := c.List(ctx, &nodes); err != nil {
			return nil, fmt.Errorf("listing nodes: %w", err)
		}
		zombies = append(zombies, nodeZombies(nodes.Items, cfg, now)...)
	}

	if cfg.OrphanedPVsMinAge > 0 {
		var pvs corev1.PersistentVolumeList
		if err := c.List(ctx, &pvs); err != nil {
			return nil, fmt.Errorf("listing persistentvolumes: %w", err)
		}
		zombies = append(zombies, pvZombies(pvs.Items, cfg, now)...)
	}

	return zombies, nil
}

func listPods(ctx context.Context, c client.Client, namespaces []string) ([]corev1.Pod, error) {
	if len(namespaces) == 0 {
		var all corev1.PodList
		if err := c.List(ctx, &all); err != nil {
			return nil, err
		}
		return all.Items, nil
	}
	var pods []corev1.Pod
	for _, ns := range namespaces {
		var list corev1.PodList
		if err := c.List(ctx, &list, client.InNamespace(ns)); err != nil {
			return nil, err
		}
		pods = append(pods, list.Items...)
	}
	return pods, nil
}

func podZombies(pods []corev1.Pod, cfg Config, now time.Time) []Zombie {
	var out []Zombie
	for i := range pods {
		p := &pods[i]
		name := p.Namespace + "/" + p.Name

		// Stuck Terminating: deletion was requested long ago and the object
		// is still here — almost always a finalizer that will never clear.
		if cfg.TerminatingPodsAfter > 0 && p.DeletionTimestamp != nil {
			stuck := now.Sub(p.DeletionTimestamp.Time)
			if stuck > cfg.TerminatingPodsAfter {
				detail := fmt.Sprintf("stuck Terminating for %s", round(stuck))
				if len(p.Finalizers) > 0 {
					detail += " (finalizers: " + strings.Join(p.Finalizers, ", ") + ")"
				}
				out = append(out, Zombie{"terminating-pods", "Pod", name, p.DeletionTimestamp.Time, detail})
			}
			continue // a deleting pod is not also a failed-pod finding
		}

		// Failed/Evicted pods nobody garbage-collected.
		if cfg.FailedPodsAfter > 0 && p.Status.Phase == corev1.PodFailed {
			since := p.CreationTimestamp.Time
			if p.Status.StartTime != nil {
				since = p.Status.StartTime.Time
			}
			if age := now.Sub(since); age > cfg.FailedPodsAfter {
				reason := p.Status.Reason
				if reason == "" {
					reason = "Failed"
				}
				out = append(out, Zombie{"failed-pods", "Pod", name, since,
					fmt.Sprintf("%s for %s, never cleaned up", reason, round(age))})
			}
		}
	}
	return out
}

func nodeZombies(nodes []corev1.Node, cfg Config, now time.Time) []Zombie {
	var out []Zombie
	for i := range nodes {
		n := &nodes[i]
		for _, cond := range n.Status.Conditions {
			if cond.Type != corev1.NodeReady {
				continue
			}
			if cond.Status == corev1.ConditionTrue {
				break
			}
			if dead := now.Sub(cond.LastTransitionTime.Time); dead > cfg.DeadNodesAfter {
				out = append(out, Zombie{"dead-nodes", "Node", n.Name, cond.LastTransitionTime.Time,
					fmt.Sprintf("NotReady for %s — workloads have moved on, the node object lingers", round(dead))})
			}
			break
		}
	}
	return out
}

func pvZombies(pvs []corev1.PersistentVolume, cfg Config, now time.Time) []Zombie {
	var out []Zombie
	for i := range pvs {
		pv := &pvs[i]
		switch pv.Status.Phase {
		case corev1.VolumeReleased:
			// Kubernetes records no phase-transition time for PVs, so every
			// Released volume is reported: its claim is gone, the storage
			// is still provisioned and billed.
			out = append(out, Zombie{"orphaned-pvs", "PersistentVolume", pv.Name, pv.CreationTimestamp.Time,
				fmt.Sprintf("Released — claim deleted, storage still provisioned (reclaimPolicy: %s)", pv.Spec.PersistentVolumeReclaimPolicy)})
		case corev1.VolumeAvailable:
			if age := now.Sub(pv.CreationTimestamp.Time); age > cfg.OrphanedPVsMinAge {
				out = append(out, Zombie{"orphaned-pvs", "PersistentVolume", pv.Name, pv.CreationTimestamp.Time,
					fmt.Sprintf("Available but unclaimed for %s", round(age))})
			}
		}
	}
	return out
}

func round(d time.Duration) time.Duration { return d.Round(time.Minute) }
