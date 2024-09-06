/*
 Copyright 2024 confidentialfilesystems
*/

package driver

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog"
	provisioncontroller "sigs.k8s.io/sig-storage-lib-external-provisioner/v6/controller"

	commonUtil "github.com/confidential-filesystems/filesystem-csi-driver-common/service/util"
	"github.com/juicedata/juicefs-csi-driver/pkg/config"
	k8s "github.com/juicedata/juicefs-csi-driver/pkg/k8sclient"
	"github.com/juicedata/juicefs-csi-driver/pkg/util"
	"github.com/prometheus/client_golang/prometheus"
)

type cfsProvisionerService struct {
	provisionerService
}

func newCfsProvisionerService(k8sClient *k8s.K8sClient, leaderElection bool,
	leaderElectionNamespace string, leaderElectionLeaseDuration time.Duration, reg prometheus.Registerer) (cfsProvisionerService, error) {
	provisionerService, _ := newProvisionerService(k8sClient, leaderElection, leaderElectionNamespace, leaderElectionLeaseDuration, reg)
	return cfsProvisionerService{
		provisionerService: provisionerService,
	}, nil
}

func (j *cfsProvisionerService) Run(ctx context.Context) {
	if j.K8sClient == nil {
		klog.Fatalf("K8sClient is nil")
	}
	serverVersion, err := j.K8sClient.Discovery().ServerVersion()
	if err != nil {
		klog.Fatalf("Error getting server version: %v", err)
	}
	pc := provisioncontroller.NewProvisionController(j.K8sClient,
		config.DriverName,
		j,
		serverVersion.GitVersion,
		provisioncontroller.LeaderElection(j.leaderElection),
		provisioncontroller.LeaseDuration(j.leaderElectionLeaseDuration),
		provisioncontroller.LeaderElectionNamespace(j.leaderElectionNamespace),
	)
	pc.Run(ctx)
}

func (j *cfsProvisionerService) Provision(ctx context.Context, options provisioncontroller.ProvisionOptions) (*corev1.PersistentVolume, provisioncontroller.ProvisioningState, error) {
	klog.V(6).Infof("Provisioner Provision: options %v", options)
	if options.PVC.Spec.Selector != nil {
		return nil, provisioncontroller.ProvisioningFinished, fmt.Errorf("claim Selector is not supported")
	}

	pvMeta := util.NewObjectMeta(*options.PVC, options.SelectedNode)

	pvName := options.PVName
	scParams := make(map[string]string)
	for k, v := range options.StorageClass.Parameters {
		if strings.HasPrefix(k, "csi.storage.k8s.io/") {
			scParams[k] = pvMeta.ResolveSecret(v, pvName)
		} else {
			scParams[k] = pvMeta.StringParser(options.StorageClass.Parameters[k])
		}
	}
	klog.V(6).Infof("Provisioner Resolved StorageClass.Parameters: %v", scParams)

	subPath := pvName
	if scParams["pathPattern"] != "" {
		subPath = scParams["pathPattern"]
	}
	// return error if set readonly in dynamic provisioner
	for _, am := range options.PVC.Spec.AccessModes {
		if am == corev1.ReadOnlyMany {
			if options.StorageClass.Parameters["pathPattern"] == "" {
				j.metrics.provisionErrors.Inc()
				return nil, provisioncontroller.ProvisioningFinished, status.Errorf(codes.InvalidArgument, "Dynamic mounting uses the sub-path named pv name as data isolation, so read-only mode cannot be used.")
			} else {
				klog.Warningf("Volume is set readonly, please make sure the subpath %s exists.", subPath)
			}
		}
	}

	if _, _, err := commonUtil.CheckPvcCredential(ctx, j.K8sClient, options.PVC, admissionv1.Create, "", config.ResourceServerUrl); err != nil {
		return nil, provisioncontroller.ProvisioningFinished, fmt.Errorf("no permission to %s: %s", admissionv1.Create, err)
	}

	mountOptions := make([]string, 0)
	for _, mo := range options.StorageClass.MountOptions {
		parsedStr := pvMeta.StringParser(mo)
		mountOptions = append(mountOptions, strings.Split(strings.TrimSpace(parsedStr), ",")...)
	}
	klog.V(6).Infof("Provisioner Resolved MountOptions: %v", mountOptions)

	// set volume context
	volCtx := make(map[string]string)
	volCtx["subPath"] = subPath
	volCtx["capacity"] = strconv.FormatInt(options.PVC.Spec.Resources.Requests.Storage().Value(), 10)
	for k, v := range scParams {
		volCtx[k] = v
	}
	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: options.PVName,
		},
		Spec: corev1.PersistentVolumeSpec{
			Capacity: corev1.ResourceList{
				corev1.ResourceName(corev1.ResourceStorage): options.PVC.Spec.Resources.Requests[corev1.ResourceName(corev1.ResourceStorage)],
			},
			PersistentVolumeSource: corev1.PersistentVolumeSource{
				CSI: &corev1.CSIPersistentVolumeSource{
					Driver:           config.DriverName,
					VolumeHandle:     pvName,
					ReadOnly:         false,
					FSType:           "juicefs",
					VolumeAttributes: volCtx,
				},
			},
			AccessModes:                   options.PVC.Spec.AccessModes,
			PersistentVolumeReclaimPolicy: *options.StorageClass.ReclaimPolicy,
			StorageClassName:              options.StorageClass.Name,
			MountOptions:                  mountOptions,
			VolumeMode:                    options.PVC.Spec.VolumeMode,
		},
	}
	if scParams[config.ControllerExpandSecretName] != "" && scParams[config.ControllerExpandSecretNamespace] != "" {
		pv.Spec.CSI.ControllerExpandSecretRef = &corev1.SecretReference{
			Name:      scParams[config.ControllerExpandSecretName],
			Namespace: scParams[config.ControllerExpandSecretNamespace],
		}
	}

	return pv, provisioncontroller.ProvisioningFinished, nil
}

func (j *cfsProvisionerService) Delete(ctx context.Context, volume *corev1.PersistentVolume) error {
	klog.V(6).Infof("Provisioner Delete: Volume %v", volume)
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      volume.Spec.ClaimRef.Name,
			Namespace: volume.Spec.ClaimRef.Namespace,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: &volume.Spec.StorageClassName,
		},
	}
	addr, cred, err := commonUtil.CheckPvcCredential(ctx, j.K8sClient, pvc, admissionv1.Delete, "", config.ResourceServerUrl)
	if err != nil {
		return fmt.Errorf("no permission to %s: %s", admissionv1.Delete, err)
	}
	// If it exists and has a `delete` value, delete the directory.
	// If it exists and has a `retain` value, safe the directory.
	policy := volume.Spec.PersistentVolumeReclaimPolicy
	if policy != corev1.PersistentVolumeReclaimDelete {
		klog.V(6).Infof("Provisioner: Volume %s retain, return.", volume.Name)
		return nil
	}
	// check all pvs of the same storageClass, if multiple pv using the same subPath, do not delete the subPath
	shouldDeleted, err := util.CheckForSubPath(ctx, j.K8sClient, volume, volume.Spec.CSI.VolumeAttributes["pathPattern"])
	if err != nil {
		klog.Errorf("Provisioner: CheckForSubPath error: %v", err)
		return err
	}
	if !shouldDeleted {
		klog.Infof("Provisioner: there are other pvs using the same subPath retained, volume %s should not be deleted, return.", volume.Name)
		return nil
	}
	klog.V(6).Infof("Provisioner: there are no other pvs using the same subPath, volume %s can be deleted.", volume.Name)
	klog.V(5).Infof("Provisioner Delete: Deleting volume subpath %q", pvc.Name)

	cfsSpecName := volume.Spec.CSI.VolumeAttributes[config.ProvisionerCrName]
	cfspec, err := commonUtil.GetCfSpec(ctx, j.K8sClient, cfsSpecName)
	if err != nil {
		return fmt.Errorf("fail to get cfspec %s, err: %s", cfsSpecName, err)
	}
	if err := util.OperateFs(ctx, cfspec, addr, cred, admissionv1.Delete); err != nil {
		return fmt.Errorf("unable to provision delete volume: fail to delete fs %s with subpath %s, err: %s", cfsSpecName, pvc.Name, err)
	}

	return nil
}
