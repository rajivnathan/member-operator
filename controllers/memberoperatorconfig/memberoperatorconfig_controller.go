package memberoperatorconfig

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	errs "github.com/pkg/errors"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/member-operator/pkg/autoscaler"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

type PostUpdateAction struct {
	name   string
	action func(logr.Logger) error
}

var actions []PostUpdateAction

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("memberoperatorconfig-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource MemberOperatorConfig
	if err = c.Watch(
		&source.Kind{Type: &toolchainv1alpha1.MemberOperatorConfig{}},
		&handler.EnqueueRequestForObject{}, &predicate.GenerationChangedPredicate{},
	); err != nil {
		return err
	}

	// Watch for changes to secrets that should trigger an update of the config cache
	// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;watch;list
	return c.Watch(
		&source.Kind{Type: &corev1.Secret{}},
		&handler.EnqueueRequestsFromMapFunc{
			ToRequests: SecretToMemberOperatorConfigMapper{client: mgr.GetClient()},
		},
		&predicate.GenerationChangedPredicate{},
	)
}

// SetupWithManager sets up the controller with the Manager.
func (r *Reconciler) SetupWithManager(mgr manager.Manager) error {
	return add(mgr, r)
}

// Reconciler reconciles a MemberOperatorConfig object
type Reconciler struct {
	Client client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=toolchain.dev.openshift.com,resources=memberoperatorconfigs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=toolchain.dev.openshift.com,resources=memberoperatorconfigs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=toolchain.dev.openshift.com,resources=memberoperatorconfigs/finalizers,verbs=update

// Reconcile reads that state of the cluster for a MemberOperatorConfig object and makes changes based on the state read
// and what is in the MemberOperatorConfig.Spec
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *Reconciler) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := r.Log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling MemberOperatorConfig")

	// Fetch the MemberOperatorConfig instance
	memberconfig := &toolchainv1alpha1.MemberOperatorConfig{}
	err := r.Client.Get(context.TODO(), request.NamespacedName, memberconfig)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			reqLogger.Error(err, "it looks like the MemberOperatorConfig resource with the name 'config' was removed - the cache will use the latest version of the resource")
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	allSecrets, err := loadSecrets(r.Client)
	if err != nil {
		return reconcile.Result{}, err
	}

	updateConfig(memberconfig, allSecrets)

	return reconcile.Result{}, r.autoscalerDeploy(request.Namespace)
}

func (r *Reconciler) autoscalerDeploy(namespace string) error {
	crtConfig, err := GetConfig(r.Client, namespace)
	if err != nil {
		return fmt.Errorf("unable to auto deploy or delete autoscaler")
	}
	if crtConfig.Autoscaler().Deploy() {
		r.Log.Info("(Re)Deploying autoscaling buffer")
		if err := autoscaler.Deploy(r.Client, r.Scheme, namespace, crtConfig.Autoscaler().BufferMemory(), crtConfig.Autoscaler().BufferReplicas()); err != nil {
			return errs.Wrap(err, "cannot deploy autoscaling buffer")
		}
		r.Log.Info("(Re)Deployed autoscaling buffer")
	} else {
		deleted, err := autoscaler.Delete(r.Client, r.Scheme, namespace)
		if err != nil {
			return errs.Wrap(err, "cannot delete previously deployed autoscaling buffer")
		}
		if deleted {
			r.Log.Info("Deleted previously deployed autoscaling buffer")
		} else {
			r.Log.Info("Skipping deployment of autoscaling buffer")
		}
	}
	return nil
}
