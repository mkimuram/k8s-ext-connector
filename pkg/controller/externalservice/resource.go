package externalservice

import (
	submarinerv1alpha1 "github.com/mkimuram/k8s-ext-connector/pkg/apis/submariner/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// genForwardPodSpec returns a spec for a forwarder pod
func genForwardPodSpec(cr *submarinerv1alpha1.ExternalService) *corev1.Pod {
	labels := map[string]string{
		ExternalServiceNamespaceLabel: cr.Namespace,
		ExternalServiceNameLabel:      cr.Name,
	}
	isPrivileged := true
	var defaultMode int32 = 256

	env := []corev1.EnvVar{
		{
			Name:  "FORWARDER_NAMESPACE",
			Value: ConnectorNamespace,
		},
		{
			Name:  "FORWARDER_NAME",
			Value: cr.Name,
		},
	}

	volumes := []corev1.Volume{
		{
			Name: "ssh-key-volume",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName:  "my-ssh-key",
					DefaultMode: &defaultMode,
				},
			},
		},
	}

	volumeMounts := []corev1.VolumeMount{
		{
			Name:      "ssh-key-volume",
			MountPath: "/etc/ssh-key",
			ReadOnly:  true,
		},
	}

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Name,
			Namespace: ConnectorNamespace,
			Labels:    labels,
		},
		Spec: corev1.PodSpec{
			// TODO: consider to restrict minimal access permissions
			ServiceAccountName: "k8s-ext-connector",
			Containers: []corev1.Container{
				{
					Name:            "forwarder",
					Image:           "docker.io/mkimuram/forwarder:v0.3.0",
					SecurityContext: &corev1.SecurityContext{Privileged: &isPrivileged},
					Env:             env,
					VolumeMounts:    volumeMounts,
				},
			},
			Volumes: volumes,
		},
	}
}

// genForwardServiceSpec returns a spec for a forwarder service
func genForwardServiceSpec(cr *submarinerv1alpha1.ExternalService) *corev1.Service {
	var ports []corev1.ServicePort

	labels := map[string]string{
		ExternalServiceNamespaceLabel: cr.Namespace,
		ExternalServiceNameLabel:      cr.Name,
	}

	for _, port := range cr.Spec.Ports {
		ports = append(ports, port)
	}

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cr.Name,
			Namespace: ConnectorNamespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Ports:    ports,
			Selector: labels,
		},
	}
}
