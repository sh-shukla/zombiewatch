package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CheckSpec configures one zombie detector.
type CheckSpec struct {
	// Enabled turns this check on.
	Enabled bool `json:"enabled"`

	// OlderThan is how long a resource must be stuck before it counts as a
	// zombie. Each check has a sensible default when omitted.
	// +optional
	OlderThan *metav1.Duration `json:"olderThan,omitempty"`
}

// ChecksSpec selects which zombie classes to hunt.
type ChecksSpec struct {
	// TerminatingPods finds pods stuck in Terminating past their grace period
	// — almost always a finalizer that will never clear.
	// +optional
	TerminatingPods *CheckSpec `json:"terminatingPods,omitempty"`

	// DeadNodes finds nodes that are NotReady long enough that their
	// workloads have moved on but the node object lingers.
	// +optional
	DeadNodes *CheckSpec `json:"deadNodes,omitempty"`

	// OrphanedPVs finds Released volumes (claim deleted, storage still
	// provisioned and billed) and long-unbound Available volumes.
	// +optional
	OrphanedPVs *CheckSpec `json:"orphanedPVs,omitempty"`

	// FailedPods finds Failed/Evicted pods piling up uncollected.
	// +optional
	FailedPods *CheckSpec `json:"failedPods,omitempty"`
}

// NotifySpec says where zombie reports go.
type NotifySpec struct {
	// WebhookURL receives reports as JSON POSTs.
	// +kubebuilder:validation:Pattern=`^https?://`
	WebhookURL string `json:"webhookURL"`

	// Format of the POST body: "generic" structured JSON, or "slack".
	// +kubebuilder:validation:Enum=generic;slack
	// +optional
	Format string `json:"format,omitempty"`

	// ResendAfter re-notifies for a zombie that is still present after this
	// long. Zero means notify once per zombie per controller lifetime.
	// +optional
	ResendAfter *metav1.Duration `json:"resendAfter,omitempty"`
}

// ZombieWatchSpec defines a scan-and-notify policy.
type ZombieWatchSpec struct {
	// Interval between scans. Defaults to 5m.
	// +optional
	Interval *metav1.Duration `json:"interval,omitempty"`

	// Namespaces limits namespaced checks to this list; empty means all.
	// +optional
	Namespaces []string `json:"namespaces,omitempty"`

	// Checks selects the zombie classes to hunt. An omitted check is off.
	// +optional
	Checks ChecksSpec `json:"checks,omitempty"`

	// Notify is where findings are delivered.
	Notify NotifySpec `json:"notify"`
}

// ZombieWatchStatus reports the last scan's outcome.
type ZombieWatchStatus struct {
	// +optional
	LastScanTime *metav1.Time `json:"lastScanTime,omitempty"`
	// +optional
	ZombieCount int `json:"zombieCount,omitempty"`
	// +optional
	LastNotifyTime *metav1.Time `json:"lastNotifyTime,omitempty"`
	// LastNotifyError is empty when the last delivery succeeded. Failures are
	// kept loud and visible here rather than only in logs.
	// +optional
	LastNotifyError string `json:"lastNotifyError,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,shortName=zw
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Zombies",type=integer,JSONPath=`.status.zombieCount`
// +kubebuilder:printcolumn:name="LastScan",type=date,JSONPath=`.status.lastScanTime`
// +kubebuilder:printcolumn:name="NotifyError",type=string,JSONPath=`.status.lastNotifyError`

// ZombieWatch is a cluster-scoped policy: hunt these zombie classes on this
// cadence, report them to this webhook.
type ZombieWatch struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ZombieWatchSpec   `json:"spec,omitempty"`
	Status ZombieWatchStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ZombieWatchList contains a list of ZombieWatch.
type ZombieWatchList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ZombieWatch `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ZombieWatch{}, &ZombieWatchList{})
}
