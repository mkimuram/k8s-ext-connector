package util

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	utilerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GetOrCreateConfigMap returns configmap with specified name and namespace,
// by getting configmap. If configMap doesn't exists it create a new configMap and return it.
func GetOrCreateConfigMap(cl client.Client, name string, namespace string) (*corev1.ConfigMap, error) {
	configMap := &corev1.ConfigMap{}
	err := cl.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: namespace}, configMap)
	if err != nil {
		// No configMap is defined yet,so create a new configMap
		if utilerrors.IsNotFound(err) {
			return createConfigMap(cl, name, namespace)
		}
		return configMap, fmt.Errorf("getOrCreateConfigMap fails in getting configmap other than not found: %v", err)
	}
	return configMap, nil
}

func createConfigMap(cl client.Client, name string, namespace string) (*corev1.ConfigMap, error) {
	configMap := &corev1.ConfigMap{}
	configMap.ObjectMeta.Name = name
	configMap.ObjectMeta.Namespace = namespace
	configMap.Data = map[string]string{}

	err := cl.Create(context.TODO(), configMap)
	if err != nil {
		return configMap, fmt.Errorf("createConfigMap: Fails in creating a new configmap: %v", err)
	}
	return configMap, nil
}

// UpdateConfigmapData updates configMap with data
func UpdateConfigmapData(cl client.Client, configMap *corev1.ConfigMap, data map[string]string) error {
	configMap.Data = data
	if err := cl.Update(context.TODO(), configMap); err != nil {
		return fmt.Errorf("updateConfigMapData fails in updating configmap: %v", err)
	}

	return nil
}
