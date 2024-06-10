/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	tutorialv1 "demo/api/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// DemoReconciler reconciles a Demo object
type DemoReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

const (
	C_PORT      = 80
	CPU_REQUEST = "100m"
	CPU_LIMIT   = "100m"
	MEM_REQUEST = "512Mi"
	MEM_LIMIT   = "512Mi"
)

// +kubebuilder:rbac:groups=tutorial.demo.com,resources=demoes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=tutorial.demo.com,resources=demoes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=tutorial.demo.com,resources=demoes/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Demo object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.18.2/pkg/reconcile

/*
	1.获取自定义资源crd--namespcename获取
		1.1.自定义的资源是否处于删除状态
	2.是否有创建关联的deployment--》使用namespace和name寻找到特定的资源
		2.1 如果没有创建关联的deployment
			2.1.1 创建service
			2.1.2 创建deployment
			2.1.3 创建成功就更新状态
		2.2 有创建查看创建的副本书相同不相同
			2.2.1 相同，直接返回
			2.2.2 不相同更新deployment的副本数和demo.spec.replicas一样 --update下
		2.3 更新demo的status数据和deployment的数据一样



*/

func (r *DemoReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	myLog := log.FromContext(ctx)
	// TODO(user): your logic here
	//found crd
	var demo tutorialv1.Demo
	if err := r.Get(ctx, req.NamespacedName, &demo); err != nil {
		myLog.Error(err, "crd not found")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	//crd weather deleting
	if demo.DeletionTimestamp != nil {
		return ctrl.Result{}, nil
	}
	//find the created deployment
	var deployment appsv1.Deployment
	key := types.NamespacedName{Namespace: req.Namespace, Name: demo.Spec.SvcName}
	err := r.Get(ctx, key, &deployment)
	//has error and deployment exists
	if err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, err
	}
	//deployment not found -->create
	if err != nil && apierrors.IsNotFound(err) {
		//create service
		if err = r.createServiceIfNotExists(ctx, &demo); err != nil {
			myLog.Error(err, "create service has error")
			return ctrl.Result{}, err
		}
		//create deployment
		if err = r.createdeployment(ctx, &demo); err != nil {
			myLog.Error(err, "create deployment logic has error")
			return ctrl.Result{}, err
		}
		//update crd status
		if err = r.updateStatus(ctx, &demo); err != nil {
			myLog.Error(err, "update status has error")
			return ctrl.Result{}, err
		}

	}
	//replicas handler
	expectReplicas := demo.Spec.Replicas
	realPeplicas := deployment.Spec.Replicas
	if expectReplicas == realPeplicas {
		return ctrl.Result{}, nil
	}
	myLog.Info("update the replicas make the status equal the expect")
	deployment.Spec.Replicas = expectReplicas
	if err = r.Update(ctx, &deployment); err != nil {
		if apierrors.IsConflict(err) {
			err := r.Get(ctx, key, &deployment)
			if err == nil {
				deployment.Spec.Replicas = expectReplicas
				err = r.Update(ctx, &deployment)
			}
		}
		if err != nil {
			myLog.Error(err, "update deployment replicas has error")
			return ctrl.Result{}, err
		}
	}
	//if update deployment success ,update the demo status
	if err = r.updateStatus(ctx, &demo); err != nil {
		return ctrl.Result{}, err
	}
	myLog.Info("demo resource reconciled")

	return ctrl.Result{}, nil
}

func (r *DemoReconciler) createServiceIfNotExists(ctx context.Context, instance *tutorialv1.Demo) error {
	myLog := log.FromContext(ctx)
	service := &corev1.Service{}

	key := types.NamespacedName{Namespace: instance.Namespace, Name: instance.Spec.SvcName}
	err := r.Get(ctx, key, service)
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}
	service = &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: instance.Namespace,
			Name:      instance.Spec.SvcName,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{{
				Name: "http",
				Port: C_PORT,
			}},
			Selector: map[string]string{
				"app": instance.Spec.SvcName,
			},
			Type: corev1.ServiceTypeNodePort,
		},
	}
	//when the crd disappear the service also
	myLog.Info("set reference")
	if err = controllerutil.SetControllerReference(instance, service, r.Scheme); err != nil {
		return err
	}
	//create service
	myLog.Info("create service")
	if err = r.Create(ctx, service); err != nil {
		myLog.Error(err, "create service has error")
		return err
	}
	myLog.Info("create service success")
	return nil
}

func (r *DemoReconciler) createdeployment(ctx context.Context, instance *tutorialv1.Demo) error {
	//mylog
	myLog := log.FromContext(ctx)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: instance.Namespace,
			Name:      instance.Spec.SvcName,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: instance.Spec.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": instance.Spec.SvcName,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": instance.Spec.SvcName,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: instance.Spec.SvcName,
							// 用指定的镜像
							Image:           instance.Spec.Image,
							ImagePullPolicy: "IfNotPresent",
							Ports: []corev1.ContainerPort{
								{
									Name:          "http",
									Protocol:      corev1.ProtocolTCP,
									ContainerPort: C_PORT,
								},
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									"cpu":    resource.MustParse(CPU_REQUEST),
									"memory": resource.MustParse(MEM_REQUEST),
								},
								Limits: corev1.ResourceList{
									"cpu":    resource.MustParse(CPU_LIMIT),
									"memory": resource.MustParse(MEM_LIMIT),
								},
							},
						},
					},
				},
			},
		},
	}
	//if crd delete deployment also
	myLog.Info("set reference")
	if err := controllerutil.SetControllerReference(instance, deployment, r.Scheme); err != nil {
		myLog.Error(err, "deployment reference err")
		return err
	}
	//create deployment
	if err := r.Create(ctx, deployment); err != nil {
		myLog.Error(err, "create deployment has error")
		return err
	}
	return nil
}

func (r DemoReconciler) updateStatus(ctx context.Context, instace *tutorialv1.Demo) error {
	myLog := log.FromContext(ctx)
	//instance‘s status replicas should be equal to the instance.spec.replicas
	instace.Status.Replicas = instace.Spec.Replicas
	if err := r.Update(ctx, instace); err != nil {
		myLog.Error(err, "update resource status has error")
		return err
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *DemoReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&tutorialv1.Demo{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Complete(r)
}
