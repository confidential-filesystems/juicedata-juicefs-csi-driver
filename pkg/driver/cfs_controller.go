/*
 Copyright 2024 confidentialfilesystems
*/

package driver

import (
	"context"
	"fmt"

	"github.com/confidential-filesystems/csi-driver-common/service/util"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/juicedata/juicefs-csi-driver/pkg/config"
	"github.com/juicedata/juicefs-csi-driver/pkg/k8sclient"
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
	pv, err := util.GetPersistentVolume(ctx, d.k8sClient, req.VolumeId)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("fail to get pv %s, err: %s", req.VolumeId, err))
	}
	if pv.Spec.ClaimRef == nil {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("pv %s has no claimRef", req.VolumeId))
	}
	pvcName := pv.Spec.ClaimRef.Name
	namespace := pv.Spec.ClaimRef.Namespace
	pvc, err := util.GetPersistentVolumeClaim(ctx, d.k8sClient, pvcName, namespace)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("fail to get pvc %s namespace %s, err: %s", pvcName, namespace, err))
	}
	if err := util.CheckPvcCredential(ctx, d.k8sClient, pvc, admissionv1.Update, config.ResourceServerUrl); err != nil {
		return nil, status.Error(codes.PermissionDenied, fmt.Sprintf("no permission to %s: %s", admissionv1.Update, err))
	}
	return d.controllerService.ControllerExpandVolume(ctx, req)
}
