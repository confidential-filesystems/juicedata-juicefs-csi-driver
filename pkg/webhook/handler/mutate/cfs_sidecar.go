/*
 Copyright 2024 confidentialfilesystems
*/

package mutate

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	commonConfig "github.com/confidential-filesystems/filesystem-csi-driver-common/config"
	commonUtil "github.com/confidential-filesystems/filesystem-csi-driver-common/service/util"
	"github.com/confidential-filesystems/filesystem-toolchain/resource"
	"github.com/juicedata/juicefs-csi-driver/pkg/config"
	"github.com/juicedata/juicefs-csi-driver/pkg/juicefs"
	"github.com/juicedata/juicefs-csi-driver/pkg/juicefs/mount/builder"
	"github.com/juicedata/juicefs-csi-driver/pkg/k8sclient"
	"github.com/juicedata/juicefs-csi-driver/pkg/util"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog"
)

type CfsSidecarMutate struct {
	SidecarMutate
	Pair []commonUtil.PVPair
}

var _ Mutate = &CfsSidecarMutate{}

func NewCfsSidecarMutate(client *k8sclient.K8sClient, jfs juicefs.Interface, pair []commonUtil.PVPair) Mutate {
	return &CfsSidecarMutate{
		SidecarMutate: SidecarMutate{
			Client:  client,
			juicefs: jfs,
		},
		Pair: pair,
	}
}

func (s *CfsSidecarMutate) Mutate(ctx context.Context, pod *corev1.Pod) (out *corev1.Pod, err error) {
	out = pod.DeepCopy()
	var (
		signer                string
		ivps                  []string
		ikekKids              []string
		sideCarContainerNames []string
		sideCarContainerName  = ""
		sideCarFses           []*resource.Filesystem
		expectRuntime         = commonConfig.RuntimeVm
	)
	// find expected signer
	for _, pair := range s.Pair {
		if signer == "" && signer != pair.Filesystem.OwnerAddress {
			signer = pair.Filesystem.OwnerAddress
		} else if signer != pair.Filesystem.OwnerAddress {
			klog.Errorf("not allowed different user's filesystems currently [%s, %s]. pod %s namespace %s", signer, pair.Filesystem.OwnerAddress, pod.Name, pod.Namespace)
			return nil, err
		}
	}
	ivps, ikekKids, err = commonUtil.CheckContainerImages(ctx, s.Client, out, signer, config.ResourceServerUrl)
	if err != nil {
		klog.Errorf("check container images of pod %s namespace %s err: %v", pod.Name, pod.Namespace, err)
		return nil, err
	}
	for i, pair := range s.Pair {
		out, sideCarContainerName, err = s.mutate(ctx, out, util.PVPair{PV: pair.PV, PVC: pair.PVC, VolumeName: pair.VolumeName}, i)
		if err != nil {
			klog.Errorf("mutate pod %s namespace %s err: %v", pod.Name, pod.Namespace, err)
			return
		}
		sideCarContainerNames = append(sideCarContainerNames, sideCarContainerName)
		sideCarFses = append(sideCarFses, &pair.Filesystem)
		if commonConfig.RuntimeTee == pair.Filesystem.GetRuntime() {
			expectRuntime = commonConfig.RuntimeTee
		}
	}
	runtimeClass, err := commonUtil.GetRuntimeClass(pod, &config.WorkloadRuntimeClassNames, expectRuntime)
	if err != nil {
		klog.Errorf("get runtime class of pod %s namespace %s err: %v", pod.Name, pod.Namespace, err)
		return nil, err
	}
	if err := commonUtil.InjectRuntimeClassAndAnnotation(out, &config.WorkloadRuntimeClassNames, runtimeClass); err != nil {
		klog.Errorf("inject runtime class of pod %s namespace %s err: %v", pod.Name, pod.Namespace, err)
		return nil, err
	}
	if err := commonUtil.InjectInitContainer(ctx, out, ivps, ikekKids, sideCarContainerNames, sideCarFses, true,
		config.WorkloadInitImage, config.ResourceServerUrl, config.GetResourceAuthExpireInSeconds(),
		&config.WorkloadRuntimeClassNames, runtimeClass); err != nil {
		klog.Errorf("inject init container to pod %s namespace %s err: %v", pod.Name, pod.Namespace, err)
		return nil, err
	}
	return
}

func (s *CfsSidecarMutate) mutate(ctx context.Context, pod *corev1.Pod, pair util.PVPair, index int) (out *corev1.Pod, sideCarContainerName string, err error) {
	// get volumeContext and mountOptions from PV
	volCtx, options, err := s.GetSettings(*pair.PV)
	if err != nil {
		klog.Errorf("get settings from pv %s of pod %s namespace %s err: %v", pair.PV.Name, pod.Name, pod.Namespace, err)
		return
	}

	out = pod.DeepCopy()
	// gen jfs settings
	jfsSetting, err := s.juicefs.Settings(ctx, pair.PV.Spec.CSI.VolumeHandle, nil, volCtx, options)
	if err != nil {
		return
	}
	mountPath := util.RandStringRunes(6)
	jfsSetting.MountPath = filepath.Join(config.PodMountBase, mountPath)

	jfsSetting.Attr.Namespace = pod.Namespace
	jfsSetting.SecretName = pair.PVC.Name + "-secret"
	s.jfsSetting = jfsSetting
	capacity := pair.PVC.Spec.Resources.Requests.Storage().Value()
	cap := capacity / 1024 / 1024 / 1024
	if cap <= 0 {
		return nil, "", fmt.Errorf("capacity %d is too small, at least 1GiB for quota", capacity)
	}

	r := builder.NewCfsContainerBuilder(jfsSetting, cap)

	// create secret per PVC
	secret := builder.NewSecret(jfsSetting.Attr.Namespace, jfsSetting.SecretName)
	builder.SetPVCAsOwner(&secret, pair.PVC)
	if err = s.createOrUpdateSecret(ctx, &secret); err != nil {
		return
	}

	// gen mount pod
	mountPod := r.NewMountSidecar(&pair)
	podStr, _ := json.Marshal(mountPod)
	klog.V(6).Infof("mount pod: %v\n", string(podStr))

	// deduplicate container name and volume name in pod when multiple volumes are mounted
	s.Deduplicate(pod, mountPod, index)
	sideCarContainerName = mountPod.Spec.Containers[0].Name

	// inject volume
	s.injectVolume(out, r, mountPod.Spec.Volumes, mountPath, pair)
	// inject label
	s.injectLabel(out)
	// inject annotation
	s.injectAnnotation(out, mountPod.Annotations)
	// inject container
	s.injectContainer(out, mountPod.Spec.Containers[0])
	// inject envs
	err = s.injectEnvs(ctx, out, pair)
	if err != nil {
		return
	}
	commonUtil.UpdateSideCarContainerImage(out)
	commonUtil.InjectSideCarImagePullSecret(out)
	klog.V(5).Infof("webhook sidecar container: %+v\n", out.Spec.Containers[0])
	return
}

func (s *CfsSidecarMutate) Deduplicate(pod, mountPod *corev1.Pod, index int) {
	commonUtil.Deduplicate(pod, mountPod, index, builder.UpdateDBDirName, builder.JfsDirName)
}

func (s *CfsSidecarMutate) injectEnvs(ctx context.Context, out *corev1.Pod, pair util.PVPair) error {
	storageClass, err := s.Client.GetStorageClass(ctx, *pair.PVC.Spec.StorageClassName)
	if err != nil {
		klog.Errorf("get storageClass %s err: %v", *pair.PVC.Spec.StorageClassName, err)
		return err
	}
	crName := storageClass.Parameters[config.ProvisionerCrName]
	cr, err := commonUtil.GetCfSpec(ctx, s.Client, crName)
	if err != nil {
		klog.Errorf("get cr %s err: %v", crName, err)
		return err
	}
	metadataUrl := cr.Spec.Metadata.Service
	if commonConfig.CfsTest && os.Getenv("CSI_CONTROLLER_TEST_META_URL") != "" {
		metadataUrl = os.Getenv("CSI_CONTROLLER_TEST_META_URL")
	}
	out.Spec.Containers[0].Env = append(out.Spec.Containers[0].Env, corev1.EnvVar{
		Name:  "metaurl",
		Value: fmt.Sprintf("rediss://%s/1?tls-cert-file=/etc/cfs/conf/certs/client.cert&tls-key-file=/etc/cfs/conf/certs/client.key&tls-ca-cert-file=/etc/cfs/conf/certs/ca&tls-server-name=%s", metadataUrl, crName),
	})
	return nil
}

func (s *CfsSidecarMutate) GetSettings(pv corev1.PersistentVolume) (volCtx map[string]string, options []string, err error) {
	volCtx = pv.Spec.CSI.VolumeAttributes
	klog.V(5).Infof("volume context of pv %s: %v", pv.Name, volCtx)

	options = []string{}
	if len(pv.Spec.AccessModes) == 1 && pv.Spec.AccessModes[0] == corev1.ReadOnlyMany {
		options = append(options, "ro")
	}
	// get mountOptions from PV.spec.mountOptions
	options = append(options, pv.Spec.MountOptions...)

	mountOptions := []string{}
	// get mountOptions from PV.volumeAttributes
	if opts, ok := volCtx["mountOptions"]; ok {
		mountOptions = strings.Split(opts, ",")
	}
	options = append(options, mountOptions...)
	klog.V(5).Infof("volume options of pv %s: %v", pv.Name, options)

	return
}