package memberstatus

import (
	"context"
	"fmt"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	"github.com/codeready-toolchain/member-operator/pkg/configuration"
	"github.com/codeready-toolchain/member-operator/version"
	"github.com/codeready-toolchain/toolchain-common/pkg/cluster"

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
	"sigs.k8s.io/kubefed/pkg/controller/util"
)

var log = logf.Log.WithName("controller_memberstatus")

const memberOperatorDeploymentName = "member-operator"

type statusComponent string

// status components
const (
	memberOperator statusComponent = "memberOperator"
	hostConnection statusComponent = "hostConnection"
)

// Add creates a new MemberStatus Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager, _ *configuration.Config) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) *ReconcileMemberStatus {
	return &ReconcileMemberStatus{
		client:         mgr.GetClient(),
		scheme:         mgr.GetScheme(),
		getHostCluster: cluster.GetHostCluster,
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
	client         client.Client
	scheme         *runtime.Scheme
	getHostCluster func() (*cluster.FedCluster, bool)
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
				// return reconcile.Result{}, r.wrapErrorWithStatusUpdate(reqLogger, memberStatus, r.setStatusFailed(toolchainv1alpha1.MemberStatusHostConnectionFailedReason), err, "cannot create MemberStatus resource")
				reqLogger.Error(err, "MemberStatus resource is not found and could not be created")
			}
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	err = r.aggregateAndUpdateStatus(reqLogger, memberStatus)
	if err != nil {
		reqLogger.Error(err, "Failed to update status")
	}

	reqLogger.Info("Finished updating the member status, requeueing...")
	return reconcile.Result{RequeueAfter: time.Second * 10}, nil
}

type statusHandler struct {
	Name         statusComponent
	handleStatus func(logger logr.Logger, memberStatus *toolchainv1alpha1.MemberStatus) error
}

func (r *ReconcileMemberStatus) aggregateAndUpdateStatus(reqLogger logr.Logger, memberStatus *toolchainv1alpha1.MemberStatus) error {

	memberOperatorStatusHandler := statusHandler{Name: memberOperator, handleStatus: r.memberOperatorHandleStatus}
	hostConnectionStatusHandler := statusHandler{Name: hostConnection, handleStatus: r.hostConnectionHandleStatus}

	statusHandlers := []statusHandler{memberOperatorStatusHandler, hostConnectionStatusHandler}
	reqLogger.Info("Retrieving individual component statuses")

	// Set member operator status
	unreadyComponents := []string{}

	for _, handler := range statusHandlers {
		err := handler.handleStatus(reqLogger, memberStatus)
		if err != nil {
			reqLogger.Error(err, "status update problem")
			unreadyComponents = append(unreadyComponents, string(handler.Name))
		}
	}
	if len(unreadyComponents) > 0 {
		// r.wrapErrorWithStatusUpdate(logger, memberStatus, setStatusNotReady, )
		// componentsStr := strings.Join(components, ",")
		err := fmt.Errorf("Components not ready: %v", unreadyComponents)
		return r.setStatusNotReady(memberStatus, err.Error())
	}
	return r.setStatusReady(memberStatus)
}

func (r *ReconcileMemberStatus) hostConnectionHandleStatus(reqLogger logr.Logger, memberStatus *toolchainv1alpha1.MemberStatus) error {
	// Look up host connection status
	fedCluster, ok := r.getHostCluster()
	if !ok {
		return fmt.Errorf("no host connection was found")
	}
	memberStatus.Status.HostConnection = *fedCluster.ClusterStatus.DeepCopy()

	// Check conditions of host connection
	if !util.IsClusterReady(fedCluster.ClusterStatus) {
		return fmt.Errorf("the host connection is not ready")
	}

	return nil
}

func (r *ReconcileMemberStatus) memberOperatorHandleStatus(reqLogger logr.Logger, memberStatus *toolchainv1alpha1.MemberStatus) error {
	operatorStatus := toolchainv1alpha1.MemberOperatorStatus{
		Version:        version.Version,
		Revision:       version.Commit,
		BuildTimestamp: version.BuildTime,
	}

	// Look up status of member deployment
	memberDeploymentName := types.NamespacedName{Namespace: memberStatus.Namespace, Name: memberOperatorDeploymentName}
	memberDeployment := &appsv1.Deployment{}
	err := r.client.Get(context.TODO(), memberDeploymentName, memberDeployment)
	if err != nil {
		return fmt.Errorf("the member operator deployment not found")
	}

	// Get and check conditions of member deployment
	conditionsReady := true
	deploymentConditions := []appsv1.DeploymentCondition{}
	for _, condition := range memberDeployment.Status.Conditions {
		deploymentConditions = append(deploymentConditions, condition)
		conditionsReady = conditionsReady && condition.Status == corev1.ConditionTrue
	}

	// Update member status
	operatorStatus.Deployment.Name = memberDeployment.Name
	operatorStatus.Deployment.DeploymentConditions = deploymentConditions
	memberStatus.Status.MemberOperator = operatorStatus

	if !conditionsReady {
		return fmt.Errorf("the member operator deployment not ready")
	}

	return nil
}

// updateStatusConditions updates Member status conditions with the new conditions
func (r *ReconcileMemberStatus) updateStatusConditions(memberStatus *toolchainv1alpha1.MemberStatus, newConditions ...toolchainv1alpha1.Condition) error {
	// the controller should always update at least the last updated timestamp of the status so the status should be updated regardless of whether
	// any specific fields were updated. This way a problem with the controller can be indicated if the last updated timestamp was not updated.
	fmt.Printf("Member status conditions: %v\n", memberStatus.Status.Conditions)
	conditionsWithTimestamps := []toolchainv1alpha1.Condition{}
	for _, condition := range memberStatus.Status.Conditions {
		fmt.Printf("LastTransitionTime: %v\n", condition.LastTransitionTime)
		fmt.Printf("LastUpdatedTime: %v\n", condition.LastUpdatedTime)
	}
	for _, condition := range newConditions {
		fmt.Printf("Current time: %v\n", metav1.Now())
		condition.LastTransitionTime = metav1.Now()
		condition.LastUpdatedTime = metav1.Now()
		conditionsWithTimestamps = append(conditionsWithTimestamps, condition)
	}
	fmt.Printf("New conditions: %v\n", newConditions)
	memberStatus.Status.Conditions = conditionsWithTimestamps
	for _, condition := range memberStatus.Status.Conditions {
		fmt.Printf("LastTransitionTime: %v\n", condition.LastTransitionTime)
		fmt.Printf("LastUpdatedTime: %v\n", condition.LastUpdatedTime)
	}
	fmt.Printf("Member status conditions: %v\n", memberStatus.Status.Conditions)
	fmt.Printf("Member status: %v\n", memberStatus.Status)
	return r.client.Status().Update(context.TODO(), memberStatus)
}

func (r *ReconcileMemberStatus) setStatusReady(memberStatus *toolchainv1alpha1.MemberStatus) error {
	fmt.Printf("setStatusReady\n")
	return r.updateStatusConditions(
		memberStatus,
		toolchainv1alpha1.Condition{
			Type:   toolchainv1alpha1.ConditionReady,
			Status: corev1.ConditionTrue,
			Reason: toolchainv1alpha1.MemberStatusAllComponentsReady,
		})
}

func (r *ReconcileMemberStatus) setStatusNotReady(memberStatus *toolchainv1alpha1.MemberStatus, message string) error {
	fmt.Printf("setStatusNotReady\n")
	return r.updateStatusConditions(
		memberStatus,
		toolchainv1alpha1.Condition{
			Type:    toolchainv1alpha1.ConditionReady,
			Status:  corev1.ConditionFalse,
			Reason:  toolchainv1alpha1.MemberStatusComponentsNotReady,
			Message: message,
		})
}

// func (r *ReconcileMemberStatus) setStatusFailed(reason string) func(memberStatus *toolchainv1alpha1.MemberStatus, message string) error {
// 	return func(memberStatus *toolchainv1alpha1.MemberStatus, message string) error {
// 		return r.updateStatusConditions(
// 			memberStatus,
// 			toBeNotReady(reason, message))
// 	}
// }

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

// func toBeDeployed() toolchainv1alpha1.Condition {
// 	return toolchainv1alpha1.Condition{
// 		Type:               toolchainv1alpha1.ConditionReady,
// 		Status:             corev1.ConditionTrue,
// 		LastUpdatedTime:    metav1.Time{Time: time.Now()},
// 		LastTransitionTime: metav1.Time{Time: time.Now()},
// 	}
// }

// func toBeNotReady(reason, msg string) toolchainv1alpha1.Condition {
// 	return toolchainv1alpha1.Condition{
// 		Type:            toolchainv1alpha1.ConditionReady,
// 		Status:          corev1.ConditionFalse,
// 		Reason:          reason,
// 		Message:         msg,
// 		LastUpdatedTime: metav1.Time{Time: time.Now()},
// 	}
// }
