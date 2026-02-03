/*
Copyright 2026 The KServe Authors.

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

package localmodel

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kserve/kserve/pkg/apis/serving/v1alpha1"
)

// LocalModelCacheDeploymentReconciler reconciles a LocalModelCacheDeployment object
type LocalModelCacheDeploymentReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=serving.kserve.io,resources=localmodelcachedeployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=serving.kserve.io,resources=localmodelcachedeployments/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=serving.kserve.io,resources=localmodelcaches,verbs=get;list;watch;create;update;patch;delete

func (r *LocalModelCacheDeploymentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("localmodel", req.Name)

	// Fetch the LocalModelCacheDeployment
	localModel := &v1alpha1.LocalModelCacheDeployment{}
	if err := r.Get(ctx, req.NamespacedName, localModel); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Calculate revision number from generation
	revision := localModel.Generation

	// Check if LocalModelCache for this revision exists
	cacheName := fmt.Sprintf("%s-v%d", localModel.Name, revision)
	existingCache := &v1alpha1.LocalModelCache{}
	err := r.Get(ctx, client.ObjectKey{Name: cacheName}, existingCache)

	if errors.IsNotFound(err) {
		// Create new LocalModelCache
		cache := &v1alpha1.LocalModelCache{
			ObjectMeta: metav1.ObjectMeta{
				Name: cacheName,
				Labels: map[string]string{
					"serving.kserve.io/localmodelcachedeployment": localModel.Name,
					"serving.kserve.io/revision":                  fmt.Sprintf("%d", revision),
				},
				OwnerReferences: []metav1.OwnerReference{
					*metav1.NewControllerRef(localModel, v1alpha1.SchemeGroupVersion.WithKind("LocalModelCacheDeployment")),
				},
			},
			Spec: v1alpha1.LocalModelCacheSpec{
				SourceModelUri: localModel.Spec.SourceModelUri,
				ModelSize:      localModel.Spec.ModelSize,
				NodeGroups:     localModel.Spec.NodeGroups,
			},
		}

		if err := r.Create(ctx, cache); err != nil {
			log.Error(err, "Failed to create LocalModelCache", "name", cacheName)
			return ctrl.Result{}, err
		}
		log.Info("Created LocalModelCache", "name", cacheName)
		existingCache = cache
	} else if err != nil {
		return ctrl.Result{}, err
	}

	// Update status
	localModel.Status.CurrentRevision = cacheName
	localModel.Status.ObservedGeneration = localModel.Generation

	// Update revision list
	r.updateRevisionList(ctx, localModel)

	if err := r.Status().Update(ctx, localModel); err != nil {
		log.Error(err, "Failed to update LocalModelCacheDeployment status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *LocalModelCacheDeploymentReconciler) updateRevisionList(ctx context.Context, localModel *v1alpha1.LocalModelCacheDeployment) {
	// List all LocalModelCaches owned by this LocalModelCacheDeployment
	cacheList := &v1alpha1.LocalModelCacheList{}
	if err := r.List(ctx, cacheList, client.MatchingLabels{
		"serving.kserve.io/localmodelcachedeployment": localModel.Name,
	}); err != nil {
		return
	}

	revisions := []v1alpha1.LocalModelCacheDeploymentRevision{}
	for _, cache := range cacheList.Items {
		var revNum int32
		if revLabel, ok := cache.Labels["serving.kserve.io/revision"]; ok {
			fmt.Sscanf(revLabel, "%d", &revNum)
		}
		rev := v1alpha1.LocalModelCacheDeploymentRevision{
			Name:     cache.Name,
			Revision: revNum,
		}
		revisions = append(revisions, rev)
	}
	localModel.Status.Revisions = revisions
}

func (r *LocalModelCacheDeploymentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.LocalModelCacheDeployment{}).
		Owns(&v1alpha1.LocalModelCache{}).
		Complete(r)
}
