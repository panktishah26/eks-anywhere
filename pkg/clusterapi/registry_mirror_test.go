package clusterapi_test

import (
	"testing"

	. "github.com/onsi/gomega"
	bootstrapv1 "sigs.k8s.io/cluster-api/bootstrap/kubeadm/api/v1beta1"

	"github.com/aws/eks-anywhere/pkg/api/v1alpha1"
	"github.com/aws/eks-anywhere/pkg/clusterapi"
)

var registryMirrorTests = []struct {
	name                 string
	registryMirrorConfig *v1alpha1.RegistryMirrorConfiguration
	wantFiles            []bootstrapv1.File
	wantRegistryConfig   bootstrapv1.RegistryMirrorConfiguration
}{
	{
		name:               "registry config nil",
		wantFiles:          []bootstrapv1.File{},
		wantRegistryConfig: bootstrapv1.RegistryMirrorConfiguration{},
	},
	{
		name: "with ca cert",
		registryMirrorConfig: &v1alpha1.RegistryMirrorConfiguration{
			Endpoint:      "1.2.3.4",
			Port:          "443",
			CACertContent: "xyz",
		},
		wantFiles: []bootstrapv1.File{
			{
				Path:  "/etc/containerd/config_append.toml",
				Owner: "root:root",
				Content: `[plugins."io.containerd.grpc.v1.cri".registry.mirrors]
  [plugins."io.containerd.grpc.v1.cri".registry.mirrors."public.ecr.aws"]
    endpoint = ["https://1.2.3.4:443"]
  [plugins."io.containerd.grpc.v1.cri".registry.configs."1.2.3.4:443".tls]
    ca_file = "/etc/containerd/certs.d/1.2.3.4:443/ca.crt"`,
			},
			{
				Path:    "/etc/containerd/certs.d/1.2.3.4:443/ca.crt",
				Owner:   "root:root",
				Content: "xyz",
			},
		},
		wantRegistryConfig: bootstrapv1.RegistryMirrorConfiguration{
			Endpoint: "1.2.3.4:443",
			CACert:   "xyz",
		},
	},
	{
		name: "with insecure skip",
		registryMirrorConfig: &v1alpha1.RegistryMirrorConfiguration{
			Endpoint:           "1.2.3.4",
			Port:               "443",
			InsecureSkipVerify: true,
		},
		wantFiles: []bootstrapv1.File{
			{
				Path:  "/etc/containerd/config_append.toml",
				Owner: "root:root",
				Content: `[plugins."io.containerd.grpc.v1.cri".registry.mirrors]
  [plugins."io.containerd.grpc.v1.cri".registry.mirrors."public.ecr.aws"]
    endpoint = ["https://1.2.3.4:443"]
  [plugins."io.containerd.grpc.v1.cri".registry.configs."1.2.3.4:443".tls]
    insecure_skip_verify = true`,
			},
		},
		wantRegistryConfig: bootstrapv1.RegistryMirrorConfiguration{
			Endpoint: "1.2.3.4:443",
		},
	},
	{
		name: "with ca cert and insecure skip",
		registryMirrorConfig: &v1alpha1.RegistryMirrorConfiguration{
			Endpoint:           "1.2.3.4",
			Port:               "443",
			CACertContent:      "xyz",
			InsecureSkipVerify: true,
		},
		wantFiles: []bootstrapv1.File{
			{
				Path:  "/etc/containerd/config_append.toml",
				Owner: "root:root",
				Content: `[plugins."io.containerd.grpc.v1.cri".registry.mirrors]
  [plugins."io.containerd.grpc.v1.cri".registry.mirrors."public.ecr.aws"]
    endpoint = ["https://1.2.3.4:443"]
  [plugins."io.containerd.grpc.v1.cri".registry.configs."1.2.3.4:443".tls]
    ca_file = "/etc/containerd/certs.d/1.2.3.4:443/ca.crt"
    insecure_skip_verify = true`,
			},
			{
				Path:    "/etc/containerd/certs.d/1.2.3.4:443/ca.crt",
				Owner:   "root:root",
				Content: "xyz",
			},
		},
		wantRegistryConfig: bootstrapv1.RegistryMirrorConfiguration{
			Endpoint: "1.2.3.4:443",
			CACert:   "xyz",
		},
	},
}

func TestSetRegistryMirrorInKubeadmControlPlaneBottleRocket(t *testing.T) {
	for _, tt := range registryMirrorTests {
		t.Run(tt.name, func(t *testing.T) {
			g := newApiBuilerTest(t)
			got := wantKubeadmControlPlane()
			clusterapi.SetRegistryMirrorInKubeadmControlPlaneForBottlerocket(got, tt.registryMirrorConfig)
			want := wantKubeadmControlPlane()
			want.Spec.KubeadmConfigSpec.ClusterConfiguration.RegistryMirror = tt.wantRegistryConfig
			want.Spec.KubeadmConfigSpec.JoinConfiguration.RegistryMirror = tt.wantRegistryConfig
			g.Expect(got).To(Equal(want))
		})
	}
}

func TestSetRegistryMirrorInKubeadmControlPlaneUbuntu(t *testing.T) {
	for _, tt := range registryMirrorTests {
		t.Run(tt.name, func(t *testing.T) {
			g := newApiBuilerTest(t)
			got := wantKubeadmControlPlane()
			g.Expect(clusterapi.SetRegistryMirrorInKubeadmControlPlaneForUbuntu(got, tt.registryMirrorConfig)).To(Succeed())
			want := wantKubeadmControlPlane()
			want.Spec.KubeadmConfigSpec.Files = tt.wantFiles
			g.Expect(got).To(Equal(want))
		})
	}
}

func TestSetRegistryMirrorInKubeadmConfigTemplateBottlerocket(t *testing.T) {
	for _, tt := range registryMirrorTests {
		t.Run(tt.name, func(t *testing.T) {
			g := newApiBuilerTest(t)
			got := wantKubeadmConfigTemplate()
			clusterapi.SetRegistryMirrorInKubeadmConfigTemplateForBottlerocket(got, tt.registryMirrorConfig)
			want := wantKubeadmConfigTemplate()
			want.Spec.Template.Spec.JoinConfiguration.RegistryMirror = tt.wantRegistryConfig
			g.Expect(got).To(Equal(want))
		})
	}
}

func TestSetRegistryMirrorInKubeadmConfigTemplateUbuntu(t *testing.T) {
	for _, tt := range registryMirrorTests {
		t.Run(tt.name, func(t *testing.T) {
			g := newApiBuilerTest(t)
			got := wantKubeadmConfigTemplate()
			g.Expect(clusterapi.SetRegistryMirrorInKubeadmConfigTemplateForUbuntu(got, tt.registryMirrorConfig)).To(Succeed())
			want := wantKubeadmConfigTemplate()
			want.Spec.Template.Spec.Files = tt.wantFiles
			g.Expect(got).To(Equal(want))
		})
	}
}
