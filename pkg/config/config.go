/*
Copyright 2021 Juicedata Inc

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

package config

import (
	"hash/fnv"
	"os"
	"strconv"
	"sync"
	"time"

	"k8s.io/klog"

	corev1 "k8s.io/api/core/v1"

	commonConfig "github.com/confidential-filesystems/filesystem-csi-driver-common/config"
)

var (
	WebPort            = MustGetWebPort() // web port used by metrics
	ByProcess          = false            // csi driver runs juicefs in process or not
	FormatInPod        = false            // put format/auth in pod (only in k8s)
	Provisioner        = false            // provisioner in controller
	CacheClientConf    = false            // cache client config files and use directly in mount containers
	MountManager       = false            // manage mount pod in controller (only in k8s)
	Webhook            = false            // inject juicefs client as sidecar in pod (only in k8s)
	ValidatingWebhook  = false            // start validating webhook, applicable to ee only
	Immutable          = false            // csi driver is running in an immutable environment
	EnableNodeSelector = false            // arrange mount pod to node with node selector instead nodeName

	DriverName         = "juicefs" // will be set by env or set as default
	NodeName           = ""
	Namespace          = ""
	PodName            = ""
	CEMountImage       = "juicedata/mount:ce-nightly" // mount pod ce image
	EEMountImage       = "juicedata/mount:ee-nightly" // mount pod ee image
	MountLabels        = ""
	HostIp             = ""
	KubeletPort        = ""
	ReconcileTimeout   = 5 * time.Minute
	ReconcilerInterval = 5

	CSIPod = corev1.Pod{}

	MountPointPath           = "/var/lib/juicefs/volume"
	JFSConfigPath            = "/var/lib/juicefs/config"
	JFSMountPriorityName     = "system-node-critical"
	JFSMountPreemptionPolicy = ""

	TmpPodMountBase       = "/tmp"
	PodMountBase          = "/jfs"
	MountBase             = "/var/lib/jfs"
	FsType                = "juicefs"
	CliPath               = "/usr/bin/juicefs"
	CeCliPath             = "/usr/local/bin/juicefs"
	CeMountPath           = "/bin/mount.juicefs"
	JfsMountPath          = "/sbin/mount.juicefs"
	DefaultClientConfPath = "/root/.juicefs"
	ROConfPath            = "/etc/juicefs"

	WorkloadRuntimeClassNames = commonConfig.RuntimeClassNames{
		TEE: []string{commonConfig.DefaultWorkLoadTeeRuntimeClassName},
		VM:  []string{commonConfig.DefaultWorkLoadVmRuntimeClassName},
	}
	ResourceServerUrl    = commonConfig.DefaultResourceServerUrl
	FSManagerPort        = commonConfig.DefaultFsManagerPort
	WorkloadInitImage    = "docker.io/library/busybox:latest"
	WorkloadSideCarImage = ""

	// default value
	DefaultMountPodCpuLimit   = "1000m"
	DefaultMountPodMemLimit   = "1Gi"
	DefaultMountPodCpuRequest = "100m"
	DefaultMountPodMemRequest = "100Mi"
)

const (
	// DriverName to be registered
	CSINodeLabelKey      = "app"
	CSINodeLabelValue    = "juicefs-csi-node"
	PodTypeKey           = "app.kubernetes.io/name"
	PodTypeValue         = "juicefs-mount"
	PodUniqueIdLabelKey  = "volume-id"
	PodJuiceHashLabelKey = "juicefs-hash"
	Finalizer            = "juicefs.com/finalizer"
	JuiceFSUUID          = "juicefs-uuid"
	UniqueId             = "juicefs-uniqueid"
	CleanCache           = "juicefs-clean-cache"
	MountContainerName   = "jfs-mount"
	JobTypeValue         = "juicefs-job"
	JfsInsideContainer   = "JFS_INSIDE_CONTAINER"

	// CSI Secret
	ProvisionerSecretName           = "csi.storage.k8s.io/provisioner-secret-name"
	ProvisionerSecretNamespace      = "csi.storage.k8s.io/provisioner-secret-namespace"
	PublishSecretName               = "csi.storage.k8s.io/node-publish-secret-name"
	PublishSecretNamespace          = "csi.storage.k8s.io/node-publish-secret-namespace"
	ControllerExpandSecretName      = "csi.storage.k8s.io/controller-expand-secret-name"
	ControllerExpandSecretNamespace = "csi.storage.k8s.io/controller-expand-secret-namespace"

	// CSI CR
	ProvisionerCrName = "csi.storage.cfs.io/provisioner-cr-name"

	// webhook
	WebhookName          = "juicefs-admission-webhook"
	True                 = "true"
	False                = "false"
	inject               = "." + CfsName + ".com/inject"
	injectSidecar        = ".juicefs.sidecar" + inject
	InjectSidecarDone    = "done" + injectSidecar
	InjectSidecarDisable = "disable" + injectSidecar

	// config in pv
	MountPodCpuLimitKey    = "juicefs/mount-cpu-limit"
	MountPodMemLimitKey    = "juicefs/mount-memory-limit"
	MountPodCpuRequestKey  = "juicefs/mount-cpu-request"
	MountPodMemRequestKey  = "juicefs/mount-memory-request"
	mountPodLabelKey       = "juicefs/mount-labels"
	mountPodAnnotationKey  = "juicefs/mount-annotations"
	mountPodServiceAccount = "juicefs/mount-service-account"
	mountPodImageKey       = "juicefs/mount-image"
	deleteDelay            = "juicefs/mount-delete-delay"
	cleanCache             = "juicefs/clean-cache"
	cachePVC               = "juicefs/mount-cache-pvc"
	cacheEmptyDir          = "juicefs/mount-cache-emptydir"
	cacheInlineVolume      = "juicefs/mount-cache-inline-volume"
	mountPodHostPath       = "juicefs/host-path"

	// DeleteDelayTimeKey mount pod annotation
	DeleteDelayTimeKey = "juicefs-delete-delay"
	DeleteDelayAtKey   = "juicefs-delete-at"

	CfsName = "confidentialfilesystems"
)

var PodLocks [1024]sync.Mutex

func GetPodLock(podName string) *sync.Mutex {
	h := fnv.New32a()
	h.Write([]byte(podName))
	index := int(h.Sum32())
	return &PodLocks[index%1024]
}

func MustGetWebPort() int {
	value, exists := os.LookupEnv("JUICEFS_CSI_WEB_PORT")
	if exists {
		port, err := strconv.Atoi(value)
		if err == nil {
			return port
		}
		klog.Errorf("Fail to parse JUICEFS_CSI_WEB_PORT %s: %v", value, err)
	}
	return 8080
}

func GetResourceAuthExpireInSeconds() int64 {
	authExpireIn := os.Getenv(commonConfig.EnvResourceAuthExpireIn)
	if authExpireIn == "" {
		return int64(commonConfig.DefaultResourceAuthExpireIn.Seconds())
	}
	expireIn, err := time.ParseDuration(authExpireIn)
	if err != nil || int64(expireIn.Seconds()) < commonConfig.DefaultMinResourceAuthExpireIn {
		expireIn = commonConfig.DefaultResourceAuthExpireIn
	}
	return int64(expireIn.Seconds())
}
