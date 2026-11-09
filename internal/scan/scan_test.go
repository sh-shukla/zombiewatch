package scan

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var now = time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)

func ago(d time.Duration) metav1.Time { return metav1.Time{Time: now.Add(-d)} }

func fullConfig() Config {
	return Config{
		TerminatingPodsAfter: 15 * time.Minute,
		DeadNodesAfter:       10 * time.Minute,
		OrphanedPVsMinAge:    time.Hour,
		FailedPodsAfter:      24 * time.Hour,
	}
}

func keys(zs []Zombie) map[string]bool {
	m := map[string]bool{}
	for _, z := range zs {
		m[z.Key()] = true
	}
	return m
}

func TestRunFindsAllZombieClasses(t *testing.T) {
	deleted := ago(30 * time.Minute)
	stuckPod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name: "stuck", Namespace: "prod",
		DeletionTimestamp: &deleted,
		Finalizers:        []string{"example.com/never-clears"},
	}}
	evicted := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "evicted", Namespace: "prod", CreationTimestamp: ago(48 * time.Hour)},
		Status:     corev1.PodStatus{Phase: corev1.PodFailed, Reason: "Evicted", StartTime: ptr(ago(48 * time.Hour))},
	}
	freshFailed := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "fresh-fail", Namespace: "prod", CreationTimestamp: ago(time.Hour)},
		Status:     corev1.PodStatus{Phase: corev1.PodFailed, StartTime: ptr(ago(time.Hour))},
	}
	deadNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-dead"},
		Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
			{Type: corev1.NodeReady, Status: corev1.ConditionUnknown, LastTransitionTime: ago(2 * time.Hour)},
		}},
	}
	blinkingNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-blink"},
		Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
			{Type: corev1.NodeReady, Status: corev1.ConditionFalse, LastTransitionTime: ago(2 * time.Minute)},
		}},
	}
	releasedPV := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{Name: "pv-released", CreationTimestamp: ago(72 * time.Hour)},
		Spec:       corev1.PersistentVolumeSpec{PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimRetain},
		Status:     corev1.PersistentVolumeStatus{Phase: corev1.VolumeReleased},
	}
	boundPV := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{Name: "pv-bound", CreationTimestamp: ago(72 * time.Hour)},
		Status:     corev1.PersistentVolumeStatus{Phase: corev1.VolumeBound},
	}

	c := fake.NewClientBuilder().
		WithObjects(stuckPod, evicted, freshFailed, deadNode, blinkingNode, releasedPV, boundPV).
		Build()

	zombies, err := Run(context.Background(), c, fullConfig(), now)
	if err != nil {
		t.Fatal(err)
	}
	got := keys(zombies)
	for _, want := range []string{
		"terminating-pods/Pod/prod/stuck",
		"failed-pods/Pod/prod/evicted",
		"dead-nodes/Node/node-dead",
		"orphaned-pvs/PersistentVolume/pv-released",
	} {
		if !got[want] {
			t.Errorf("missing expected zombie %q in %v", want, got)
		}
	}
	if len(zombies) != 4 {
		t.Errorf("want exactly 4 zombies (fresh failures and healthy objects excluded), got %d: %v", len(zombies), got)
	}
}

func TestNamespaceFilter(t *testing.T) {
	deleted := ago(time.Hour)
	mk := func(ns string) *corev1.Pod {
		return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
			Name: "stuck", Namespace: ns,
			DeletionTimestamp: &deleted,
			Finalizers:        []string{"x/y"},
		}}
	}
	c := fake.NewClientBuilder().WithObjects(mk("prod"), mk("dev")).Build()

	cfg := fullConfig()
	cfg.Namespaces = []string{"prod"}
	zombies, err := Run(context.Background(), c, cfg, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(zombies) != 1 || zombies[0].Object != "prod/stuck" {
		t.Fatalf("want only prod/stuck, got %+v", zombies)
	}
}

func TestDisabledChecksFindNothing(t *testing.T) {
	deadNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-dead"},
		Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
			{Type: corev1.NodeReady, Status: corev1.ConditionFalse, LastTransitionTime: ago(2 * time.Hour)},
		}},
	}
	c := fake.NewClientBuilder().WithObjects(deadNode).Build()
	zombies, err := Run(context.Background(), c, Config{}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(zombies) != 0 {
		t.Fatalf("all checks disabled, want none, got %+v", zombies)
	}
}

func ptr(t metav1.Time) *metav1.Time { return &t }
