package memberstatus

import (
	"context"
	"fmt"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	"github.com/codeready-toolchain/member-operator/pkg/configuration"
	"github.com/codeready-toolchain/toolchain-common/pkg/condition"

	"github.com/go-logr/logr"
	errs "github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var log = logf.Log.WithName("controller_memberstatus")

// Add creates a new MemberStatus Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager, _ *configuration.Config) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) *ReconcileMemberStatus {
	return &ReconcileMemberStatus{
		client: mgr.GetClient(),
		scheme: mgr.GetScheme(),
	}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r *ReconcileMemberStatus) error {
	// Create a new controller
	c, err := controller.New("memberstatus-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource MemberStatus
	err = c.Watch(&source.Kind{Type: &toolchainv1alpha1.MemberStatus{}}, &handler.EnqueueRequestForObject{}, predicate.GenerationChangedPredicate{})
	if err != nil {
		return err
	}

	return nil
}

// blank assignment to verify that ReconcileMemberStatus implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileMemberStatus{}

// ReconcileMemberStatus reconciles a MemberStatus object
type ReconcileMemberStatus struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	scheme *runtime.Scheme
}

// Reconcile reads the state of the cluster for a MemberStatus object and makes changes based on the state read
// and what is in the MemberStatus.Spec
func (r *ReconcileMemberStatus) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling MemberStatus")

	// Fetch the MemberStatus
	memberStatus := &toolchainv1alpha1.MemberStatus{}
	err := r.client.Get(context.TODO(), request.NamespacedName, memberStatus)
	if err != nil {
		if errors.IsNotFound(err) {
			// Create MemberStatus resource if it does not exist
			if err := CreateOrUpdateResources(r.client, r.scheme, request.NamespacedName.Namespace); err != nil {
				return reconcile.Result{}, r.wrapErrorWithStatusUpdate(reqLogger, memberStatus, r.setStatusFailed(toolchainv1alpha1.RegistrationServiceDeployingFailedReason), err, "cannot create MemberStatus resource")
			}
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	// Look up status of member deployment
	memberDeploymentName := types.NamespacedName{Namespace: request.Namespace, Name: "member-operator"}
	memberDeployment := &appsv1.Deployment{}
	err = r.client.Get(context.TODO(), memberDeploymentName, memberDeployment)
	if err != nil {
		// member deployment not found
		r.wrapErrorWithStatusUpdate(reqLogger, memberStatus, r.setStatusFailed(toolchainv1alpha1.RegistrationServiceDeployingFailedReason), err, "member deployment not found, this should never happen")
	} else {
		reqLogger.Info("Retrieved member deployment: " + memberDeployment.Name)
		for _, condition := range memberDeployment.Status.Conditions {
			reqLogger.Info(fmt.Sprintf("member deployment %s: %s", condition.Type, condition.Status))
		}
	}

	updateStatusConditions(r.client, memberStatus, toBeDeployed())

	reqLogger.Info("Finished updating the member status, requeueing...")
	return reconcile.Result{RequeueAfter: time.Second * 10}, nil
}

// updateStatusConditions updates Member status conditions with the new conditions
func updateStatusConditions(cl client.Client, memberStatus *toolchainv1alpha1.MemberStatus, newConditions ...toolchainv1alpha1.Condition) error {
	// the controller should always update at least the last updated timestamp of the status so the status should be updated regardless of whether
	// any specific fields were updated. This way the last updated timestamp can be used to indicate that the controller is not working correctly or is down
	memberStatus.Status.Conditions, _ = condition.AddOrUpdateStatusConditions(memberStatus.Status.Conditions, newConditions...)
	return cl.Status().Update(context.TODO(), memberStatus)
}

func (r *ReconcileMemberStatus) setStatusFailed(reason string) func(memberStatus *toolchainv1alpha1.MemberStatus, message string) error {
	return func(memberStatus *toolchainv1alpha1.MemberStatus, message string) error {
		return updateStatusConditions(
			r.client,
			memberStatus,
			toBeNotReady(reason, message))
	}
}

// wrapErrorWithStatusUpdate wraps the error and update the MemberStatus status. If the update fails then the error is logged.
func (r *ReconcileMemberStatus) wrapErrorWithStatusUpdate(logger logr.Logger, memberStatus *toolchainv1alpha1.MemberStatus,
	statusUpdater func(memberStatus *toolchainv1alpha1.MemberStatus, message string) error, err error, format string, args ...interface{}) error {
	if err == nil {
		return nil
	}
	if err := statusUpdater(memberStatus, err.Error()); err != nil {
		logger.Error(err, "Error updating MemberStatus status")
	}
	return errs.Wrapf(err, format, args...)
}

func toBeDeployed() toolchainv1alpha1.Condition {
	return toolchainv1alpha1.Condition{
		Type:            toolchainv1alpha1.ConditionReady,
		Status:          corev1.ConditionTrue,
		LastUpdatedTime: metav1.Time{Time: time.Now()},
	}
}

func toBeNotReady(reason, msg string) toolchainv1alpha1.Condition {
	return toolchainv1alpha1.Condition{
		Type:            toolchainv1alpha1.ConditionReady,
		Status:          corev1.ConditionFalse,
		Reason:          reason,
		Message:         msg,
		LastUpdatedTime: metav1.Time{Time: time.Now()},
	}
}
