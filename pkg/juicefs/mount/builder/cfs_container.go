/*
 Copyright 2024 confidentialfilesystems
*/

package builder

import (
	"fmt"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	utilpointer "k8s.io/utils/pointer"

	commonConfig "github.com/confidential-filesystems/csi-driver-common/config"
	commonUtil "github.com/confidential-filesystems/csi-driver-common/service/util"
	"github.com/juicedata/juicefs-csi-driver/pkg/config"
	"github.com/juicedata/juicefs-csi-driver/pkg/util"
	"github.com/juicedata/juicefs-csi-driver/pkg/util/security"
)

type CfsContainerBuilder struct {
	ContainerBuilder
}

var _ SidecarInterface = &CfsContainerBuilder{}

func NewCfsContainerBuilder(setting *config.JfsSetting, capacity int64) SidecarInterface {
	return &CfsContainerBuilder{
		ContainerBuilder{PodBuilder{BaseBuilder{
			jfsSetting: setting,
			capacity:   capacity,
		}}},
	}
}

// NewMountSidecar generates a pod with a cfs sidecar
func (r *CfsContainerBuilder) NewMountSidecar(pair *util.PVPair) *corev1.Pod {
	pod := r.NewCfsMountPod("")
	// no annotation and label for sidecar
	pod.Annotations = map[string]string{}
	pod.Labels = map[string]string{}

	volumes, volumeMounts := r.genSidecarVolumes(pair)
	pod.Spec.Volumes = append(pod.Spec.Volumes, volumes...)
	pod.Spec.Containers[0].VolumeMounts = append(pod.Spec.Containers[0].VolumeMounts, volumeMounts...)

	// check mount & create subpath & set quota
	capacity := strconv.FormatInt(r.capacity, 10)
	subpath := r.jfsSetting.SubPath
	community := "ce"
	if !r.jfsSetting.IsCe {
		community = "ee"
	}
	quotaPath := r.getQuotaPath()
	name := r.jfsSetting.Name
	bash := "bash"
	cmdTemplate := "time subpath=%s name=%s capacity=%s community=%s quotaPath=%s %s '%s' >> /proc/1/fd/1"
	if commonConfig.CfsTest {
		cmdTemplate = "echo \"" + cmdTemplate + "\""
		bash = "sh"
	}
	pod.Spec.Containers[0].Lifecycle.PostStart = &corev1.Handler{
		Exec: &corev1.ExecAction{Command: []string{bash, "-c",
			fmt.Sprintf(cmdTemplate,
				security.EscapeBashStr(subpath),
				security.EscapeBashStr(name),
				capacity,
				community,
				security.EscapeBashStr(quotaPath),
				checkMountScriptPath,
				security.EscapeBashStr(r.jfsSetting.MountPath),
			)}},
	}

	// overwrite subdir
	r.overwriteSubdirWithSubPath()

	mountCmd := r.genCfsMountCommand()
	cmd := mountCmd
	initCmd := r.genInitCommand()
	if initCmd != "" {
		cmd = strings.Join([]string{initCmd, mountCmd}, "\n")
	}
	pod.Spec.Containers[0].Command = []string{"sh", "-c", cmd}
	return pod
}

func (r *CfsContainerBuilder) OverwriteVolumeMounts(mount *corev1.VolumeMount) {
	// overwrite volumeMounts with right propagation
	commonUtil.OverwriteVolumeMounts(mount)

	return
}

func (r *CfsContainerBuilder) OverwriteVolumes(volume *corev1.Volume, mountPath string) {
	// overwrite original volume and use emptyDir volume mountpoint instead
	commonUtil.OverwriteVolumes(volume, mountPath)
}

// genSidecarVolumes generates volumes and volumeMounts for sidecar container
// extra volumes and volumeMounts are used to check mount status
func (r *CfsContainerBuilder) genSidecarVolumes(pair *util.PVPair) (volumes []corev1.Volume, volumeMounts []corev1.VolumeMount) {
	var mode int32 = 0755
	secretName := r.jfsSetting.SecretName
	volumes = []corev1.Volume{
		{
			Name: "cfs-check-mount",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName:  secretName,
					DefaultMode: utilpointer.Int32Ptr(mode),
				},
			},
		},
		{
			Name: commonConfig.ContainerSideCarDummyConfVolumeName,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{
					Medium: corev1.StorageMediumMemory,
				},
			},
		}}
	volumeMounts = []corev1.VolumeMount{
		{
			Name:      "cfs-check-mount",
			MountPath: checkMountScriptPath,
			SubPath:   checkMountScriptName,
		},
		{
			Name:      commonConfig.ContainerSideCarDummyConfVolumeName,
			MountPath: "/etc/cfs/conf",
		}}
	if pair != nil {
		bi := corev1.MountPropagationBidirectional
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:             pair.VolumeName,
			MountPath:        r.jfsSetting.MountPath,
			MountPropagation: &bi,
		})
	}
	return
}
