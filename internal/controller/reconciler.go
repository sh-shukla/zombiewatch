// Package controller reconciles ZombieWatch policies: scan on a cadence,
// notify the webhook about new zombies, keep status honest.
package controller

import (
	"context"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	opsv1alpha1 "github.com/sh-shukla/zombiewatch/api/v1alpha1"
	"github.com/sh-shukla/zombiewatch/internal/notify"
	"github.com/sh-shukla/zombiewatch/internal/scan"
)

// Defaults applied when the spec leaves a knob unset.
const (
	defaultInterval        = 5 * time.Minute
	defaultTerminatingPods = 15 * time.Minute
	defaultDeadNodes       = 10 * time.Minute
	defaultOrphanedPVs     = time.Hour
	defaultFailedPods      = 24 * time.Hour
)

// Reconciler runs the scan-notify loop for each ZombieWatch.
type Reconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder

	// Now and Send are seams for tests; nil means real clock / real webhook.
	Now  func() time.Time
	Send func(ctx context.Context, url, format string, p notify.Payload) error

	mu       sync.Mutex
	notified map[string]map[string]time.Time // watch name -> zombie key -> last notified
}

// +kubebuilder:rbac:groups=zombiewatch.sh-shukla.dev,resources=zombiewatches,verbs=get;list;watch
// +kubebuilder:rbac:groups=zombiewatch.sh-shukla.dev,resources=zombiewatches/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=pods;nodes;persistentvolumes,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	now := time.Now
	if r.Now != nil {
		now = r.Now
	}
	send := notify.Send
	if r.Send != nil {
		send = r.Send
	}

	var watch opsv1alpha1.ZombieWatch
	if err := r.Get(ctx, req.NamespacedName, &watch); err != nil {
		r.forget(req.Name)
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	cfg := resolve(watch.Spec)
	interval := defaultInterval
	if watch.Spec.Interval != nil {
		interval = watch.Spec.Interval.Duration
	}

	zombies, err := scan.Run(ctx, r.Client, cfg, now())
	if err != nil {
		// Fail loud: a half-run scan must not pass for a clean cluster.
		logger.Error(err, "scan failed")
		r.event(&watch, "Warning", "ScanFailed", err.Error())
		return ctrl.Result{}, err
	}

	watch.Status.LastScanTime = &metav1.Time{Time: now()}
	watch.Status.ZombieCount = len(zombies)

	toSend := r.pick(watch.Name, zombies, resendAfter(watch.Spec), now())
	if len(toSend) > 0 {
		payload := notify.Payload{Source: "zombiewatch", Watch: watch.Name, ScanTime: now(), Zombies: toSend}
		if err := send(ctx, watch.Spec.Notify.WebhookURL, watch.Spec.Notify.Format, payload); err != nil {
			watch.Status.LastNotifyError = err.Error()
			r.event(&watch, "Warning", "NotifyFailed", err.Error())
			r.unmark(watch.Name, toSend) // retry these on the next scan
		} else {
			watch.Status.LastNotifyError = ""
			watch.Status.LastNotifyTime = &metav1.Time{Time: now()}
			r.event(&watch, "Normal", "Notified", "reported zombies to webhook")
		}
	}

	if err := r.Status().Update(ctx, &watch); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: interval}, nil
}

// pick returns the zombies due for notification and marks them sent, while
// dropping tracking for zombies that have finally died properly.
func (r *Reconciler) pick(watchName string, zombies []scan.Zombie, resend time.Duration, now time.Time) []scan.Zombie {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.notified == nil {
		r.notified = map[string]map[string]time.Time{}
	}
	seen := r.notified[watchName]
	if seen == nil {
		seen = map[string]time.Time{}
		r.notified[watchName] = seen
	}

	current := map[string]bool{}
	var due []scan.Zombie
	for _, z := range zombies {
		current[z.Key()] = true
		last, ok := seen[z.Key()]
		if !ok || (resend > 0 && now.Sub(last) > resend) {
			due = append(due, z)
			seen[z.Key()] = now
		}
	}
	for key := range seen {
		if !current[key] {
			delete(seen, key)
		}
	}
	return due
}

func (r *Reconciler) unmark(watchName string, zombies []scan.Zombie) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, z := range zombies {
		delete(r.notified[watchName], z.Key())
	}
}

func (r *Reconciler) forget(watchName string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.notified, watchName)
}

func (r *Reconciler) event(watch *opsv1alpha1.ZombieWatch, kind, reason, msg string) {
	if r.Recorder != nil {
		r.Recorder.Event(watch, kind, reason, msg)
	}
}

func resolve(spec opsv1alpha1.ZombieWatchSpec) scan.Config {
	cfg := scan.Config{Namespaces: spec.Namespaces}
	cfg.TerminatingPodsAfter = threshold(spec.Checks.TerminatingPods, defaultTerminatingPods)
	cfg.DeadNodesAfter = threshold(spec.Checks.DeadNodes, defaultDeadNodes)
	cfg.OrphanedPVsMinAge = threshold(spec.Checks.OrphanedPVs, defaultOrphanedPVs)
	cfg.FailedPodsAfter = threshold(spec.Checks.FailedPods, defaultFailedPods)
	return cfg
}

func threshold(c *opsv1alpha1.CheckSpec, def time.Duration) time.Duration {
	if c == nil || !c.Enabled {
		return 0
	}
	if c.OlderThan != nil {
		return c.OlderThan.Duration
	}
	return def
}

func resendAfter(spec opsv1alpha1.ZombieWatchSpec) time.Duration {
	if spec.Notify.ResendAfter != nil {
		return spec.Notify.ResendAfter.Duration
	}
	return 0
}

// SetupWithManager wires the reconciler into the manager.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&opsv1alpha1.ZombieWatch{}).
		Complete(r)
}
