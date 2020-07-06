/*
Copyright 2020 The KubeOne Authors.

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

package state

import (
	"sync"

	"github.com/Masterminds/semver"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/kubermatic/kubeone/pkg/apis/kubeone"
)

type Cluster struct {
	ControlPlane    []Host
	Workers         []Host
	ExpectedVersion *semver.Version
	Lock            sync.Mutex
}

type Host struct {
	// TODO: Consider renaming Config.Config as it's repetitive
	Config *kubeone.HostConfig

	ContainerRuntime ComponentStatus
	Kubelet          ComponentStatus

	// Applicable only for CP nodes
	APIServer ContainerStatus

	IsInCluster bool
	Kubeconfig  []byte
}

type ComponentStatus struct {
	Version *semver.Version
	Status  uint64
}

type ContainerStatus struct {
	Status uint64
}

const (
	SystemDStatusUnknown    = 1 << iota // systemd unit unknown
	SystemdDStatusDead                  // systemd unit dead
	SystemDStatusRestarting             // systemd unit restarting
	ComponentInstalled                  // installed (package, or direct download)
	SystemDStatusActive                 // systemd unit is activated
	SystemDStatusRunning                // systemd unit is running
	KubeletInitialized                  // kubelet config found (means node is initialized)
	PodRunning                          // pod is running
)

/*
	Cluster level checks
*/

// IsProvisioned returns is the target cluster provisioned.
// The cluster is consider provisioned if there is at least one initialized host
func (c *Cluster) IsProvisioned() bool {
	for i := range c.ControlPlane {
		if c.ControlPlane[i].Initialized() {
			return true
		}
	}

	return false
}

// IsDegraded checks is there a non-healthy host in a cluster
func (c *Cluster) IsDegraded() bool {
	for i := range c.ControlPlane {
		if !c.ControlPlane[i].ControlPlaneHealthy() {
			return true
		}
	}
	return false
}

// IsBroken checks is there a broken node in a cluster.
// If there's a broken node, IsDegraded will also return true, but
// there is manual intervention required (i.e. remove the instance)
func (c *Cluster) IsBroken() bool {
	for i := range c.ControlPlane {
		if c.ControlPlane[i].IsInCluster && !c.ControlPlane[i].APIServer.Healthy() {
			return true
		}
	}
	return false
}

// Healthy checks the cluster overall healthiness
func (c *Cluster) Healthy() bool {
	if !c.QuorumSatisfied() {
		return false
	}

	for i := range c.ControlPlane {
		if !c.ControlPlane[i].ControlPlaneHealthy() {
			return false
		}
	}

	for i := range c.Workers {
		if !c.Workers[i].WorkerHealthy() {
			return false
		}
	}

	return true
}

// QuorumSatisfied checks is number of healthy nodes satisfying the quorum
func (c *Cluster) QuorumSatisfied() bool {
	var healthyNodes int
	quorum := int(float64(((len(c.ControlPlane) / 2) + 1)))
	tolerance := len(c.ControlPlane) - quorum

	for i := range c.ControlPlane {
		if c.ControlPlane[i].ControlPlaneHealthy() {
			healthyNodes++
		}
	}

	return healthyNodes >= tolerance
}

// UpgradeNeeded compares actual and expected Kubernetes versions for control plane and static worker nodes
func (c *Cluster) UpgradeNeeded() bool {
	for i := range c.ControlPlane {
		// TODO: We should eventually error if expected version is lower than
		// current, since downgrades aren't allowed
		if c.ExpectedVersion.GreaterThan(c.ControlPlane[i].Kubelet.Version) {
			return true
		}
	}

	for i := range c.Workers {
		if c.ExpectedVersion.GreaterThan(c.Workers[i].Kubelet.Version) {
			return true
		}
	}

	return false
}

// UpgradeMachinesNeeded compares actual and expected Kubernetes version for Machines
// TODO: Implement UpgradeMachinesNeeded (always returns false)
func (c *Cluster) UpgradeMachinesNeeded() bool {
	return false
}

/*
	Host level checks
*/

// RestConfig grabs Kubeconfig from a node
func (h *Host) RestConfig() (*rest.Config, error) {
	return clientcmd.RESTConfigFromKubeConfig(h.Kubeconfig)
}

// Initialized checks is a host provisioned and is kubelet initialized
func (h *Host) Initialized() bool {
	return h.IsProvisioned() && h.Kubelet.Status&KubeletInitialized != 0
}

// IsProvisioned checks are CRI and Kubelet provisioned on a host
func (h *Host) IsProvisioned() bool {
	return h.ContainerRuntime.IsProvisioned() && h.Kubelet.IsProvisioned()
}

// ControlPlaneHealthy checks is a control-plane host part of the cluster and are CRI, Kubelet, and API server healthy
func (h *Host) ControlPlaneHealthy() bool {
	return h.healthy() && h.APIServer.Healthy()
}

// WorkerHealthy checks is a worker host part of the cluster and are CRI and Kubelet healthy
func (h *Host) WorkerHealthy() bool {
	return h.healthy()
}

func (h *Host) healthy() bool {
	return h.IsInCluster && h.ContainerRuntime.Healthy() && h.Kubelet.Healthy()
}

/*
	Component status level checks
*/

// IsProvisioned checks is a component running, installed and active
// TODO: IsProvisioned should just check is component installed
func (cs *ComponentStatus) IsProvisioned() bool {
	return cs.Status&(SystemDStatusRunning|ComponentInstalled|SystemDStatusActive) != 0
}

// Healthy checks is a component running and not restarting
func (cs *ComponentStatus) Healthy() bool {
	return cs.Status&SystemDStatusRunning != 0 && cs.Status&SystemDStatusRestarting == 0
}

/*
	Container status level checks
*/

// Healthy checks is a pod running
func (cs *ContainerStatus) Healthy() bool {
	return cs.Status&PodRunning != 0
}
