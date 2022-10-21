// Copyright © 2021 FORTH-ICS
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.package common

package api

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	corev1 "k8s.io/api/core/v1"
)

// Defaults for root command options
const (
	DefaultNodeName             = "virtual-kubelet"
	DefaultInformerResyncPeriod = 1 * time.Minute
	DefaultMetricsAddr          = ":10255"
	DefaultListenPort           = 10250
	DefaultPodSyncWorkers       = 1
	DefaultKubeNamespace        = corev1.NamespaceAll

	DefaultTaintKey    = "virtual-kubelet.io/provider"
	DefaultTaintValue  = "hpk"
	DefaultTaintEffect = string(corev1.TaintEffectNoSchedule)
)

var (
	BuildVersion = "N/A"
	BuildTime    = "N/A"
	K8sVersion   = "v1.25.0"
)

const (
	DefaultMaxWorkers   = 1
	DefaultMaxQueueSize = 100

	ExitCodeExtension = ".exitCode"
	JobIdExtension    = ".jid"

	PauseContainerName  = "pause"
	PauseContainerImage = "scratch"

	RuntimeDir               = ".hpk"
	TemporaryDir             = ".tmp"
	PodSecretVolDir          = "/secrets"
	PodConfigMapVolDir       = "/configmaps"
	PodDownwardApiVolDir     = "/downwardapis"
	DefaultContainerRegistry = "docker://"
)

/*
+-----+---+--------------------------+
| rwx | 7 | Read, write and execute  |
| rw- | 6 | Read, write              |
| r-x | 5 | Read, and execute        |
| r-- | 4 | Read,                    |
| -wx | 3 | Write and execute        |
| -w- | 2 | Write                    |
| --x | 1 | Execute                  |
| --- | 0 | no permissions           |
+------------------------------------+

+------------+------+-------+
| Permission | Octal| Field |
+------------+------+-------+
| rwx------  | 0700 | User  |
| ---rwx---  | 0070 | Group |
| ------rwx  | 0007 | Other |
+------------+------+-------+
*/

const (
	PodGlobalDirectoryPermissions = 0o777
	PodSpecJsonFilePermissions    = 0o600
	ContainerJobPermissions       = 0o777

	SecretPodDataPermissions      = 0o760
	PodSecretVolPermissions       = 0o755
	PodSecretFilePermissions      = 0o644
	PodConfigMapVolPermissions    = 0o755
	PodConfigMapFilePermissions   = 0o644
	PodDownwardApiVolPermissions  = 0o755
	PodDownwardApiFilePermissions = 0o644
)

// ObjectKey identifies a Kubernetes Object.
type ObjectKey = types.NamespacedName

// ObjectKeyFromObject returns the ObjectKey given a runtime.Object.
func ObjectKeyFromObject(obj metav1.Object) ObjectKey {
	return ObjectKey{Namespace: obj.GetNamespace(), Name: obj.GetName()}
}
