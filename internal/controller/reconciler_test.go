package controller

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	opsv1alpha1 "github.com/sh-shukla/zombiewatch/api/v1alpha1"
	"github.com/sh-shukla/zombiewatch/internal/notify"
)

func TestReconcileNotifiesOnceThenResends(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := opsv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	watch := &opsv1alpha1.ZombieWatch{
		ObjectMeta: metav1.ObjectMeta{Name: "default"},
		Spec: opsv1alpha1.ZombieWatchSpec{
			Checks: opsv1alpha1.ChecksSpec{
				DeadNodes: &opsv1alpha1.CheckSpec{Enabled: true},
			},
			Notify: opsv1alpha1.NotifySpec{
				WebhookURL:  "http://example.invalid/hook",
				ResendAfter: &metav1.Duration{Duration: time.Hour},
			},
		},
	}
	start := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	deadNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-dead"},
		Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{{
			Type: corev1.NodeReady, Status: corev1.ConditionFalse,
			LastTransitionTime: metav1.Time{Time: start.Add(-2 * time.Hour)},
		}}},
	}

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(watch, deadNode).
		WithStatusSubresource(&opsv1alpha1.ZombieWatch{}).
		Build()

	clock := start
	var sent []notify.Payload
	r := &Reconciler{
		Client: c,
		Scheme: scheme,
		Now:    func() time.Time { return clock },
		Send: func(_ context.Context, url, _ string, p notify.Payload) error {
			if url != "http://example.invalid/hook" {
				t.Errorf("unexpected url %q", url)
			}
			sent = append(sent, p)
			return nil
		},
	}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: watch.Name}}

	// First scan: one zombie, one notification.
	if _, err := r.Reconcile(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 1 || len(sent[0].Zombies) != 1 {
		t.Fatalf("want 1 notification with 1 zombie, got %+v", sent)
	}

	// Second scan 10 minutes later: same zombie, within resend window — quiet.
	clock = start.Add(10 * time.Minute)
	if _, err := r.Reconcile(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 1 {
		t.Fatalf("zombie still within resend window, want no new notification, got %d", len(sent))
	}

	// Third scan past resendAfter: notified again.
	clock = start.Add(2 * time.Hour)
	if _, err := r.Reconcile(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 2 {
		t.Fatalf("resend window elapsed, want 2 notifications, got %d", len(sent))
	}

	// Status reflects the scan.
	var got opsv1alpha1.ZombieWatch
	if err := c.Get(context.Background(), req.NamespacedName, &got); err != nil {
		t.Fatal(err)
	}
	if got.Status.ZombieCount != 1 || got.Status.LastScanTime == nil || got.Status.LastNotifyError != "" {
		t.Fatalf("status not honest: %+v", got.Status)
	}
}
