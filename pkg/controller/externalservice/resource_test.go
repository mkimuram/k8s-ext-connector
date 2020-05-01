package externalservice

import (
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/mkimuram/k8s-ext-connector/pkg/apis/submariner/v1alpha1"
)

var (
	isPrivileged       = true
	defaultMode  int32 = 256
)

func TestGenForwardPodSpec(t *testing.T) {
	testCases := []struct {
		name     string
		es       *v1alpha1.ExternalService
		expected *corev1.Pod
	}{
		{
			name: "Normal case",
			es: &v1alpha1.ExternalService{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "ns1",
					Name:      "es1",
				},
			},
			expected: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "es1",
					Namespace: "external-services",
					Labels: map[string]string{
						ExternalServiceNamespaceLabel: "ns1",
						ExternalServiceNameLabel:      "es1",
					},
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: "k8s-ext-connector",
					Containers: []corev1.Container{
						{
							Name:            "forwarder",
							Image:           "forwarder:0.2",
							SecurityContext: &corev1.SecurityContext{Privileged: &isPrivileged},
							Env: []corev1.EnvVar{
								{
									Name:  "FORWARDER_NAMESPACE",
									Value: "external-services",
								},
								{
									Name:  "FORWARDER_NAME",
									Value: "es1",
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "ssh-key-volume",
									MountPath: "/etc/ssh-key",
									ReadOnly:  true,
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "ssh-key-volume",
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName:  "my-ssh-key",
									DefaultMode: &defaultMode,
								},
							},
						},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Logf("test case: %s", tc.name)
		spec := genForwardPodSpec(tc.es)

		if !reflect.DeepEqual(tc.expected, spec) {
			t.Errorf("expected:%v, but got:%v", tc.expected, spec)
		}
	}
}

func TestGenForwardServiceSpec(t *testing.T) {
	testCases := []struct {
		name     string
		es       *v1alpha1.ExternalService
		expected *corev1.Service
	}{
		{
			name: "Normal case",
			es: &v1alpha1.ExternalService{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "ns1",
					Name:      "es1",
				},
				Spec: v1alpha1.ExternalServiceSpec{
					TargetIP: "",
					Sources: []v1alpha1.Source{
						{
							Service: v1alpha1.ServiceRef{
								Name:      "svc1",
								Namespace: "es1",
							},
							SourceIP: "192.168.122.200",
						},
					},
					Ports: []corev1.ServicePort{
						{
							Protocol: "TCP",
							Port:     80,
						},
					},
				},
			},
			expected: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "es1",
					Namespace: "external-services",
					Labels: map[string]string{
						ExternalServiceNamespaceLabel: "ns1",
						ExternalServiceNameLabel:      "es1",
					},
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Protocol: "TCP",
							Port:     80,
						},
					},
					Selector: map[string]string{
						ExternalServiceNamespaceLabel: "ns1",
						ExternalServiceNameLabel:      "es1",
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Logf("test case: %s", tc.name)
		spec := genForwardServiceSpec(tc.es)

		if !reflect.DeepEqual(tc.expected, spec) {
			t.Errorf("expected:%v, but got:%v", tc.expected, spec)
		}
	}
}
