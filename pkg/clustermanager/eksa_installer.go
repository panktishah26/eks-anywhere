package clustermanager

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/go-logr/logr"
	"golang.org/x/exp/maps"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	anywherev1 "github.com/aws/eks-anywhere/pkg/api/v1alpha1"
	"github.com/aws/eks-anywhere/pkg/cluster"
	"github.com/aws/eks-anywhere/pkg/constants"
	"github.com/aws/eks-anywhere/pkg/features"
	"github.com/aws/eks-anywhere/pkg/types"
	"github.com/aws/eks-anywhere/pkg/yamlutil"
)

// EKSAInstaller allows to install eks-a components in a cluster.
type EKSAInstaller struct {
	client KubernetesClient
}

// NewEKSAInstaller constructs a new EKSAInstaller.
func NewEKSAInstaller(client KubernetesClient) *EKSAInstaller {
	return &EKSAInstaller{
		client: client,
	}
}

// Install configures and applies eks-a components in a cluster accordingly to a spec.
func (i *EKSAInstaller) Install(ctx context.Context, log logr.Logger, cluster *types.Cluster, spec *cluster.Spec) error {
	generator := EKSAComponentGenerator{log: log}
	components, err := generator.buildEKSAComponentsSpec(spec)
	if err != nil {
		return err
	}

	objs := make([]runtime.Object, 0, len(components.rest)+1)
	objs = append(objs, components.deployment)
	for _, o := range components.rest {
		objs = append(objs, o)
	}

	for _, o := range objs {
		if err = i.client.Apply(ctx, cluster.KubeconfigFile, o); err != nil {
			return fmt.Errorf("applying eksa components: %v", err)
		}
	}

	if err := i.client.WaitForDeployment(ctx, cluster, deploymentWaitStr, "Available", constants.EksaControllerManagerDeployment, constants.EksaSystemNamespace); err != nil {
		return fmt.Errorf("waiting for eksa-controller-manager: %v", err)
	}

	return nil
}

// Upgrade re-installs the eksa components in a cluster if the VersionBundle defined in the
// new spec has a different eks-a components version. Workload clusters are ignored.
func (i *EKSAInstaller) Upgrade(ctx context.Context, log logr.Logger, cluster *types.Cluster, currentSpec, newSpec *cluster.Spec) (*types.ChangeDiff, error) {
	log.V(1).Info("Checking for EKS-A components upgrade")
	if !newSpec.Cluster.IsSelfManaged() {
		log.V(1).Info("Skipping EKS-A components upgrade, not a self-managed cluster")
		return nil, nil
	}
	changeDiff := EksaChangeDiff(currentSpec, newSpec)
	if changeDiff == nil {
		log.V(1).Info("Nothing to upgrade for controller and CRDs")
		return nil, nil
	}
	log.V(1).Info("Starting EKS-A components upgrade")
	oldVersion := currentSpec.VersionsBundle.Eksa.Version
	newVersion := newSpec.VersionsBundle.Eksa.Version
	if err := i.Install(ctx, log, cluster, newSpec); err != nil {
		return nil, fmt.Errorf("upgrading EKS-A components from version %v to version %v: %v", oldVersion, newVersion, err)
	}

	return changeDiff, nil
}

// EKSAComponentGenerator generates and configures eks-a components.
type EKSAComponentGenerator struct {
	log logr.Logger
}

func (g *EKSAComponentGenerator) buildEKSAComponentsSpec(spec *cluster.Spec) (*eksaComponents, error) {
	components, err := g.parseEKSAComponentsSpec(spec)
	if err != nil {
		return nil, err
	}

	g.configureEKSAComponents(components, spec)

	return components, nil
}

func (g *EKSAComponentGenerator) configureEKSAComponents(c *eksaComponents, spec *cluster.Spec) {
	// TODO(g-gaston): we should do this with a custom ControllerManagerConfig.
	// This requires wider changes in the controller manager setup and config manifest,
	// so leaving this for later.
	setManagerFlags(c.deployment, spec)
	setManagerEnvVars(c.deployment, spec)
}

func setManagerFlags(d *appsv1.Deployment, spec *cluster.Spec) {
	gates := []string{}
	for _, g := range managerEnabledGates(spec) {
		gates = append(gates, fmt.Sprintf("%s=true", g))
	}

	args := d.Spec.Template.Spec.Containers[0].Args
	if len(gates) > 0 {
		args = append(args, fmt.Sprintf("--feature-gates=%s", strings.Join(gates, ",")))
	}

	d.Spec.Template.Spec.Containers[0].Args = args
}

func setManagerEnvVars(d *appsv1.Deployment, spec *cluster.Spec) {
	envVars := d.Spec.Template.Spec.Containers[0].Env
	proxy := spec.Cluster.ProxyConfiguration()
	if proxy != nil {
		proxyEnvVarNames := maps.Keys(proxy)
		sort.Strings(proxyEnvVarNames)
		for _, name := range proxyEnvVarNames {
			envVars = append(envVars, v1.EnvVar{Name: name, Value: proxy[name]})
		}
	}

	d.Spec.Template.Spec.Containers[0].Env = envVars
}

func managerEnabledGates(spec *cluster.Spec) []string {
	g := []string{}
	// TODO(g-gaston): inject a "features" checker instead of using global one
	if features.IsActive(features.FullLifecycleAPI()) || fullLifeCycleControllerForProvider(spec.Cluster) {
		g = append(g, features.FullLifecycleGate)
	}

	return g
}

func fullLifeCycleControllerForProvider(cluster *anywherev1.Cluster) bool {
	// TODO(g-gaston): inject a dependency where this check can be delegated
	// We can use some kind of configurator registering that allow to decouple this generator
	// from the logic that drives the gates.
	return cluster.Spec.DatacenterRef.Kind == anywherev1.VSphereDatacenterKind ||
		cluster.Spec.DatacenterRef.Kind == anywherev1.DockerDatacenterKind
}

func (g *EKSAComponentGenerator) parseEKSAComponentsSpec(spec *cluster.Spec) (*eksaComponents, error) {
	// TODO(g-gaston): this load operation shouldn't be done by the cluster spec, we should have a "reader" here
	componentsManifest, err := spec.LoadManifest(spec.VersionsBundle.Eksa.Components)
	if err != nil {
		return nil, fmt.Errorf("loading manifest for eksa components: %v", err)
	}

	p := yamlutil.NewParser(g.log)
	err = p.RegisterMappings(
		yamlutil.NewMapping(
			"Deployment", func() yamlutil.APIObject {
				return &appsv1.Deployment{}
			},
		),
	)
	if err != nil {
		return nil, fmt.Errorf("registering yaml mappings for eksa components: %v", err)
	}
	p.RegisterMappingForAnyKind(func() yamlutil.APIObject {
		return &unstructured.Unstructured{}
	})

	components := &eksaComponents{}

	if err = p.Parse(componentsManifest.Content, components); err != nil {
		return nil, fmt.Errorf("parsing eksa components: %v", err)
	}

	return components, nil
}

type eksaComponents struct {
	deployment *appsv1.Deployment
	rest       []*unstructured.Unstructured
}

func (c *eksaComponents) BuildFromParsed(lookup yamlutil.ObjectLookup) error {
	for _, obj := range lookup {
		if obj.GetObjectKind().GroupVersionKind().Kind == "Deployment" {
			c.deployment = obj.(*appsv1.Deployment)
		} else {
			c.rest = append(c.rest, obj.(*unstructured.Unstructured))
		}
	}
	return nil
}

// EksaChangeDiff computes the version diff in eksa components between two specs.
func EksaChangeDiff(currentSpec, newSpec *cluster.Spec) *types.ChangeDiff {
	if currentSpec.VersionsBundle.Eksa.Version != newSpec.VersionsBundle.Eksa.Version {
		return &types.ChangeDiff{
			ComponentReports: []types.ComponentChangeDiff{
				{
					ComponentName: "EKS-A",
					NewVersion:    newSpec.VersionsBundle.Eksa.Version,
					OldVersion:    currentSpec.VersionsBundle.Eksa.Version,
				},
			},
		}
	}
	return nil
}
