package util

import (
	"context"
	"fmt"
	"strings"

	commonUtil "github.com/confidential-filesystems/filesystem-csi-driver-common/service/util"
	cfsCert "github.com/confidential-filesystems/filesystem-toolchain/cert"
	"github.com/confidential-filesystems/filesystem-toolchain/resource"
	"github.com/confidential-filesystems/filesystem-toolchain/util"
	"github.com/go-resty/resty/v2"
	"github.com/juicedata/juicefs-csi-driver/pkg/config"
	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/klog/v2"
)

const (
	FsManagerUpdatePath = "/v1/mgmt/filesystem"
	FsManagerDeletePath = "/v1/mgmt/filesystem/subpath"
)

type ErrRespVo struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func OperateFs(ctx context.Context, cfspec *commonUtil.CfSpec, ownerAddr, action string, operation admissionv1.Operation) (err error) {
	var response *resty.Response
	ca, cert, key, err := resource.GetClientCerts(ctx, config.ResourceServerUrl, ownerAddr, nil)
	if err != nil {
		klog.Infof("failed to get client certs of %s: %v", ownerAddr, err)
		return
	}
	client, err := util.NewRestyClientWithCert(ca, cert, key, cfsCert.DefaultServerCommonName)
	if err != nil {
		klog.Infof("failed to new resty client with cert: %v", err)
		return
	}
	service := fmt.Sprintf("https://%s:%d", cfspec.Spec.Metadata.Service[:strings.LastIndex(cfspec.Spec.Metadata.Service, ":")], config.FSManagerPort)
	request := client.R().SetContext(ctx).SetBody(map[string]interface{}{"name": cfspec.Spec.Filesystem.Name, "action": action}).SetError(ErrRespVo{})
	switch operation {
	case admissionv1.Update:
		response, err = request.Put(service + FsManagerUpdatePath)
	case admissionv1.Delete:
		response, err = request.Delete(service + FsManagerDeletePath)
	default:
		return fmt.Errorf("unknown operation %s", operation)
	}
	if err != nil {
		klog.Infof("resty to %s cfs %s failed: %v", operation, cfspec.Spec.Filesystem.Name, err)
		return
	}
	if response.IsError() {
		klog.Infof("failed to %s cfs %s: %v", operation, cfspec.Spec.Filesystem.Name, response.Error())
		return fmt.Errorf("failed to %s cfs %s: %v", operation, cfspec.Spec.Filesystem.Name, response.Error())
	}
	return nil
}
