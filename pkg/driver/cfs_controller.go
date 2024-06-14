/*
 Copyright 2024 confidentialfilesystems
*/

package driver

import (
	"context"
	"fmt"
	"strconv"

	commonUtil "github.com/confidential-filesystems/csi-driver-common/service/util"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/juicedata/juicefs-csi-driver/pkg/config"
	"github.com/juicedata/juicefs-csi-driver/pkg/k8sclient"
	"github.com/juicedata/juicefs-csi-driver/pkg/util"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/klog"
)

type cfsControllerService struct {
	controllerService
	k8sClient *k8sclient.K8sClient
}

func newCfsControllerService(k8sClient *k8sclient.K8sClient) (cfsControllerService, error) {
	controllerService, _ := newControllerService(k8sClient)

	return cfsControllerService{
		controllerService: controllerService,
		k8sClient:         k8sClient,
	}, nil
}

func (d *cfsControllerService) CreateVolume(ctx context.Context, req *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	klog.V(6).Infof("CreateVolume: called with args %#v", req)
	return nil, status.Error(codes.Unimplemented, "")
}

func (d *cfsControllerService) DeleteVolume(ctx context.Context, req *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	klog.V(6).Infof("CreateVolume: called with args %#v", req)
	return nil, status.Error(codes.Unimplemented, "")
}

func (d *cfsControllerService) ControllerExpandVolume(ctx context.Context, req *csi.ControllerExpandVolumeRequest) (*csi.ControllerExpandVolumeResponse, error) {
	klog.V(6).Infof("ControllerExpandVolume request: %+v", *req)

	volumeID := req.GetVolumeId()
	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID not provided")
	}

	// get cap
	capRange := req.GetCapacityRange()
	if capRange == nil {
		return nil, status.Error(codes.InvalidArgument, "Capacity range not provided")
	}

	newSize := capRange.GetRequiredBytes()
	maxVolSize := capRange.GetLimitBytes()
	if maxVolSize > 0 && maxVolSize < newSize {
		return nil, status.Error(codes.InvalidArgument, "After round-up, volume size exceeds the limit specified")
	}

	// get mount options
	volCap := req.GetVolumeCapability()
	if volCap == nil {
		return nil, status.Error(codes.InvalidArgument, "Volume capability not provided")
	}
	klog.V(5).Infof("ControllerExpandVolume: volume_capability is %s", volCap)
	options := []string{}
	if m := volCap.GetMount(); m != nil {
		// get mountOptions from PV.spec.mountOptions or StorageClass.mountOptions
		options = append(options, m.MountFlags...)
	}

	capacity, err := strconv.ParseInt(strconv.FormatInt(newSize, 10), 10, 64)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "invalid capacity %d: %v", capacity, err)
	}

	pv, err := commonUtil.GetPersistentVolume(ctx, d.k8sClient, volumeID)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("fail to get pv %s, err: %s", req.VolumeId, err))
	}
	if pv.Spec.ClaimRef == nil || pv.Spec.CSI == nil {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("pv %s has no claimRef or csi", req.VolumeId))
	}
	pvcName := pv.Spec.ClaimRef.Name
	namespace := pv.Spec.ClaimRef.Namespace
	pvc, err := commonUtil.GetPersistentVolumeClaim(ctx, d.k8sClient, pvcName, namespace)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("fail to get pvc %s namespace %s, err: %s", pvcName, namespace, err))
	}
	addr, cred, err := commonUtil.CheckPvcCredential(ctx, d.k8sClient, pvc, admissionv1.Update, config.ResourceServerUrl)
	if err != nil {
		return nil, status.Error(codes.PermissionDenied, fmt.Sprintf("no permission to %s: %s", admissionv1.Update, err))
	}
	cfsSpecName := pv.Spec.CSI.VolumeAttributes[config.ProvisionerCrName]
	cfspec, err := commonUtil.GetCfSpec(ctx, d.k8sClient, cfsSpecName)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("fail to get cfspec %s, err: %s", cfsSpecName, err))
	}
	if err = util.OperateFs(ctx, cfspec, addr, cred, admissionv1.Update); err != nil {
		return nil, status.Error(codes.Internal, fmt.Sprintf("unable to provision expand volume: fail to update fs %s, subpath %s, err: %s", cfspec.Name, pvc.Name, err))
	}

	return &csi.ControllerExpandVolumeResponse{
		CapacityBytes:         newSize,
		NodeExpansionRequired: false,
	}, nil
}
